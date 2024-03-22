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
)

const warning = `
████████████████████████████████████████████████████████████████████████████████

                                **  WARNING  **

Enabling NXP HABv4 secure boot is an irreversible action that permanently fuses
verification key hashes on the device.

Any errors in the process or loss of the signing PKI will result in a bricked
device incapable of executing unsigned code. This is a security feature, not a
bug.

The use of this tool is therefore **at your own risk**.

████████████████████████████████████████████████████████████████████████████████
`

var (
	// knownSRKHashes maps known SRK hash values to the release environment they came from.
	// These values MUST NOT be changed unless you really know what you're doing!
	knownSRKHashes = map[string]string{
		// CI: From https://github.com/transparency-dev/armored-witness-os/blob/main/release/cloudbuild_ci.yaml#L188-L191C18
		"b8ba457320663bf006accd3c57e06720e63b21ce5351cb91b4650690bb08d85a": "CI",
	}
)

type Config struct {
	devs []Device

	hidPath string

	status      bool
	consoleLogs bool
	crashLogs   bool
	hab         bool

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

	flag.StringVar(&conf.hidPath, "d", "", "HID path of witness device to act upon (use -s to list devices)")
	flag.BoolVar(&conf.status, "s", false, "get witness status")
	flag.BoolVar(&conf.consoleLogs, "l", false, "get witness console/debug logs")
	flag.BoolVar(&conf.crashLogs, "L", false, "get crash logs from most recent witness failure")
	flag.BoolVar(&conf.hab, "H", false, "set HAB fuses")
	flag.BoolVar(&conf.dhcp, "A", true, "enable DHCP")
	flag.StringVar(&conf.ip, "a", "10.0.0.1", "set IP address")
	flag.StringVar(&conf.mask, "m", "255.255.255.0", "set Netmask")
	flag.StringVar(&conf.gw, "g", "10.0.0.2", "set Gateway")
	flag.StringVar(&conf.dns, "r", "8.8.8.8:53", "set DNS resolver")
	flag.StringVar(&conf.ntp, "n", "time.google.com", "set NTP server")
}

func (c *Config) detect() error {
	devs, err := detect()
	if err != nil {
		return err
	}
	if len(devs) == 0 {
		return errors.New("no devices found")
	}
	// If the user specified a device in particular, limit to just that one:
	if len(c.hidPath) > 0 {
		for _, d := range devs {
			if d.usb.Path == conf.hidPath {
				c.devs = []Device{d}
				return nil
			}
		}

	}
	c.devs = devs
	return nil
}

func main() {
	defer func() {
		if flag.NFlag() == 0 {
			flag.PrintDefaults()
		}
	}()

	flag.Parse()

	if err := conf.detect(); err != nil {
		log.Fatalf("detect(): %v", err)
	}

	switch {
	case conf.hab:
		s, err := conf.devs[0].status()
		if err != nil {
			log.Printf("Failed to get status on %q: %c", conf.devs[0].usb.Path, err)
		}
		log.Print(warning)
		log.Print()

		env, ok := knownSRKHashes[s.SRKHash]
		if !ok {
			log.Printf("WARNING: SRK hash '%s' is UNKNOWN!", s.SRKHash)
		} else {
			log.Printf("Will fuse to %s release environment (SRK Hash: %s)", env, s.SRKHash)
		}

		if confirm("Proceed?") {
			log.Print("Asking device to fuse itself...")
			if err := conf.devs[0].hab(); err != nil {
				log.Fatalf("%v", err)
			}
		} else {
			if err := errors.New("User cancelled"); err != nil {
				log.Fatalf("%v", err)
			}
		}
	case conf.status:
		for _, d := range conf.devs {
			log.Printf("👁️‍🗨️ @ %s", d.usb.Path)
			s, err := d.status()
			if err != nil {
				log.Printf("Failed to get status on %q: %v", d.usb.Path, err)
			}
			log.Printf("%s\n\n", s.Print())
		}
	case conf.consoleLogs:
		for _, d := range conf.devs {
			log.Printf("👁️‍🗨️ @ %s", d.usb.Path)
			s, err := d.consoleLogs()
			if err != nil {
				log.Printf("Failed to get console logs on %q: %v", d.usb.Path, err)
			}
			log.Printf("%s\n\n", s)
		}
	case conf.crashLogs:
		for _, d := range conf.devs {
			log.Printf("👁️‍🗨️ @ %s", d.usb.Path)
			s, err := d.crashLogs()
			if err != nil {
				log.Printf("Failed to get crash logs on %q: %v", d.usb.Path, err)
			}
			log.Printf("%s\n\n", s)
		}
	case conf.dhcp || len(conf.ip) > 0 || len(conf.gw) > 0 || len(conf.dns) > 0 || len(conf.ntp) > 0:
		if len(conf.devs) != 1 {
			log.Fatal("Please specify which device to configure using -d")
		}
		if err := conf.devs[0].cfg(conf.dhcp, conf.ip, conf.mask, conf.gw, conf.dns, conf.ntp); err != nil {
			log.Fatalf("%v", err)
		}
	}
}
