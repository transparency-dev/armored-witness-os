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
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/usbarmory/armory-witness-log/api"
	"github.com/usbarmory/armory-witness-log/api/verify"

	"github.com/usbarmory/tamago/soc/nxp/imx6ul"

	"golang.org/x/mod/sumdb/note"
)

const (
	elfPath   = "trusted-applet.elf"
	logOrigin = "Armory Witness Test 1"
)

func verifyProof(elf []byte, proof []byte, oldProof *api.ProofBundle) (pb *api.ProofBundle, err error) {
	if len(proof) == 0 {
		return nil, errors.New("missing proof")
	}

	pb = &api.ProofBundle{}

	if err = json.Unmarshal(proof, pb); err != nil {
		return
	}

	logSigV, err := note.NewVerifier(string(LogPublicKey))

	if err != nil {
		return
	}

	frSigV, err := note.NewVerifier(string(FRPublicKey))

	if err != nil {
		return
	}

	var oldCP api.Checkpoint

	if oldProof != nil {
		verifiers := note.VerifierList(logSigV)

		if n, _ := note.Open(oldProof.NewCheckpoint, verifiers); n != nil {
			if err = oldCP.Unmarshal([]byte(n.Text)); err != nil {
				return
			}
		}
	}

	var sum [32]byte

	switch {
	case imx6ul.DCP != nil:
		sum, err = imx6ul.DCP.Sum256(elf)
	case imx6ul.CAAM != nil:
		sum, err = imx6ul.CAAM.Sum256(elf)
	default:
		sum = sha256.Sum256(elf)
	}

	if err != nil {
		return
	}

	hashes := map[string][]byte{
		elfPath: sum[:],
	}

	if err = verify.Bundle(*pb, oldCP, logSigV, frSigV, hashes, logOrigin); err != nil {
		return
	}

	// leaf hashes are not needed so we can save space
	pb.LeafHashes = nil

	return
}
