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
	"flag"
	"log"
	"os"

	"github.com/flynn/u2f/u2fhid"

	"github.com/transparency-dev/armored-witness-os/api"
)

type Config struct {
	dev *u2fhid.Device

	status bool

	otaELF string
	otaSig string

	dhcp bool
	ip   string
	gw   string
	mask string
	dns  string
	ntp  string
}

var conf *Config

func init() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)

	conf = &Config{}

	flag.BoolVar(&conf.status, "s", false, "get witness status")
	flag.StringVar(&conf.otaELF, "o", "", "trusted applet payload")
	flag.StringVar(&conf.otaSig, "O", "", "trusted applet signature")
	flag.BoolVar(&conf.dhcp, "A", true, "enable DHCP")
	flag.StringVar(&conf.ip, "a", "10.0.0.1", "set IP address")
	flag.StringVar(&conf.mask, "m", "255.255.255.0", "set Netmask")
	flag.StringVar(&conf.gw, "g", "10.0.0.2", "set Gateway")
	flag.StringVar(&conf.dns, "r", "8.8.8.8:53", "set DNS resolver")
	flag.StringVar(&conf.ntp, "n", "time.google.com", "set NTP server")
}

func detect() (err error) {
	if conf.dev != nil {
		return
	}

	conf.dev, err = detectU2F()

	if err != nil {
		return
	}

	if conf.dev == nil {
		return errors.New("no device found")
	}

	return
}

func main() {
	var err error

	defer func() {
		if flag.NFlag() == 0 {
			flag.PrintDefaults()
		}

		if err != nil {
			log.Fatalf("fatal error, %s", err)
		}
	}()

	flag.Parse()

	switch {
	case conf.status:
		var s *api.Status

		s, err = status()

		if err == nil {
			log.Print(s.Print())
		}
	case len(conf.otaELF) > 0 || len(conf.otaSig) > 0:
		err = ota(conf.otaELF, conf.otaSig)
	case conf.dhcp || len(conf.ip) > 0 || len(conf.gw) > 0 || len(conf.dns) > 0 || len(conf.ntp) > 0:
		err = cfg(conf.dhcp, conf.ip, conf.mask, conf.gw, conf.dns, conf.ntp)
	}
}
