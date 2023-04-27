// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build !tamago
// +build !tamago

package main

import (
	"errors"
	"flag"
	"log"
	"os"

	"github.com/flynn/u2f/u2fhid"

	"github.com/usbarmory/armory-witness/api"
)

type Config struct {
	dev *u2fhid.Device

	status bool

	otaELF string
	otaSig string

	ip   string
	gw   string
	mask string
	dns  string
}

var conf *Config

func init() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)

	conf = &Config{}

	flag.BoolVar(&conf.status, "s", false, "get witness status")
	flag.StringVar(&conf.otaELF, "o", "", "trusted applet payload")
	flag.StringVar(&conf.otaSig, "O", "", "trusted applet signature")
	flag.StringVar(&conf.ip, "a", "10.0.0.1", "set IP address")
	flag.StringVar(&conf.mask, "m", "255.255.255.0", "set Netmask")
	flag.StringVar(&conf.gw, "g", "10.0.0.2", "set Gateway")
	flag.StringVar(&conf.dns, "r", "8.8.8.8", "set DNS resolver")
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
	case len(conf.ip) > 0 || len(conf.gw) > 0 || len(conf.dns) > 0:
		err = cfg(conf.ip, conf.mask, conf.gw, conf.dns)
	}
}
