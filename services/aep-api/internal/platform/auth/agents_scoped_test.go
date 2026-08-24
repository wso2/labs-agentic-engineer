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
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// mcpTestManager returns a ready TaskTokenManager over a fresh PKCS#1 key.
func mcpTestManager(t *testing.T) *TaskTokenManager {
	t.Helper()
	pemKey, _ := writeTestKey(t, "pkcs1")
	mgr, err := NewTaskTokenManager(TaskTokenConfig{
		PrivateKey: pemKey,
		Issuer:     "aep-bff",
		Audience:   "git-service", // manager default aud; IssueMCPToken overrides it
		TTL:        1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewTaskTokenManager: %v", err)
	}
	return mgr
}

// orgEchoHandler is a next handler that writes the MCP-bound org to the body, so
// a test can assert which org the verifier resolved onto the context.
func orgEchoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org, ok := MCPOrgFromContext(r.Context())
		if !ok {
			http.Error(w, "no org in context", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(org))
	})
}

// serveWith runs the verifier middleware over orgEchoHandler for one request and
// returns the recorder.
func serveWith(v *AgentsScopedVerifier, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	v.Middleware(orgEchoHandler()).ServeHTTP(w, req)
	return w
}

// TestAgentsScoped_401Matrix covers every rejection path: missing token, garbage
// token, wrong-audience token, and expired token — all 401, none reaching next.
func TestAgentsScoped_401Matrix(t *testing.T) {
	mgr := mcpTestManager(t)
	v := NewAgentsScopedVerifier(mgr, nil)

	// wrong-aud: a validly-signed BFF token minted for agents-service, not MCP.
	wrongAud, err := mgr.IssueServiceToken("agents-service", "org-x", 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueServiceToken: %v", err)
	}

	// org-less: right audience, validly signed, but no ocOrgId claim. Every MCP
	// tool is org-scoped, so the verifier must fail closed rather than bind "".
	orgless, err := mgr.IssueServiceToken(AudienceMCP, "", 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueServiceToken (org-less): %v", err)
	}

	// expired: manually crafted with a past exp, signed by the manager's key.
	expClaims := TaskClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    mgr.issuer,
			Audience:  jwt.ClaimStrings{AudienceMCP},
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
		},
		OcOrgID: "org-x",
	}
	expTok := jwt.NewWithClaims(jwt.SigningMethodRS256, expClaims)
	expTok.Header["kid"] = mgr.keyID
	expired, err := expTok.SignedString(mgr.privateKey)
	if err != nil {
		t.Fatalf("sign expired: %v", err)
	}

	cases := []struct {
		name   string
		header string
	}{
		{"missing", ""},
		{"not-bearer", "Basic abc"},
		{"garbage", "Bearer not-a-jwt"},
		{"wrong-aud", "Bearer " + wrongAud},
		{"expired", "Bearer " + expired},
		{"org-less", "Bearer " + orgless},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/internal/v1/mcp", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			w := serveWith(v, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d (body %q)", w.Code, w.Body.String())
			}
		})
	}
}

// TestAgentsScoped_ValidTokenBindsOrg proves a valid MCP token reaches next with
// the org from its ocOrgId claim.
func TestAgentsScoped_ValidTokenBindsOrg(t *testing.T) {
	mgr := mcpTestManager(t)
	v := NewAgentsScopedVerifier(mgr, nil)

	tok, err := mgr.IssueMCPToken("claim-org")
	if err != nil {
		t.Fatalf("IssueMCPToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	w := serveWith(v, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %q)", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "claim-org" {
		t.Fatalf("bound org = %q, want claim-org", got)
	}
}

// TestAgentsScoped_ClaimWinsOverRequestOrg proves the resolved org comes SOLELY
// from the signed claim: a mismatched org in the path/query/header is ignored.
func TestAgentsScoped_ClaimWinsOverRequestOrg(t *testing.T) {
	mgr := mcpTestManager(t)
	v := NewAgentsScopedVerifier(mgr, nil)

	tok, err := mgr.IssueMCPToken("claim-org")
	if err != nil {
		t.Fatalf("IssueMCPToken: %v", err)
	}
	// Attacker-controlled org planted everywhere the source used to trust it.
	req := httptest.NewRequest(http.MethodPost, "/internal/organizations/attacker-org/mcp?orgHandle=attacker-org", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Oc-Org-Id", "attacker-org")
	req.SetPathValue("orgHandle", "attacker-org")

	w := serveWith(v, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %q)", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "claim-org" {
		t.Fatalf("resolved org = %q, want claim-org (claim must win over request-supplied org)", got)
	}
}

// TestAgentsScoped_NilManager503 asserts an unconfigured verifier fails closed.
func TestAgentsScoped_NilManager503(t *testing.T) {
	v := NewAgentsScopedVerifier(nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/mcp", nil)
	w := serveWith(v, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestAgentsScoped_PublisherCCBindsOrg(t *testing.T) {
	mgr := mcpTestManager(t)
	pub, mint := newPublisherVerifier(t)
	v := NewAgentsScopedVerifier(mgr, pub)

	tok := mint("claim-org", "claim-org")
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	w := serveWith(v, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %q)", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "claim-org" {
		t.Fatalf("bound org = %q, want claim-org", got)
	}
}

func TestAgentsScoped_BFFMCPStillWorksWithPublisherWired(t *testing.T) {
	mgr := mcpTestManager(t)
	pub, _ := newPublisherVerifier(t)
	v := NewAgentsScopedVerifier(mgr, pub)

	tok, err := mgr.IssueMCPToken("bff-org")
	if err != nil {
		t.Fatalf("IssueMCPToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	w := serveWith(v, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %q)", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "bff-org" {
		t.Fatalf("bound org = %q, want bff-org", got)
	}
}

func TestAgentsScoped_TaskJWT401EvenWithPublisherWired(t *testing.T) {
	mgr := mcpTestManager(t)
	pub, _ := newPublisherVerifier(t)
	v := NewAgentsScopedVerifier(mgr, pub)

	tok, err := mgr.IssueServiceToken("git-service", "org-x", 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueServiceToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	w := serveWith(v, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (body %q)", w.Code, w.Body.String())
	}
}

func TestAgentsScoped_PublisherToken401WhenPublisherNil(t *testing.T) {
	mgr := mcpTestManager(t)
	_, mint := newPublisherVerifier(t)
	v := NewAgentsScopedVerifier(mgr, nil)

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+mint("claim-org", "claim-org"))

	w := serveWith(v, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (body %q)", w.Code, w.Body.String())
	}
}

// Both BFF and publisher verify fail: joined error for the log, HTTP still 401
// with a generic body.
func TestAgentsScoped_BothVerifyFailStill401(t *testing.T) {
	mgr := mcpTestManager(t)
	pub, _ := newPublisherVerifier(t)
	v := NewAgentsScopedVerifier(mgr, pub)

	_, err := v.resolveOrg("Bearer not-a-jwt")
	if err == nil {
		t.Fatal("expected resolveOrg error")
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok || len(joined.Unwrap()) != 2 {
		t.Fatalf("want errors.Join of 2, got %T %v", err, err)
	}

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/mcp", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	w := serveWith(v, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (body %q)", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != "unauthorized" {
		t.Fatalf("body = %q, want unauthorized", got)
	}
}
