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
