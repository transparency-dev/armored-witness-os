// Copyright 2023 The Armored Witness OS authors. All Rights Reserved.
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
	"fmt"
	"log"

	usbarmory "github.com/usbarmory/tamago/board/usbarmory/mk2"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"
	"github.com/usbarmory/tamago/soc/nxp/usdhc"
)

const (
	// fakeCardBlockSize is the number of bytes in a single memory block.
	fakeCardBlockSize = int64(512)
	// fakeCardNumBlocks defines the claimed size of the storage.
	fakeCardNumBlocks = int64(4<<30) / fakeCardBlockSize
)

// storage will return MMC backed storage if running on real hardware, or
// a fake in-memory storage device otherwise.
func storage() (Card, *RPMB) {
	if imx6ul.Native {
		s := usbarmory.MMC
		return s, &RPMB{storage: s}
	}
	return newFakeCard(fakeCardNumBlocks), &RPMB{}
}

// fakeCard is an implementation of an in-memory storage device.
//
// Rather than allocating a slab of RAM to emulate the entire device, it
// uses a map internally to associate slices (<= fakeCardBlockSize bytes)
// with sector numbers - this allows us to save RAM on unused/unwritten blocks.
type fakeCard struct {
	info usdhc.CardInfo
	mem  map[int64][]byte
}

// Read returns size bytes at offset in the fake storage
func (fc *fakeCard) Read(offset int64, size int64) ([]byte, error) {
	l := int64(fakeCardNumBlocks * fakeCardBlockSize)
	if offset >= l {
		return nil, fmt.Errorf("offset (%d) past end of storage (%d)", offset, l)
	}
	if offset+size > l {
		size = l - offset
	}
	if offset%fakeCardBlockSize != 0 {
		panic(fmt.Sprintf("non sector-aligned read at %d", offset))
	}
	r := make([]byte, size)
	base := offset / fakeCardBlockSize
	for i, rem := int64(0), size; rem > 0; i, rem = i+1, rem-fakeCardBlockSize {
		copy(r[i*fakeCardBlockSize:], fc.mem[base+i])
	}
	return r, nil
}

func (fc *fakeCard) WriteBlocks(lba int, b []byte) error {
	if l := fakeCardNumBlocks; int64(lba) >= l {
		return fmt.Errorf("lba (%d) >= device blocks (%d)", lba, l)
	}
	// If the data isn't a multiple of the blocksize, pad it up
	// so that it is.
	if r := int64(len(b)) % fakeCardBlockSize; r != 0 {
		b = append(b, make([]byte, fakeCardBlockSize-r)...)
	}
	for i, rem := int64(0), int64(len(b)); rem > 0; i, rem = i+1, rem-fakeCardBlockSize {
		buf := make([]byte, fakeCardBlockSize)
		copy(buf, b[i*fakeCardBlockSize:])
		fc.mem[int64(lba)+i] = buf
	}
	return nil
}

func (fc *fakeCard) Info() usdhc.CardInfo {
	return fc.info
}

func (fc *fakeCard) Detect() error {
	log.Println("Using fake MMC storage")
	return nil
}

// newFakeCard creates a new in-memory block device.
func newFakeCard(numBlocks int64) *fakeCard {
	return &fakeCard{
		mem: make(map[int64][]byte),
		info: usdhc.CardInfo{
			BlockSize: int(fakeCardBlockSize),
			Blocks:    int(numBlocks),
		},
	}
}
