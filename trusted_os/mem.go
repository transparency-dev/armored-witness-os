// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package main

import (
	_ "unsafe"

	"github.com/usbarmory/tamago/dma"
)

const (
	// Secure Monitor
	secureStart = 0x80000000
	secureSize  = 0x0e000000 // 224MB

	// Secure Monitor DMA
	secureDMAStart = 0x8e000000
	secureDMASize  = 0x02000000 // 32MB

	// Secure Monitor Applet
	appletStart = 0x90000000
	appletSize  = 0x10000000 // 256MB
)

//go:linkname ramStart runtime.ramStart
var ramStart uint32 = secureStart

//go:linkname ramSize runtime.ramSize
var ramSize uint32 = secureSize

var appletRegion *dma.Region

func init() {
	appletRegion, _ = dma.NewRegion(appletStart, appletSize, false)
	appletRegion.Reserve(appletSize, 0)

	dma.Init(secureDMAStart, secureDMASize)
}
