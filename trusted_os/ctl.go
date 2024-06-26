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
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"runtime"
	"sync"

	"google.golang.org/protobuf/proto"

	usbarmory "github.com/usbarmory/tamago/board/usbarmory/mk2"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"
	"github.com/usbarmory/tamago/soc/nxp/usb"

	"github.com/transparency-dev/armored-witness-os/api"
	"github.com/transparency-dev/armored-witness-os/api/rpc"
	"github.com/transparency-dev/armored-witness-os/internal/hab"
)

const (
	// Table 22–6, MII management register set, 802.3-2008
	MII_STATUS = 0x1
	// Table 22–8, Status register bit definitions, 802.3-2008
	STATUS_LINK = 2

	// Max bytes to return via HID - we use 64 as a safe guess for protobuf wire overhead
	maxChunkSize = api.MaxMessageSize - 64
)

// witnessStatus represents the latest view of the witness applet's status.
// It's intended to be updated periodially by the applet via RPC to the OS.
var witnessStatus *rpc.WitnessStatus

type controlInterface struct {
	sync.Mutex

	Device *usb.Device
	RPC    *RPC
	// SRKHash, if set, is the hex encoded SHA256 which may be fused into the device to enable HAB.
	SRKHash string

	ota *otaBuffer

	logBuffer []byte
}

func getStatus() (s *api.Status) {
	s = &api.Status{
		SRKHash:  SRKHash,
		Revision: Revision,
		Build:    Build,
		Version:  osVersion.String(),
		Runtime:  fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH),
		// TODO(jayhou): set IdentityCounter here.
	}
	if witnessStatus != nil {
		s.Witness = &api.WitnessStatus{
			Identity:          witnessStatus.Identity,
			IP:                witnessStatus.IP,
			IDAttestPublicKey: witnessStatus.IDAttestPublicKey,
			AttestedID:        witnessStatus.AttestedID,
			AttestedBastionID: witnessStatus.AttestedBastionID,
		}
	}

	switch {
	case LAN != nil:
		miiStatus := LAN.ReadPHYRegister(usbarmory.PHY_ADDR, MII_STATUS)
		s.Link = miiStatus&(1<<STATUS_LINK) > 0
		s.MAC = LAN.MAC.String()
	case USB != nil:
		mode, err := usbarmory.FrontPortMode()
		s.Link = err != nil && mode == usbarmory.STATE_ATTACHED_SRC
	}

	if imx6ul.Native {
		s.HAB = imx6ul.SNVS.Available()
		s.Serial = fmt.Sprintf("%X", imx6ul.UniqueID())
	}

	return
}

func (ctl *controlInterface) HandleMessage(_ []byte) (_ []byte) {
	return
}

func (ctl *controlInterface) Status(_ []byte) (res []byte) {
	res, _ = proto.Marshal(getStatus())
	return
}

func (ctl *controlInterface) Config(req []byte) (res []byte) {
	if len(req) == 0 {
		return api.ErrorResponse(errors.New("empty configuration"))
	}

	ctl.RPC.Cfg = req

	if ctl.RPC.Ctx != nil {
		var err error

		log.Printf("SM received configuration update, restarting applet")
		ctl.RPC.Ctx.Stop()
		<-ctl.RPC.Ctx.Done()

		if _, err = loadApplet(taELF, ctl); err != nil {
			return api.ErrorResponse(err)
		}
	} else {
		log.Printf("SM received configuration update w/o applet running")
	}

	return api.EmptyResponse()
}

func (ctl *controlInterface) HAB(_ []byte) []byte {
	srkh, err := hex.DecodeString(ctl.SRKHash)
	if err != nil {
		return api.ErrorResponse(fmt.Errorf("built-in SRK hash is invalid: %v", err))
	}
	if len(srkh) != sha256.Size {
		return api.ErrorResponse(errors.New("built-in SRK hash is wrong size"))
	}

	sv := imx6ul.SNVS.Monitor()
	log.Printf("SNVS Monitor state:\n%+v", sv)
	/*
		if sv.State != snvs.SSM_STATE_TRUSTED && sv.State != snvs.SSM_STATE_SECURE {
			return api.ErrorResponse(fmt.Errorf("SNVS State is invalid (0b%04b)", sv.State))
		}
	*/

	log.Printf("SM activating HAB with SRK hash %x", srkh)
	if err := hab.Activate(srkh); err != nil {
		return api.ErrorResponse(err)
	}

	return api.EmptyResponse()
}

func (ctl *controlInterface) handleLogsRequest(r []byte, l func() []byte) (res []byte) {
	req := &api.LogMessagesRequest{}
	if err := proto.Unmarshal(r, req); err != nil {
		log.Printf("Failed to parse LogMessages request: %v", err)
		return api.ErrorResponse(err)
	}
	if !req.Continue {
		log.Printf("Grabbing log messages...")
		logs := l()
		ll := len(logs)
		b := &bytes.Buffer{}
		gz := gzip.NewWriter(b)
		if _, err := gz.Write(logs); err != nil {
			log.Printf("Failed to gzip logs: %v", err)
		}
		if err := gz.Close(); err != nil {
			log.Printf("Failed to close gzip writer: %v", err)

		}
		logs = nil
		ctl.logBuffer = b.Bytes()
		log.Printf("Compressed %d bytes of log messages to %d send", ll, len(ctl.logBuffer))
	}
	ret := &api.LogMessagesResponse{}
	logChunk := 1024
	if l := len(ctl.logBuffer); l > logChunk {
		ret.More = true
		ret.Payload, ctl.logBuffer = ctl.logBuffer[:logChunk], ctl.logBuffer[logChunk:]
	} else {
		ret.More = false
		ret.Payload = ctl.logBuffer
		ctl.logBuffer = nil
	}
	b, _ := proto.Marshal(ret)
	return b
}

func (ctl *controlInterface) ConsoleLogs(r []byte) (res []byte) {
	return ctl.handleLogsRequest(r, func() []byte { return getConsoleLogs() })
}

func (ctl *controlInterface) CrashLogs(r []byte) (res []byte) {
	return ctl.handleLogsRequest(r, func() []byte {
		l, err := retrieveLastCrashLog(ctl.RPC.Storage)
		if err != nil {
			return []byte(fmt.Sprintf("Failed to retrieve crash logs: %v", err))
		}
		return l
	})
}

func (ctl *controlInterface) Start() {
	device := &usb.Device{}
	serial := fmt.Sprintf("%X", imx6ul.UniqueID())

	if err := configureDevice(device, serial); err != nil {
		log.Fatal(err)
	}

	if err := configureHID(device, ctl); err != nil {
		log.Fatal(err)
	}

	if err := configureUART(device); err != nil {
		log.Fatal(err)
	}

	if Control == nil {
		return
	}

	Control.Device = device
	Control.DeviceMode()

	irqHandler[Control.IRQ] = func() {
		Control.ServiceInterrupts()
	}

	Control.EnableInterrupt(usb.IRQ_URI) // reset
	Control.EnableInterrupt(usb.IRQ_PCI) // port change detect
	Control.EnableInterrupt(usb.IRQ_UI)  // transfer completion

	imx6ul.GIC.EnableInterrupt(Control.IRQ, true)
}
