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
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
)

// webhookRelayInfo is the HKDF label that turns CREDENTIAL_ENCRYPTION_KEY
// into the webhook relay key. It is versioned because changing it moves every
// org's derived relay channel, and the hooks GitHub holds point at the old
// one: a new label is a deliberate migration, never a refactor.
const webhookRelayInfo = "ae-studio-webhook-relay/v1"

// webhookRelayKeyLen is the derived relay key's length: one SHA-256 block.
const webhookRelayKeyLen = 32

// Relay flag values (AE_STUDIO_WEBHOOK_RELAY_ENABLED). Unset is "".
const (
	webhookRelayOn  = "true"
	webhookRelayOff = "false"
)

var errWebhookRelayPlaceholderKey = errors.New(
	"AE_STUDIO_WEBHOOK_RELAY_ENABLED needs AE_STUDIO_WEBHOOK_RELAY_SEED or a non-placeholder CREDENTIAL_ENCRYPTION_KEY")

// resolveWebhookRelayKey returns the HMAC key aestudio.WebhookRelayURL derives
// each org's relay channel from; empty is no relay. flag is
// AE_STUDIO_WEBHOOK_RELAY_ENABLED, already checked to be "", "true" or
// "false"; seed is AE_STUDIO_WEBHOOK_RELAY_SEED; credKey is the decoded
// CREDENTIAL_ENCRYPTION_KEY.
//
//	"false"           off, even with a seed
//	seed set          the seed's text (a local install's channels)
//	"true", no seed   HKDF-SHA256(credKey, no salt, webhookRelayInfo)
//	otherwise         off
//
// HKDF lets an install that already holds a secret key turn the relay on
// without a second secret, and its label separates the two uses: a channel id
// is an HMAC under the derived key and says nothing about the AES key.
// Rotating CREDENTIAL_ENCRYPTION_KEY therefore moves every derived channel,
// stranding the hooks that point at the old ones. The all-zero default key is
// public, so deriving from it would make every channel computable: refused.
func resolveWebhookRelayKey(flag, seed string, credKey []byte) ([]byte, error) {
	switch {
	case flag == webhookRelayOff:
		return nil, nil
	case seed != "":
		return []byte(seed), nil
	case flag == webhookRelayOn:
		if isAllZero(credKey) {
			return nil, errWebhookRelayPlaceholderKey
		}
		return hkdf.Key(sha256.New, credKey, nil, webhookRelayInfo, webhookRelayKeyLen)
	default:
		return nil, nil
	}
}

func isAllZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
