// Copyright 2023 The Armored Witness Applet authors. All Rights Reserved.
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

// Package rpc provides an RPC-based update client.
package rpc

import (
	"github.com/coreos/go-semver/semver"
	"github.com/transparency-dev/armored-witness-boot/config"
	"github.com/transparency-dev/armored-witness-common/release/firmware"
	"github.com/transparency-dev/armored-witness-os/api/rpc"
	"github.com/usbarmory/GoTEE/syscall"
	"k8s.io/klog/v2"
)

const fwUpdateChunkSize = 1 << 20

// fakeAppletVersion is what we return when the updater asks
// what version of the applet is installed.
//
// This is a placeholder until the updater is fixed to not try to update
// applets.
//
// The latest (and final) release from the armored-witness-applet
// repo is v0.3.5, so this version must be higher.
var fakeAppletVersion = *semver.New("0.3.999")

// Client is an implementation of the Local interface which uses RPCs to the TrustedOS
// to perform the updates.
type Client struct {
}

// GetInstalledVersions returns the semantic versions of the OS
// installed on this device. These will be the same versions that are
// currently running.
func (r Client) GetInstalledVersions() (os, applet semver.Version, err error) {
	iv := &rpc.InstalledVersions{}
	err = syscall.Call("RPC.GetInstalledVersions", nil, iv)
	return iv.OS, fakeAppletVersion, err

}

// InstallOS updates the OS to the version contained in the firmware bundle.
// If the update is successful, the RPC will not return.
func (r Client) InstallOS(fb firmware.Bundle) error {
	return sendChunkedUpdate("OS", "RPC.InstallOS", fb)
}

// InstallApplet updates the Applet to the version contained in the firmware bundle.
func (r Client) InstallApplet(fb firmware.Bundle) error {
	klog.Infof("Ignoring request to update Applet, since the applet is now embedded in the OS.")
	return nil
}

// sendChunkedUpdate sends a chunked OS or Applet firmware update request via one or
// more RPCs to the OS.
func sendChunkedUpdate(t string, rpcName string, fb firmware.Bundle) error {
	klog.Infof("Requesting %s install from OS...", t)
	fu := &rpc.FirmwareUpdate{}
	for i := uint(0); ; i++ {
		fu.Sequence = i
		size := fwUpdateChunkSize
		if rem := len(fb.Firmware); size >= rem {
			fu.Proof = config.ProofBundle{
				Checkpoint:     fb.Checkpoint,
				LogIndex:       fb.Index,
				InclusionProof: fb.InclusionProof,
				Manifest:       fb.Manifest,
			}
			size = rem
		}
		fu.Image = fb.Firmware[:size]
		fb.Firmware = fb.Firmware[size:]
		klog.Infof("Sending %s chunk %d", t, i)
		if err := syscall.Call(rpcName, fu, nil); err != nil {
			return err
		}
	}
}

// Reboot instructs the device to reboot after new firmware is installed.
// This call will not return and deferred functions will not be run.
func (r Client) Reboot() {
	_ = syscall.Call("RPC.Reboot", nil, nil)
}
