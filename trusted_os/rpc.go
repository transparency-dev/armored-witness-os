// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package main

import (
	"crypto/aes"
	"errors"
	"fmt"
	"log"
	"strings"

	usbarmory "github.com/usbarmory/tamago/board/usbarmory/mk2"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"
	"github.com/usbarmory/tamago/soc/nxp/usdhc"

	"github.com/usbarmory/GoTEE/monitor"

	"github.com/usbarmory/armory-witness/api"
	"github.com/usbarmory/armory-witness/internal/hab"
	"github.com/usbarmory/armory-witness/internal/rpc"
)

// RPC represents an example receiver for user/system mode RPC over system
// calls.
type RPC struct {
	RPMB        *RPMB
	Ctx         *monitor.ExecCtx
	Cfg         *api.Configuration
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
func (r *RPC) Config(current api.Configuration, previous *api.Configuration) error {
	if r.Cfg == nil {
		defer func() {
			log.Printf("SM starting network MAC:%s", r.Cfg.MAC)
			startNetworking(r.Cfg.MAC)
		}()
	} else if previous != nil {
		*previous = *r.Cfg
	}

	r.Cfg = &current

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

	*key, err = imx6ul.DCP.DeriveKey(r.Diversifier[:], diversifier, -1)

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