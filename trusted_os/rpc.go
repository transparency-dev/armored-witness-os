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
	Storage     Card
	Ctx         *monitor.ExecCtx
	Cfg         []byte
	Diversifier [32]byte
}

// Version receives the Trusted Applet version for verification.
func (r *RPC) Version(version string, _ *bool) error {
	if !imx6ul.Native || !imx6ul.SNVS.Available() {
		log.Print("SM skipping applet version verification")
		return nil
	}

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
			netStart()
		}()
	} else if previous != nil {
		*previous = r.Cfg
	}

	r.Cfg = current

	return nil
}

// Address sets the Ethernet MAC address on LAN models.
func (r *RPC) Address(mac net.HardwareAddr, _ *bool) error {
	if LAN != nil {
		LAN.SetMAC(mac)
	}

	return nil
}

func isAppletMemory(addr uint32) bool {
	return addr >= appletStart && addr < appletStart + appletSize
}

// Register registers the Trusted Applet event handler.
func (r *RPC) Register(handler rpc.Handler, _ *bool) error {
	if !isAppletMemory(handler.G) || !isAppletMemory(handler.P) {
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

// SetWitnessStatus informs the OS of the witness Trusted Applet's status.
func (r *RPC) SetWitnessStatus(status rpc.WitnessStatus, _ *bool) error {
	witnessStatus = &status
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
	if r.Storage == nil {
		return errors.New("missing Storage")
	}

	*info = r.Storage.Info()

	return nil
}

// WriteBlocks transfers full blocks of data to the storage media.
func (r *RPC) WriteBlocks(xfer rpc.WriteBlocks, _ *bool) error {
	if r.Storage == nil {
		return errors.New("missing Storage")
	}

	return r.Storage.WriteBlocks(xfer.LBA, xfer.Data)
}

// Read transfers data from the storage media.
func (r *RPC) Read(xfer rpc.Read, out *[]byte) (err error) {
	if r.Storage == nil {
		return errors.New("missing Storage")
	}

	*out, err = r.Storage.Read(xfer.Offset, xfer.Size)

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
func (r *RPC) DeriveKey(diversifier [aes.BlockSize]byte, key *[sha256.Size]byte) (err error) {
	switch {
	case imx6ul.Native && !debug && !imx6ul.SNVS.Available():
		return errors.New("Weird - SNVS not available but we're not in debug?!")
	case !imx6ul.Native && debug:
		// we support emulation only on debug builds, use input buffer as dummy key
		return
	case !imx6ul.Native && !debug:
		return errors.New("Weird - under emulation but we're not in debug?!")
	}

	switch {
	case imx6ul.CAAM != nil:
		div := sha256.Sum256(append(r.Diversifier[:], diversifier[:]...))
		err = imx6ul.CAAM.DeriveKey(div[:], key[:])
	case imx6ul.DCP != nil:
		var k []byte
		k, err = imx6ul.DCP.DeriveKey(r.Diversifier[:], diversifier[:], -1)
		copy(key[:], k)
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

// GetInstalledVersions returns the semantic versions of the OS and Applet
// installed on this device. These will be the same versions that are
// currently running.
func (r *RPC) GetInstalledVersions(_ *any, v *rpc.InstalledVersions) error {
	if v != nil {
		v.Applet = loadedAppletVersion
		v.OS = osVersion
	}
	return nil
}

// InstallOS updates the OS to the version contained in the firmware bundle.
//
// This RPC supports sending the (potentially large) firmware image either:
// - In one RPC call, or
// - Spread over multiple RPC calls, breaking the firmware image it into multiple "chunks" of arbitrary size
//
// For a given install attempt:
//   - An RPC call with the Sequence field set to zero indicates a fresh attempt to install firmware.
//   - If firmware is being sent in chunks via multiple RPC calls, each subsequent RPC call should:
//     1. increment the Sequence field by 1 each time.
//     2. Pass a chunk of firmware image which is contiguous with the previous chunk.
//   - An RPC call with the Proof set to a non-zero value indicates that all firmware chunks have been sent.
//     This will cause the firmware update to be finalised, and if successful, this RPC will not
//     return and the device will reboot.
func (r *RPC) InstallOS(b *rpc.FirmwareUpdate, _ *bool) error {
	if b.Sequence == 0 {
		// Dump previous partial attempts
		osFirmwareBuffer = make([]byte, 0, len(b.Image))
	}
	// Extend our firmware buffer
	osFirmwareBuffer = append(osFirmwareBuffer, b.Image...)
	b.Image = nil

	// Return early if we're don't yet have the full image.
	if len(b.Proof.Checkpoint) == 0 {
		return nil
	}

	if err := updateOS(r.Storage, osFirmwareBuffer, b.Proof); err != nil {
		return err
	}
	r.Ctx.Stop()
	<-r.Ctx.Done()

	return r.Reboot(nil, nil)
}

var osFirmwareBuffer []byte

// InstallApplet updates the Applet to the version contained in the firmware bundle.
// This RPC supports sending the (potentially large) firmware image either:
// - In one RPC call, or
// - Spread over multiple RPC calls, breaking the firmware image it into multiple "chunks" of arbitrary size
//
// For a given install attempt:
//   - An RPC call with the Sequence field set to zero indicates a fresh attempt to install firmware.
//   - If firmware is being sent in chunks via multiple RPC calls, each subsequent RPC call should:
//     1. increment the Sequence field by 1 each time.
//     2. Pass a chunk of firmware image which is contiguous with the previous chunk.
//   - An RPC call with the Proof set to a non-zero value indicates that all firmware chunks have been sent.
//     This will cause the firmware update to be finalised, and if successful, this RPC will not
//     return and the device will reboot.
func (r *RPC) InstallApplet(b *rpc.FirmwareUpdate, _ *bool) error {
	if b.Sequence == 0 {
		// Dump previous partial attempts
		appletFirmwareBuffer = make([]byte, 0, len(b.Image))
	}
	// Extend our firmware buffer
	appletFirmwareBuffer = append(appletFirmwareBuffer, b.Image...)
	b.Image = nil

	// Return early if we're don't yet have the full image.
	if len(b.Proof.Checkpoint) == 0 {
		return nil
	}
	if err := updateApplet(r.Storage, appletFirmwareBuffer, b.Proof); err != nil {
		return err
	}
	r.Ctx.Stop()
	// This must be done in a go-routine because the ExecCtx.Stop() above can only be
	// actioned once the RPC has returned.
	go func() {
		<-r.Ctx.Done()
		r.Reboot(nil, nil)
	}()

	return r.Reboot(nil, nil)
}

var appletFirmwareBuffer []byte

// Reboot resets the system.
func (r *RPC) Reboot(_ *any, _ *bool) error {
	log.Printf("SM rebooting")
	usbarmory.Reset()

	return nil
}

// ConsoleLog returns the console log for the current running OS & applet.
func (r *RPC) ConsoleLog(_ *any, ret *[]byte) error {
	if ret == nil {
		return errors.New("nil buffer passed")
	}

	*ret = getConsoleLogs()
	return nil
}

// CrashLog returns the console log stored by the OS after the last
// applet exit, if any.
func (r *RPC) CrashLog(_ *any, ret *[]byte) error {
	if ret == nil {
		return errors.New("nil buffer passed")
	}

	l, err := retrieveLastCrashLog(r.Storage)
	if err != nil {
		return err
	}
	*ret = l
	return nil
}
