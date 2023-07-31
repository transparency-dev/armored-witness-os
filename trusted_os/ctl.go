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

	s = &api.Status{
		Serial:   fmt.Sprintf("%X", imx6ul.UniqueID()),
		HAB:      imx6ul.SNVS.Available(),
		Revision: Revision,
		Build:    Build,
		Version:  version,
		Runtime:  fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH),
	}

	switch {
	case LAN != nil:
		miiStatus := LAN.ReadPHYRegister(usbarmory.PHY_ADDR, MII_STATUS)
		s.Link = miiStatus&(1<<STATUS_LINK) > 0
	case USB != nil:
		mode, err := usbarmory.FrontPortMode()
		s.Link = err != nil && mode == usbarmory.STATE_ATTACHED_SRC
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

		if _, err = loadApplet(taELF, ctl); err != nil {
			return api.ErrorResponse(err)
		}
	} else {
		log.Printf("SM received configuration update w/o applet running")
	}

	return api.EmptyResponse()
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

	Control.EnableInterrupt(usb.IRQ_URI) // reset
	Control.EnableInterrupt(usb.IRQ_PCI) // port change detect
	Control.EnableInterrupt(usb.IRQ_UI)  // transfer completion

	irqHandler[Control.IRQ] = func() {
		Control.ServiceInterrupts()
	}

	imx6ul.GIC.EnableInterrupt(Control.IRQ, true)
}
