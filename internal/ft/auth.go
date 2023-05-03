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

//go:build !disable_fr_auth
// +build !disable_fr_auth

package ft

import (
	_ "embed"
)

const DisableAuth = false

// FRPublicKey represents the applet releases manifest authentication key.
//
//go:embed armored-witness.pub
var FRPublicKey []byte

// LogPublicKey represents the applet releases transparency log.
// authentication key.
//
//go:embed armored-witness-log.pub
var LogPublicKey []byte
