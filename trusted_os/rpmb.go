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

package main

import (
	"bytes"
	"crypto/aes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"strconv"

	"golang.org/x/crypto/pbkdf2"

	"github.com/usbarmory/tamago/soc/nxp/imx6ul"
	"github.com/usbarmory/tamago/soc/nxp/usdhc"

	"github.com/usbarmory/crucible/otp"

	"github.com/transparency-dev/armored-witness-os/rpmb"
)

const (
	// RPMB sector for CVE-2020-13799 mitigation
	dummySector = 0
	// version epoch length
	versionLength = 4
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
	Storage   *usdhc.USDHC
	partition *rpmb.RPMB
}

func (r *RPMB) init() (err error) {
	// derive key for RPBM MAC generation
	dk, err := imx6ul.DCP.DeriveKey([]byte(diversifierMAC), make([]byte, aes.BlockSize), -1)

	if err != nil {
		return fmt.Errorf("could not derive RPMB key (%v)", err)
	}

	uid := imx6ul.UniqueID()

	// setup RPMB partition
	r.partition, err = rpmb.Init(
		r.Storage,
		pbkdf2.Key(dk, uid[:], iter, sha256.Size, sha256.New),
		dummySector,
	)

	var e *rpmb.OperationError
	_, err = r.partition.Counter(false)

	if !(errors.As(err, &e) && e.Result == rpmb.AuthenticationKeyNotYetProgrammed) {
		return
	}

	// Fuse a bit to indicate previous key programming to prevent malicious
	// eMMC replacement to intercept ProgramKey().
	//
	// If already fused refuse to do any programming and bail.
	if res, err := otp.ReadOCOTP(rpmbFuseBank, rpmbFuseWord, 0, 1); err != nil || bytes.Equal(res, []byte{1}) {
		return fmt.Errorf("could not read RPMB program key flag (%x, %v)", res, err)
	}

	if err = otp.BlowOCOTP(rpmbFuseBank, rpmbFuseWord, 0, 1, []byte{1}); err != nil {
		return fmt.Errorf("could not fuse RPMB program key flag (%v)", err)
	}

	log.Print("RPMB authentication key not yet programmed, programming")

	if err = r.partition.ProgramKey(); err != nil {
		return fmt.Errorf("could not program RPMB key")
	}

	return
}

func parseVersion(s string) (version uint32, err error) {
	v, err := strconv.Atoi(s)

	if err != nil {
		return
	}

	return uint32(v), nil
}

// expectedVersion returns the version epoch stored in an RPMB area of the
// internal eMMC.
func (r *RPMB) expectedVersion(offset uint16) (version uint32, err error) {
	if r.partition == nil {
		return 0, errors.New("RPMB has not been initialized")
	}

	buf := make([]byte, versionLength)

	if err = r.partition.Read(offset, buf); err != nil {
		return
	}

	return binary.BigEndian.Uint32(buf), nil
}

// updateVersion writes a new version epoch in an RPMB area of the internal
// eMMC.
func (r *RPMB) updateVersion(offset uint16, version uint32) (err error) {
	if r.partition == nil {
		return errors.New("RPMB has not been initialized")
	}

	buf := make([]byte, versionLength)
	binary.BigEndian.PutUint32(buf, version)

	return r.partition.Write(offset, buf)
}

// checkVersion verifies version information against RPMB stored data.
//
// If the passed version is older than the RPMB area information of the
// internal eMMC an error is returned.
//
// If the passed version is more recent than the RPMB area information then the
// internal eMMC is updated with it.
func (r *RPMB) checkVersion(offset uint16, s string) (err error) {
	version, err := parseVersion(s)

	if err != nil {
		return
	}

	expectedVersion, err := r.expectedVersion(offset)

	if err != nil {
		return
	}

	switch {
	case expectedVersion > version:
		return errors.New("version mismatch")
	case expectedVersion == version:
		return
	case expectedVersion < version:
		return r.updateVersion(offset, version)
	}

	return
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
