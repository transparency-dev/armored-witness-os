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
	"errors"
	"fmt"
	"log"
	_ "unsafe"

	"github.com/usbarmory/tamago/arm"
	usbarmory "github.com/usbarmory/tamago/board/usbarmory/mk2"
	"github.com/usbarmory/tamago/soc/nxp/usb"

	usbserial "github.com/usbarmory/imx-usbserial"

	"github.com/usbarmory/GoTEE/monitor"
)

const debug = true

var serial *usbserial.UART

func init() {
	// TODO(al): Probably want to reinstate this check after wave0!
	/*
			if imx6ul.SNVS.Available() {
			panic("fatal error, debug firmware not allowed on secure booted units")
		}
	*/
}

//go:linkname printk runtime.printk
func printk(c byte) {
	usbarmory.UART2.Tx(c)

	if serial != nil {
		serial.WriteByte(c)
	}
}

func configureUART(device *usb.Device) (err error) {
	if LAN == nil {
		return
	}

	serial = &usbserial.UART{
		Device: device,
	}

	return serial.Init()
}

func fileLine(buf []byte, pc uint32) (s string) {
	exe, err := elf.NewFile(bytes.NewReader(buf))

	if err != nil {
		return
	}

	addr := exe.Section(".text").Addr

	lineTableData, err := exe.Section(".gopclntab").Data()

	if err != nil {
		return
	}

	lineTable := gosym.NewLineTable(lineTableData, addr)

	if err != nil {
		return
	}

	symTableData, err := exe.Section(".gosymtab").Data()

	if err != nil {
		return
	}

	symTable, err := gosym.NewTable(symTableData, lineTable)

	if err != nil {
		return
	}

	file, line, _ := symTable.PCToLine(uint64(pc))

	return fmt.Sprintf("%s:%d", file, line)
}

func lookupSym(buf []byte, name string) (*elf.Symbol, error) {
	f, err := elf.NewFile(bytes.NewReader(buf))

	if err != nil {
		return nil, err
	}

	syms, err := f.Symbols()

	if err != nil {
		return nil, err
	}

	for _, sym := range syms {
		if sym.Name == name {
			return &sym, nil
		}
	}

	return nil, errors.New("symbol not found")
}

// segfault schedules the execution context to its fatal error function in
// order to have applet dump its own stack trace and exit. The target must be a
// GOOS=tamago applet which imports the runtime.CallOnG0 symbol, runtime.Exit
// must be set to graceful termination.
//
// Example of required applet main statements:
//
// ```
//
//	func init() {
//		runtime.Exit = applet.Exit
//		...
//	}
//
//	func main() {
//		...
//		runtime.CallOnG0()
//	}
//
// ```
func segfault(buf []byte, ctx *monitor.ExecCtx) (err error) {
	var sym *elf.Symbol

	if sym, err = lookupSym(taELF, "runtime.fatalthrow"); err != nil {
		return fmt.Errorf("could not find runtime.fatalthrow symbol, %v", err)
	}

	ctx.R0 = uint32(ctx.ExceptionVector)
	ctx.R1 = uint32(sym.Value)
	ctx.R2 = 0
	ctx.R3 = ctx.R15

	if sym, err = lookupSym(taELF, "runtime.CallOnG0"); err != nil {
		return fmt.Errorf("could not find runtime.CallOnG0 symbol, %v", err)
	}

	ctx.R15 = uint32(sym.Value)

	log.Printf("SM invoking applet %s handler pc:%#.8x", arm.VectorName(ctx.ExceptionVector), ctx.R3)

	err = ctx.Run()

	log.Printf("SM applet stopped sp:%#.8x lr:%#.8x pc:%#.8x err:%v", ctx.R13, ctx.R14, ctx.R15, err)

	return
}

func inspect(buf []byte, ctx *monitor.ExecCtx) (err error) {
	if false {
		log.Printf("PC\t%s", fileLine(buf, ctx.R15)) // PC
		log.Printf("LR\t%s", fileLine(buf, ctx.R14)) // LR

		switch ctx.ExceptionVector {
		case arm.UNDEFINED, arm.PREFETCH_ABORT, arm.DATA_ABORT:
			return segfault(buf, ctx)
		}
	}

	return
}
