// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package ft

import (
	"encoding/binary"
	"errors"
)

func Extract(buf []byte) (proof []byte, elf []byte, err error) {
	var length uint32

	if len(buf) < 4 {
		err = errors.New("invalid length")
		return
	}

	length = binary.BigEndian.Uint32(buf[0:4])

	proof = buf[0:length]
	elf = buf[length:]

	return
}
