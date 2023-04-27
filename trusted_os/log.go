// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package main

import (
	"bytes"
	"os"
)

var buf bytes.Buffer

const (
	outputLimit = 1024
	flushChr = 0x0a // \n
)

func bufferedStdoutLog(c byte) (err error) {
	buf.WriteByte(c)

	if c == flushChr || buf.Len() > outputLimit {
		_, err = os.Stdout.Write(buf.Bytes())
		buf.Reset()
	}

	return
}
