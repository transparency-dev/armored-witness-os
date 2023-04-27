// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build !tamago
// +build !tamago

package main

import (
	"log"

	flynn_hid "github.com/flynn/hid"
	"github.com/flynn/u2f/u2fhid"

	"github.com/usbarmory/armory-witness/api"
)

func detectU2F() (dev *u2fhid.Device, err error) {
	devices, err := flynn_hid.Devices()

	if err != nil {
		return nil, err
	}

	for _, d := range devices {
		if d.UsagePage == api.HIDUsagePage &&
			d.VendorID == api.VendorID &&
			d.ProductID == api.ProductID {

			dev, err = u2fhid.Open(d)

			if err != nil {
				log.Fatal(err)
			}

			return
		}
	}

	return
}
