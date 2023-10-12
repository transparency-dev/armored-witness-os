// Copyright 2023 The Armored Witness authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// The proofbundle tool builds serialised proof bundles for use when
// embedding the appled into the OS build, only useful for development work.
package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/transparency-dev/armored-witness-common/release/firmware"
	"github.com/transparency-dev/armored-witness-common/release/firmware/ftlog"
	"github.com/transparency-dev/merkle/rfc6962"
	"github.com/transparency-dev/serverless-log/client"
	"golang.org/x/mod/sumdb/note"
	"k8s.io/klog"
)

var (
	outputFile         = flag.String("output_file", "", "File to write the bundle to.")
	logBaseURL         = flag.String("log_url", "", "Base URL for the firmware transparency log to use.")
	logOrigin          = flag.String("log_origin", "", "FT log origin string.")
	logPubKeyFile      = flag.String("log_pubkey_file", "", "File containing the FT log's public key in Note verifier format.")
	appletFile         = flag.String("applet_file", "", "Applet firmware image to build bundle for.")
	manifestFile       = flag.String("manifest_file", "", "Manifest to build a bundle for.")
	manifestPubKeyFile = flag.String("manifest_pubkey_file", "", "File containing a Note verifier string to verify manifest signatures.")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	mv := verifierOrDie(*manifestPubKeyFile, "manifest")
	manifest, _ := loadManifestOrDie(*manifestFile, mv)
	fwBin, err := os.ReadFile(*appletFile)
	if err != nil {
		klog.Exitf("Failed to read applet %q: %v", *appletFile, err)
	}

	logFetcher := newFetcherOrDie(*logBaseURL)
	logHasher := rfc6962.DefaultHasher
	logVerifier := verifierOrDie(*logPubKeyFile, "log")
	lst, err := client.NewLogStateTracker(
		ctx,
		logFetcher,
		logHasher,
		nil,
		logVerifier,
		*logOrigin,
		client.UnilateralConsensus(logFetcher),
	)
	if _, _, _, err := lst.Update(ctx); err != nil {
		klog.Exitf("Update: %v", err)
	}

	idx, err := client.LookupIndex(ctx, logFetcher, logHasher.HashLeaf(manifest))
	if err != nil {
		klog.Exitf("LookupIndex: %v", err)
	}
	klog.Infof("Found manifest at index %d", idx)

	incP, err := lst.ProofBuilder.InclusionProof(ctx, idx)
	if err != nil {
		klog.Exitf("InclusionProof: %v", err)
	}

	bundle := firmware.Bundle{
		Checkpoint:     lst.LatestConsistentRaw,
		Index:          idx,
		InclusionProof: incP,
		Manifest:       manifest,
		Firmware:       fwBin,
	}
	v := firmware.BundleVerifier{
		LogOrigin:         *logOrigin,
		LogVerifer:        logVerifier,
		ManifestVerifiers: []note.Verifier{mv},
	}
	if err := v.Verify(bundle); err != nil {
		klog.Exitf("Failed to verify proof bundle: %v", err)
	}

	// We don't want the firmware in the encoded config, we only
	// needed it to verify the bundle above.
	bundle.Firmware = nil
	jsn, _ := json.MarshalIndent(&bundle, "", " ")
	klog.Infof("ProofBundle:\n%s", string(jsn))

	b := &bytes.Buffer{}
	enc := gob.NewEncoder(b)
	if enc.Encode(bundle); err != nil {
		klog.Exitf("Failed to encode bundle: %v", err)
	}

	if err := os.WriteFile(*outputFile, b.Bytes(), 0o644); err != nil {
		klog.Exitf("WriteFile: %v", err)
	}

	klog.Infof("Wrote %d bytes of proof bundle to %q", b.Len(), *outputFile)
}

// newFetcherOrDie creates a Fetcher for the log at the given root location.
func newFetcherOrDie(logURL string) client.Fetcher {
	root, err := url.Parse(logURL)
	if err != nil {
		klog.Exitf("Couldn't parse log_base_url: %v", err)
	}

	get := getByScheme[root.Scheme]
	if get == nil {
		klog.Exitf("Unsupported URL scheme %s", root.Scheme)
	}

	r := func(ctx context.Context, p string) ([]byte, error) {
		u, err := root.Parse(p)
		if err != nil {
			return nil, err
		}
		return get(ctx, u)
	}
	return r
}

var getByScheme = map[string]func(context.Context, *url.URL) ([]byte, error){
	"http":  readHTTP,
	"https": readHTTP,
	"file": func(_ context.Context, u *url.URL) ([]byte, error) {
		return os.ReadFile(u.Path)
	},
}

func readHTTP(ctx context.Context, u *url.URL) ([]byte, error) {
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
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
	return io.ReadAll(resp.Body)
}

func verifierOrDie(p string, thing string) note.Verifier {
	vs, err := os.ReadFile(p)
	if err != nil {
		klog.Exitf("Failed to read %s pub key file %q: %v", thing, p, err)
	}
	v, err := note.NewVerifier(string(vs))
	if err != nil {
		klog.Exitf("Invalid %s note verifier string %q: %v", thing, vs, err)
	}
	return v
}

func loadManifestOrDie(p string, v note.Verifier) ([]byte, ftlog.FirmwareRelease) {
	b, err := os.ReadFile(p)
	if err != nil {
		klog.Exitf("Failed to read manifest %q: %v", p, err)
	}
	n, err := note.Open(b, note.VerifierList(v))
	if err != nil {
		klog.Exitf("Failed to verify manifest: %v", err)
	}
	var fr ftlog.FirmwareRelease
	if err := json.Unmarshal([]byte(n.Text), &fr); err != nil {
		klog.Exitf("Invalid manifest contents %q: %v", n.Text, err)
	}
	return b, fr
}
