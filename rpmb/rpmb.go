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

// Package rpmb implements Replay Protected Memory Block (RPMB) configuration
// and control on eMMCs accessed through TamaGo NXP uSDHC driver.
//
// This package is only meant to be used with `GOOS=tamago GOARCH=arm` as
// supported by the TamaGo framework for bare metal Go on ARM SoCs, see
// https://github.com/usbarmory/tamago.
//
// The API supports mitigations for CVE-2020-13799 as described in the whitepaper linked at:
//
//	https://www.westerndigital.com/support/productsecurity/wdc-20008-replay-attack-vulnerabilities-rpmb-protocol-applications
package rpmb

import (
	"errors"
	"fmt"
	"sync"

	"github.com/usbarmory/tamago/soc/nxp/usdhc"
)

const keyLen = 32

// RPMB defines a Replay Protected Memory Block partition access instance.
type RPMB struct {
	sync.Mutex

	card *usdhc.USDHC
	key  [keyLen]byte
	init bool
}

// Init returns a new RPMB instance for a specific MMC card and MAC key. The
// dummyBlock argument is an unused sector, required for CVE-2020-13799
// mitigation to invalidate uncommitted writes.
func Init(card *usdhc.USDHC, key []byte, dummyBlock uint16, writeDummy bool) (p *RPMB, err error) {
	if card == nil {
		return nil, fmt.Errorf("no MMC card set")
	}

	if !card.Info().MMC {
		return nil, fmt.Errorf("no MMC card detected")
	}

	if len(key) != keyLen {
		return nil, errors.New("invalid MAC key size")
	}

	p = &RPMB{
		card: card,
		init: true,
	}

	copy(p.key[:], key)

	// invalidate uncommitted writes (CVE-2020-13799) if the RPMB has previously been programmed
	if writeDummy {
		if err = p.Write(dummyBlock, nil); err != nil {
			return nil, err
		}
	}

	return
}

// ProgramKey programs the RPMB partition authentication key.
//
// *WARNING*: this is a one-time irreversible operation for the specific MMC
// card associated to the RPMB partition instance.
func (p *RPMB) ProgramKey() (err error) {
	cfg := &Config{
		ResultRead: true,
	}

	req := &DataFrame{
		KeyMAC: p.key,
		Req:    AuthenticationKeyProgramming,
	}

	_, err = p.op(req, cfg)

	return
}

// Counter returns the RPMB partition write counter, the argument boolean
// indicates whether the read operation should be authenticated.
func (p *RPMB) Counter(auth bool) (n uint32, err error) {
	cfg := &Config{
		RandomNonce: auth,
		ResponseMAC: auth,
	}

	req := &DataFrame{
		Req: WriteCounterRead,
	}

	res, err := p.op(req, cfg)

	if err != nil {
		return
	}

	return res.Counter(), nil
}

// Write performs an authenticated data transfer to the card RPMB partition,
// the input buffer can contain up to 256 bytes of data.
//
// The write operation mitigates CVE-2020-13799 by verifying that the response
// counter is equal to a single increment of the request counter, otherwise an
// error is returned.
func (p *RPMB) Write(offset uint16, buf []byte) (err error) {
	return p.transfer(AuthenticatedDataWrite, offset, buf)
}

// Read performs an authenticated data transfer from the card RPMB partition,
// the input buffer can contain up to 256 bytes of data.
func (p *RPMB) Read(offset uint16, buf []byte) (err error) {
	return p.transfer(AuthenticatedDataRead, offset, buf)
}
