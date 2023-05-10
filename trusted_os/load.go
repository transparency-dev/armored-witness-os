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
	"crypto/sha256"
	"fmt"
	"log"

	"github.com/usbarmory/tamago/arm"
	"github.com/usbarmory/tamago/bits"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"

	"github.com/usbarmory/armory-boot/exec"

	"github.com/usbarmory/GoTEE/monitor"
)

// Watchdog interval (in ms) to force context switching (User -> System mode)
// to prevent Trusted Applet starvation of Trusted OS resources.
const watchdogTimeout = 5000

// loadApplet loads a TamaGo unikernel as trusted applet.
func loadApplet(elf []byte, ctl *controlInterface) (ta *monitor.ExecCtx, err error) {
	image := &exec.ELFImage{
		Region: appletRegion,
		ELF:    elf,
	}

	if err = image.Load(); err != nil {
		return
	}

	if ta, err = monitor.Load(image.Entry(), image.Region, true); err != nil {
		return nil, fmt.Errorf("SM could not load applet: %v", err)
	}

	log.Printf("SM loaded applet addr:%#x entry:%#x size:%d", ta.Memory.Start(), ta.R15, len(elf))

	if imx6ul.Native {
		if ctl.RPC.Diversifier, err = imx6ul.DCP.Sum256(elf); err != nil {
			return
		}
	} else {
		ctl.RPC.Diversifier = sha256.Sum256(elf)
	}

	// register RPC receiver
	ta.Server.Register(ctl.RPC)
	ctl.RPC.Ctx = ta

	// set stack pointer to end of available memory
	ta.R13 = uint32(ta.Memory.End())

	// override default handler
	ta.Handler = handler
	ta.Debug = true

	// enable FIQs
	bits.Clear(&ta.SPSR, CPSR_FIQ)

	go run(ta)

	return
}

func run(ctx *monitor.ExecCtx) (err error) {
	// route IRQs as FIQs to serve them through applet handler
	imx6ul.GIC.FIQEn(true)

	mode := arm.ModeName(int(ctx.SPSR) & 0x1f)
	ns := ctx.NonSecure()

	log.Printf("SM starting mode:%s sp:%#.8x pc:%#.8x ns:%v", mode, ctx.R13, ctx.R15, ns)

	imx6ul.GIC.EnableInterrupt(imx6ul.WDOG1.IRQ, true)
	imx6ul.WDOG1.EnableInterrupt()
	imx6ul.WDOG1.EnableTimeout(watchdogTimeout)

	err = ctx.Run()

	// TODO: investigate why this is necessary
	imx6ul.ARM.EnableInterrupts(false)
	// restore routing to IRQ handler
	imx6ul.GIC.FIQEn(false)

	log.Printf("SM stopped mode:%s sp:%#.8x lr:%#.8x pc:%#.8x ns:%v", mode, ctx.R13, ctx.R14, ctx.R15, ns)

	return
}
