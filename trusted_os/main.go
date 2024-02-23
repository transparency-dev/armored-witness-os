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
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/coreos/go-semver/semver"
	usbarmory "github.com/usbarmory/tamago/board/usbarmory/mk2"
	"github.com/usbarmory/tamago/soc/nxp/enet"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"
	"github.com/usbarmory/tamago/soc/nxp/usb"
	"golang.org/x/mod/sumdb/note"

	// for now just test compilation of these
	"github.com/transparency-dev/armored-witness-common/release/firmware"
	_ "github.com/transparency-dev/armored-witness-os/internal/hab"
	_ "github.com/transparency-dev/armored-witness-os/rpmb"
)

const (
	Build = ""
)

// initialized at compile time (see Makefile)
var (
	Revision               string
	Version                string
	SRKHash                string
	LogVerifier            string
	LogOrigin              string
	AppletManifestVerifier string
	OSManifestVerifier1    string
	OSManifestVerifier2    string
)

var (
	Control *usb.USB

	// USB armory Mk II (rev. β) - UA-MKII-β
	// USB armory Mk II (rev. γ) - UA-MKII-γ
	USB *usb.USB

	// USB armory Mk II LAN - UA-MKII-LAN
	LAN *enet.ENET

	// osVersion is the semver parsed representation of the Version string above.
	osVersion semver.Version
	// loadedAppletVersion is taken from the manifest used to verify the
	// applet.
	loadedAppletVersion semver.Version

	AppletBundleVerifier firmware.BundleVerifier
	OSBundleVerifier     firmware.BundleVerifier
)

// A Trusted Applet can be embedded for testing purposes with QEMU.
var (
	//go:embed assets/trusted_applet.elf
	taELF []byte

	//go:embed assets/trusted_applet.proofbundle
	taProofBundle []byte
)

func init() {
	log.SetFlags(log.Ltime)
	log.SetOutput(os.Stdout)

	if imx6ul.Native {
		imx6ul.SetARMFreq(imx6ul.Freq528)

		if imx6ul.DCP != nil {
			imx6ul.DCP.Init()
		}

		model, _ := usbarmory.Model()

		switch model {
		case usbarmory.BETA, usbarmory.GAMMA:
			USB = usbarmory.USB1
			USB.Init()

			Control = usbarmory.USB2
			Control.Init()

			if debug {
				debugConsole, _ := usbarmory.DetectDebugAccessory(250 * time.Millisecond)
				<-debugConsole
			}
		case usbarmory.LAN:
			LAN = usbarmory.ENET2
			LAN.RingSize = 512
			LAN.Init()

			Control = usbarmory.USB1
			Control.Init()
		}
	} else {
		LAN = imx6ul.ENET1
		LAN.RingSize = 512
		LAN.Init()
	}

	imx6ul.GIC.Init(true, false)

	log.Printf("%s/%s (%s) • TEE security monitor (Secure World system/monitor) • %s %s",
		runtime.GOOS, runtime.GOARCH, runtime.Version(),
		Revision, Build)
}

func main() {
	// Increase default fuse-blowing timeout
	if imx6ul.Native && imx6ul.OCOTP != nil {
		imx6ul.OCOTP.Timeout = 100 * time.Millisecond
	}

	var err error

	usbarmory.LED("blue", false)
	usbarmory.LED("white", false)

	Storage := storage()
	if Storage != nil {
		if err = Storage.Detect(); err != nil {
			log.Fatalf("SM failed to detect storage, %v", err)
		}
	}

	rpmb, err := newRPMB(Storage)
	if err != nil {
		log.Fatalf("SM could not initialize rollback protection, %v", err)
	}

	rpc := &RPC{
		RPMB:        rpmb,
		Storage:     Storage,
		Diversifier: sha256.Sum256([]byte(AppletManifestVerifier)),
	}

	ctl := &controlInterface{
		RPC:     rpc,
		SRKHash: SRKHash,
	}

	// TODO: disable for now
	if false && imx6ul.SNVS.Available() {
		log.Printf("SM version verification (%s)", Version)

		if err = rpmb.init(); err != nil {
			log.Fatalf("SM could not initialize rollback protection, %v", err)
		}

		if err = rpmb.checkVersion(osVersionSector, Version); err != nil {
			log.Fatalf("SM firmware rollback check failure, %v", err)
		}
	}

	if err := determineLoadedOSBlock(Storage); err != nil {
		log.Printf("Failed to determine OS MMC block (no OS installed?): %v", err)
	}

	log.Printf("SM log verification pub: %s", LogVerifier)
	logVerifier, err := note.NewVerifier(LogVerifier)
	if err != nil {
		log.Fatalf("SM invalid AppletLogVerifier: %v", err)
	}
	log.Printf("SM applet verification pub: %s", AppletManifestVerifier)
	AppletBundleVerifier, err = createBundleVerifier(LogOrigin, logVerifier, []string{AppletManifestVerifier})
	if err != nil {
		log.Fatalf("SM failed to create applet bundle verifier: %v", err)
	}
	OSBundleVerifier, err = createBundleVerifier(LogOrigin, logVerifier, []string{OSManifestVerifier1, OSManifestVerifier2})
	if err != nil {
		log.Fatalf("SM failed to create OS bundle verifier: %v", err)
	}

	if v, err := semver.NewVersion(Version); err != nil {
		log.Printf("Failed to parse OS version %q: %v", Version, err)
	} else {
		osVersion = *v
	}

	var ta *firmware.Bundle
	if len(taELF) > 0 && len(taProofBundle) > 0 {
		// Handle embedded applet & proof.
		dec := gob.NewDecoder(bytes.NewBuffer(taProofBundle))
		ta = &firmware.Bundle{}
		if err := dec.Decode(ta); err != nil {
			log.Printf("SM invalid embedded proof bundle: %v", err)
		}
		ta.Firmware = taELF
	} else {
		if ta, err = read(Storage); err != nil {
			log.Printf("SM could not load applet, %v", err)
		}
	}

	if ta != nil {
		go func() {
			for {
				log.Print("SM Verifying applet bundle")
				manifest, err := AppletBundleVerifier.Verify(*ta)
				if err != nil {
					log.Printf("SM applet verification error, %v", err)
				}
				loadedAppletVersion = manifest.Git.TagName
				log.Printf("SM Loaded applet version %s", loadedAppletVersion.String())

				usbarmory.LED("white", true)

				ta, err := loadApplet(ta.Firmware, ctl)
				if err != nil {
					log.Printf("SM applet execution error, %v", err)
				}

				<-ta.Done()
			}
		}()
	}

	go func() {
		l := true
		for {
			usbarmory.LED("white", l)
			l = !l
			time.Sleep(500 * time.Millisecond)
		}
	}()

	// start USB control interface
	ctl.Start()

	// never returns
	handleInterrupts()

	if debug {
		// We never hit this due to handleInterrupts not returning, but having this line here
		// forces the linker to keep the symbol present which is necessary for the inspect()
		// function to work for debug builds.
		runtime.CallOnG0()
	}
}

func createBundleVerifier(logOrigin string, logVerifier note.Verifier, manifestVerifiers []string) (firmware.BundleVerifier, error) {
	vs := []note.Verifier{}
	for _, v := range manifestVerifiers {
		nv, err := note.NewVerifier(v)
		if err != nil {
			return firmware.BundleVerifier{}, fmt.Errorf("invalid manifest verifier %q: %v", v, err)
		}
		vs = append(vs, nv)
	}
	bv := firmware.BundleVerifier{
		LogOrigin:         logOrigin,
		LogVerifer:        logVerifier,
		ManifestVerifiers: vs,
	}
	return bv, nil
}
