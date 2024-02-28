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

//go:build !fake_rpmb
// +build !fake_rpmb

package main

import (
	"bytes"
	"crypto/aes"
	"crypto/sha256"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log"

	"golang.org/x/crypto/pbkdf2"

	"github.com/usbarmory/tamago/soc/nxp/imx6ul"
	"github.com/usbarmory/tamago/soc/nxp/usdhc"

	"github.com/coreos/go-semver/semver"
	"github.com/usbarmory/crucible/otp"

	"github.com/transparency-dev/armored-witness-os/rpmb"
)

const (
	// RPMB sector for CVE-2020-13799 mitigation
	dummySector = 0
	// version epoch length
	versionLength = 32
	// RPMB sector for OS rollback protection
	osVersionSector = 1
	// RPMB sector for TA rollback protection
	taVersionSector = 2
	// RPMB sector for TA use
	taUserSector = 3
	// RPMB OTP flag bank
	rpmbFuseBank = 4
	// RPMB OTP flag word
	rpmbFuseWord = 6

	diversifierMAC = "ArmoryWitnessMAC"
	iter           = 4096
)

type RPMB struct {
	storage   Card
	partition *rpmb.RPMB
}

func newRPMB(storage Card) (r *RPMB, err error) {
	return &RPMB{storage: storage}, nil
}

// isProgrammed returns true if the RPMB key program key flag is set to 1.
func (p *RPMB) isProgrammed() (bool, error) {
	res, err := otp.ReadOCOTP(rpmbFuseBank, rpmbFuseWord, 0, 1)
	if err != nil {
		return false, fmt.Errorf("could not read RPMB program key flag (%x, %v)", res, err)
	}
	return bytes.Equal(res, []byte{1}), nil
}

func (r *RPMB) init() error {
	// derived key for RPBM MAC generation
	var dk []byte
	var err error

	switch {
	case imx6ul.CAAM != nil:
		dk = make([]byte, sha256.Size) // dk needs to be correctly sized to receive the key.
		err = imx6ul.CAAM.DeriveKey([]byte(diversifierMAC), dk)
	case imx6ul.DCP != nil:
		dk, err = imx6ul.DCP.DeriveKey([]byte(diversifierMAC), make([]byte, aes.BlockSize), -1)
	default:
		err = errors.New("unsupported hardware")
	}
	if err != nil {
		return fmt.Errorf("could not derive RPMB key (%v)", err)
	}

	uid := imx6ul.UniqueID()

	card, ok := r.storage.(*usdhc.USDHC)
	if !ok {
		return errors.New("could not assert type *usdhc.USDHC from Card")
	}

	isProgrammed, err := r.isProgrammed()
	if err != nil {
		return err
	}
	// setup RPMB
	r.partition, err = rpmb.Init(
		card,
		pbkdf2.Key(dk, uid[:], iter, sha256.Size, sha256.New),
		dummySector,
		isProgrammed,
	)
	if err != nil {
		return fmt.Errorf("RPMB could not be initialized: %v", err)
	}

	_, err = r.partition.Counter(false)
	if err != nil {
		var e *rpmb.OperationError
		if !errors.As(err, &e) {
			return fmt.Errorf("RPMB failed to read counter: %v", err)
		}
		if e.Result != rpmb.AuthenticationKeyNotYetProgrammed {
			return fmt.Errorf("RPMB failed to read counter with operatation error: %v", err)
		}
	}

	// Fuse a bit to indicate previous key programming to prevent malicious
	// eMMC replacement to intercept ProgramKey().
	//
	// If already fused refuse to do any programming and bail.
	if isProgrammed {
		log.Printf("RPMB program key flag already fused")
		return nil
	}

	if err = otp.BlowOCOTP(rpmbFuseBank, rpmbFuseWord, 0, 1, []byte{1}); err != nil {
		return fmt.Errorf("could not fuse RPMB program key flag (%v)", err)
	}

	log.Print("RPMB authentication key not yet programmed, programming")

	if err = r.partition.ProgramKey(); err != nil {
		return fmt.Errorf("could not program RPMB key")
	}

	return nil
}

func parseVersion(s string) (version *semver.Version, err error) {
	return semver.NewVersion(s)
}

// expectedVersion returns the version epoch stored in an RPMB area of the
// internal eMMC.
func (r *RPMB) expectedVersion(offset uint16) (*semver.Version, error) {
	if r.partition == nil {
		return nil, errors.New("RPMB has not been initialized")
	}

	buf := make([]byte, versionLength)
	if err := r.partition.Read(offset, buf); err != nil {
		return nil, err
	}
	var v string
	if err := gob.NewDecoder(bytes.NewBuffer(buf)).Decode(&v); err != nil {
		if err == io.EOF {
			// We've not previously stored a version, so return 0.0.0
			return semver.NewVersion("0.0.0")
		}
		return nil, err
	}

	return semver.NewVersion(v)
}

// updateVersion writes a new version epoch in an RPMB area of the internal
// eMMC.
func (r *RPMB) updateVersion(offset uint16, version semver.Version) error {
	if r.partition == nil {
		return errors.New("RPMB has not been initialized")
	}
	buf := &bytes.Buffer{}
	if err := gob.NewEncoder(buf).Encode(version.String()); err != nil {
		return err
	}
	return r.partition.Write(offset, buf.Bytes())
}

// checkVersion verifies version information against RPMB stored data.
//
// If the passed version is older than the RPMB area information of the
// internal eMMC an error is returned.
//
// If the passed version is more recent than the RPMB area information then the
// internal eMMC is updated with it.
func (r *RPMB) checkVersion(offset uint16, s string) error {
	runningVersion, err := parseVersion(s)
	if err != nil {
		return err
	}

	expectedVersion, err := r.expectedVersion(offset)
	if err != nil {
		return err
	}

	switch {
	case runningVersion.LessThan(*expectedVersion):
		return errors.New("version mismatch")
	case expectedVersion.Equal(*runningVersion):
		return nil
	case expectedVersion.LessThan(*runningVersion):
		return r.updateVersion(offset, *runningVersion)
	}

	return nil
}

// transfer performs an authenticated data transfer to the card RPMB partition,
// the input buffer can contain up to 256 bytes of data, n can be passed to
// retrieve the partition write counter.
func (r *RPMB) transfer(offset uint16, buf []byte, n *uint32, write bool) (err error) {
	if write {
		err = r.partition.Write(offset, buf)
	} else {
		err = r.partition.Read(offset, buf)
	}

	if err != nil && n != nil {
		*n, err = r.partition.Counter(true)
	}

	return
}
