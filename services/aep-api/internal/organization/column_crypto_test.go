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

package organization

import (
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

func TestSealOpenWebhookSecrets_RoundTrip(t *testing.T) {
	cipher, err := secrets.NewColumnCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	in := WebhookSecrets{
		{Secret: "a", AddedAt: time.Unix(1, 0).UTC()},
		{Secret: "b", AddedAt: time.Unix(2, 0).UTC()},
	}
	sealed, err := sealWebhookSecrets(cipher, in)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if sealed[0].Secret == "a" || !cipher.IsSealed(sealed[0].Secret) {
		t.Fatalf("not sealed: %+v", sealed)
	}
	opened, err := openWebhookSecrets(cipher, sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if opened[0].Secret != "a" || opened[1].Secret != "b" {
		t.Fatalf("opened = %+v", opened)
	}
}

func TestOpenWebhookSecrets_ToleratesPlaintext(t *testing.T) {
	cipher, err := secrets.NewColumnCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	opened, err := openWebhookSecrets(cipher, WebhookSecrets{{Secret: "still-plain"}})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if opened[0].Secret != "still-plain" {
		t.Fatalf("got %q", opened[0].Secret)
	}
}
