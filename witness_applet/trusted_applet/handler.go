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

package main

import (
	"log"
	"math"
	"runtime"
	"time"

	"github.com/usbarmory/GoTEE/syscall"
	enet "github.com/usbarmory/imx-enet"

	"github.com/transparency-dev/armored-witness-os/api/rpc"
)

func eventHandler() {
	var handler rpc.Handler

	handler.G, handler.P = runtime.GetG()

	if err := syscall.Call("RPC.Register", handler, nil); err != nil {
		log.Fatalf("TA event handler registration error, %v", err)
	}

	n := 0
	out := make([]byte, enet.MTU)

	for {
		// To avoid losing interrupts, re-enabling must happen only
		// after we are sleeping.
		go syscall.Write(FIQ, nil, 0)

		// sleep indefinitely until woken up by runtime.WakeG
		time.Sleep(math.MaxInt64)

		// check for Ethernet RX event
		for n = rxFromEth(out); n > 0; n = rxFromEth(out) {
			rx(out[0:n])
		}
	}
}
