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
)

// webhookRelayChannelLen is the smee.io channel id length: 22 base64url
// characters are 132 bits of the HMAC, unguessable without the seed.
const webhookRelayChannelLen = 22

// WebhookRelayURL is org's smee.io relay channel: https://smee.io/ +
// base64url(HMAC-SHA256(seed, ocOrgID)) cut to 22 characters (no padding).
// It is derived, never stored: the same seed gives every org the same
// channel on every converge, and a channel says nothing about another org's.
// seed is the resolved relay key (config.AEStudioConfig.WebhookRelaySeed);
// an empty seed is no relay and answers "".
func WebhookRelayURL(seed []byte, ocOrgID string) string {
	if len(seed) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, seed)
	mac.Write([]byte(ocOrgID))
	return "https://smee.io/" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:webhookRelayChannelLen]
}
