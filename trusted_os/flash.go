// Copyright 2022 The Armored Witness OS authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"fmt"
	"log"
	"runtime"
	"time"

	usbarmory "github.com/usbarmory/tamago/board/usbarmory/mk2"
	"github.com/usbarmory/tamago/soc/nxp/usdhc"

	"github.com/transparency-dev/armored-witness-boot/config"
	"github.com/transparency-dev/armored-witness-common/release/firmware"
)

// imx6_usdhc: 15 GB/14 GiB card detected {MMC:true SD:false HC:true HS:true DDR:false Rate:150 BlockSize:512 Blocks:30576640
const (
	expectedBlockSize = 512 // Expected size of MMC block in bytes
	otaLimit          = 31457280

	/*
		 * Trusted Applet is now embedded in the OS, so these constants are not used.
		 * Eventually we can reclaim these blocks.
		taConfBlock       = 0x200000
		taBlockA          = 0x200050
		taBlockB          = 0x2FD050
	*/

	osConfBlock       = 0x5000
	osBlockA          = 0x5050
	osBlockB          = 0x102828
	crashLogBlock     = 0x1D20000 // For storing contents of log ringbuffer on applet crash for later investigation.
	crashLogNumBlocks = 0x800     // 1MB
	batchSize         = 2048
)

const (
	Firmware_OS FirmwareType = iota
)

var (
	// osLoadedFromBlock is set to the first block of MMC the running OS firmware was loaded from.
	// This will be set by the determineLoadedOS func below.
	osLoadedFromBlock int64
)

// FirmwareType represents the types of updatable firmware.
type FirmwareType int

func (ft FirmwareType) String() string {
	switch ft {
	case Firmware_OS:
		return "OS"
	}
	panic(fmt.Errorf("Unknown FirmwareType %v", ft))
}

type otaBuffer struct {
	total  uint32
	seq    uint32
	sig    []byte
	buf    []byte
	bundle *config.ProofBundle
}

// Card mostly mirrors the public API of the usdhc.Card struct, allowing
// substitutions for testing.
type Card interface {
	// Read reads size bytes at offset from the underlying storage.
	Read(offset int64, size int64) ([]byte, error)
	//WriteBlocks writes data at sector lba onwards on the underlying storage.
	WriteBlocks(lba int, data []byte) error
	// Info returns information about the underlying storage.
	Info() usdhc.CardInfo
	// Detect causes the underlying storage to probe itself.
	Detect() error
}

// readConfig reads and parses a firmware config structure stored in the given block.
func readConfig(card Card, configBlock int64) (*config.Config, error) {
	buf, err := card.Read(configBlock*expectedBlockSize, config.MaxLength)
	if err != nil {
		return nil, err
	}

	conf := &config.Config{}
	if err := conf.Decode(buf); err != nil {
		return nil, err
	}

	return conf, nil
}

// determineLoadedOSBlock reads the current OS config, and updates osLoadedFromBlock with the
// MMC block index where the corresponding firmware image can be found.
func determineLoadedOSBlock(card Card) error {
	blockSize := card.Info().BlockSize
	if blockSize != expectedBlockSize {
		return fmt.Errorf("h/w invariant error - expected MMC blocksize %d, found %d", expectedBlockSize, blockSize)
	}

	conf, err := readConfig(card, osConfBlock)
	if err != nil {
		return fmt.Errorf("failed to read OS config: %v", err)
	}

	osLoadedFromBlock = conf.Offset / expectedBlockSize
	switch osLoadedFromBlock {
	case osBlockA:
		log.Print("Loaded OS from slot A")
	case osBlockB:
		log.Print("Loaded OS from slot B")
	default:
		log.Printf("Loaded OS from unexpected block %d", osLoadedFromBlock)
	}
	return nil
}

// flash writes a buffer to internal storage.
//
// Since this function is writing blocks to MMC, it will pad the passed in
// buf with zeros to ensure full MMC blocks are written.
func flash(card Card, buf []byte, lba int) (err error) {
	blockSize := card.Info().BlockSize
	if blockSize != expectedBlockSize {
		return fmt.Errorf("h/w invariant error - expected MMC blocksize %d, found %d", expectedBlockSize, blockSize)
	}

	// write in chunks to limit DMA requirements
	bytesPerChunk := blockSize * batchSize
	for blocks := 0; len(buf) > 0; {
		var chunk []byte
		if len(buf) >= bytesPerChunk {
			chunk = buf[:bytesPerChunk]
			buf = buf[bytesPerChunk:]
		} else {
			// The final chunk could end with a partial MMC block, so it may need padding with zeroes to make up
			// a whole MMC block size. We'll do this with a separate buffer rather than trying to extend the
			// passed-in buf as doing so will potentially cause a re-alloc & copy which would temporarily use double
			// the amount of RAM.
			roundedUpSize := ((len(buf) / blockSize) + 1) * blockSize
			chunk = make([]byte, roundedUpSize)
			copy(chunk, buf)
			buf = []byte{}
		}
		if err = card.WriteBlocks(lba+blocks, chunk); err != nil {
			return
		}
		blocks += len(chunk) / blockSize

		log.Printf("flashed %d blocks", blocks)
	}

	return
}

func blinkenLights() (func(), func()) {
	var exit = make(chan bool)
	cancel := func() { close(exit) }

	blink := func() {
		var on bool

		for {
			select {
			case <-exit:
				usbarmory.LED("white", false)
				return
			default:
			}

			on = !on
			usbarmory.LED("white", on)

			runtime.Gosched()
			time.Sleep(100 * time.Millisecond)
		}
	}

	return blink, cancel
}

// updateOS verifies an OS update and flashes it to internal storage
func updateOS(storage Card, osELF []byte, pb config.ProofBundle) (err error) {
	// First, verify everything is correct and that, as far as we can tell,
	// we would succeed in loadering and launching this applet upon next boot.
	bundle := firmware.Bundle{
		Checkpoint:     pb.Checkpoint,
		Index:          pb.LogIndex,
		InclusionProof: pb.InclusionProof,
		Manifest:       pb.Manifest,
		Firmware:       osELF,
	}
	if _, err := OSBundleVerifier.Verify(bundle); err != nil {
		return err
	}
	log.Printf("SM verified OS bundle for update")

	return flashFirmware(storage, Firmware_OS, osELF, pb)
}

// flashFirmware writes config & elf bytes to the MMC in the correct region for the specificed type of firmware.
func flashFirmware(storage Card, t FirmwareType, elf []byte, pb config.ProofBundle) error {
	if storage == nil {
		return fmt.Errorf("Flashing %s error: missing Storage", t)
	}

	blink, cancel := blinkenLights()
	defer cancel()
	go blink()

	confBlock := 0
	elfBlock := 0
	switch t {
	case Firmware_OS:
		confBlock = osConfBlock
		if osLoadedFromBlock == osBlockA {
			elfBlock = osBlockB
			log.Print("SM will flash OS to slot B")
		} else {
			// If the running OS was loaded from OS slot B, or there was no valid config, store in slot A
			elfBlock = osBlockA
			log.Print("SM will flash OS to slot A")
		}
	default:
		return fmt.Errorf("unknown firmware type %v", t)
	}

	// Convert the signature to an armory-witness-boot format to serialize
	// all required information for applet loading.
	conf := &config.Config{
		Size:   int64(len(elf)),
		Bundle: pb,
		Offset: int64(elfBlock) * expectedBlockSize,
	}

	confEnc, err := conf.Encode()
	if err != nil {
		return err
	}

	// Flash firmware bytes first before updating config so that in case of any error the unit
	// will still boot the previous working firmware.
	log.Printf("SM flashing %s (%d bytes) @ 0x%x", t, len(elf), elfBlock)
	if err = flash(storage, elf, elfBlock); err != nil {
		return fmt.Errorf("%s flashing error: %v", t, err)
	}

	log.Printf("SM flashing %s config (%d bytes) @ 0x%x", t, len(confEnc), confBlock)
	if err = flash(storage, confEnc, confBlock); err != nil {
		return fmt.Errorf("%s signature flashing error: %v", t, err)
	}

	log.Printf("SM %s update complete", t)
	return nil
}

func storeAppletCrashLog(storage Card, l []byte) error {
	log.Printf("SM storing applet exit log")
	defer log.Printf("SM applet exit log stored")

	maxLogSize := crashLogNumBlocks * expectedBlockSize
	if ll := len(l); ll > maxLogSize {
		l = l[ll-maxLogSize:]
	} else if ll < maxLogSize {
		// Pad up so we overwrite all of any prior logging to avoid confusion.
		l = append(l, make([]byte, maxLogSize-ll)...)
	}
	return storage.WriteBlocks(crashLogBlock, l)
}

func retrieveLastCrashLog(storage Card) ([]byte, error) {
	maxLogSize := crashLogNumBlocks * expectedBlockSize
	r, err := storage.Read(crashLogBlock*expectedBlockSize, int64(maxLogSize))
	if err != nil {
		return nil, err
	}
	if p := bytes.IndexByte(r, 0); p > 0 {
		r = r[:p]
	}
	return r, nil
}
