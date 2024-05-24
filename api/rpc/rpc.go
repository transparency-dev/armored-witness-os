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

package rpc

import (
	"github.com/coreos/go-semver/semver"
	"github.com/transparency-dev/armored-witness-boot/config"
)

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

// WitnessStatus represents the witness applet's status.
type WitnessStatus struct {
	// Identity is the note-compatible public key used to verify witness signatures.
	Identity string
	// IP is the currently-assigned IP address of the witness applet.
	IP string
	// IDAttestPublicKey is the stable-derived use by this device to attest to witness IDs.
	IDAttestPublicKey string
	// AttestedID is a note formatted attestation for the current witness ID.
	AttestedID string
	// AttestedBastionID is a note formatted attestation for the current Bastion ID.
	AttestedBastionID string
}

// FirmwareUpdate represents a firmware update.
type FirmwareUpdate struct {
	// Sequence is a counter used to ensure correct ordering of chunks of firmware.
	Sequence uint

	// Image is the firmware image to be applied.
	Image []byte

	//  Proof contains firmware transparency artefacts for the new firmware image.
	Proof config.ProofBundle
}

// InstalledVersions represents the installed/running versions
// of the TrustedOS and applet.
type InstalledVersions struct {
	OS     semver.Version
	Applet semver.Version
}
