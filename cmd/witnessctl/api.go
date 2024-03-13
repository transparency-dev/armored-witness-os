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

//go:build !tamago
// +build !tamago

package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/cheggaaa/pb/v3"
	flynn_hid "github.com/flynn/hid"
	"github.com/flynn/u2f/u2fhid"
	"google.golang.org/protobuf/proto"

	"github.com/transparency-dev/armored-witness-os/api"
)

// we use 64 as a safe guess for protobuf wire overhead
const maxChunkSize = api.MaxMessageSize - 64

func confirm(msg string) bool {
	var res string

	fmt.Printf("%s (y/n): ", msg)
	fmt.Scanln(&res)

	return res == "y"
}

type Device struct {
	u2f *u2fhid.Device
	usb *flynn_hid.DeviceInfo
}

func (d Device) status() (s *api.Status, err error) {
	res, err := d.u2f.Command(api.U2FHID_ARMORY_INF, nil)

	if err != nil {
		return
	}

	s = &api.Status{}
	err = proto.Unmarshal(res, s)

	return
}

func (d Device) hab() error {
	buf, err := d.u2f.Command(api.U2FHID_ARMORY_HAB, nil)
	if err != nil {
		return err
	}
	res := &api.Response{}
	if err := proto.Unmarshal(buf, res); err != nil {
		return err
	}
	if res.Error != api.ErrorCode_NONE {
		return fmt.Errorf("%v: %s", res.Error, res.Payload)
	}
	return nil
}

func (d Device) consoleLogs() (string, error) {
	buf, err := d.u2f.Command(api.U2FHID_ARMORY_CONSOLE_LOGS, nil)
	if err != nil {
		return "", err
	}

	return string(buf), nil
}

func (d Device) sendUpdateHeader(signature []byte, total int) (err error) {
	update := &api.AppletUpdate{
		Total: uint32(total),
		Seq:   uint32(0), // MUST be 0
		Payload: &api.AppletUpdate_Header{
			Header: &api.AppletUpdateHeader{
				Signature: signature,
				// TODO: fill this out
			},
		},
	}

	buf, err := d.u2f.Command(api.U2FHID_ARMORY_OTA, []byte(update.Bytes()))

	if err != nil {
		return err
	}

	res := &api.Response{}

	if err = proto.Unmarshal(buf, res); err != nil {
		return err
	}

	if res.Error != api.ErrorCode_NONE {
		return fmt.Errorf("%+v", res)
	}

	return
}

func (d Device) sendUpdateChunk(data []byte, seq int, total int) (err error) {
	if seq <= 0 {
		return fmt.Errorf("seq is %d, it must be >= 0 to send update chunks", seq)
	}
	update := &api.AppletUpdate{
		Total: uint32(total),
		Seq:   uint32(seq),
		Payload: &api.AppletUpdate_Data{
			Data: data,
		},
	}

	buf, err := d.u2f.Command(api.U2FHID_ARMORY_OTA, []byte(update.Bytes()))

	if err != nil {
		return err
	}

	res := &api.Response{}

	if err = proto.Unmarshal(buf, res); err != nil {
		return err
	}

	if res.Error != api.ErrorCode_NONE {
		return fmt.Errorf("%+v", res)
	}

	return
}

func (d Device) ota(taELFPath string, taSigPath string) (err error) {
	if len(taELFPath) == 0 {
		return errors.New("trusted applet payload path must be specified (-o)")
	}

	if len(taSigPath) == 0 {
		return errors.New("trusted applet signature path must be specified (-O)")
	}

	taELF, err := os.ReadFile(taELFPath)

	if err != nil {
		return
	}

	taSig, err := os.ReadFile(taSigPath)

	if err != nil {
		return
	}

	chunkSize := maxChunkSize
	totalSize := len(taELF)

	total := totalSize / chunkSize
	seq := 0

	if total == 0 {
		total = 1
	} else if totalSize%chunkSize != 0 {
		total += 1
	}

	if len(taSig) > maxChunkSize {
		return errors.New("signature size exceeds maximum update chunk size")
	}

	log.Printf("sending trusted applet signature to armored witness")

	if err = d.sendUpdateHeader(taSig, total); err != nil {
		return
	}

	bar := pb.StartNew(totalSize)
	bar.SetWriter(os.Stdout)
	bar.Set(pb.Bytes, true)

	start := time.Now()

	defer func(start time.Time) {
		log.Printf("sent %d bytes in %v", totalSize, time.Since(start))
	}(start)
	defer bar.Finish()

	log.Printf("sending trusted applet payload to armored witness")

	for i := 0; i < totalSize; i += chunkSize {
		seq += 1

		if i+chunkSize > totalSize {
			chunkSize = totalSize - i
		}

		if err = d.sendUpdateChunk(taELF[i:i+chunkSize], seq, total); err != nil {
			return
		}

		bar.Add(chunkSize)
	}

	return
}

func (d Device) cfg(dhcp bool, ip string, mask string, gw string, dns string, ntp string) error {
	if len(ip) == 0 || len(gw) == 0 || len(dns) == 0 {
		return errors.New("trusted applet IP, gatewy and DNS addresses must all be specified for configuration change (flags: -a -g -r)")
	}

	if addr := net.ParseIP(ip); addr == nil {
		return errors.New("IP address is invalid")
	}

	if addr := net.ParseIP(mask); addr == nil {
		return errors.New("Netmask is invalid")
	}

	if addr := net.ParseIP(gw); addr == nil {
		return errors.New("Gateway address is invalid")
	}

	if _, _, err := net.SplitHostPort(dns); err != nil {
		return fmt.Errorf("DNS address is invalid: %v", err)
	}

	c := &api.Configuration{
		DHCP:      dhcp,
		IP:        ip,
		Netmask:   mask,
		Gateway:   gw,
		Resolver:  dns,
		NTPServer: ntp,
	}

	log.Printf("sending configuration update to armored witness")

	buf, err := d.u2f.Command(api.U2FHID_ARMORY_CFG, c.Bytes())

	if err != nil {
		return err
	}

	res := &api.Response{}

	if err = proto.Unmarshal(buf, res); err != nil {
		return err
	}

	if res.Error != api.ErrorCode_NONE {
		return fmt.Errorf("%+v", res)
	}

	return nil
}
