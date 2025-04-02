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

	"github.com/coreos/go-semver/semver"
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
func wakeHandlerPreGo123(g uint32, p uint32)
func wakeHandlerGo123(g uint32, p uint32)
func wakeHandlerGo124(g uint32, p uint32)

// handler123Cutover is the semver representation of the cut over between wakeHandler implementations above.
// Anything less that this should use the legacy PreGo123 version.
const handler123Cutover = "1.23.0"
const handler124Cutover = "1.24.0"

var (
	// wHandler is the wakeHandler implementation to be used, 1.24+ by default.
	wHandler           func(g uint32, p uint32)
	wHandler123Cutover = *semver.New(handler123Cutover)
	wHandler124Cutover = *semver.New(handler124Cutover)
)

func configureWakeHandler(rtVersion semver.Version) {
	switch {
	case rtVersion.LessThan(wHandler123Cutover):
		log.Printf("SM Using legacy pre-%s wakeHandler", wHandler123Cutover.String())
		wHandler = wakeHandlerPreGo123
	case rtVersion.LessThan(wHandler124Cutover):
		log.Printf("SM Using legacy %s wakeHandler", wHandler123Cutover.String())
		wHandler = wakeHandlerGo123
	default:
		log.Printf("SM Using OS runtime %s wakeHandler", wHandler124Cutover.String())
		wHandler = wakeHandlerGo124
	}
}

func isr() {
	irq := imx6ul.GIC.GetInterrupt(true)

	if handle, ok := irqHandler[irq]; ok {
		handle()
		return
	}
	log.Printf("SM unexpected IRQ %d", irq)
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

	isr()

	// mask FIQs, applet handler will request unmasking when done
	bits.Set(&ctx.SPSR, CPSR_FIQ)

	wHandler(appletHandlerG, appletHandlerP)

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
