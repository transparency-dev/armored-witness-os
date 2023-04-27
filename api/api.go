// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package api

import (
	"bytes"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/gsora/fidati/u2fhid"
)

const (
	// http://pid.codes/1209/2702/
	VendorID  = 0x1209
	ProductID = 0x2702

	HIDUsagePage = 0xff00

	// Maximum Message size according to U2F HID standard (see formula in
	// [FIDO U2F // HID Protocol Specification, 2.4]).
	MaxMessageSize = 7609
)

// U2FHID vendor specific commands
const (
	// Status
	U2FHID_ARMORY_INF = iota + u2fhid.VendorCommandFirst
	// Trusted Applet configuration
	U2FHID_ARMORY_CFG
	// Trusted Applet update
	U2FHID_ARMORY_OTA
)

var emptyResponse []byte

// ErrorResponse converts an error in an API Message.
func ErrorResponse(err error) (res []byte) {
	msg := &Response{
		Error:   ErrorCode_GENERIC_ERROR,
		Payload: []byte(err.Error()),
	}

	res, _ = proto.Marshal(msg)

	return
}

// EmptyResponse for when no relevant data is available.
func EmptyResponse() []byte {
	if len(emptyResponse) == 0 {
		emptyResponse, _ = proto.Marshal(&Response{})
	}

	return emptyResponse
}

// Bytes serializes an API message.
func (p *Response) Bytes() (buf []byte) {
	buf, _ = proto.Marshal(p)
	return
}

// Bytes serializes an API message.
func (p *Configuration) Bytes() (buf []byte) {
	buf, _ = proto.Marshal(p)
	return
}

// Bytes serializes an API message.
func (p *AppletUpdate) Bytes() (buf []byte) {
	buf, _ = proto.Marshal(p)
	return
}

// Print returns the Trusted OS status in textual format.
func (p *Status) Print() string {
	var status bytes.Buffer

	status.WriteString("----------------------------------------------------------- Trusted OS ----\n")
	status.WriteString(fmt.Sprintf("Serial number ..........: %s\n", p.Serial))
	status.WriteString(fmt.Sprintf("Secure Boot ............: %v\n", p.HAB))
	status.WriteString(fmt.Sprintf("Revision ...............: %s\n", p.Revision))
	status.WriteString(fmt.Sprintf("Build ..................: %s\n", p.Build))
	status.WriteString(fmt.Sprintf("Version ................: %d (%s)\n", p.Version, time.Unix(int64(p.Version), 0)))
	status.WriteString(fmt.Sprintf("Runtime ................: %s\n", p.Runtime))
	status.WriteString(fmt.Sprintf("Link ...................: %v", p.Link))

	return status.String()
}
