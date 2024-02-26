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

package rpmb

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
)

const (
	FrameLength = 512
	macOffset   = 284
)

// p99, Table 18 — RPMB Request/Response Message Types, JESD84-B51
const (
	AuthenticationKeyProgramming = iota + 1
	WriteCounterRead
	AuthenticatedDataWrite
	AuthenticatedDataRead
	ResultRead
	AuthenticatedDeviceConfigurationWrite
	AuthenticatedDeviceConfigurationRead
)

// p100, Table 20 — RPMB Operation Results, JESD84-B51
const (
	OperationOK = iota
	GeneralFailure
	AuthenticationFailure
	CounterFailure
	AddressFailure
	WriteFailure
	ReadFailure
	AuthenticationKeyNotYetProgrammed
)

type OperationError struct {
	Result uint16
}

func (e *OperationError) Error() string {
	return fmt.Sprintf("operation failed (%x)", e.Result)
}

// Request configuration
type Config struct {
	// compute request MAC before sending
	RequestMAC bool
	// validate response MAC after receiving
	ResponseMAC bool
	// set Nonce field with random value
	RandomNonce bool
	// get response with a result read request
	ResultRead bool
}

// p98, Table 17 — Data Frame Files for RPMB, JESD84-B51
type DataFrame struct {
	StuffBytes   [196]byte
	KeyMAC       [32]byte
	Data         [256]byte
	Nonce        [16]byte
	WriteCounter [4]byte
	Address      [2]byte
	BlockCount   [2]byte
	Result       [2]byte
	Resp         byte
	Req          byte
}

// Counter returns the data frame WriteCounter in uint32 format.
func (d *DataFrame) Counter() uint32 {
	return binary.BigEndian.Uint32(d.WriteCounter[:])
}

// Bytes converts the data frame structure to byte array format.
func (d *DataFrame) Bytes() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, d)
	return buf.Bytes()
}

func (p *RPMB) op(req *DataFrame, cfg *Config) (res *DataFrame, err error) {
	var rel bool

	p.Lock()
	defer p.Unlock()

	if !p.init {
		return nil, errors.New("RPMB instance not initialized")
	}

	mac := hmac.New(sha256.New, p.key[:])

	if cfg.RequestMAC {
		mac.Write(req.Bytes()[FrameLength-macOffset:])
		copy(req.KeyMAC[:], mac.Sum(nil))
		mac.Reset()
	}

	if cfg.RandomNonce {
		copy(req.Nonce[:], rng(len(req.Nonce)))
	}

	switch req.Req {
	case AuthenticationKeyProgramming, AuthenticatedDataWrite, AuthenticatedDeviceConfigurationWrite:
		rel = true
	default:
		rel = false
	}

	// send request
	if err = p.card.WriteRPMB(req.Bytes(), rel); err != nil {
		return
	}

	// read result when required
	if cfg.ResultRead {
		resReq := DataFrame{
			Req: ResultRead,
		}

		// send result read request
		if err = p.card.WriteRPMB(resReq.Bytes(), false); err != nil {
			return
		}
	}

	buf := make([]byte, FrameLength)
	res = &DataFrame{}

	// read response
	if err = p.card.ReadRPMB(buf); err != nil {
		return
	}

	// parse response
	if err = binary.Read(bytes.NewReader(buf), binary.LittleEndian, res); err != nil {
		return
	}

	// validate response

	if cfg.ResponseMAC {
		mac.Write(buf[FrameLength-macOffset:])

		if !hmac.Equal(res.KeyMAC[:], mac.Sum(nil)) {
			return nil, errors.New("invalid response MAC")
		}
	}

	if req.Req != res.Resp {
		return nil, errors.New("request/response type mismatch")
	}

	if req.Nonce != res.Nonce {
		return nil, errors.New("nonce mismatch")
	}

	result := binary.BigEndian.Uint16(res.Result[:])

	if result != uint16(OperationOK) {
		return nil, &OperationError{result}
	}

	return
}

func (p *RPMB) transfer(kind byte, offset uint16, buf []byte) (err error) {
	if len(buf) > FrameLength/2 {
		return errors.New("transfer size must not exceed 256 bytes")
	}

	cfg := &Config{
		RequestMAC:  true,
		ResponseMAC: true,
	}

	req := &DataFrame{
		Req: kind,
	}

	if kind == AuthenticatedDataWrite {
		counter, err := p.Counter(true)

		if err != nil {
			return err
		}

		binary.BigEndian.PutUint32(req.WriteCounter[:], counter)

		cfg.ResultRead = true
	} else {
		cfg.RandomNonce = true
	}

	binary.BigEndian.PutUint16(req.BlockCount[:], 1)
	binary.BigEndian.PutUint16(req.Address[:], offset)
	copy(req.Data[:], buf)

	res, err := p.op(req, cfg)

	if err != nil {
		return
	}

	if kind == AuthenticatedDataRead {
		copy(buf, res.Data[:])
	} else if res.Counter() != req.Counter()+1 {
		return errors.New("write counter mismatch")
	}

	return
}

func rng(n int) []byte {
	buf := make([]byte, n)

	if _, err := rand.Read(buf); err != nil {
		log.Fatal(err)
	}

	return buf
}
