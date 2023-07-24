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
	"github.com/usbarmory/tamago/dma"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"
)

var appletRegion *dma.Region

func init() {
	appletRegion, _ = dma.NewRegion(appletStart, appletSize, false)
	appletRegion.Reserve(appletSize, 0)

	dma.Init(secureDMAStart, secureDMASize)

	deriveKeyMemory, _ := dma.NewRegion(imx6ul.OCRAM_START, imx6ul.OCRAM_SIZE, false)

	switch {
	case imx6ul.CAAM != nil:
		imx6ul.CAAM.DeriveKeyMemory = deriveKeyMemory
	case imx6ul.DCP != nil:
		imx6ul.DCP.DeriveKeyMemory = deriveKeyMemory
	}
}
