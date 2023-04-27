// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build !disable_fr_auth
// +build !disable_fr_auth

package ft

import (
	_ "embed"
)

const DisableAuth = false

// FRPublicKey represents the applet releases manifest authentication key.
//
//go:embed armory-witness.pub
var FRPublicKey []byte

// LogPublicKey represents the applet releases transparency log.
// authentication key.
//
//go:embed armory-witness-log.pub
var LogPublicKey []byte
