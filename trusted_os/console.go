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

const debug = false

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
