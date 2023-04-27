// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package main

import (
	"errors"
	"log"
	"net"

	"github.com/usbarmory/tamago/soc/nxp/enet"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"

	"github.com/usbarmory/GoTEE/monitor"
)

const RXQueueSize = 1000

// Trusted OS syscalls
const (
	RX   = 0x10000000
	TX   = 0x10000001
	FIQ  = 0x10000002
	FREQ = 0x10000003
)

var rxQueue = make(chan []byte, RXQueueSize)

func rxFromEth(buf []byte) {
	select {
	case rxQueue <- buf:
	default:
	}
}

func rxFromApplet(ctx *monitor.ExecCtx) (err error) {
	var n uint

	select {
	case buf := <-rxQueue:
		off := ctx.A1() - ctx.Memory.Start()
		n = uint(len(buf))

		if !(off >= 0 && off < (ctx.Memory.Size()-n)) {
			return errors.New("invalid offset")
		}

		ctx.Memory.Write(ctx.Memory.Start(), int(off), buf)
	default:
	}

	ctx.Ret(n)

	return
}

func txFromApplet(ctx *monitor.ExecCtx) (err error) {
	off := ctx.A1() - ctx.Memory.Start()
	n := ctx.A2()
	buf := make([]byte, n)

	if !(off >= 0 && off < (ctx.Memory.Size()-uint(n))) {
		return errors.New("invalid offset")
	}

	ctx.Memory.Read(ctx.Memory.Start(), int(off), buf)
	Network.Tx(buf)

	return
}

func startNetworking(mac string) {
	hostAddress, err := net.ParseMAC(mac)

	if err != nil {
		log.Fatalf("invalid MAC, %v", err)
	}

	imx6ul.GIC.EnableInterrupt(Network.IRQ, true)

	Network.SetMAC(hostAddress)
	Network.EnableInterrupt(enet.IRQ_RXF)
	Network.Start(false)
}
