// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

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

	abconfig "github.com/usbarmory/armory-boot/config"
	"github.com/usbarmory/armory-witness-boot/config"

	"github.com/usbarmory/armory-witness/api"
)

const (
	otaLimit   = 31457280
	confSector = 2097152
	taSector   = confSector + config.MaxLength/512
)

type otaBuffer struct {
	total uint32
	seq   uint32
	sig   []byte
	buf   []byte
}

// read reads the trusted applet and its signature from internal storage, the
// applet and signatures are *not* verified by this function.
func read(card *usdhc.USDHC) (taELF []byte, taSig []byte, err error) {
	buf, err := card.Read(confSector, config.MaxLength)

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
func flash(card *usdhc.USDHC, buf []byte, lba int) (err error) {
	blockSize := card.Info().BlockSize
	blocks := len(buf) / blockSize
	batch := 64

	// write in batch to limit DMA requirements
	for i := 0; i < blocks; i += batch {
		if i+batch > blocks {
			batch = blocks - i
		}

		start := i * blockSize
		end := start + blockSize*batch

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
			time.Sleep(1 * time.Second)
		}
	}()

	log.Printf("SM applet verification")
	if err = abconfig.Verify(taELF, taSig, PublicKey); err != nil {
		return fmt.Errorf("applet verification error, %v", err)
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

	log.Printf("SM flashing applet signature")

	if err = flash(Storage, taSig, confSector); err != nil {
		return fmt.Errorf("applet signature flashing error, %v", err)
	}

	log.Printf("SM flashing applet")

	if err = flash(Storage, taELF, taSector); err != nil {
		return fmt.Errorf("applet flashing error, %v", err)
	}

	log.Printf("SM applet update complete")
	usbarmory.LED("white", false)

	log.Printf("SM rebooting")
	time.Sleep(1 * time.Second)
	usbarmory.Reset()

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

		go func(buf []byte) {
			if err = updateApplet(ctl.ota.buf, ctl.ota.sig); err != nil {
				log.Printf("firmware update error, %v", err)
			}
		}(ctl.ota.buf)

		ctl.ota = nil
	}

	return
}
