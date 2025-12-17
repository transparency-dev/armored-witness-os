// Copyright 2024 The Armored Witness OS authors. All Rights Reserved.
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

//go:build tamper
// +build tamper

package main

import (
	"runtime"

	"github.com/usbarmory/tamago/soc/nxp/caam"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"
	"github.com/usbarmory/tamago/soc/nxp/snvs"
)

// enableTamperDetection configures strict SNVS tamper detection policy with
// immediate hard fail on security violations
func enableTamperDetection() {
	sp := snvs.SecurityPolicy{
		Clock:             true,
		Temperature:       true,
		Voltage:           true,
		SecurityViolation: true,
		HardFail:          true,
		HAC:               0,
	}

	imx6ul.SNVS.SetPolicy(sp)
}

// enableTextRTIC starts CAAM integrity monitor on the Go runtime memory region
func enableTextRTIC() (err error) {
	var blocks []caam.MemoryBlock

	textStart, textEnd := runtime.TextRegion()

	blocks = append(blocks, caam.MemoryBlock{
		Address: textStart,
		Length:  textEnd - textStart,
	})

	return imx6ul.CAAM.EnableRTIC(blocks)
}

func init() {
	if imx6ul.Native && imx6ul.SNVS.Available() {
		enableTamperDetection()
	}

	if imx6ul.CAAM != nil {
		_ = enableTextRTIC()
	}
}
