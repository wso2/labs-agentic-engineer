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

package config

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// goldenRelayKeyHex is HKDF-SHA256(ikm = bytes 0x00..0x1f, salt = none,
// info = "ae-studio-webhook-relay/v1", 32 bytes), computed outside Go with
// both `openssl kdf -keylen 32 -kdfopt digest:SHA256 -kdfopt hexkey:0001…1f
// -kdfopt info:ae-studio-webhook-relay/v1 HKDF` and a Python RFC 5869
// extract+expand. It pins the algorithm and the label: a refactor that
// changes either moves every derived relay channel and strands its hooks.
const goldenRelayKeyHex = "19eb1a9f23ae0af88adc67a16e9544ea0fe1733b0477f252f31c35a426de4660"

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestResolveWebhookRelayKey_GoldenVector(t *testing.T) {
	got, err := resolveWebhookRelayKey("true", "", mustB64(t, testRelayCredKey))
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != goldenRelayKeyHex {
		t.Fatalf("derived relay key = %x, want the golden vector", got)
	}
}

// A different credential key gives a different relay key, and the relay key
// is never the credential key itself (domain separation by the HKDF label).
func TestResolveWebhookRelayKey_PerCredentialKey(t *testing.T) {
	k1, k2 := mustB64(t, testRelayCredKey), mustB64(t, testRelayCredKey2)
	a, err := resolveWebhookRelayKey("true", "", k1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := resolveWebhookRelayKey("true", "", k2)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Error("two credential keys derived the same relay key")
	}
	if bytes.Equal(a, k1) || bytes.Equal(b, k2) {
		t.Error("the relay key equals the credential key")
	}
}

// The refusal for a placeholder credential key carries neither the key nor
// anything derived from it.
func TestResolveWebhookRelayKey_ErrorCarriesNoKeyMaterial(t *testing.T) {
	zero := make([]byte, 32)
	_, err := resolveWebhookRelayKey("true", "", zero)
	if err == nil {
		t.Fatal("the all-zero placeholder key must be refused")
	}
	msg := err.Error()
	for _, s := range []string{
		base64.StdEncoding.EncodeToString(zero), hex.EncodeToString(zero),
		testRelayCredKey, goldenRelayKeyHex,
	} {
		if strings.Contains(msg, s) {
			t.Errorf("error %q carries key material %q", msg, s)
		}
	}
}
