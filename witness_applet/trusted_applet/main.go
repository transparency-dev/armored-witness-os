// Copyright 2022 The Armored Witness Applet authors. All Rights Reserved.
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
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"runtime"
	"sync"

	"strings"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	f_note "github.com/transparency-dev/formats/note"

	"github.com/usbarmory/GoTEE/applet"
	"github.com/usbarmory/GoTEE/syscall"
	"github.com/usbarmory/tamago/soc/nxp/usdhc"
	"google.golang.org/protobuf/proto"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"

	"github.com/transparency-dev/armored-witness-common/release/firmware/update"
	"github.com/transparency-dev/armored-witness-os/api"
	"github.com/transparency-dev/armored-witness-os/api/rpc"
	"github.com/transparency-dev/armored-witness-os/witness_applet/trusted_applet/internal/storage"
	"github.com/transparency-dev/armored-witness-os/witness_applet/trusted_applet/internal/storage/slots"

	"github.com/transparency-dev/witness/monitoring"
	"github.com/transparency-dev/witness/monitoring/prometheus"
	"github.com/transparency-dev/witness/omniwitness"
	sumdb_note "golang.org/x/mod/sumdb/note"
	"k8s.io/klog/v2"

	_ "golang.org/x/crypto/x509roots/fallback"
)

const (
	// slotsPartitionStartBlock defines where our witness data storage partition starts.
	// Changing this location is overwhelmingly likely to result in data loss.
	slotsPartitionStartBlock = 0x400000
	// slotsPartitionLengthBlocks specifies the size of the slots partition.
	// Increasing this value is relatively safe, if you're sure there is no data
	// stored in blocks which follow the current partition.
	//
	// We're starting with enough space for 4096 slots of 512KB each, which should be plenty.
	slotsPartitionLengthBlocks = 0x400000

	// slotSizeBytes is the size of each individual slot in the partition.
	// Changing this is overwhelmingly likely to result in data loss.
	slotSizeBytes = 512 << 10

	// updateCheckInterval is the time between checking the FT Log for firmware
	// updates.
	updateCheckInterval = 5 * time.Minute

	// bastionRateLimit is the maximum number of bastion requests per second to serve.
	bastionRateLimit = float64(30)
)

var (
	Revision string
	Version  string

	RestDistributorBaseURL string
	BastionAddr            string

	cfg *api.Configuration

	persistence *storage.SlotPersistence
)

var (
	doOnce                       sync.Once
	counterWitnessStarted        monitoring.Counter
	counterFirmwareUpdateAttempt monitoring.Counter
	counterFirmwareUpdateSuccess monitoring.Counter
)

func initMetrics() {
	doOnce.Do(func() {
		mf := monitoring.GetMetricFactory()
		counterWitnessStarted = mf.NewCounter("witness_started", "Number of times the witness was started")
		counterFirmwareUpdateAttempt = mf.NewCounter("firmware_update_attempt", "Number of times the updater ran to check if firmware could be updated")
		counterFirmwareUpdateSuccess = mf.NewCounter("firmware_update_success", "Number of times the updater suceeded when checking if firmware could be updated. This does not mean that firmware was installed. It more closely resembles a NOOP for firmware update.")
		// Unfortunately, the default prom gatherer has _some_ Go collectors, but not all, so we have to
		// unregister it in order to be able to register the newer way with expanded coverage.
		// error for dupes.
		prom.Unregister(prom.NewGoCollector())
		prom.Register(collectors.NewGoCollector(collectors.WithGoCollectorRuntimeMetrics(collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile("/.*")})))
	})
}

func init() {
	runtime.Exit = func(_ int32) { applet.Exit() }
}

func main() {
	klog.InitFlags(nil)
	flag.Set("vmodule", "journal=1,slots=1,storage=1")
	flag.Set("logtostderr", "true")
	flag.Parse()

	ctx := context.Background()
	defer applet.Exit()

	mf := prometheus.MetricFactory{
		Prefix: "omniwitness_",
	}
	monitoring.SetMetricFactory(mf)
	initMetrics()

	klog.Infof("%s/%s (%s) • TEE user applet • %s",
		runtime.GOOS, runtime.GOARCH, runtime.Version(),
		Revision)

	// Verify if we are allowed to run on this unit by sending version
	// information for rollback protection check.
	if err := syscall.Call("RPC.Version", strings.TrimPrefix(Version, "v"), nil); err != nil {
		log.Fatalf("TA version check error for version %q: %v", Version, err)
	}
	// Set default configuration, the applet is reponsible of implementing
	// its own configuration storage strategy.
	cfg = &api.Configuration{
		DHCP:      DHCP,
		IP:        IP,
		Netmask:   Netmask,
		Gateway:   Gateway,
		Resolver:  DefaultResolver,
		NTPServer: DefaultNTP,
	}

	// Send network configuration to Trusted OS for network initialization.
	//
	// The same call can also be used to receive configuration updates from
	// the Trusted OS, this gives the opportunity to check if the returned
	// configuration is identical to the stored one or different (meaning
	// that a configuration update has been requested through the control
	// interface).
	//
	// In this example the sent configuration is always updated with the
	// received one.
	var cfgResp []byte
	if err := syscall.Call("RPC.Config", cfg.Bytes(), &cfgResp); err != nil {
		klog.Errorf("TA configuration error, %v", err)
	}

	if len(cfgResp) > 0 {
		if err := proto.Unmarshal(cfgResp, cfg); err != nil {
			log.Fatalf("TA configuration invalid: %v", err)
		}
	}
	var status api.Status

	if err := syscall.Call("RPC.Status", nil, &status); err != nil {
		log.Fatalf("TA status error, %v", err)
	}

	for _, line := range strings.Split(status.Print(), "\n") {
		klog.Info(line)
	}

	syscall.Call("RPC.LED", rpc.LEDStatus{Name: "blue", On: true}, nil)
	defer syscall.Call("RPC.LED", rpc.LEDStatus{Name: "blue", On: false}, nil)

	// (Re-)create our witness identity based on the device's internal secret key.
	deriveIdentityKeys()
	// Update our status in OS so custodian can inspect our signing identity even if there's no network.
	syscall.Call("RPC.SetWitnessStatus", rpc.WitnessStatus{
		Identity:          witnessPublicKey,
		IDAttestPublicKey: attestPublicKey,
		AttestedID:        witnessPublicKeyAttestation,
		AttestedBastionID: bastionIDAttestation,
	}, nil)

	klog.Infof("Attestation key:\n%s", attestPublicKey)
	klog.Infof("Attested identity key:\n%s", witnessPublicKeyAttestation)

	go func() {
		l := true
		for {
			syscall.Call("RPC.LED", rpc.LEDStatus{Name: "blue", On: l}, nil)
			l = !l
			time.Sleep(500 * time.Millisecond)
		}
	}()

	if err := startNetworking(); err != nil {
		log.Fatalf("TA could not initialize networking, %v", err)
	}

	syscall.Call("RPC.Address", iface.NIC.MAC, nil)

	// Register and run our RPC handler so we can receive ethernet frames.
	go eventHandler()

	klog.Infof("Opening storage...")
	part := openStorage()
	klog.Infof("Storage opened.")

	// Set this to true to "wipe" the storage.
	// Currently this simply force-writes an entry with zero bytes to
	// each known slot.
	// If the journal(s) become corrupt a larger hammer will be required.
	reinit := false
	if reinit {
		for i := 10; i > 0; i-- {
			klog.Infof("Erasing in %ds", i)
			time.Sleep(time.Second)
		}
		if err := part.Erase(); err != nil {
			klog.Exitf("Failed to erase partition: %v", err)
		}
		klog.Exit("Erase completed")
	}

	persistence = storage.NewSlotPersistence(part)
	if err := persistence.Init(); err != nil {
		klog.Exitf("Failed to create persistence layer: %v", err)
	}
	persistence.Init()

	// Wait for a DHCP address to be assigned if that's what we're configured to do
	if cfg.DHCP {
		hostname := "armoredwitness"
		if v, err := f_note.NewVerifier(witnessPublicKey); err == nil {
			hostname = cleanForDNS(v.Name())
		}
		runDHCP(ctx, nicID, fmt.Sprintf("AW-%s", status.Serial), hostname, runWithNetworking)
	} else {
		for {
			if err := runWithNetworking(ctx); err != nil && err != context.Canceled {
				klog.Warningf("runWithNetworking: %v", err)
			}
		}
	}

	// This forces the linker to keep the symbol present which is necessary for the inspect()
	// function in the OS to work.
	runtime.CallOnG0()
}

func cleanForDNS(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			return r
		default:
			return '-'
		}
	}, s)
}

// runWithNetworking should only be called when we have an IP network configured.
// ctx should become Done if the network becomes unconfigured for any reason (e.g.
// DHCP lease expires).
//
// Everything which relies on IP networking being present should be started in
// here, and should gracefully stop when the passed-in context is Done.
func runWithNetworking(ctx context.Context) error {
	addr, tcpErr := iface.Stack.GetMainNICAddress(nicID, ipv4.ProtocolNumber)
	if tcpErr != nil {
		return fmt.Errorf("runWithNetworking has no network configured: %v", tcpErr)
	}
	klog.Infof("TA Version:%s MAC:%s IP:%s GW:%s DefaultDNS:%s", Version, iface.NIC.MAC.String(), addr, iface.Stack.GetRouteTable(), DefaultResolver)
	// Update status with latest IP address too.
	syscall.Call("RPC.SetWitnessStatus", rpc.WitnessStatus{
		Identity:          witnessPublicKey,
		IDAttestPublicKey: attestPublicKey,
		AttestedID:        witnessPublicKeyAttestation,
		AttestedBastionID: bastionIDAttestation,
		IP:                addr.Address.String(),
	}, nil)

	// Avoid the situation where, at boot, we get a DHCP lease and then immediately update our
	// local clock from 1970 to now, whereupon we consider the DHCP lease invalid and have to tear down
	// the witness etc. below.
	coldStart := time.Now().Before(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC))

	select {
	case <-runNTP(ctx):
		if coldStart {
			klog.Info("Large NTP date change detected, waiting for network to restart...")
			// Give a bit of space so we don't spin while we wait for DHCP to do its thing.
			time.Sleep(time.Second)
			return nil
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	triggerUpdate := updateChecker(ctx, updateCheckInterval)

	listenCfg := &net.ListenConfig{}

	adminListener, err := listenCfg.Listen(ctx, "tcp", ":8081")
	if err != nil {
		return fmt.Errorf("TA could not initialize admin listener, %v", err)
	}
	defer func() {
		klog.Info("Closing admin port (8081)")
		if err := adminListener.Close(); err != nil {
			klog.Errorf("Error closing admin port: %v", err)
		}
	}()
	go func() {
		srvMux := http.NewServeMux()
		srvMux.Handle("/metrics", promhttp.Handler())
		srvMux.Handle("/crashlog", &logHandler{RPC: "RPC.CrashLog"})
		srvMux.Handle("/consolelog", &logHandler{RPC: "RPC.ConsoleLog"})
		srvMux.HandleFunc("/updatecheck", func(w http.ResponseWriter, _ *http.Request) {
			triggerUpdate <- struct{}{}
			w.Header().Add("Content-Type", "text/plain")
			w.Write([]byte("ok, check /consolelog!"))
		})
		srvMux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
			var s api.Status
			if err := syscall.Call("RPC.Status", nil, &s); err != nil {
				w.Header().Add("Content-Type", "text/plain")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(err.Error()))
				return
			}
			w.Header().Add("Content-Type", "text/plain")
			w.Write([]byte(s.Print()))
		})
		srv := &http.Server{
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
			Handler:      srvMux,
		}
		if err := srv.Serve(adminListener); err != http.ErrServerClosed {
			klog.Errorf("Error serving metrics: %v", err)
		}
	}()

	signerLegacy, err := sumdb_note.NewSigner(witnessSigningKey)
	if err != nil {
		klog.Exitf("Failed to init signer v0: %v", err)
	}
	signerCosigV1, err := f_note.NewSignerForCosignatureV1(witnessSigningKey)
	if err != nil {
		klog.Exitf("Failed to init signer v1: %v", err)
	}

	// Set up and start omniwitness
	opConfig := omniwitness.OperatorConfig{
		WitnessKeys:            []sumdb_note.Signer{signerLegacy, signerCosigV1},
		WitnessVerifier:        signerCosigV1.Verifier(),
		RestDistributorBaseURL: RestDistributorBaseURL,
		FeedInterval:           30 * time.Second,
		DistributeInterval:     30 * time.Second,
	}
	if BastionAddr != "" {
		klog.Infof("Bastion host %q configured", BastionAddr)
		opConfig.BastionAddr = BastionAddr
		opConfig.BastionKey = bastionSigningKey
		opConfig.BastionRateLimit = bastionRateLimit
	}
	mainListener, err := listenCfg.Listen(ctx, "tcp", ":80")
	if err != nil {
		return fmt.Errorf("could not initialize HTTP listener: %v", err)
	}
	defer func() {
		if err := mainListener.Close(); err != nil {
			klog.Errorf("mainListener: %v", err)
		}
	}()

	klog.Info("Starting witness...")
	klog.Infof("I am %q", witnessPublicKey)
	counterWitnessStarted.Inc()
	if err := omniwitness.Main(ctx, opConfig, persistence, mainListener, http.DefaultClient); err != nil {
		return fmt.Errorf("omniwitness.Main failed: %v", err)
	}

	return ctx.Err()
}

func updateChecker(ctx context.Context, i time.Duration) chan<- struct{} {
	var updateFetcher *update.Fetcher
	var updateClient *update.Updater
	var err error

	trigger := make(chan struct{}, 1)

	go func(ctx context.Context) {
		t := time.NewTicker(i)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				trigger <- struct{}{}
			case <-ctx.Done():
				close(trigger)
				return
			}
		}
	}(ctx)

	go func(ctx context.Context) {
		for {
			select {
			case _, ok := <-trigger:
				if !ok {
					return
				}
				if updateFetcher == nil || updateClient == nil {
					updateFetcher, updateClient, err = updater(ctx)
					if err != nil {
						klog.Errorf("Failed to create updater: %v", err)
						continue
					}
				}
				counterFirmwareUpdateAttempt.Inc()
				klog.V(1).Info("Scanning for available updates")
				if err := updateFetcher.Scan(ctx); err != nil {
					klog.Errorf("UpdateFetcher.Scan: %v", err)
					continue
				}
				if err := updateClient.Update(ctx); err != nil {
					klog.Errorf("Update: %v", err)
				}
				counterFirmwareUpdateSuccess.Inc()
			case <-ctx.Done():
				return
			}
		}
	}(ctx)

	return trigger
}

func openStorage() *slots.Partition {
	var info usdhc.CardInfo
	if err := syscall.Call("RPC.CardInfo", nil, &info); err != nil {
		klog.Exitf("Failed to get cardinfo: %v", err)
	}
	klog.Infof("CardInfo: %+v", info)
	// dev is our access to the MMC storage.
	dev := &storage.Device{CardInfo: &info}
	bs := dev.BlockSize()
	geo := slots.Geometry{
		Start:  slotsPartitionStartBlock,
		Length: slotsPartitionLengthBlocks,
	}
	sl := slotSizeBytes / bs
	for i := uint(0); i < geo.Length; i += sl {
		geo.SlotLengths = append(geo.SlotLengths, sl)
	}

	p, err := slots.OpenPartition(dev, geo)
	if err != nil {
		klog.Exitf("Failed to open partition: %v", err)
	}
	return p
}

type logHandler struct {
	RPC string
}

func (c *logHandler) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	var l []byte
	if err := syscall.Call(c.RPC, nil, &l); err != nil {
		klog.Errorf("Failed to fetch log from %v: %v", c.RPC, err)
		res.WriteHeader(http.StatusInternalServerError)
		return
	}
	res.Header().Add("Content-Type", "text/plain")
	res.Write(l)
}
