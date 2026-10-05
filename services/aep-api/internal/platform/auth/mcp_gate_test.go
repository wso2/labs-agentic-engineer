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

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// mcpOrgEcho writes the org PublisherMCPGate bound, so a test can assert which
// org reached the handler.
func mcpOrgEcho() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org, ok := MCPOrgFromContext(r.Context())
		if !ok {
			http.Error(w, "no org in context", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(org))
	})
}

func serveMCPGate(v *PublisherTokenVerifier, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/mcp?orgHandle=attacker-org", nil)
	req.Header.Set("X-Oc-Org-Id", "attacker-org")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	PublisherMCPGate(v, mcpOrgEcho()).ServeHTTP(w, req)
	return w
}

// A publisher token reaches next with the org from the verified token; an org
// planted in the query or a header is ignored.
func TestPublisherMCPGate_BindsTokenOrg(t *testing.T) {
	v, mint := newPublisherVerifier(t)
	w := serveMCPGate(v, "Bearer "+mint("claim-org", "claim-org"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "claim-org" {
		t.Fatalf("bound org = %q, want claim-org", got)
	}
}

// Every rejection is a 401 that never reaches next.
func TestPublisherMCPGate_401Matrix(t *testing.T) {
	v, mint := newPublisherVerifier(t)
	cases := map[string]struct {
		v      *PublisherTokenVerifier
		header string
	}{
		"missing":           {v, ""},
		"not-bearer":        {v, "Basic abc"},
		"garbage":           {v, "Bearer not-a-jwt"},
		"ouHandle-mismatch": {v, "Bearer " + mint("org-a", "org-b")},
		"nil-verifier":      {nil, "Bearer " + mint("org-a", "org-a")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if w := serveMCPGate(tc.v, tc.header); w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %q)", w.Code, w.Body.String())
			}
		})
	}
}
