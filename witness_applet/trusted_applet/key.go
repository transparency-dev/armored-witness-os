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

package main

import (
	"crypto/aes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"log"

	"github.com/goombaio/namegenerator"
	"github.com/transparency-dev/armored-witness-os/api"
	"github.com/usbarmory/GoTEE/syscall"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/mod/sumdb/note"
)

var (
	attestSigningKey string
	// attestPublicKey can be used to verify that a given witnessPublicKey
	// was derived on a known device.
	attestPublicKey             string
	witnessPublicKey            string
	witnessSigningKey           string
	witnessPublicKeyAttestation string
	bastionSigningKey           ed25519.PrivateKey
	bastionIDAttestation        string
)

// deriveIdentityKeys creates this witness' signing and attestation identities.
//
// Keys are derived using the OS' DeriveKey RPC, which in turn uses the hardware
// secret along with several diversification parameters including one passed in
// from this function.
//
// The witness signing ID diversifier includes the value of securely stored counter
// which will be incremented each time a new identity is required.
//
// The device attestation identity uses a static diversifier, and so is intended
// to remain stable throughout the lifetime of the (fused) device.
//
// TODO(al): The derived key should change if the device is wiped.
//
// Since we never store these derived keys anywhere, for any given device (and,
// in the case of the witness ID, counter) this function MUST reproduce the
// same key on each boot.
func deriveIdentityKeys() {
	var status api.Status
	if err := syscall.Call("RPC.Status", nil, &status); err != nil {
		log.Fatalf("Failed to fetch Status: %v", err)
	}

	// Add an obvious prefix to key names when we're running without secure boot
	prefix := ""
	if !status.HAB {
		prefix = "DEV:"
	}

	// Other than via the identity counter, the diversifier and key name in here
	// MUST NOT be changed, or we'll break the invariant that this key is static.
	witnessSigningKey, witnessPublicKey = deriveNoteSigner(
		fmt.Sprintf("%sWitnessKey-id:%d", prefix, status.IdentityCounter),
		status.Serial,
		func(rnd io.Reader) string {
			return fmt.Sprintf("%sArmoredWitness-%s", prefix, randomName(rnd))
		})

	// The diversifier or key names in here MUST NOT be changed, or we'll
	// break the invariant that this attestation key is static for the *lifetime of the
	// (fused) device*!
	attestSigningKey, attestPublicKey = deriveNoteSigner(
		fmt.Sprintf("%sID-Attestation", prefix),
		status.Serial,
		func(_ io.Reader) string {
			return fmt.Sprintf("%sAW-ID-Attestation-%s", prefix, status.Serial)
		})

	// Attest to the witness ID so we can convince others that successive witness IDs
	// were derived on a known armored witness unit.
	witnessPublicKeyAttestation = attestID(&status, witnessPublicKey)

	// Other than via the counter, the diversifier and key name in here
	// MUST NOT be changed, or we'll break the invariant that this key is static.
	bastionSec, bastionPub := deriveEd25519(
		fmt.Sprintf("%sBastionKey-id:0", prefix),
		status.Serial)
	bastionSigningKey = bastionSec
	bastionID := fmt.Sprintf("%064x", sha256.Sum256(bastionPub))

	// Attest to the bastion ID so we can convince others that this ID
	// was derived on a known armored witness unit
	bastionIDAttestation = attestBastion(&status, bastionID)

}

// attestID uses attestSigningKey to sign a note which binds the passed in witness ID to this device's
// serial number and current identity counter.
//
// The witness ID attestation note contents is formatted like so:
//
//	"ArmoredWitness ID attestation v1"
//	<Device serial string>
//	<Witness identity counter in decimal>
//	<Witness identity note verifier string>
//
// Returns the note verifier string which can be used to open the note, and the note containing the witness ID attestation.
func attestID(status *api.Status, pubkey string) string {
	aN := &note.Note{
		Text: fmt.Sprintf("ArmoredWitness ID attestation v1\n%s\n%d\n%s\n", status.Serial, status.IdentityCounter, witnessPublicKey),
	}
	return attestNote(status, aN)
}

// attestBastion uses attestSigningKey to sign a note which binds the passed in witness ID to this device's
// serial number and current identity counter.
//
// The witness ID attestation note contents is formatted like so:
//
//	"ArmoredWitness BastionID attestation v1"
//	<Device serial string>
//	<Witness BastionID counter in decimal>
//	<Witness BastionID ASCII hex string>
//
// Returns the note verifier string which can be used to open the note, and the note containing the witness ID attestation.
func attestBastion(status *api.Status, bastionID string) string {
	aN := &note.Note{
		Text: fmt.Sprintf("ArmoredWitness BastionID attestation v1\n%s\n%d\n%s\n", status.Serial, 0, bastionID),
	}
	return attestNote(status, aN)
}

func attestNote(status *api.Status, aN *note.Note) string {
	aSigner, err := note.NewSigner(attestSigningKey)
	if err != nil {
		panic(fmt.Errorf("failed to create attestation signer: %v", err))
	}
	attestation, err := note.Sign(aN, aSigner)
	if err != nil {
		panic(fmt.Errorf("failed to sign witness ID attestation: %v", err))
	}

	return string(attestation)
}

// deriveNoteSigner uses the h/w secret to derive a new note.Signer.
//
// diversifier should uniquely specify the key's intended usage, uniqueID should be the
// device's h/w unique identifier, hab should reflect the device's secure boot status, and keyName
// should be a function which will return the name for the key - it may use the provided Reader as
// a source of entropy while generating the name if needed.
func deriveNoteSigner(diversifier string, uniqueID string, keyName func(io.Reader) string) (string, string) {
	r := deriveHKDF(diversifier, uniqueID)
	sec, pub, err := note.GenerateKey(r, keyName(r))
	if err != nil {
		log.Fatalf("Failed to generate derived note key: %v", err)
	}
	return sec, pub
}

// deriveEd25519 uses the hardware secret to device a new ed25519 keypair.
//
// diversifier should uniquely specify the key's intended usage, uniqueID should be the
// device's h/w unique identifier.
func deriveEd25519(diversifier string, uniqueID string) (ed25519.PrivateKey, ed25519.PublicKey) {
	r := deriveHKDF(diversifier, uniqueID)
	pub, priv, err := ed25519.GenerateKey(r)
	if err != nil {
		log.Fatalf("Failed to generate derived ed25519 key: %v", err)
	}
	return priv, pub
}

// randomName generates a random human-friendly name.
func randomName(rnd io.Reader) string {
	// Figure out our name
	nSeed := make([]byte, 8)
	if _, err := rnd.Read(nSeed); err != nil {
		log.Fatalf("Failed to read name entropy: %v", err)
	}

	ng := namegenerator.NewNameGenerator(int64(binary.LittleEndian.Uint64(nSeed)))
	return ng.Generate()
}

// deriveHKDF uses the OS' DeriveKey RPC to get a reproducible AES key from the hardware,
// and uses that, along with the passed in uniqueID, to produce bytes from a HKDF which may
// be used to produce other types of key.
func deriveHKDF(diversifier string, uniqueID string) io.Reader {
	// We'll use the provided RPC call to do the derivation in h/w, but since this is based on
	// AES it expects the diversifier to be 16 bytes long.
	// We'll hash our diversifier text and truncate to 16 bytes, and use that:
	diversifierHash := sha256.Sum256([]byte(diversifier))
	var aesKey [sha256.Size]byte
	if err := syscall.Call("RPC.DeriveKey", ([aes.BlockSize]byte)(diversifierHash[:aes.BlockSize]), &aesKey); err != nil {
		log.Fatalf("Failed to derive h/w key, %v", err)
	}

	return hkdf.New(sha256.New, aesKey[:], []byte(uniqueID), nil)
}
