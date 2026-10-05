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

package edge

import (
	"bytes"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// Component test for the playground-token mint route (POST
// /internal/v1/mcp/playground-token): the real mounted mux (NewHandler →
// mountRoutes) over a real TaskTokenManager. Proves the route is entirely
// ABSENT (404 by omission) unless Config.PlaygroundTokenEnabled is set; when
// enabled, the minted token verifies against the mint's key with audience
// aep-api-mcp and the right org, both the "default" default and an explicit
// orgHandle; and the MCP JSON-RPC mount itself behaves identically regardless
// of the flag. /internal/v1/mcp no longer accepts these tokens (publisher
// client only); the mint goes with token minting (Task 5.4).

// newPlaygroundTokenTestServer returns the server and the public half of the
// mint's signing key.
func newPlaygroundTokenTestServer(t *testing.T, enabled bool) (*httptest.Server, *rsa.PublicKey) {
	t.Helper()
	priv := mustGenerateRSAKey(t)
	mgr, err := auth.NewTaskTokenManager(auth.TaskTokenConfig{
		PrivateKey: string(encodePKCS1(t, priv)),
		Issuer:     "aep-bff",
		Audience:   "git-service",
		TTL:        time.Hour,
	})
	if err != nil {
		t.Fatalf("NewTaskTokenManager: %v", err)
	}
	handler := NewHandler(AppParams{
		Config: config.Config{PlaygroundTokenEnabled: enabled},
		Deps:   Deps{TaskTokens: mgr, PublisherTokens: newMCPIdP(t).verifier},
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, &priv.PublicKey
}

func postPlaygroundToken(t *testing.T, srv *httptest.Server, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/internal/v1/mcp/playground-token", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST playground-token: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// assertVerifiesAsMCPToken checks token's signature against the mint's key and
// asserts it is an aud-aep-api-mcp identity JWT scoped to wantOrg, not just
// any signed blob.
func assertVerifiesAsMCPToken(t *testing.T, pub *rsa.PublicKey, token, wantOrg string) {
	t.Helper()
	var claims auth.TaskClaims
	if _, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return pub, nil },
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("aep-bff")); err != nil {
		t.Fatalf("token does not verify: %v", err)
	}
	if !slices.Contains(claims.Audience, auth.AudienceMCP) {
		t.Fatalf("token audience = %v, want %q", claims.Audience, auth.AudienceMCP)
	}
	if claims.OcOrgID != wantOrg {
		t.Fatalf("token org = %q, want %q", claims.OcOrgID, wantOrg)
	}
}

// TestPlaygroundToken_DisabledByDefault404 proves the route is absent (not
// merely unauthorized) when the flag is off — the zero-value posture every
// non-compose environment gets.
func TestPlaygroundToken_DisabledByDefault404(t *testing.T) {
	srv, _ := newPlaygroundTokenTestServer(t, false)
	resp := postPlaygroundToken(t, srv, `{}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (route must not be mounted when disabled)", resp.StatusCode)
	}
}

// TestPlaygroundToken_Enabled_DefaultOrg proves the happy path with an empty
// body: 200, a non-empty token that verifies for org "default", and the
// documented 5-minute (300s) TTL in the response.
func TestPlaygroundToken_Enabled_DefaultOrg(t *testing.T) {
	srv, pub := newPlaygroundTokenTestServer(t, true)
	resp := postPlaygroundToken(t, srv, `{}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Token            string `json:"token"`
		ExpiresInSeconds int    `json:"expiresInSeconds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Token == "" {
		t.Fatalf("token is empty")
	}
	if body.ExpiresInSeconds != 300 {
		t.Errorf("expiresInSeconds = %d, want 300", body.ExpiresInSeconds)
	}
	assertVerifiesAsMCPToken(t, pub, body.Token, "default")
}

// TestPlaygroundToken_Enabled_ExplicitOrgHandle proves an explicit orgHandle
// in the body is what the minted token gets scoped to.
func TestPlaygroundToken_Enabled_ExplicitOrgHandle(t *testing.T) {
	srv, pub := newPlaygroundTokenTestServer(t, true)
	resp := postPlaygroundToken(t, srv, `{"orgHandle":"acme"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertVerifiesAsMCPToken(t, pub, body.Token, "acme")
}

// TestPlaygroundToken_MCPMountUnaffectedByFlag proves the JSON-RPC mount's own
// auth posture (401 with no bearer) is identical whether the playground-token
// flag is on or off — the new route is additive, not a side effect.
func TestPlaygroundToken_MCPMountUnaffectedByFlag(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		srv, _ := newPlaygroundTokenTestServer(t, enabled)
		resp, err := http.Post(srv.URL+"/internal/v1/mcp", "application/json",
			bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)))
		if err != nil {
			t.Fatalf("POST mcp: %v", err)
		}
		resp.Body.Close() //nolint:errcheck
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("enabled=%v: mcp mount status = %d, want 401 (no bearer) regardless of the playground-token flag", enabled, resp.StatusCode)
		}
	}
}
