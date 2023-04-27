// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build debug
// +build debug

package main

import (
	"bytes"
	_ "unsafe"

	usbarmory "github.com/usbarmory/tamago/board/usbarmory/mk2"
	"github.com/usbarmory/tamago/soc/nxp/usb"
)

var serialBuf bytes.Buffer

//go:linkname printk runtime.printk
func printk(c byte) {
	usbarmory.UART2.Tx(c)
	serialBuf.WriteByte(c)
}

func serialControl(_ []byte, lastErr error) (in []byte, err error) {
	return
}

func serialTx(_ []byte, lastErr error) (in []byte, err error) {
	in = serialBuf.Bytes()
	serialBuf.Reset()
	return
}

func serialRx(out []byte, lastErr error) (_ []byte, err error) {
	for _, c := range out {
		usbarmory.UART2.Tx(c)
	}

	return
}

func addControlInterface(device *usb.Device) (iface *usb.InterfaceDescriptor) {
	iface = &usb.InterfaceDescriptor{}
	iface.SetDefaults()

	iface.NumEndpoints = 1
	iface.InterfaceClass = usb.COMMUNICATION_INTERFACE_CLASS
	iface.InterfaceSubClass = usb.ACM_SUBCLASS
	iface.InterfaceProtocol = usb.AT_COMMAND_PROTOCOL

	iInterface, _ := device.AddString(`CDC Virtual Serial Port`)
	iface.Interface = iInterface

	// Set IAD to be inserted before first interface, to support multiple
	// functions in this same configuration.
	iface.IAD = &usb.InterfaceAssociationDescriptor{}
	iface.IAD.SetDefaults()
	// alternate settings do not count
	iface.IAD.InterfaceCount = 1
	iface.IAD.FunctionClass = iface.InterfaceClass
	iface.IAD.FunctionSubClass = iface.InterfaceSubClass

	iFunction, _ := device.AddString(`CDC`)
	iface.IAD.Function = iFunction

	header := &usb.CDCHeaderDescriptor{}
	header.SetDefaults()

	iface.ClassDescriptors = append(iface.ClassDescriptors, header.Bytes())

	acm := &usb.CDCAbstractControlManagementDescriptor{}
	acm.SetDefaults()

	iface.ClassDescriptors = append(iface.ClassDescriptors, acm.Bytes())

	union := &usb.CDCUnionDescriptor{}
	union.SetDefaults()

	numInterfaces := 1 + len(device.Configurations[0].Interfaces)
	union.MasterInterface = uint8(numInterfaces - 1)
	union.SlaveInterface0 = uint8(numInterfaces)

	iface.ClassDescriptors = append(iface.ClassDescriptors, union.Bytes())

	cm := &usb.CDCCallManagementDescriptor{}
	cm.SetDefaults()

	iface.ClassDescriptors = append(iface.ClassDescriptors, cm.Bytes())

	ep1IN := &usb.EndpointDescriptor{}
	ep1IN.SetDefaults()
	ep1IN.EndpointAddress = 0x81
	ep1IN.Attributes = 3
	ep1IN.MaxPacketSize = 64
	ep1IN.Interval = 11
	ep1IN.Function = serialControl

	iface.Endpoints = append(iface.Endpoints, ep1IN)

	device.Configurations[0].AddInterface(iface)

	return
}

func addDataInterfaces(device *usb.Device) {
	iface1 := &usb.InterfaceDescriptor{}
	iface1.SetDefaults()

	iface1.NumEndpoints = 2
	iface1.InterfaceClass = usb.DATA_INTERFACE_CLASS

	iInterface, _ := device.AddString(`CDC Data`)
	iface1.Interface = iInterface

	ep2IN := &usb.EndpointDescriptor{}
	ep2IN.SetDefaults()
	ep2IN.EndpointAddress = 0x82
	ep2IN.Attributes = 2
	ep2IN.MaxPacketSize = 512
	ep2IN.Function = serialTx

	iface1.Endpoints = append(iface1.Endpoints, ep2IN)

	ep2OUT := &usb.EndpointDescriptor{}
	ep2OUT.SetDefaults()
	ep2OUT.EndpointAddress = 0x02
	ep2IN.MaxPacketSize = 512
	ep2OUT.Attributes = 2
	ep2OUT.Function = serialRx

	iface1.Endpoints = append(iface1.Endpoints, ep2OUT)

	device.Configurations[0].AddInterface(iface1)

	return
}

func configureUART(device *usb.Device) (_ error) {
	addControlInterface(device)
	addDataInterfaces(device)

	return
}
