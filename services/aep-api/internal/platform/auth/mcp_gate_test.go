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
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// mcpOrgEcho writes the org MCPGate bound, so a test can assert which org
// reached the handler.
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

// mcpGateStack is both MCP verifiers over one Thunder test key, and a signer
// for any (aud, ouHandle) client_credentials token under that key.
type mcpGateStack struct {
	publisher *PublisherTokenVerifier
	studio    *StudioClientVerifier
	sign      func(aud, ouHandle string) string
}

func newMCPGateStack(t *testing.T, clients StudioClientLookup) mcpGateStack {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "mcp-kid"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwtassertion.JWKS{Keys: []jwtassertion.JSONWebKey{{
			Kty: "RSA", Kid: kid, Use: "sig", Alg: "RS256",
			N: base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	jwks := jwtassertion.NewJWKSCache(srv.URL)
	s := mcpGateStack{
		publisher: NewPublisherTokenVerifier(jwks, pubIssuer, pubAudPrefix),
		studio:    NewStudioClientVerifier(jwks, pubIssuer, clients),
	}
	if s.publisher == nil || s.studio == nil {
		t.Fatal("a verifier is nil")
	}
	s.sign = func(aud, ouHandle string) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, PublisherClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    pubIssuer,
				Audience:  jwt.ClaimStrings{aud},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			OuHandle: ouHandle,
		})
		tok.Header["kid"] = kid
		signed, err := tok.SignedString(priv)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		return signed
	}
	return s
}

// serveMCPGate sends authHeader through MCPGate with an attacker org planted
// in the query and a header, and records the answer.
func serveMCPGate(publisher *PublisherTokenVerifier, studio *StudioClientVerifier, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/mcp?orgHandle=attacker-org", nil)
	req.Header.Set("X-Oc-Org-Id", "attacker-org")
	req.Header.Set("X-Impersonate-Org", "attacker-org")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	MCPGate(publisher, studio, mcpOrgEcho()).ServeHTTP(w, req)
	return w
}

// The coding runner's publisher token and the AE Studio tools pod's
// ae-studio-<org> client token each reach next with the org their verifier
// bound; an org planted in the query or a header is ignored.
func TestMCPGate_BindsTheVerifiedOrg(t *testing.T) {
	s := newMCPGateStack(t, storedStudioClients{"acme": "ae-studio-acme"})
	for name, tc := range map[string]struct{ token, org string }{
		"publisher token":                  {s.sign("aep-publisher-claim-org", "claim-org"), "claim-org"},
		"ae-studio client token, recorded": {s.sign("ae-studio-acme", "acme"), "acme"},
	} {
		t.Run(name, func(t *testing.T) {
			w := serveMCPGate(s.publisher, s.studio, "Bearer "+tc.token)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
			}
			if got := w.Body.String(); got != tc.org {
				t.Fatalf("bound org = %q, want %q", got, tc.org)
			}
		})
	}
}

// Every refusal is the same generic 401 that never reaches next: an
// ae-studio token for an org it is not recorded for answers exactly like
// garbage, so the gate is no oracle for which orgs have a client.
func TestMCPGate_401Matrix(t *testing.T) {
	s := newMCPGateStack(t, storedStudioClients{"acme": "ae-studio-acme", "rotated": "ae-studio-rotated-2"})
	cases := map[string]struct {
		publisher *PublisherTokenVerifier
		studio    *StudioClientVerifier
		header    string
	}{
		"missing":                                {s.publisher, s.studio, ""},
		"not-bearer":                             {s.publisher, s.studio, "Basic abc"},
		"garbage":                                {s.publisher, s.studio, "Bearer not-a-jwt"},
		"publisher ouHandle mismatch":            {s.publisher, s.studio, "Bearer " + s.sign("aep-publisher-org-a", "org-b")},
		"ae-studio audience naming another org":  {s.publisher, s.studio, "Bearer " + s.sign("ae-studio-acme", "evil")},
		"ae-studio audience without an org":      {s.publisher, s.studio, "Bearer " + s.sign("ae-studio-acme", "")},
		"ae-studio client of an org with none":   {s.publisher, s.studio, "Bearer " + s.sign("ae-studio-initech", "initech")},
		"ae-studio client not the recorded one":  {s.publisher, s.studio, "Bearer " + s.sign("ae-studio-rotated", "rotated")},
		"AE-only client (no org claim)":          {s.publisher, s.studio, "Bearer " + s.sign("ae-studio-internal-client", "")},
		"user token":                             {s.publisher, s.studio, "Bearer " + s.sign("aep-console", "acme")},
		"publisher token, no publisher verifier": {nil, s.studio, "Bearer " + s.sign("aep-publisher-acme", "acme")},
		"ae-studio token, no studio verifier":    {s.publisher, nil, "Bearer " + s.sign("ae-studio-acme", "acme")},
		"no verifier at all":                     {nil, nil, "Bearer " + s.sign("aep-publisher-acme", "acme")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := serveMCPGate(tc.publisher, tc.studio, tc.header)
			if w.Code != http.StatusUnauthorized || w.Body.String() != "unauthorized\n" {
				t.Fatalf("status = %d body %q, want 401 unauthorized", w.Code, w.Body.String())
			}
		})
	}
}

// A recorded client that cannot be read is no verdict on the caller: 503,
// as on the ae-studio/ ops, and next never runs.
func TestMCPGate_StudioClientLookupFailureIs503(t *testing.T) {
	s := newMCPGateStack(t, storedStudioClients{"!err": "db down"})
	w := serveMCPGate(s.publisher, s.studio, "Bearer "+s.sign("ae-studio-acme", "acme"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %q)", w.Code, w.Body.String())
	}
}

// MCP is the only route an ae-studio client token shares with the coding
// runner: the runner ops (runs/, the validation context) keep taking the
// publisher token alone.
func TestStudioClientToken_NeverClearsARunnerOp(t *testing.T) {
	s := newMCPGateStack(t, storedStudioClients{"acme": "ae-studio-acme"})
	a := NewRunnerAuthorizer(s.publisher, func(context.Context, string) (RunnerCycle, error) {
		t.Fatal("the cycle lookup must not run for an ae-studio client token")
		return RunnerCycle{}, nil
	})
	_, err := a.Authorize(context.Background(), "Bearer "+s.sign("ae-studio-acme", "acme"), "cycle-1")
	if statusOf(err) != http.StatusUnauthorized {
		t.Fatalf("err = %v, want a 401", err)
	}
}
