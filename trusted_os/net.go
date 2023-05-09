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

func startNetworking() {
	imx6ul.GIC.EnableInterrupt(Network.IRQ, true)

	Network.EnableInterrupt(enet.IRQ_RXF)
	Network.Start(false)
}
