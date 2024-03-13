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
	"encoding/binary"
	"unicode/utf16"

	"github.com/gsora/fidati"
	"github.com/gsora/fidati/u2fhid"

	"github.com/usbarmory/tamago/soc/nxp/usb"

	"github.com/transparency-dev/armored-witness-os/api"
)

func configureDevice(device *usb.Device, serial string) (err error) {
	// Supported Language Code Zero: English
	device.SetLanguageCodes([]uint16{0x0409})

	// device descriptor
	device.Descriptor = &usb.DeviceDescriptor{}
	device.Descriptor.SetDefaults()

	// p5, Table 1-1. Device Descriptor Using Class Codes for IAD,
	// USB Interface Association Descriptor Device Class Code and Use Model.
	device.Descriptor.DeviceClass = 0xef
	device.Descriptor.DeviceSubClass = 0x02
	device.Descriptor.DeviceProtocol = 0x01

	device.Descriptor.VendorId = api.VendorID
	device.Descriptor.ProductId = api.ProductID

	device.Descriptor.Device = 0x0001

	iManufacturer, _ := device.AddString(`WithSecure Foundry`)
	device.Descriptor.Manufacturer = iManufacturer

	iProduct, _ := device.AddString(`Armory Witness`)
	device.Descriptor.Product = iProduct

	iSerial, _ := device.AddString(serial)
	device.Descriptor.SerialNumber = iSerial

	conf := &usb.ConfigurationDescriptor{}
	conf.SetDefaults()

	if err = device.AddConfiguration(conf); err != nil {
		return
	}

	// device qualifier
	device.Qualifier = &usb.DeviceQualifierDescriptor{}
	device.Qualifier.SetDefaults()
	device.Qualifier.NumConfigurations = uint8(len(device.Configurations))

	return
}

func configureHID(device *usb.Device, ctl *controlInterface) (err error) {
	// Windows blocks non-administrative access to FIDO devices, for this
	// reason we override its standard usage page identifier with a custom
	// one, which grants access.
	binary.LittleEndian.PutUint16(u2fhid.DefaultReport[1:], api.HIDUsagePage)

	hid, err := u2fhid.NewHandler(ctl)

	if err != nil {
		return
	}

	if err = fidati.ConfigureUSB(device.Configurations[0], device, hid); err != nil {
		return
	}

	numInterfaces := len(device.Configurations[0].Interfaces)

	// override interface name
	var buf []byte

	r := []rune("U2FHID interface descriptor")
	u := utf16.Encode([]rune(r))

	for i := 0; i < len(u); i++ {
		buf = append(buf, byte(u[i]&0xff))
		buf = append(buf, byte(u[i]>>8))
	}

	interfaceName := device.Configurations[0].Interfaces[numInterfaces-1].Interface
	copy(device.Strings[interfaceName][2:], buf)

	// avoid conflict with Serial over USB
	device.Configurations[0].Interfaces[numInterfaces-1].Endpoints[usb.OUT].EndpointAddress = 0x03
	device.Configurations[0].Interfaces[numInterfaces-1].Endpoints[usb.IN].EndpointAddress = 0x83

	if err = hid.AddMapping(api.U2FHID_ARMORY_INF, ctl.Status); err != nil {
		return
	}

	if err = hid.AddMapping(api.U2FHID_ARMORY_CFG, ctl.Config); err != nil {
		return
	}

	if err = hid.AddMapping(api.U2FHID_ARMORY_OTA, ctl.Update); err != nil {
		return
	}

	if err = hid.AddMapping(api.U2FHID_ARMORY_HAB, ctl.HAB); err != nil {
		return
	}

	if err = hid.AddMapping(api.U2FHID_ARMORY_CONSOLE_LOGS, ctl.ConsoleLogs); err != nil {
		return
	}

	return
}
