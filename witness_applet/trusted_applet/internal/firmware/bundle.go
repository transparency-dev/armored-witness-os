// Copyright 2023 The Armored Witness Applet authors. All Rights Reserved.
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

// Package firmware provides definitions of the firmware executable and
// associated metadata.
package firmware

// Bundle represents the required information for firmware to be installed
// onto the device.
type Bundle struct {
	// Checkpoint is an append-only commitment from the log that includes the
	// Manifest as a leaf.
	Checkpoint []byte
	// Index is the position in the log that Manifest is committed to as a leaf.
	Index uint64
	// InclusionProof is a chain of hashes that proves that Manifest is the
	// leaf at Index in the log committed to by Checkpoint.
	InclusionProof [][]byte
	// Manifest is the metadata about Firmware, including its type, provenance,
	// and semantic version. This includes a hash of Firmware, which binds this
	// executable to Checkpoint.
	Manifest []byte
	// Firmware is the elf executable data committed to by Manifest.
	Firmware []byte
}
