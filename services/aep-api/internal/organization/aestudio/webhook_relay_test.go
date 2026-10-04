// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package aestudio

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"testing"
)

func TestWebhookRelayURL_DeterministicUnguessable(t *testing.T) {
	seed := []byte("0123456789abcdef0123456789abcdef")
	a, b := WebhookRelayURL(seed, "default"), WebhookRelayURL(seed, "default")
	if a != b {
		t.Fatal("must be deterministic")
	}
	mac := hmac.New(sha256.New, seed)
	mac.Write([]byte("default"))
	want := "https://smee.io/" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:22]
	if a != want {
		t.Fatalf("got %s want %s", a, want)
	}
	if WebhookRelayURL(seed, "other") == a || WebhookRelayURL(nil, "default") != "" {
		t.Fatal("per org, and empty without a seed")
	}
	if !regexp.MustCompile(`^https://smee\.io/[A-Za-z0-9_-]{22}$`).MatchString(a) {
		t.Fatalf("smee.io channel ids must match ^[a-zA-Z0-9-_]{1,128}$: %s", a)
	}
}
