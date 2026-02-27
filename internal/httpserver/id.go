// Copyright 2026 Kurok1
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

package httpserver

import (
	"crypto/rand"
	"encoding/hex"
)

func newResponseID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "resp_fallback"
	}
	return "resp_" + hex.EncodeToString(b[:])
}
