// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build disable_fr_auth
// +build disable_fr_auth

package ft

const DisableAuth = true

var (
	FRPublicKey  []byte
	LogPublicKey []byte
)
