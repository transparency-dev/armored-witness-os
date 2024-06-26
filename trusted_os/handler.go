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

	"github.com/usbarmory/GoTEE/monitor"
	"github.com/usbarmory/GoTEE/syscall"
)

const CPSR_FIQ = 6

var (
	appletHandlerG uint32
	appletHandlerP uint32
)

var irqHandler = make(map[int]func())

// defined in handler.s
func wakeHandler(g uint32, p uint32)

func isr() (err error) {
	irq, end := imx6ul.GIC.GetInterrupt(true)

	if end != nil {
		close(end)
	}

	if handle, ok := irqHandler[irq]; ok {
		handle()
		return nil
	}

	return fmt.Errorf("unexpected IRQ %d", irq)
}

func handleInterrupts() {
	arm.RegisterInterruptHandler()

	for {
		arm.WaitInterrupt()

		if err := isr(); err != nil {
			log.Printf("SM IRQ handling error: %v", err)
		}
	}
}

func fiqHandler(ctx *monitor.ExecCtx) (_ error) {
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

	if err := isr(); err != nil {
		log.Printf("SM FIQ handling error: %v", err)
	}

	// mask FIQs, applet handler will request unmasking when done
	bits.Set(&ctx.SPSR, CPSR_FIQ)

	wakeHandler(appletHandlerG, appletHandlerP)

	return
}

// The exception handler is responsible for the following tasks:
//   - override GoTEE default handling for SYS_WRITE to avoid interleaved logs
//   - serve RX/TX syscalls for Ethernet packets I/O
//   - service Ethernet IRQs for incoming packets
//
// As a precaution against an unexpectedly long syscall handler, we also service
// the watchdog whenever we transmit a packet.
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
			// Ensure the watchdog doesn't get starved by servicing it here as a precaution.
			// The logic is that if we're either sending data out or ACKing received
			// packets then we're almost certainly not wedged, so servicing the dog
			// is reasonable.
			imx6ul.WDOG2.Service(watchdogTimeout)
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
