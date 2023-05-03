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
	"log"

	"github.com/usbarmory/tamago/arm"
	"github.com/usbarmory/tamago/bits"
	"github.com/usbarmory/tamago/soc/nxp/enet"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"

	"github.com/usbarmory/GoTEE/monitor"
	"github.com/usbarmory/GoTEE/syscall"
)

const CPSR_FIQ = 6

var (
	appletHandlerG uint32
	appletHandlerP uint32
)

// defined in handler.s
func wakeAppletHandler(g uint32, p uint32)

func fiqHandler(ctx *monitor.ExecCtx) (err error) {
	// We want to handle FIQs only when raised from User mode (e.g.
	// Trusted Applet running) as we need to wake up the applet
	// handler with FIQs masked.
	//
	// If we get here from System mode (e.g. Trusted OS running)
	// resume execution with FIQ masked (FIQs are masked soon as
	// possible when switching back to the Trusted OS but we can be
	// raced between exception vector and disabling instruction).
	if _, saved := ctx.Mode(); saved != arm.USR_MODE {
		bits.Set(&ctx.SPSR, CPSR_FIQ)
		return
	}

	irq, end := imx6ul.GIC.GetInterrupt(true)

	if end != nil {
		end <- true
	}

	switch irq {
	case Control.IRQ:
		Control.ServiceInterrupts()
	case imx6ul.WDOG1.IRQ:
		imx6ul.WDOG1.Service(watchdogTimeout)
	case Network.IRQ:
		for buf := Network.Rx(); buf != nil; buf = Network.Rx() {
			rxFromEth(buf)
			Network.ClearInterrupt(enet.IRQ_RXF)
		}
	default:
		log.Printf("SM received unexpected IRQ %d", irq)
		return
	}

	// mask FIQs, applet handler will request unmasking when done
	bits.Set(&ctx.SPSR, CPSR_FIQ)

	wakeAppletHandler(appletHandlerG, appletHandlerP)

	return
}

// The exception handler is responsible for the following tasks:
//   - override GoTEE default handling for SYS_WRITE to avoid interleaved logs
//   - serve RX/TX syscalls for Ethernet packets I/O
//   - service Ethernet IRQs for incoming packets
func handler(ctx *monitor.ExecCtx) (err error) {
	switch ctx.ExceptionVector {
	case arm.FIQ:
		return fiqHandler(ctx)
	case arm.SUPERVISOR:
		switch ctx.A0() {
		case syscall.SYS_WRITE:
			return bufferedStdoutLog(byte(ctx.A1()))
		case RX:
			return rxFromApplet(ctx)
		case TX:
			return txFromApplet(ctx)
		case FIQ:
			bits.Clear(&ctx.SPSR, CPSR_FIQ)
		case FREQ:
			return imx6ul.SetARMFreq(uint32(ctx.A1()))
		default:
			return monitor.SecureHandler(ctx)
		}
	default:
		log.Fatalf("unhandled exception %x", ctx.ExceptionVector)
	}

	return
}
