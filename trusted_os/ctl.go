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
	"fmt"
	"log"
	"runtime"
	"sync"

	"google.golang.org/protobuf/proto"

	usbarmory "github.com/usbarmory/tamago/board/usbarmory/mk2"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"
	"github.com/usbarmory/tamago/soc/nxp/usb"

	"github.com/transparency-dev/armored-witness-os/api"
)

const (
	// Table 22–6, MII management register set, 802.3-2008
	MII_STATUS = 0x1
	// Table 22–8, Status register bit definitions, 802.3-2008
	STATUS_LINK = 2
)

type controlInterface struct {
	sync.Mutex

	Device *usb.Device
	RPC    *RPC

	ota *otaBuffer
}

func getStatus() (s *api.Status) {
	version, _ := parseVersion(Version)

	miiStatus := Network.ReadPHYRegister(usbarmory.PHY_ADDR, MII_STATUS)

	s = &api.Status{
		Serial:   fmt.Sprintf("%X", imx6ul.UniqueID()),
		HAB:      imx6ul.SNVS.Available(),
		Revision: Revision,
		Build:    Build,
		Version:  version,
		Runtime:  fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH),
	}

	if miiStatus&(1<<STATUS_LINK) > 0 {
		s.Link = true
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
	if err := proto.Unmarshal(req, ctl.RPC.Cfg); err != nil {
		return api.ErrorResponse(err)
	}

	if ctl.RPC.Ctx != nil {
		var err error

		log.Printf("SM received configuration update, restarting applet")
		ctl.RPC.Ctx.Stop()

		if _, err = loadApplet(taELF, ctl); err != nil {
			return api.ErrorResponse(err)
		}
	}

	return api.EmptyResponse()
}

func (ctl *controlInterface) Start(irq bool) {
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

	Control.Device = device

	if !imx6ul.Native {
		return
	}

	Control.Init()
	Control.DeviceMode()

	if irq {
		imx6ul.GIC.EnableInterrupt(Control.IRQ, true)

		Control.EnableInterrupt(usb.IRQ_URI) // reset
		Control.EnableInterrupt(usb.IRQ_PCI) // port change detect
		Control.EnableInterrupt(usb.IRQ_UI)  // transfer completion
	} else {
		Control.Reset()
		// never returns
		Control.Start(device)
	}
}
