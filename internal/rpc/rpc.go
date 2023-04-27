// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package rpc

// Handler represents an RPC request for event handler registration.
type Handler struct {
	G uint32
	P uint32
}

// LEDStatus represents an RPC LED state request.
type LEDStatus struct {
	Name string
	On   bool
}

// WriteBlocks represents an RPC request for internal eMMC write.
type WriteBlocks struct {
	LBA  int
	Data []byte
}

// Read represents an RPC request for internal eMMC read.
type Read struct {
	Offset int64
	Size   int64
}
