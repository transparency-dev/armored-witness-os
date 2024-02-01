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
	status.WriteString(fmt.Sprintf("Serial number ..............: %s\n", p.Serial))
	status.WriteString(fmt.Sprintf("Secure Boot ................: %v\n", p.HAB))
	status.WriteString(fmt.Sprintf("Revision ...................: %s\n", p.Revision))
	status.WriteString(fmt.Sprintf("Version ....................: %d (%s)\n", p.Version, time.Unix(int64(p.Version), 0)))
	status.WriteString(fmt.Sprintf("Runtime ....................: %s\n", p.Runtime))
	status.WriteString(fmt.Sprintf("Link .......................: %v\n", p.Link))
	status.WriteString(fmt.Sprintf("IdentityCounter ............: %d", p.IdentityCounter))
	if p.Witness != nil {
		status.WriteString(fmt.Sprintf("Witness/Identity ...........: %v\n", p.Witness.Identity))
		status.WriteString(fmt.Sprintf("Witness/IP .................: %v", p.Witness.IP))
	} else {
		status.WriteString(fmt.Sprint("Witness ....................: <no status>"))
	}

	return status.String()
}
