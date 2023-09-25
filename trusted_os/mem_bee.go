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

//go:build bee
// +build bee

package main

import (
	_ "unsafe"
)

// The following memory regions are within an alias of external DDR, required
// when memory encryption is enforced by the i.MX6UL Bus Encryption Engine
// (BEE).
const (
	// The Secure Monitor RAM cannot be used as reserved area for
	// arm.Init() as the L1/L2 page tables cannot be placed in BEE aliased
	// memory due to its caching requirements, we therefore override
	// vecTableStart with the alias physical pointer.
	physicalStart = 0x80000000 // imx6ul.MMDC_BASE

	// Secure Monitor DMA
	//
	// BEE aliased regions must be accessed either through cache or 16 byte
	// accesses, this makes it impractical for peripheral driver DMA use
	// and we must therefore keep DMA on a non-aliased region.
	secureDMAStart = 0x8e000000
	secureDMASize  = 0x02000000 // 32MB

	// Secure Monitor
	secureStart = 0x10000000 // bee.AliasRegion0
	secureSize  = 0x0e000000 // 224MB

	// Secure Monitor Applet
	appletStart = 0x20000000
	appletSize  = 0x10000000 // 256MB
)

//go:linkname ramStart runtime.ramStart
var ramStart uint32 = secureStart

//go:linkname ramSize runtime.ramSize
var ramSize uint32 = secureSize

//go:linkname vecTableStart github.com/usbarmory/tamago/arm.vecTableStart
var vecTableStart uint32 = physicalStart
