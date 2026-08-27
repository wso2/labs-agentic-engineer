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

package webhook

import (
	"bytes"
	"encoding/json"
	"strings"
)

// redactPublishedCredentials strips a deliberately-published credential body out
// of a webhook payload before it is stored.
//
// The build publishes each test user's login as an issue comment, because that
// ticket is where the validation agent reads it (ADR-0022). GitHub then delivers
// that comment straight back as an `issue_comment` event, and every verified
// delivery's RAW body is persisted for audit — so without this, each published
// password would also land in `webhook_payloads` in cleartext, cross-org, with
// no retention sweep and no reader. That is a second copy of a credential in the
// one place this service seals everything else, and it is invisible: the ADR
// bounds the blast radius at "whoever can read the repository", and a database
// row is not that.
//
// The fast path is a byte scan, so a delivery carrying no marker is not
// re-encoded at all. When the marker IS present, the two fields a body can
// arrive in are replaced with a notice; if the payload cannot be re-encoded, or
// the marker survives somewhere this does not model, the whole body is dropped
// rather than stored. Nothing reads these rows back, so losing one audit body
// costs less than keeping a credential.
func redactPublishedCredentials(payload []byte) []byte {
	if !bytes.Contains(payload, []byte(credentialNeedle)) {
		return payload
	}
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		return []byte(redactedPayload)
	}
	// `comment.body` on an issue_comment event, `issue.body` on an issues event.
	for _, field := range []string{"comment", "issue"} {
		obj, ok := doc[field].(map[string]any)
		if !ok {
			continue
		}
		if body, ok := obj["body"].(string); ok && strings.Contains(body, credentialNeedle) {
			obj["body"] = redactedBody
		}
	}
	out, err := json.Marshal(doc)
	if err != nil || bytes.Contains(out, []byte(credentialNeedle)) {
		return []byte(redactedPayload)
	}
	return out
}

// credentialNeedle is the part of PublishedCredentialsMarker that survives every
// JSON encoding of it, and it is what this file matches on.
//
// The full marker does NOT survive: Go's encoder escapes `<` and `>` to
// `\u003c` / `\u003e` by default, so a payload re-encoded anywhere on the way
// here carries `\u003c!-- aep:test-users --\u003e` and a scan for the literal
// marker finds nothing — including the re-encode check below, which is the one
// that catches a shape this does not model. The needle has no escapable
// character in it, so it matches either spelling.
//
// TestNeedleIsPartOfTheMarker pins the two together.
const credentialNeedle = "aep:test-users"

const (
	redactedBody    = "[redacted by aep: this comment publishes test-user credentials, which are not stored outside the repository]"
	redactedPayload = `{"aep":"redacted: this delivery carried published test-user credentials"}`
)
