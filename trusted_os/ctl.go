// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

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

	"github.com/usbarmory/armory-witness/api"
)

const (
	// Table 22–6, MII management register set, 802.3-2008
	MII_STATUS = 0x1
	// Table 22–8, Status register bit definitions, 802.3-2008
	STATUS_LINK = 2
)

type controlInterface struct {
	sync.Mutex

	RPC *RPC

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

	if miiStatus & (1 << STATUS_LINK) > 0 {
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

func (ctl *controlInterface) Start() {
	device := &usb.Device{}

	if err := configureDevice(device); err != nil {
		log.Fatal(err)
	}

	if err := configureHID(device, ctl); err != nil {
		log.Fatal(err)
	}

	if err := configureUART(device); err != nil {
		log.Fatal(err)
	}

	imx6ul.GIC.EnableInterrupt(Control.IRQ, true)

	Control.Init()
	Control.DeviceMode()
	Control.Reset()

	Control.EnableInterrupt(usb.IRQ_UI)

	// never returns
	Control.Start(device)

	log.Fatal("internal error")
}
