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
	"net"

	"github.com/usbarmory/tamago/soc/nxp/enet"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"
	"github.com/usbarmory/tamago/soc/nxp/usb"

	"github.com/usbarmory/GoTEE/monitor"

	"github.com/usbarmory/imx-usbnet"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
)

const (
	RXQueueSize = 1000
	TXQueueSize = 1000
)

// Trusted OS syscalls
const (
	RX   = 0x10000000
	TX   = 0x10000001
	FIQ  = 0x10000002
	FREQ = 0x10000003
)

// default Trusted OS USB network settings
const (
	deviceMAC = "1a:55:89:a2:69:41"
	hostMAC   = "1a:55:89:a2:69:42"
)

var (
	rxQueue chan []byte
	txQueue chan []byte
)

func asyncRx(buf []byte) {
	select {
	case rxQueue <- buf:
	default:
	}
}

func asyncTx() (buf []byte) {
	select {
	case buf = <-txQueue:
	default:
	}

	return
}

func rxFromApplet(ctx *monitor.ExecCtx) (err error) {
	select {
	case buf := <-rxQueue:
		off, n, err := ctx.TransferRegion()

		if err != nil {
			return err
		}

		r := len(buf)

		if r > n {
			return fmt.Errorf("invalid transfer size (%d > %d)", r, n)
		}

		ctx.Memory.Write(ctx.Memory.Start(), off, buf)
		ctx.Ret(r)
	default:
		ctx.Ret(0)
	}

	return
}

func txFromApplet(ctx *monitor.ExecCtx) (err error) {
	off, n, err := ctx.TransferRegion()

	if err != nil {
		return
	}

	buf := make([]byte, n)

	ctx.Memory.Read(ctx.Memory.Start(), off, buf)

	switch {
	case LAN != nil:
		LAN.Tx(buf)
	case USB != nil:
		txQueue <- buf
	}

	return
}

func netStartUSB() {
	hostHardwareAddr, _ := net.ParseMAC(hostMAC)
	deviceHardwareAddr, _ := net.ParseMAC(deviceMAC)

	device := &usb.Device{}
	usbnet.ConfigureDevice(device, deviceMAC)

	linkAddr, _ := tcpip.ParseMACAddress(deviceMAC)

	nic := &usbnet.NIC{
		HostMAC:   hostHardwareAddr,
		DeviceMAC: deviceHardwareAddr,
		Link:      channel.New(256, usbnet.MTU, linkAddr),
		Device:    device,
	}

	rxQueue = make(chan []byte, RXQueueSize)
	txQueue = make(chan []byte, TXQueueSize)

	nic.Rx = func(buf []byte, lastErr error) (_ []byte, err error) {
		asyncRx(buf)
		return
	}

	nic.Tx = func(_ []byte, lastErr error) (in []byte, err error) {
		in = asyncTx()
		return
	}

	if err := nic.Init(); err != nil {
		panic(err)
	}

	USB.Device = device

	USB.Init()
	USB.DeviceMode()

	USB.EnableInterrupt(usb.IRQ_URI) // reset
	USB.EnableInterrupt(usb.IRQ_PCI) // port change detect
	USB.EnableInterrupt(usb.IRQ_UI)  // transfer completion

	irqHandler[USB.IRQ] = func() {
		USB.ServiceInterrupts()
	}

	imx6ul.GIC.EnableInterrupt(USB.IRQ, true)
}

func netStartLAN() {
	rxQueue = make(chan []byte, RXQueueSize)

	irqHandler[LAN.IRQ] = func() {
		for buf := LAN.Rx(); buf != nil; buf = LAN.Rx() {
			asyncRx(buf)
			LAN.ClearInterrupt(enet.IRQ_RXF)
		}

	}

	LAN.EnableInterrupt(enet.IRQ_RXF)
	LAN.Start(false)

	imx6ul.GIC.EnableInterrupt(LAN.IRQ, true)
}

func netStart() {
	switch {
	case LAN != nil:
		netStartLAN()
	case USB != nil:
		netStartUSB()
	}
}
