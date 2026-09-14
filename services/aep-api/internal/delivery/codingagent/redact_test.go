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

package codingagent

import (
	"strings"
	"testing"
)

// A synthetic token in the shape that leaked into the console build log.
// Never use a real credential (or a prefix of one) as a fixture.
const leakedPAT = "github_pat_11TESTONLY_abcdefghijklmnopqrstuvwxyz0123456789"

func TestRedactSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		// want is the exact output when non-empty; otherwise the assertions
		// below (secret absent, keep present) carry the case.
		want   string
		secret string
		keep   string
	}{
		{
			name:   "clone command with URL userinfo",
			in:     "Command failed: git clone 'https://x-access-token:" + leakedPAT + "@github.com/asdlc-repos/store.git' '/home/aep/ws'",
			want:   "Command failed: git clone 'https://x-access-token:[REDACTED]@github.com/asdlc-repos/store.git' '/home/aep/ws'",
			secret: leakedPAT,
		},
		{
			name:   "bare fine-grained PAT",
			in:     "token=" + leakedPAT + " expired",
			want:   "token=[REDACTED] expired",
			secret: leakedPAT,
		},
		{
			// gh writes the token at rest in the agent's own workspace as
			// `oauth_token: <token>` on the credhelper path, and that token is
			// minted inside bash so the runner cannot enroll its literal — shape
			// here is the only layer that sees it.
			name:   "gh hosts.yml oauth_token",
			in:     "    oauth_token: aQ7fL2mZ9xR4tY6uP1sD3gH5jK8nB0vC",
			want:   "    oauth_token: [REDACTED]",
			secret: "aQ7fL2mZ9xR4tY6uP1sD3gH5jK8nB0vC",
		},
		{
			name: "oauth_token key with no value is left alone",
			in:   "oauth_token:",
			want: "oauth_token:",
		},
		{
			name:   "installation token",
			in:     "using ghs_abcdefghijklmnopqrstuvwxyz0123456789 for push",
			want:   "using [REDACTED] for push",
			secret: "ghs_abcdefghijklmnopqrstuvwxyz0123456789",
		},
		{
			name:   "authorization bearer header",
			in:     `curl -H "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.signature123456" ok`,
			secret: "eyJhbGciOiJIUzI1NiJ9.payload.signature123456",
			keep:   "Authorization: ",
		},
		{
			name:   "x-api-key header",
			in:     "x-api-key: sk-ant-0123456789abcdefghijklmn",
			secret: "sk-ant-0123456789abcdefghijklmn",
			keep:   "x-api-key: ",
		},
		{
			name: "host:port URL is not credential-shaped",
			in:   "posting to http://host.k3d.internal:8080/internal/v1/executions",
			want: "posting to http://host.k3d.internal:8080/internal/v1/executions",
		},
		{
			name: "ordinary progress line untouched",
			in:   "[oneshot] materialised 3 skill(s); preload=1 org skill(s)",
			want: "[oneshot] materialised 3 skill(s); preload=1 org skill(s)",
		},
		{
			name: "plain clone URL untouched",
			in:   "fatal: unable to access 'https://github.com/asdlc-repos/store.git/': Could not resolve host: github.com",
			want: "fatal: unable to access 'https://github.com/asdlc-repos/store.git/': Could not resolve host: github.com",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := redactSecrets(tc.in)
			if tc.want != "" && got != tc.want {
				t.Errorf("redactSecrets()\n got: %s\nwant: %s", got, tc.want)
			}
			if tc.secret != "" && strings.Contains(got, tc.secret) {
				t.Errorf("secret survived redaction: %s", got)
			}
			if tc.keep != "" && !strings.Contains(got, tc.keep) {
				t.Errorf("redaction ate context %q: %s", tc.keep, got)
			}
		})
	}
}

// TestTextToProgressEventsRedacts pins the console-feed boundary: a leaking
// line reaching the BFF must not reach the UI as-is.
func TestTextToProgressEventsRedacts(t *testing.T) {
	t.Parallel()

	raw := "[oneshot] provisionWorkspace failed: Command failed: git clone " +
		"'https://x-access-token:" + leakedPAT + "@github.com/asdlc-repos/store.git' '/home/aep/ws'"

	events, _ := textToProgressEvents(raw)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Kind != "log" {
		t.Errorf("kind = %q, want log", events[0].Kind)
	}
	if strings.Contains(events[0].Summary, leakedPAT) {
		t.Errorf("progress event leaked the token: %s", events[0].Summary)
	}
	if !strings.Contains(events[0].Summary, "provisionWorkspace failed") {
		t.Errorf("redaction destroyed the diagnostic: %s", events[0].Summary)
	}
}
