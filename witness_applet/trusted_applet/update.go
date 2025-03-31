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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/machinebox/progress"
	"github.com/transparency-dev/armored-witness-common/release/firmware"
	"github.com/transparency-dev/armored-witness-common/release/firmware/ftlog"
	"github.com/transparency-dev/armored-witness-common/release/firmware/update"
	"github.com/transparency-dev/serverless-log/client"
	"github.com/transparency-dev/armored-witness-os/witness_applet/trusted_applet/internal/update/rpc"
	"golang.org/x/mod/sumdb/note"
	"k8s.io/klog/v2"
)

// These vars are set at compile time using the -X flag, see the Makefile.
var (
	updateBinariesURL                    string
	updateLogURL                         string
	updateLogOrigin                      string
	updateLogVerifier                    string
	updateAppletVerifier                 string
	updateOSVerifier1, updateOSVerifier2 string
)

// updater returns an updater struct configured from the compiled-in
// parameters above.
func updater(ctx context.Context) (*update.Fetcher, *update.Updater, error) {
	if updateLogURL[len(updateLogURL)-1] != '/' {
		updateLogURL += "/"
	}
	logBaseURL, err := url.Parse(updateLogURL)
	if err != nil {
		return nil, nil, fmt.Errorf("firmware log URL invalid: %v", err)
	}

	logVerifier, err := note.NewVerifier(updateLogVerifier)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid firmware log verifier: %v", err)
	}
	appletVerifier, err := note.NewVerifier(updateAppletVerifier)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid applet verifier: %v", err)
	}
	osVerifier1, err := note.NewVerifier(updateOSVerifier1)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid OS verifier 1: %v", err)
	}
	osVerifier2, err := note.NewVerifier(updateOSVerifier2)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid OS verifier 2: %v", err)
	}

	if updateBinariesURL[len(updateBinariesURL)-1] != '/' {
		updateBinariesURL += "/"
	}
	binBaseURL, err := url.Parse(updateBinariesURL)
	if err != nil {
		return nil, nil, fmt.Errorf("binaries URL invalid: %v", err)
	}
	bf := newFetcher(binBaseURL, 5*time.Minute, true)
	binFetcher := func(ctx context.Context, r ftlog.FirmwareRelease) ([]byte, []byte, error) {
		p, err := update.BinaryPath(r)
		if err != nil {
			return nil, nil, fmt.Errorf("BinaryPath: %v", err)
		}
		klog.Infof("Fetching %v bin from %q", r.Component, p)
		// We don't auto-update the bootloader, so no need to fetch HAB signatures.
		bin, err := bf(ctx, p)
		return bin, nil, err
	}

	updateFetcher, err := update.NewFetcher(ctx,
		update.FetcherOpts{
			LogFetcher:     newFetcher(logBaseURL, 30*time.Second, false),
			LogOrigin:      updateLogOrigin,
			LogVerifier:    logVerifier,
			BinaryFetcher:  binFetcher,
			AppletVerifier: appletVerifier,
			OSVerifiers:    [2]note.Verifier{osVerifier1, osVerifier2},
			// Note that we leave BootVerifier and RecoveryVerifier unset as we
			// cannot update those components.
		})
	if err != nil {
		return nil, nil, fmt.Errorf("NewFetcher: %v", err)
	}

	fwVerifier := newFWVerifier(updateLogOrigin, logVerifier, appletVerifier, []note.Verifier{osVerifier1, osVerifier2})
	updater, err := update.NewUpdater(&rpc.Client{}, updateFetcher, fwVerifier)
	if err != nil {
		return nil, nil, fmt.Errorf("NewUdater: %v", err)
	}
	return updateFetcher, updater, nil
}

type fwVerifier struct {
	logOrigin            string
	logVerifier          note.Verifier
	appletBundleVerifier firmware.BundleVerifier
	osBundleVerifier     firmware.BundleVerifier
}

func newFWVerifier(logOrigin string, logVerifier note.Verifier, appletVerifier note.Verifier, osVerifiers []note.Verifier) fwVerifier {
	return fwVerifier{
		logOrigin:   logOrigin,
		logVerifier: logVerifier,
		appletBundleVerifier: firmware.BundleVerifier{
			LogOrigin:         logOrigin,
			LogVerifer:        logVerifier,
			ManifestVerifiers: []note.Verifier{appletVerifier},
		},
		osBundleVerifier: firmware.BundleVerifier{
			LogOrigin:         logOrigin,
			LogVerifer:        logVerifier,
			ManifestVerifiers: osVerifiers,
		},
	}
}

func (fw fwVerifier) Verify(b firmware.Bundle) error {
	allVerifiers := append(append([]note.Verifier{}, fw.appletBundleVerifier.ManifestVerifiers...), fw.osBundleVerifier.ManifestVerifiers...)
	m, err := note.Open(b.Manifest, note.VerifierList(allVerifiers...))
	if err != nil {
		return fmt.Errorf("failed to open manifest: %v", err)
	}
	r := ftlog.FirmwareRelease{}
	if err := json.Unmarshal([]byte(m.Text), &r); err != nil {
		return fmt.Errorf("failed to unmarshal manifest: %v", err)
	}
	switch r.Component {
	case ftlog.ComponentApplet:
		_, err := fw.appletBundleVerifier.Verify(b)
		return err
	case ftlog.ComponentOS:
		_, err := fw.osBundleVerifier.Verify(b)
		return err
	default:
		return fmt.Errorf("non updatable component %q", r.Component)
	}
}

// New creates a Fetcher for the log at the given root location.
func newFetcher(root *url.URL, httpTimeout time.Duration, logProgress bool) client.Fetcher {
	return func(ctx context.Context, p string) ([]byte, error) {
		u, err := root.Parse(p)
		if err != nil {
			return nil, err
		}
		return readHTTP(ctx, u, httpTimeout, logProgress)
	}
}

func readHTTP(ctx context.Context, u *url.URL, timeout time.Duration, logProgress bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	// Clone DefaultClient and set a timeout.
	dc := *http.DefaultClient
	hc := &dc
	hc.Timeout = timeout
	resp, err := hc.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("http.Client.Do(): %v", err)
	}
	switch resp.StatusCode {
	case http.StatusNotFound:
		klog.Infof("Not found: %q", u.String())
		return nil, os.ErrNotExist
	case http.StatusOK:
		break
	default:
		return nil, fmt.Errorf("unexpected http status %q", resp.Status)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			klog.Errorf("resp.Body.Close(): %v", err)
		}
	}()

	pr := progress.NewReader(resp.Body)
	if logProgress && resp.ContentLength > 0 {
		go func() {
			progressChan := progress.NewTicker(ctx, pr, resp.ContentLength, 1*time.Second)
			for p := range progressChan {
				klog.Infof("Downloading %q: %d%%, %v remaining...", u.String(), int(p.Percent()), p.Remaining().Round(time.Second))
			}
		}()
	}
	b, err := io.ReadAll(pr)
	if logProgress {
		klog.Infof("Downloading %q: finished", u.String())
	}

	return b, nil
}
