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

	"github.com/usbarmory/tamago/arm"
	"github.com/usbarmory/tamago/bits"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"

	"github.com/usbarmory/armory-boot/exec"

	"github.com/usbarmory/GoTEE/monitor"
)

const (
	// Watchdog interval (in ms) to force context switching (User -> System mode)
	// to prevent Trusted Applet starvation of Trusted OS resources.
	watchdogTimeout = 60 * 1000

	// watchdogWarningInterval (in ms) controls how long before the watchdog triggers
	// the service request interrupt will be raised.
	watchdogWarningInterval = 5 * 1000
)

// loadApplet loads a TamaGo unikernel as trusted applet.
func loadApplet(elf []byte, ctl *controlInterface) (ta *monitor.ExecCtx, err error) {
	image := &exec.ELFImage{
		Region: appletRegion,
		ELF:    elf,
	}

	// BEE requires caching to be activated before image loading
	imx6ul.ARM.ConfigureMMU(uint32(image.Region.Start()), uint32(image.Region.End()), 0, arm.MemoryRegion)

	if err = image.Load(); err != nil {
		return
	}

	if ta, err = monitor.Load(image.Entry(), image.Region, true); err != nil {
		return nil, fmt.Errorf("SM could not load applet: %v", err)
	}

	log.Printf("SM applet loaded addr:%#x entry:%#x size:%d", ta.Memory.Start(), ta.R15, len(elf))

	// register RPC receiver
	ta.Server.Register(ctl.RPC)
	ctl.RPC.Ctx = ta

	// set stack pointer to end of available memory
	ta.R13 = uint32(ta.Memory.End())

	// override default handler
	ta.Handler = handler

	// enable FIQs
	bits.Clear(&ta.SPSR, CPSR_FIQ)

	return ta, run(ta)
}

func run(ctx *monitor.ExecCtx) (err error) {
	mode := arm.ModeName(int(ctx.SPSR) & 0x1f)
	ns := ctx.NonSecure()

	log.Printf("SM applet started mode:%s sp:%#.8x pc:%#.8x ns:%v", mode, ctx.R13, ctx.R15, ns)

	irqHandler[imx6ul.WDOG2.IRQ] = func() {
		imx6ul.WDOG2.Service(watchdogTimeout)
	}

	// activate watchdog to prevent resource starvation
	imx6ul.GIC.EnableInterrupt(imx6ul.WDOG2.IRQ, true)
	imx6ul.WDOG2.EnableInterrupt(watchdogWarningInterval)
	imx6ul.WDOG2.EnableTimeout(watchdogTimeout)

	// route IRQs as FIQs to serve them through applet handler
	imx6ul.GIC.FIQEn(true)

	err = ctx.Run()

	// restore routing to IRQ handler
	imx6ul.GIC.FIQEn(false)

	// Re-enable interrupts as the monitor exception handler disables them
	// when switching back to System Mode.
	imx6ul.ARM.EnableInterrupts(false)

	log.Printf("SM applet stopped mode:%s sp:%#.8x lr:%#.8x pc:%#.8x ns:%v err:%v", mode, ctx.R13, ctx.R14, ctx.R15, ns, err)

	if err != nil && debug {
		err = inspect(taELF, ctx)
	}

	return
}
