// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package ft

import (
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

	sum, err := imx6ul.DCP.Sum256(elf)

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
