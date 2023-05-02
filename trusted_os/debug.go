// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build debug
// +build debug

package main

import (
	_ "unsafe"

	usbarmory "github.com/usbarmory/tamago/board/usbarmory/mk2"
	"github.com/usbarmory/tamago/soc/nxp/usb"

	"github.com/usbarmory/imx-usbserial"
)

var serial usbserial.UART

//go:linkname printk runtime.printk
func printk(c byte) {
	usbarmory.UART2.Tx(c)
	serial.WriteByte(c)
}

func configureUART(device *usb.Device) (err error) {
	serial.Device = device
	return serial.Init()
}
