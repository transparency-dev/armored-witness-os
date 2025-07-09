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

//go:build fake_rpmb
// +build fake_rpmb

package main

import (
	"errors"

	"github.com/coreos/go-semver/semver"
	"github.com/usbarmory/rpmb"
)

const (
	// version epoch length
	versionLength = 32
	// RPMB sector for OS rollback protection
	osVersionSector = 1
	// RPMB sector for TA rollback protection
	taVersionSector = 2

	// RPMB sector for TA use
	taUserSector = 3

	sectorLength = 256
	numSectors   = 16
)

type RPMB struct {
	mem     [numSectors][sectorLength]byte
	counter uint32
}

func newRPMB(_ Card) (*RPMB, error) {
	return &RPMB{
		mem: [numSectors][sectorLength]byte{},
	}, nil
}

func (r *RPMB) init() error {
	return nil
}

func parseVersion(s string) (version *semver.Version, err error) {
	return semver.NewVersion(s)
}

// expectedVersion returns the version epoch stored in a fake RPMB area.
func (r *RPMB) expectedVersion(offset uint16) (*semver.Version, error) {
	v := string(r.mem[offset][:versionLength])

	return semver.NewVersion(v)
}

// updateVersion writes a new version epoch in a fake RPMB area.
func (r *RPMB) updateVersion(offset uint16, version semver.Version) error {
	copy(r.mem[offset][:], []byte(version.String()))
	r.counter++

	return nil
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

// transfer performs a data transfer to the fake RPMB area,
// the input buffer can contain up to 256 bytes of data, n can be passed to
// retrieve the write counter.
func (r *RPMB) transfer(sector uint16, buf []byte, n *uint32, write bool) (err error) {
	if len(buf) > rpmb.FrameLength/2 {
		return errors.New("transfer size must not exceed 256 bytes")
	}

	if write {
		copy(r.mem[sector][:], buf)
		r.counter++
	} else {
		copy(buf, r.mem[sector][:])
	}

	if n != nil {
		*n = r.counter
	}
	return
}
