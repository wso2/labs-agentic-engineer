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
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// scopeEchoHandler writes the bound issues scope as "org/project".
func scopeEchoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := IssuesMCPScopeFromContext(r.Context())
		if !ok {
			http.Error(w, "no scope in context", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(scope.OrgID + "/" + scope.ProjectID))
	})
}

func serveIssues(v *IssuesMCPVerifier, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/issues/mcp", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	w := httptest.NewRecorder()
	v.Middleware(scopeEchoHandler()).ServeHTTP(w, req)
	return w
}

func TestIssuesMCP_TokenCarriesAudienceAndProject(t *testing.T) {
	mgr := mcpTestManager(t)
	tok, err := mgr.IssueIssuesMCPToken("acme", "acme-expenses")
	if err != nil {
		t.Fatalf("IssueIssuesMCPToken: %v", err)
	}
	claims, err := mgr.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !hasAudience(claims.Audience, AudienceIssuesMCP) || len(claims.Audience) != 1 {
		t.Errorf("aud = %v, want [%s]", claims.Audience, AudienceIssuesMCP)
	}
	if claims.OcOrgID != "acme" || claims.ProjectID != "acme-expenses" {
		t.Errorf("claims org/project = %q/%q", claims.OcOrgID, claims.ProjectID)
	}
	ttl := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
	if ttl != mcpTokenTTL {
		t.Errorf("ttl = %v, want %v", ttl, mcpTokenTTL)
	}
}

func TestIssuesMCP_ValidTokenBindsScope(t *testing.T) {
	mgr := mcpTestManager(t)
	tok, err := mgr.IssueIssuesMCPToken("acme", "acme-expenses")
	if err != nil {
		t.Fatalf("IssueIssuesMCPToken: %v", err)
	}
	w := serveIssues(NewIssuesMCPVerifier(mgr), "Bearer "+tok)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "acme/acme-expenses" {
		t.Errorf("bound scope = %q, want acme/acme-expenses", got)
	}
}

func TestIssuesMCP_401Matrix(t *testing.T) {
	mgr := mcpTestManager(t)
	discovery, err := mgr.IssueMCPToken("acme")
	if err != nil {
		t.Fatalf("IssueMCPToken: %v", err)
	}
	// Right audience + org, but no project claim (IssueServiceToken never sets one).
	projectless, err := mgr.IssueServiceToken(AudienceIssuesMCP, "acme", 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueServiceToken: %v", err)
	}
	orgless, err := mgr.IssueIssuesMCPToken("", "acme-expenses")
	if err != nil {
		t.Fatalf("IssueIssuesMCPToken (org-less): %v", err)
	}
	expClaims := TaskClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    mgr.issuer,
			Audience:  jwt.ClaimStrings{AudienceIssuesMCP},
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
		},
		OcOrgID:   "acme",
		ProjectID: "acme-expenses",
	}
	expTok := jwt.NewWithClaims(jwt.SigningMethodRS256, expClaims)
	expTok.Header["kid"] = mgr.keyID
	expired, err := expTok.SignedString(mgr.privateKey)
	if err != nil {
		t.Fatalf("sign expired: %v", err)
	}

	cases := map[string]string{
		"missing":         "",
		"not-bearer":      "Basic abc",
		"garbage":         "Bearer not-a-jwt",
		"discovery-token": "Bearer " + discovery,
		"project-less":    "Bearer " + projectless,
		"org-less":        "Bearer " + orgless,
		"expired":         "Bearer " + expired,
	}
	v := NewIssuesMCPVerifier(mgr)
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			w := serveIssues(v, header)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401 (body %q)", w.Code, w.Body.String())
			}
		})
	}
}

func TestIssuesMCP_NilManager503(t *testing.T) {
	w := serveIssues(NewIssuesMCPVerifier(nil), "Bearer x")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// An issues token must not open the org-wide discovery surface, even with the
// publisher fallback wired.
func TestIssuesMCP_TokenRejectedByAgentsScopedVerifier(t *testing.T) {
	mgr := mcpTestManager(t)
	tok, err := mgr.IssueIssuesMCPToken("acme", "acme-expenses")
	if err != nil {
		t.Fatalf("IssueIssuesMCPToken: %v", err)
	}
	pub, _ := newPublisherVerifier(t)
	for name, v := range map[string]*AgentsScopedVerifier{
		"bff-only":       NewAgentsScopedVerifier(mgr, nil),
		"with-publisher": NewAgentsScopedVerifier(mgr, pub),
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/internal/v1/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			if w := serveWith(v, req); w.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", w.Code)
			}
		})
	}
}
