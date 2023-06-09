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
	"bytes"
	"debug/elf"
	"debug/gosym"
	"fmt"
	"os"
)

var buf bytes.Buffer

const (
	outputLimit = 1024
	flushChr    = 0x0a // \n
)

func bufferedStdoutLog(c byte) (err error) {
	buf.WriteByte(c)

	if c == flushChr || buf.Len() > outputLimit {
		_, err = os.Stdout.Write(buf.Bytes())
		buf.Reset()
	}

	return
}

func fileLine(buf[]byte, pc uint32) (s string) {
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
