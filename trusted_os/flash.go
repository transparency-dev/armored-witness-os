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
	"errors"
	"fmt"
	"log"
	"runtime"
	"time"

	"google.golang.org/protobuf/proto"

	usbarmory "github.com/usbarmory/tamago/board/usbarmory/mk2"
	"github.com/usbarmory/tamago/soc/nxp/usdhc"

	"github.com/transparency-dev/armored-witness-boot/config"
	abconfig "github.com/usbarmory/armory-boot/config"

	"github.com/transparency-dev/armored-witness-os/api"
)

const (
	otaLimit     = 31457280
	taConfSector = 0x200000
	batchSize    = 2048
	taSector     = taConfSector + config.MaxLength/512
)

type otaBuffer struct {
	total uint32
	seq   uint32
	sig   []byte
	buf   []byte
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

// read reads the trusted applet and its signature from internal storage, the
// applet and signatures are *not* verified by this function.
func read(card Card) (taELF []byte, taSig []byte, err error) {
	buf, err := card.Read(taConfSector, config.MaxLength)

	if err != nil {
		return
	}

	conf := &config.Config{}

	if err = conf.Decode(buf); err != nil {
		return
	}

	if len(conf.Signatures) < 1 {
		return nil, nil, errors.New("invalid applet signature")
	}

	taSig = conf.Signatures[1]
	taELF, err = card.Read(conf.Offset, conf.Size)

	return
}

// flash writes a buffer to internal storage
func flash(card Card, buf []byte, lba int) (err error) {
	blockSize := card.Info().BlockSize

	if blockSize == 0 {
		return errors.New("invalid block size")
	}

	blocks := len(buf) / blockSize
	batch := batchSize

	// write in batch to limit DMA requirements
	for i := 0; i < blocks; i += batch {
		if i+batch > blocks {
			batch = blocks - i
		}

		start := i * blockSize
		end := start + blockSize*batch

		if i%batch == 0 {
			log.Printf("flashed %d/%d applet blocks", i, blocks)
		}

		if err = card.WriteBlocks(lba+i, buf[start:end]); err != nil {
			return
		}
	}

	return
}

// updateApplet verifies an applet update and flashes it to internal storage
func updateApplet(taELF []byte, taSig []byte) (err error) {
	var exit = make(chan bool)

	defer func() {
		exit <- true
	}()

	go func() {
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
	}()

	log.Printf("SM applet verification")
	if err = abconfig.Verify(taELF, taSig, PublicKey); err != nil {
		return fmt.Errorf("applet verification error: %v", err)
	}

	// Convert the signature to an armory-witness-boot format to serialize
	// all required information for applet loading.
	conf := &config.Config{
		Offset:     taSector,
		Size:       int64(len(taELF)),
		Signatures: [][]byte{taSig},
	}

	if taSig, err = conf.Encode(); err != nil {
		return
	}

	if Storage == nil {
		return fmt.Errorf("applet flashing error: missing Storage")
	}

	log.Printf("SM flashing applet signature")

	if err = flash(Storage, taSig, taConfSector); err != nil {
		return fmt.Errorf("applet signature flashing error: %v", err)
	}

	log.Printf("SM flashing applet")

	if err = flash(Storage, taELF, taSector); err != nil {
		return fmt.Errorf("applet flashing error: %v", err)
	}

	log.Printf("SM applet update complete")
	usbarmory.LED("white", false)

	return
}

// Update is the handler for U2FHID_ARMORY_OTA requests, which consist of
// applet updates.
func (ctl *controlInterface) Update(req []byte) (res []byte) {
	var err error

	defer func() {
		if err != nil {
			log.Printf("applet update error, %v", err)
			res = api.ErrorResponse(err)
		} else {
			resMsg := &api.Response{}
			res = resMsg.Bytes()
		}
	}()

	update := &api.AppletUpdate{}

	if err = proto.Unmarshal(req, update); err != nil {
		return
	}

	ctl.Lock()
	defer ctl.Unlock()

	if update.Seq == 0 {
		ctl.ota = &otaBuffer{
			total: update.Total,
			sig:   update.Data,
		}

		log.Printf("starting applet update (%d chunks)", ctl.ota.total)
		return
	} else if ctl.ota == nil ||
		update.Seq != ctl.ota.seq+1 ||
		update.Total != ctl.ota.total {

		err = errors.New("invalid firmware update sequence")
		return
	}

	if len(ctl.ota.buf) > otaLimit {
		err = errors.New("size limit exceeded")
		return
	}

	ctl.ota.seq = update.Seq
	ctl.ota.buf = append(ctl.ota.buf, update.Data...)

	if ctl.ota.seq%100 == 0 {
		log.Printf("received %d/%d applet update chunks", ctl.ota.seq, ctl.ota.total)
	}

	if ctl.ota.seq == ctl.ota.total {
		log.Printf("received all %d firmware update chunks", ctl.ota.total)

		go func(buf []byte, sig []byte) {
			// avoid USB control interface timeout
			time.Sleep(500 * time.Millisecond)

			if err = updateApplet(buf, sig); err != nil {
				log.Printf("firmware update error, %v", err)
			}

			if ctl.RPC.Ctx != nil {
				log.Printf("SM received applet update, restarting applet")
				ctl.RPC.Ctx.Stop()
			}

			// FIXME: restarting the applet results in networking
			// issues, investigate (or just reboot?).

			if _, err = loadApplet(taELF, ctl); err != nil {
				log.Printf("SM applet execution error, %v", err)
			}
		}(ctl.ota.buf, ctl.ota.sig)

		ctl.ota = nil
	}

	return
}
