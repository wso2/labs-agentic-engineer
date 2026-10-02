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
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/organization"
	orghttpapi "github.com/wso2/aep/aep-api/internal/organization/httpapi"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// GET /api/v1/ae-studio reads OpenChoreo with aep-api's own M2M identity, so
// the /api/v1 JWT verifier and tenant gate are the only checks on the read.
// These tests run that read through the PRODUCTION verifier (no InboundAuth
// seam): real RS256 tokens against a JWKS server, the configured issuer and
// the console client's audience (JWT_AUDIENCE), then the real tenant gate in
// ENFORCE. Tokens carry Thunder's real client ids and grant_type claims.

const (
	studioIssuer  = "thunder-test"
	consoleClient = "aep-console-client" // JWT_AUDIENCE: Thunder sets aud = client id
	publisherAcme = "aep-publisher-acme" // a publisher M2M client and its aud
	studioKID     = "studio-kid"
	grantUser     = "authorization_code"
	grantMachine  = "client_credentials"
	m2mEntityID   = "01a0f2c3-0000-7000-8000-00000000c11e" // Thunder's sub on an M2M token
)

type studioTokenClaims struct {
	Sub       string `json:"sub,omitempty"`
	ClientID  string `json:"client_id,omitempty"`
	GrantType string `json:"grant_type,omitempty"`
	OuHandle  string `json:"ouHandle,omitempty"`
	jwt.RegisteredClaims
}

// newVerifiedStudioStack builds the real /api/v1 chain (NewHandler with a
// ThunderJWKS and no InboundAuth) and a signer for tokens it trusts.
func newVerifiedStudioStack(t *testing.T) (http.Handler, func(studioTokenClaims) string) {
	t.Helper()
	priv := mustGenerateRSAKey(t)
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwtassertion.JWKS{Keys: []jwtassertion.JSONWebKey{{
			Kty: "RSA", Kid: studioKID, Use: "sig", Alg: "RS256",
			N: base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
		}}})
	}))
	t.Cleanup(jwks.Close)

	orgs, err := orghttpapi.New(organization.Deps{AEStudio: studioByOrg{
		"acme":   {State: organization.AEStudioReady, URLs: &organization.AEStudioURLs{DesignAgent: "http://acme-d", Collab: "ws://acme-c", Tools: "http://acme-t"}},
		"globex": {State: organization.AEStudioProvisioning},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(AppParams{
		Config: config.Config{
			JWTAllowedIssuer:   studioIssuer,
			JWTAllowedAudience: consoleClient,
			TenantGateMode:     "enforce",
		},
		Deps:        Deps{Organization: orgs},
		ThunderJWKS: jwtassertion.NewJWKSCache(jwks.URL),
	})

	sign := func(c studioTokenClaims) string {
		t.Helper()
		if c.Issuer == "" {
			c.Issuer = studioIssuer
		}
		c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
		tok.Header["kid"] = studioKID
		signed, err := tok.SignedString(priv)
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		return signed
	}
	return h, sign
}

func getAEStudioAs(h http.Handler, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/ae-studio", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// verifierRejected reports whether the 401 came from the JWT verifier (its
// RFC 9728 invalid_token challenge) rather than the tenant gate.
func verifierRejected(w *httptest.ResponseRecorder) bool {
	return strings.Contains(w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
}

// (a) A client_credentials token with the console audience and no org claim
// is refused by the verifier: /api/v1 takes user tokens only.
func TestGetAeStudio_CCTokenWithoutOrgIs401AtTheVerifier(t *testing.T) {
	h, sign := newVerifiedStudioStack(t)
	w := getAEStudioAs(h, sign(studioTokenClaims{
		Sub: m2mEntityID, ClientID: consoleClient, GrantType: grantMachine,
		RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{consoleClient}},
	}))
	if w.Code != http.StatusUnauthorized || !verifierRejected(w) {
		t.Fatalf("status = %d challenge = %q, want the verifier's 401 (%s)", w.Code, w.Header().Get("WWW-Authenticate"), w.Body)
	}
}

// A verified user token with no org claim passes the verifier and the tenant
// gate refuses it, so the gate still backs the verifier.
func TestGetAeStudio_UserTokenWithoutOrgIs401AtTheGate(t *testing.T) {
	h, sign := newVerifiedStudioStack(t)
	w := getAEStudioAs(h, sign(studioTokenClaims{
		Sub: "alice", ClientID: consoleClient, GrantType: grantUser,
		RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{consoleClient}},
	}))
	if w.Code != http.StatusUnauthorized || verifierRejected(w) || !strings.Contains(w.Body.String(), "authentication required") {
		t.Fatalf("status = %d challenge = %q, want the tenant gate's 401 (%s)", w.Code, w.Header().Get("WWW-Authenticate"), w.Body)
	}
}

// (b) A publisher-style client_credentials token (org claim, but audience
// aep-publisher-<org>) is refused by the verifier: wrong audience.
func TestGetAeStudio_PublisherAudienceTokenIs401AtTheVerifier(t *testing.T) {
	h, sign := newVerifiedStudioStack(t)
	w := getAEStudioAs(h, sign(studioTokenClaims{
		Sub: m2mEntityID, ClientID: publisherAcme, GrantType: grantMachine, OuHandle: "acme",
		RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{publisherAcme}},
	}))
	if w.Code != http.StatusUnauthorized || !verifierRejected(w) {
		t.Fatalf("status = %d challenge = %q, want the verifier's 401 (%s)", w.Code, w.Header().Get("WWW-Authenticate"), w.Body)
	}
	if strings.Contains(w.Body.String(), "acme-d") {
		t.Fatalf("leaked acme's AE Studio: %s", w.Body)
	}
}

// (c) + (d) A user token reads its own org's AE Studio, and only that org's.
func TestGetAeStudio_VerifiedUserTokenReadsOnlyItsOrg(t *testing.T) {
	h, sign := newVerifiedStudioStack(t)
	user := func(sub, org string) string {
		return sign(studioTokenClaims{
			Sub: sub, ClientID: consoleClient, GrantType: grantUser, OuHandle: org,
			RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{consoleClient}},
		})
	}

	acme := getAEStudioAs(h, user("alice", "acme"))
	if acme.Code != http.StatusOK || !strings.Contains(acme.Body.String(), `"state":"ready"`) || !strings.Contains(acme.Body.String(), "http://acme-d") {
		t.Fatalf("acme user: %d %s", acme.Code, acme.Body)
	}

	globex := getAEStudioAs(h, user("bob", "globex"))
	if globex.Code != http.StatusOK || !strings.Contains(globex.Body.String(), `"state":"provisioning"`) {
		t.Fatalf("globex user: %d %s", globex.Code, globex.Body)
	}
	if strings.Contains(globex.Body.String(), "acme") || strings.Contains(globex.Body.String(), "ready") {
		t.Fatalf("leaked acme's AE Studio to a globex user: %s", globex.Body)
	}
}

// A client_credentials token with the console audience AND an org claim is
// refused by the verifier: an org claim never makes a machine an org member.
func TestGetAeStudio_CCTokenWithOrgForBFFAudienceIsRefused(t *testing.T) {
	h, sign := newVerifiedStudioStack(t)
	w := getAEStudioAs(h, sign(studioTokenClaims{
		Sub: m2mEntityID, ClientID: consoleClient, GrantType: grantMachine, OuHandle: "acme",
		RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{consoleClient}},
	}))
	if w.Code != http.StatusUnauthorized || !verifierRejected(w) {
		t.Fatalf("status = %d challenge = %q, want the verifier's 401 (%s)", w.Code, w.Header().Get("WWW-Authenticate"), w.Body)
	}
	if strings.Contains(w.Body.String(), "acme-d") {
		t.Fatalf("leaked acme's AE Studio: %s", w.Body)
	}
}
