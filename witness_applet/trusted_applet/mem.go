// Copyright 2022 The Armored Witness Applet authors. All Rights Reserved.
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

//go:build !bee
// +build !bee

package main

import (
	_ "unsafe"
)

const (
	appletStart = 0x90000000
	appletSize  = 0x10000000 // 256MB
)

//go:linkname ramStart runtime.ramStart
var ramStart uint32 = appletStart

//go:linkname ramSize runtime.ramSize
var ramSize uint32 = appletSize

//go:linkname ramStackOffset runtime.ramStackOffset
var ramStackOffset uint32 = 0x100
