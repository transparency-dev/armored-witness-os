// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build !debug
// +build !debug

package main

import (
	"io"
	"log"
	_ "unsafe"

	"github.com/usbarmory/tamago/soc/nxp/imx6ul"
	"github.com/usbarmory/tamago/soc/nxp/usb"
)

// The Trusted OS does not log any sensitive information to the serial console,
// however it is desirable to silence any potential stack trace or runtime
// errors to avoid unwanted information leaks.
//
// The TamaGo board support for the USB armory Mk II enables the serial console
// (UART2) at runtime initialization, which therefore invokes imx6.UART2.Init()
// before init().
//
// To this end the runtime printk function, responsible for all console logging
// operations (i.e. stdout/stderr), is overridden with a NOP. Secondarily UART2
// is disabled at the first opportunity (init()).

func init() {
	// disable console
	imx6ul.UART2.Disable()
	// silence logging
	log.SetOutput(io.Discard)
}

//go:linkname printk runtime.printk
func printk(c byte) {
	// ensure that any serial output is supressed before UART2 disabling
}

func configureUART(device *usb.Device) error {
	return nil
}
