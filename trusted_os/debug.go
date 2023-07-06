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

//go:build debug
// +build debug

package main

import (
	"bytes"
	"debug/elf"
	"debug/gosym"
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/usbarmory/tamago/arm"
	usbarmory "github.com/usbarmory/tamago/board/usbarmory/mk2"
	"github.com/usbarmory/tamago/dma"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"
	"github.com/usbarmory/tamago/soc/nxp/usb"

	"github.com/usbarmory/imx-usbserial"
)

const debug = true

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

func watchdogForensics(applet []byte) (string, error) {
	f, err := elf.NewFile(bytes.NewReader(applet))
	if err != nil {
		return "", fmt.Errorf("failed to open ELF: %v", err)
	}

	syms, err := f.Symbols()
	if err != nil {
		return "", fmt.Errorf("failed to fetch symbols: %v", err)
	}

	var allGPtr, allGLen, textStart, textEnd uint32
	for _, s := range syms {
		switch t, n := elf.ST_TYPE(s.Info), s.Name; {
		case t == elf.STT_OBJECT && n == "runtime.allgptr":
			allGPtr = uint32(s.Value)
		case t == elf.STT_OBJECT && n == "runtime.allglen":
			allGLen = uint32(s.Value)
		case t == elf.STT_FUNC && n == "runtime.text":
			textStart = uint32(s.Value)
		case t == elf.STT_FUNC && n == "runtime.etext":
			textEnd = uint32(s.Value)
		}
	}

	r := fmt.Sprintf("watchdogForensics: allGPtr 0x%x allGLen 0x%x textStart 0x%x textEnd 0x%x\n", allGPtr, allGLen, textStart, textEnd)
	if allGPtr == 0 || allGLen == 0 || textStart == 0 || textEnd == 0 {
		return "", fmt.Errorf("didn't find all syms, not doing forensics: %s", r)
	}

	st, err := symTable(f)
	if err != nil {
		return "", fmt.Errorf("failed to create symbol table: %v", err)
	}

	for i := uint32(0); i < allGLen; i++ {
		gptr := (*uint32)(unsafe.Pointer(uintptr(allGPtr + i*4)))

		if gptr == nil {
			break
		}

		g := (*g)(unsafe.Pointer(uintptr(*gptr)))

		if g == nil {
			break
		}

		r += fmt.Sprintf("g[%d]: %x\n", i, g)

		if g.m == nil {
			fmt.Printf("\tsched: %x\n", g.sched)
		} else {
			stack := mem(uint(g.stacklo), int(g.stackhi-g.stacklo), nil)

			for i := 0; i < len(stack); i += 4 {
				try := binary.LittleEndian.Uint32(stack[i : i+4])

				if try >= uint32(textStart) && try <= uint32(textEnd) {
					if l, err := PCToLine(st, try); err == nil {
						r += fmt.Sprintf("\tpotential LR: %s\n", l)
					}
				}
			}
		}
	}

	return r, nil
}

type m struct {
	g0      *g
	morebuf gobuf
}

type gobuf struct {
	sp   uint32
	pc   uint32
	g    uint32
	ctxt uint32
	ret  uint32
	lr   uint32
	bp   uint32
}

type g struct {
	stacklo     uint32
	stackhi     uint32
	stackguard0 uint32
	stackguard1 uint32
	_panic      uint32
	_defer      uint32
	m           *m
	sched       gobuf
	syscallsp   uint32
	syscallpc   uint32
}

func symTable(f *elf.File) (symTable *gosym.Table, err error) {
	addr := f.Section(".text").Addr

	lineTableData, err := f.Section(".gopclntab").Data()

	if err != nil {
		return
	}

	lineTable := gosym.NewLineTable(lineTableData, addr)

	if err != nil {
		return
	}

	symTableData, err := f.Section(".gosymtab").Data()

	if err != nil {
		return
	}

	return gosym.NewTable(symTableData, lineTable)
}

func PCToLine(st *gosym.Table, pc uint32) (s string, err error) {
	file, line, _ := st.PCToLine(uint64(pc))

	return fmt.Sprintf("%s:%d", file, line), nil
}

func mem(start uint, size int, w []byte) (b []byte) {
	// temporarily map page zero if required
	if z := uint32(1 << 20); uint32(start) < z {
		imx6ul.ARM.ConfigureMMU(0, z, (arm.TTE_AP_001<<10)|arm.TTE_SECTION)
		defer imx6ul.ARM.ConfigureMMU(0, z, 0)
	}

	return memCopy(start, size, w)
}

func memCopy(start uint, size int, w []byte) (b []byte) {
	mem, err := dma.NewRegion(start, size, true)

	if err != nil {
		panic("could not allocate memory copy DMA")
	}

	start, buf := mem.Reserve(size, 0)
	defer mem.Release(start)

	if len(w) > 0 {
		copy(buf, w)
	} else {
		b = make([]byte, size)
		copy(b, buf)
	}

	return
}
