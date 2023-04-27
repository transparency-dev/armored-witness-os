// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package main

import (
	_ "embed"
	"log"
	"os"
	"runtime"

	usbarmory "github.com/usbarmory/tamago/board/usbarmory/mk2"
	"github.com/usbarmory/tamago/soc/nxp/imx6ul"

	"github.com/usbarmory/armory-boot/config"

	// for now just test compilation of these
	_ "github.com/usbarmory/armory-witness/internal/ft"
	_ "github.com/usbarmory/armory-witness/internal/hab"
	_ "github.com/usbarmory/armory-witness/rpmb"
)

// initialized at compile time (see Makefile)
var (
	Build     string
	Revision  string
	Version   string
	PublicKey string
)

var (
	Network = usbarmory.ENET2
	Storage = usbarmory.MMC
	Control = usbarmory.USB1
)

// A Trusted Applet can be embedded for testing purposes with QEMU.
var (
	//go:embed assets/trusted_applet.elf
	taELF []byte

	//go:embed assets/trusted_applet.sig
	taSig []byte
)

func init() {
	log.SetFlags(log.Ltime)
	log.SetOutput(os.Stdout)

	if len(PublicKey) == 0 {
		log.Fatal("SM applet authentication key is missing")
	}

	if imx6ul.Native {
		imx6ul.SetARMFreq(imx6ul.Freq792)
		imx6ul.DCP.Init()
	}

	imx6ul.GIC.Init(false, true)

	log.Printf("%s/%s (%s) • TEE security monitor (Secure World system/monitor) • %s %s",
		runtime.GOOS, runtime.GOARCH, runtime.Version(),
		Revision, Build)
}

func main() {
	var err error

	usbarmory.LED("blue", false)
	usbarmory.LED("white", false)

	if imx6ul.Native {
		if err = Storage.Detect(); err != nil {
			log.Fatalf("SM failed to detect storage, %v", err)
		}
	} else {
		Network = imx6ul.ENET1
	}

	Network.Init()

	rpmb := &RPMB{
		Storage: Storage,
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

	if len(taELF) == 0 && len(taSig) == 0 {
		if taELF, taSig, err = read(Storage); err != nil {
			log.Printf("SM could not load applet, %v", err)
		}
	}

	ctl := &controlInterface{
		RPC: &RPC{
			RPMB: rpmb,
		},
	}

	if len(taELF) != 0 && len(taSig) != 0 {
		log.Printf("SM applet verification")

		if err := config.Verify(taELF, taSig, PublicKey); err != nil {
			log.Fatalf("SM applet verification error, %v", err)
		}

		log.Printf("SM applet verified")

		usbarmory.LED("white", true)

		if _, err = loadApplet(taELF, ctl); err != nil {
			log.Fatalf("SM applet execution error, %v", err)
		}
	}

	// never returns
	ctl.Start()
}
