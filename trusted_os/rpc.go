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
	"crypto/aes"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"

	usbarmory "github.com/usbarmory/tamago/board/usbarmory/mk2"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"
	"github.com/usbarmory/tamago/soc/nxp/usdhc"

	"github.com/usbarmory/GoTEE/monitor"

	"github.com/transparency-dev/armored-witness-os/api"
	"github.com/transparency-dev/armored-witness-os/api/rpc"
	"github.com/transparency-dev/armored-witness-os/internal/hab"
)

// RPC represents an example receiver for user/system mode RPC over system
// calls.
type RPC struct {
	RPMB        *RPMB
	Ctx         *monitor.ExecCtx
	Cfg         []byte
	Diversifier [32]byte
}

// Version receives the Trusted Applet version for verification.
func (r *RPC) Version(version string, _ *bool) error {
	// TODO: disable for now
	return nil

	log.Printf("SM applet version verification (%s)", version)

	if err := r.RPMB.checkVersion(taVersionSector, version); err != nil {
		log.Printf("SM stopping applet, %v", err)
		r.Ctx.Stop()
	}

	return nil
}

// Config receives network configuration from the Trusted Applet. It also
// returns the previous configuration to allow the Trusted Applet to evaluate
// whether any updates from the control interface must be applied.
func (r *RPC) Config(current []byte, previous *[]byte) error {
	if len(r.Cfg) == 0 {
		defer func() {
			log.Println("SM starting network")
			startNetworking()
		}()
	} else if previous != nil {
		*previous = r.Cfg
	}

	r.Cfg = current

	return nil
}

func (r *RPC) Address(mac net.HardwareAddr, _ *bool) error {
	Network.SetMAC(mac)
	return nil
}

func (r *RPC) Register(handler rpc.Handler, _ *bool) error {
	if handler.G == 0 || handler.P == 0 {
		return errors.New("invalid argument")
	}

	log.Printf("SM registering applet event handler g:%#x p:%#x", handler.G, handler.P)
	appletHandlerG = handler.G
	appletHandlerP = handler.P

	return nil
}

// Status returns Trusted OS status information.
func (r *RPC) Status(_ any, status *api.Status) error {
	if status == nil {
		return errors.New("invalid argument")
	}

	s := getStatus()
	*status = *s

	return nil
}

// LED receives a LED state request.
func (r *RPC) LED(led rpc.LEDStatus, _ *bool) error {
	if strings.EqualFold(led.Name, "white") {
		return errors.New("LED is secure only")
	}

	return usbarmory.LED(led.Name, led.On)
}

// CardInfo returns the storage media information.
func (r *RPC) CardInfo(_ any, info *usdhc.CardInfo) error {
	*info = r.RPMB.Storage.Info()
	return nil
}

// WriteBlocks transfers full blocks of data to the storage media.
func (r *RPC) WriteBlocks(xfer rpc.WriteBlocks, _ *bool) error {
	return r.RPMB.Storage.WriteBlocks(xfer.LBA, xfer.Data)
}

// Read transfers data from the storage media.
func (r *RPC) Read(xfer rpc.Read, out *[]byte) (err error) {
	*out, err = r.RPMB.Storage.Read(xfer.Offset, xfer.Size)
	return
}

// WriteRPMB performs an authenticated data transfer to the card RPMB partition
// sector allocated to the Trusted Applet. The input buffer can contain up to
// 256 bytes of data, n can be passed to retrieve the partition write counter.
func (r *RPC) WriteRPMB(buf []byte, n *uint32) (err error) {
	return r.RPMB.transfer(taUserSector, buf, n, true)
}

// ReadRPMB performs an authenticated data transfer from the card RPMB
// partition sector allocated to the Trusted Applet. The input buffer can
// contain up to 256 bytes of data, n can be set to retrieve the partition
// write counter.
func (r *RPC) ReadRPMB(buf []byte, n *uint32) error {
	return r.RPMB.transfer(taUserSector, buf, n, false)
}

// DeriveKey derives a hardware unique key in a manner equivalent to PKCS#11
// C_DeriveKey with CKM_AES_CBC_ENCRYPT_DATA.
//
// The diversifier is AES-CBC encrypted using the internal OTPMK key.
func (r *RPC) DeriveKey(diversifier []byte, key *[]byte) (err error) {
	if !imx6ul.SNVS.Available() {
		return errors.New("SNVS not available")
	}

	if len(diversifier) != aes.BlockSize {
		return fmt.Errorf("diversifier must be exactly %d long", aes.BlockSize)
	}

	switch {
	case imx6ul.CAAM != nil:
		div := sha256.Sum256(append(r.Diversifier[:], diversifier...))
		err = imx6ul.CAAM.DeriveKey(div[:], *key)
	case imx6ul.DCP != nil:
		*key, err = imx6ul.DCP.DeriveKey(r.Diversifier[:], diversifier, -1)
	default:
		err = errors.New("unsupported hardware")
	}

	return
}

// HAB activates secure boot.
func (r *RPC) HAB(srk []byte, _ *bool) error {
	log.Printf("SM activating HAB")
	return hab.Activate(srk)
}

// Reboot resets the system.
func (r *RPC) Reboot(_ *any, _ *bool) error {
	log.Printf("SM rebooting")
	usbarmory.Reset()

	return nil
}
