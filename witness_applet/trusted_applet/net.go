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
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"k8s.io/klog/v2"

	"github.com/beevik/ntp"
	"github.com/transparency-dev/armored-witness-os/api"
	"github.com/transparency-dev/armored-witness-os/witness_applet/third_party/dhcp"
	"go.mercari.io/go-dnscache"

	"github.com/usbarmory/GoTEE/applet"
	"github.com/usbarmory/GoTEE/syscall"
	enet "github.com/usbarmory/imx-enet"
)

// default Trusted Applet network settings
const (
	DHCP            = true
	IP              = "10.0.0.1"
	Netmask         = "255.255.255.0"
	Gateway         = "10.0.0.2"
	DefaultResolver = "8.8.8.8:53"
	DefaultNTP      = "time.google.com"

	nicID = tcpip.NICID(1)

	// Timeout for any http requests.
	httpTimeout = 30 * time.Second

	// DNS cache settings.
	dnsUpdateFreq    = 1 * time.Minute
	dnsUpdateTimeout = 5 * time.Second
)

// Trusted OS syscalls
const (
	RX   = 0x10000000
	TX   = 0x10000001
	FIQ  = 0x10000002
	FREQ = 0x10000003
)

var (
	iface *enet.Interface
)

func init() {
	net.SetDefaultNS([]string{DefaultResolver})
}

// runDHCP starts the dhcp client.
//
// When an IP is successfully leased and configured on the interface, f is called with a context
// which will become Done when the leased address expires. Callers can use this as a mechanism to
// ensure that networking clients/services are only run while a leased IP is held.
//
// This function blocks until the passed-in ctx is Done.
func runDHCP(ctx context.Context, nicID tcpip.NICID, clientID string, hostname string, f func(context.Context) error) {
	// This context tracks the lifetime of the IP lease we get (if any) from the DHCP server.
	// We'll only know what that lease is once we acquire the new IP, which happens inside
	// the aquired func below.
	var (
		childCtx    context.Context
		cancelChild context.CancelFunc
	)
	// fDone is used to ensure that we wait for the passed-in func f to complete before
	// make changes to the network stack or attempt to rerun f when we've acquired a new lease.
	fDone := make(chan bool, 1)
	defer close(fDone)

	// acquired handles our dhcp.Client events - acquiring, releasing, renewing DHCP leases.
	acquired := func(oldAddr, newAddr tcpip.AddressWithPrefix, cfg dhcp.Config) {
		klog.Infof("DHCPC: lease update - old: %v, new: %v", oldAddr.String(), newAddr.String())
		// Handled renewals first, old and new addresses will be equivalent in this case.
		// We may still have to reconfigure the networking stack, even though our assigned IP
		// isn't changing, the DHCP server could have changed routing or DNS info.
		if oldAddr.Address == newAddr.Address && oldAddr.PrefixLen == newAddr.PrefixLen {
			klog.Infof("DHCPC: existing lease on %v renewed", newAddr.String())
			// reconfigure network stuff in-case DNS or gateway routes have changed.
			configureNetFromDHCP(newAddr, cfg)
			// f should already be running, no need to interfere with it.
			return
		}

		// If oldAddr is specified, then our lease on that address has experied - remove it
		// from our stack.
		if !oldAddr.Address.Unspecified() {
			// Since we're changing our primary IP address we must tell f to exit,
			// and wait for it to do so
			cancelChild()
			klog.Info("Waiting for child to complete...")
			<-fDone

			klog.Infof("DHCPC: Releasing %v", oldAddr.String())
			if err := iface.Stack.RemoveAddress(nicID, oldAddr.Address); err != nil {
				klog.Errorf("Failed to remove expired address from stack: %v", err)
			}
		}

		// If newAddr is specified, then we've been granted a lease on a new IP address, so
		// we'll configure our stack to use it, along with whatever routes and DNS info
		// we've been sent.
		if !newAddr.Address.Unspecified() {
			klog.Infof("DHCPC: Acquired %v", newAddr.String())

			newProtoAddr := tcpip.ProtocolAddress{
				Protocol:          ipv4.ProtocolNumber,
				AddressWithPrefix: newAddr,
			}
			if err := iface.Stack.AddProtocolAddress(nicID, newProtoAddr, stack.AddressProperties{PEB: stack.FirstPrimaryEndpoint}); err != nil {
				klog.Errorf("Failed to add newly acquired address to stack: %v", err)
			} else {
				configureNetFromDHCP(newAddr, cfg)

				// Set up a context we'll use to control f's execution lifetime.
				// This will get canceled above if/when our IP lease expires.
				childCtx, cancelChild = context.WithCancel(ctx)

				// And execute f in its own goroutine so we don't block the dhcp.Client.
				go func(childCtx context.Context) {
					// Signal when we exit:
					defer func() { fDone <- true }()

					klog.Info("DHCP: running f")
					for {
						if err := f(childCtx); err != nil {
							klog.Errorf("runDHCP f: %v", err)
							if errors.Is(err, context.Canceled) {
								break
							}
						}
					}
				}(childCtx)
			}
		} else {
			klog.Infof("DHCPC: no address acquired")
		}
	}

	// Start the DHCP client.
	c := dhcp.NewClient(iface.Stack, nicID, iface.Link.LinkAddress(), clientID, hostname, 30*time.Second, time.Second, time.Second, acquired)
	klog.Info("Starting DHCPClient...")
	c.Run(ctx)
}

// configureNetFromDHCP sets up network related configuration, e.g. DNS servers,
// gateway routes, etc. based on config received from the DHCP server.
// Note that this function does not update the network stack's assigned IP address.
func configureNetFromDHCP(newAddr tcpip.AddressWithPrefix, cfg dhcp.Config) {
	if len(cfg.DNS) > 0 {
		resolvers := []string{}
		for _, r := range cfg.DNS {
			resolver := fmt.Sprintf("%s:53", r.String())
			resolvers = append(resolvers, resolver)
		}
		klog.Infof("DHCPC: Using DNS server(s) %v", resolvers)
		net.SetDefaultNS(resolvers)
	}
	// Set up routing for new address
	// Start with the implicit route to local segment
	table := []tcpip.Route{
		{Destination: newAddr.Subnet(), NIC: nicID},
	}
	// add any additional routes from the DHCP server
	if len(cfg.Router) > 0 {
		for _, gw := range cfg.Router {
			table = append(table, tcpip.Route{Destination: header.IPv4EmptySubnet, Gateway: gw, NIC: nicID})
			klog.Infof("DHCPC: Using Gateway %v", gw)
		}
	}
	iface.Stack.SetRouteTable(table)
}

// runNTP starts periodically attempting to sync the system time with NTP.
// Returns a channel which become closed once we have obtained an initial time.
func runNTP(ctx context.Context) chan bool {
	if cfg.NTPServer == "" {
		klog.Info("NTP disabled.")
		return nil
	}

	r := make(chan bool)

	go func(ctx context.Context) {
		// i specifies the interval between checking in with the NTP server.
		// Initially we'll check in more frequently until we have set a time.
		i := time.Second * 10
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(i):
			}

			ip, err := net.DefaultResolver.LookupIP(ctx, "ip4", cfg.NTPServer)
			if err != nil {
				klog.Errorf("Failed to resolve NTP server %q: %v", DefaultNTP, err)
				continue
			}
			ntpR, err := ntp.QueryWithOptions(
				ip[0].String(),
				ntp.QueryOptions{},
			)
			if err != nil {
				klog.Errorf("Failed to get NTP time: %v", err)
				continue
			}
			if err := ntpR.Validate(); err != nil {
				klog.Errorf("got invalid time from NTP server: %v", err)
				continue
			}
			applet.ARM.SetTimer(ntpR.Time.UnixNano())

			// We've got some sort of sensible time set now, so check in with NTP
			// much less frequently.
			i = time.Hour
			if r != nil {
				// Signal that we've got an initial time.
				close(r)
				r = nil
			}
		}
	}(ctx)

	return r
}

func rxFromEth(buf []byte) int {
	n := syscall.Read(RX, buf, uint(len(buf)))

	if n == 0 || n > int(enet.MTU) {
		return 0
	}

	return n
}

func rx(buf []byte) {
	if len(buf) < 14 {
		return
	}

	hdr := buf[0:14]
	proto := tcpip.NetworkProtocolNumber(binary.BigEndian.Uint16(buf[12:14]))
	payload := buf[14:]

	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
		ReserveHeaderBytes: len(hdr),
		Payload:            buffer.MakeWithData(payload),
	})

	copy(pkt.LinkHeader().Push(len(hdr)), hdr)

	iface.Link.InjectInbound(proto, pkt)
}

func tx() (buf []byte) {
	var pkt *stack.PacketBuffer

	if pkt = iface.NIC.Link.Read(); pkt.IsNil() {
		return
	}

	proto := make([]byte, 2)
	binary.BigEndian.PutUint16(proto, uint16(pkt.NetworkProtocolNumber))

	// Ethernet frame header
	buf = append(buf, pkt.EgressRoute.RemoteLinkAddress...)
	buf = append(buf, iface.NIC.MAC...)
	buf = append(buf, proto...)

	for _, v := range pkt.AsSlices() {
		buf = append(buf, v...)
	}

	return
}

type txNotification struct{}

func (n *txNotification) WriteNotify() {
	buf := tx()
	syscall.Write(TX, buf, uint(len(buf)))
}

// mac creates a stable "local administered" MAC address for the network based on the
// provided unit serial number.
func mac(serial string) string {
	m := sha256.Sum256([]byte(fmt.Sprintf("MAC:%s", serial)))
	// The first byte of the MAC address has a couple of flags which must be set correctly:
	// - Unicast(0)/multicast(1) in the least significant bit of the byte.
	//   This must be set to unicast.
	// - Universally unique(0)/Local administered(1) in the second least significant bit.
	//   Since we're not using an organisationally unique prefix triplet, this must be set to
	//   Locally administered
	m[0] &= 0xfe
	m[0] |= 0x02
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", m[0], m[1], m[2], m[3], m[4], m[5])
}

func startNetworking() (err error) {
	// Set the default resolver from the config, if we're using DHCP this may be updated.
	net.SetDefaultNS([]string{cfg.Resolver})

	var status api.Status
	if err := syscall.Call("RPC.Status", nil, &status); err != nil {
		return fmt.Errorf("failed to fetch Status: %v", err)
	}

	iface = &enet.Interface{
		Stack: stack.New(stack.Options{
			NetworkProtocols: []stack.NetworkProtocolFactory{
				ipv4.NewProtocol,
				arp.NewProtocol,
			},
			TransportProtocols: []stack.TransportProtocolFactory{
				tcp.NewProtocol,
				icmp.NewProtocol4,
				udp.NewProtocol,
			},
		}),
	}

	if cfg.DHCP {
		// This is essentially the contents of enet.Init (plus enet.configure)
		// with anything to do with setting up static IP addresses/routes
		// stripped out.
		//
		// TODO(al): Refactor imx-enet to make this cleaner
		macAddress := mac(status.Serial)
		linkAddress, err := net.ParseMAC(macAddress)
		if err != nil {
			return fmt.Errorf("invalid MAC: %v", err)
		}

		if iface.NICID == 0 {
			iface.NICID = enet.NICID
		}

		gvHWAddress, err := tcpip.ParseMACAddress(macAddress)
		if err != nil {
			return fmt.Errorf("invalid MAC: %v", err)
		}
		iface.Link = channel.New(256, enet.MTU, gvHWAddress)
		iface.Link.LinkEPCapabilities |= stack.CapabilityResolutionRequired

		linkEP := stack.LinkEndpoint(iface.Link)

		if err := iface.Stack.CreateNIC(iface.NICID, linkEP); err != nil {
			return fmt.Errorf("%v", err)
		}

		iface.NIC = &enet.NIC{
			MAC:    linkAddress,
			Link:   iface.Link,
			Device: nil,
		}
		err = iface.NIC.Init()

	} else {
		if err = iface.Init(nil, cfg.IP, cfg.Netmask, mac(status.Serial), cfg.Gateway); err != nil {
			return
		}
	}

	iface.EnableICMP()
	iface.Link.AddNotify(&txNotification{})

	resolver, err := dnscache.New(dnsUpdateFreq, dnsUpdateTimeout)
	if err != nil {
		return fmt.Errorf("failed to create DNS cache: %v", err)
	}
	// hook interface into Go runtime
	net.SocketFunc = iface.Socket
	http.DefaultClient = &http.Client{
		Timeout: httpTimeout,
		Transport: &http.Transport{
			DialContext: dnscache.DialFunc(resolver, (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext),
			DisableKeepAlives:     true,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	return
}
