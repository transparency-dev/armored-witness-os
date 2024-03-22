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
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"

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

func (d Device) getLogMessages(cmd byte) (string, error) {
	r, w := io.Pipe()
	defer r.Close()

	errC := make(chan error, 1)
	// Kick off a goroutine to fetch chunks of log and pipe it into the
	// decompressor.
	go func() {
		// Signal that there's no more compressed data.
		defer w.Close()
		defer close(errC)

		req := &api.LogMessagesRequest{}
		rsp := &api.LogMessagesResponse{More: true}
		for rsp.More {
			rb, _ := proto.Marshal(req)
			buf, err := d.u2f.Command(cmd, rb)
			if err != nil {
				errC <- err
				return
			}
			if err := proto.Unmarshal(buf, rsp); err != nil {
				errC <- err
				return
			}
			w.Write(rsp.GetPayload())
			req.Continue = true

			// Don't overload the HID endpoint
			time.Sleep(20 * time.Millisecond)
		}
	}()

	gz, err := gzip.NewReader(r)
	if err != nil {
		log.Printf("Failed to create gzip reader: %v", err)
		return "", err
	}
	gz.Close()

	// Grab the decompressed logs, and return
	s, err := io.ReadAll(gz)
	if err != nil {
		return "", err
	}

	return string(s), <-errC
}

func (d Device) consoleLogs() (string, error) {
	return d.getLogMessages(api.U2FHID_ARMORY_CONSOLE_LOGS)
}

func (d Device) crashLogs() (string, error) {
	return d.getLogMessages(api.U2FHID_ARMORY_CRASH_LOGS)
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
