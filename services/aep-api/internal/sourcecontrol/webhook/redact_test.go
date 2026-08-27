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
	"encoding/json"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The build publishes test-user logins as an issue comment on purpose; GitHub
// hands that comment straight back to us, and every verified delivery's raw body
// is stored. These tests are the gate between the two.

const publishedComment = "<!-- aep:machine -->\n### Test user logins\n\n" +
	"<!-- aep:test-users -->\n\n| Username | Password | Role | Cold start |\n" +
	"| --- | --- | --- | --- |\n| `test-trainer` | `Aep1!SecretValue-123` | Trainer | no |\n"

func TestRedactRemovesAPublishedPasswordFromAnIssueComment(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"action":  "created",
		"comment": map[string]any{"id": 42, "body": publishedComment},
		"issue":   map[string]any{"number": 7, "title": "Provision roles and test users"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := string(redactPublishedCredentials(body))

	if strings.Contains(got, "Aep1!SecretValue-123") {
		t.Fatalf("the stored payload keeps the password:\n%s", got)
	}
	if strings.Contains(got, sourcecontrol.PublishedCredentialsMarker) {
		t.Errorf("the marker survived, so the body was not the one rewritten:\n%s", got)
	}
	// The delivery is still a usable audit row: everything that is not the
	// credential body is intact.
	var doc map[string]any
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("the redacted payload is not valid JSON: %v", err)
	}
	if doc["action"] != "created" {
		t.Errorf("action was lost: %v", doc["action"])
	}
	issue, _ := doc["issue"].(map[string]any)
	if issue == nil || issue["title"] != "Provision roles and test users" {
		t.Errorf("the issue identity was lost: %v", doc["issue"])
	}
	comment, _ := doc["comment"].(map[string]any)
	if comment == nil || !strings.Contains(comment["body"].(string), "redacted by aep") {
		t.Errorf("the body should say WHY it is empty, not be empty: %v", doc["comment"])
	}
}

// The same table can arrive as an issue BODY if somebody moves it there, and the
// receiver must not care which field it was.
//
// The password is safe either way — an unrewritten body trips the re-encode
// check and the whole payload is dropped — so this asserts the FIELD was
// rewritten, by checking the rest of the delivery survived. Without that, the
// test passes on the drop-everything path and stops testing this field at all.
func TestRedactRemovesAPublishedPasswordFromAnIssueBody(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"action": "edited",
		"issue":  map[string]any{"number": 7.0, "body": publishedComment},
	})

	got := string(redactPublishedCredentials(body))

	if strings.Contains(got, "Aep1!SecretValue-123") {
		t.Fatalf("the stored payload keeps the password:\n%s", got)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	issue, _ := doc["issue"].(map[string]any)
	if issue == nil || issue["number"] != 7.0 {
		t.Fatalf("the whole delivery was dropped instead of the one body: %s", got)
	}
}

// A marker in a shape this does not model must cost the BODY, not leak. Storing
// nothing is the right answer: no code reads these rows back.
func TestRedactDropsThePayloadWhenTheMarkerSurvives(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"action": "created",
		// Not `comment.body` or `issue.body` — somewhere this does not rewrite.
		"review": map[string]any{"body": publishedComment},
	})

	got := string(redactPublishedCredentials(body))

	if strings.Contains(got, "Aep1!SecretValue-123") || strings.Contains(got, sourcecontrol.PublishedCredentialsMarker) {
		t.Fatalf("an unmodelled shape leaked:\n%s", got)
	}
	if !strings.Contains(got, "redacted") {
		t.Errorf("the replacement should say what happened: %s", got)
	}
}

func TestRedactDropsThePayloadWhenItCannotBeParsed(t *testing.T) {
	body := []byte("not json at all " + sourcecontrol.PublishedCredentialsMarker + " Aep1!SecretValue-123")
	if got := string(redactPublishedCredentials(body)); strings.Contains(got, "Aep1!SecretValue-123") {
		t.Fatalf("unparseable payload leaked:\n%s", got)
	}
}

// The needle and the marker must not drift apart: the writer anchors on the
// marker, this file matches on the needle, and a marker that stopped containing
// it would silently stop being redacted.
func TestNeedleIsPartOfTheMarker(t *testing.T) {
	if !strings.Contains(sourcecontrol.PublishedCredentialsMarker, credentialNeedle) {
		t.Fatalf("marker %q no longer contains the needle %q",
			sourcecontrol.PublishedCredentialsMarker, credentialNeedle)
	}
	// And the needle must survive Go's HTML-escaping encoder, which is the
	// spelling the marker arrives in after any re-encode.
	encoded, err := json.Marshal(sourcecontrol.PublishedCredentialsMarker)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), credentialNeedle) {
		t.Fatalf("the needle does not survive JSON encoding: %s", encoded)
	}
}

// The fast path must be byte-identical, not a re-encode: every other delivery
// this service receives goes through here, and re-serialising them all would
// silently rewrite payloads nothing asked us to touch.
func TestRedactLeavesAnOrdinaryPayloadExactlyAsItArrived(t *testing.T) {
	body := []byte(`{"action":"created","comment":{"body":"Starting validation: 3 criteria"},"z":1,"a":2}`)

	got := redactPublishedCredentials(body)

	if string(got) != string(body) {
		t.Fatalf("an unrelated payload was rewritten:\nwant %s\ngot  %s", body, got)
	}
}
