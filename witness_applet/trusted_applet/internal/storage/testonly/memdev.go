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

// Package testonly provides support for storage tests.
package testonly

import (
	"fmt"
	"testing"
)

// MemBlockSize is the number of bytes in a single memory block.
const MemBlockSize = 512

// MemDev is a simple in-memory block device.
type MemDev struct {
	Storage [][MemBlockSize]byte

	// OnBlockWritten is called just after a mem block has been written.
	OnBlockWritten func(lba uint)
}

// BlockSize returns the block size of the underlying storage system.
func (md MemDev) BlockSize() uint {
	return MemBlockSize
}

// ReadBlocks reads len(b) bytes into b from contiguous storage blocks starting
// at the given block address.
// b must be an integer multiple of the device's block size.
func (md MemDev) ReadBlocks(lba uint, b []byte) error {
	if lba >= uint(len(md.Storage)) {
		return fmt.Errorf("lba (%d) >= device blocks (%d)", lba, len(md.Storage))
	}
	lenB := uint(len(b))
	bl := lenB / MemBlockSize
	if l := uint(len(md.Storage)); lba+bl > l {
		bl = l - lba
	}
	for i := uint(0); i < bl; i++ {
		copy(b[i*MemBlockSize:], md.Storage[lba+i][:])
	}
	return nil
}

// WriteBlocks writes len(b) bytes from b to contiguous storage blocks starting
// at the given block address.
// b must be an integer multiple of the device's block size.
//
// Returns the number of blocks written, or an error.
func (md MemDev) WriteBlocks(lba uint, b []byte) (uint, error) {
	if lba >= uint(len(md.Storage)) {
		return 0, fmt.Errorf("lba (%d) >= device blocks (%d)", lba, len(md.Storage))
	}
	// If the data isn't a multiple of the blocksize, pad it up
	// so that it is.
	if r := len(b) % MemBlockSize; r != 0 {
		b = append(b, make([]byte, MemBlockSize-r)...)
	}
	lenB := uint(len(b))
	bl := lenB / MemBlockSize
	if l := uint(len(md.Storage)); lba+bl > l {
		bl = l - lba
	}
	for i := uint(0); i < bl; i++ {
		copy(md.Storage[lba+i][:], b[i*MemBlockSize:])
		if md.OnBlockWritten != nil {
			md.OnBlockWritten(lba + i)
		}
	}
	return bl, nil
}

// NewMemDev creates a new in-memory block device.
func NewMemDev(t *testing.T, numBlocks uint) *MemDev {
	t.Helper()
	return &MemDev{Storage: make([][MemBlockSize]byte, numBlocks)}
}
