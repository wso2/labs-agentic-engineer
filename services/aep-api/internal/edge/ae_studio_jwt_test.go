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
// the default JWT_AUDIENCE, then the real tenant gate in ENFORCE.

const (
	studioIssuer   = "thunder-test"
	studioAudience = "aep-bff" // the JWT_AUDIENCE default (config_loader.go)
	studioKID      = "studio-kid"
)

type studioTokenClaims struct {
	Sub      string `json:"sub,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	OuHandle string `json:"ouHandle,omitempty"`
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
			JWTAllowedAudience: studioAudience,
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

// (a) A client_credentials token for aep-bff carries no org claim: it passes
// the verifier (right issuer, audience and signature) and the tenant gate
// refuses it.
func TestGetAeStudio_VerifiedCCTokenWithoutOrgIs401AtTheGate(t *testing.T) {
	h, sign := newVerifiedStudioStack(t)
	w := getAEStudioAs(h, sign(studioTokenClaims{
		Sub: "some-m2m-client", ClientID: "some-m2m-client",
		RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{studioAudience}},
	}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", w.Code, w.Body)
	}
	if verifierRejected(w) {
		t.Fatalf("the verifier refused a well-formed aep-bff token; the gate should be what refuses it: %s", w.Body)
	}
	if !strings.Contains(w.Body.String(), "authentication required") {
		t.Fatalf("want the tenant gate's refusal, got %s", w.Body)
	}
}

// (b) A publisher-style client_credentials token (org claim, but audience
// aep-publisher-<org>) is refused by the verifier: wrong audience.
func TestGetAeStudio_PublisherAudienceTokenIs401AtTheVerifier(t *testing.T) {
	h, sign := newVerifiedStudioStack(t)
	w := getAEStudioAs(h, sign(studioTokenClaims{
		Sub: "publisher-client", ClientID: "publisher-client", OuHandle: "acme",
		RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"aep-publisher-acme"}},
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
			Sub: sub, ClientID: "aep-console", OuHandle: org,
			RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{studioAudience}},
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

// A client_credentials token minted for aep-bff WITH an org claim is
// indistinguishable from a user token to this edge: the verifier checks
// signature, issuer and audience only, and the gate checks only the org claim.
// Whether /api/v1 must refuse it (e.g. on a grant-type or subject-kind claim)
// is an open verifier-semantics decision, so this pins the desired behaviour
// without changing the verifier.
func TestGetAeStudio_CCTokenWithOrgForBFFAudienceIsRefused(t *testing.T) {
	t.Skip("finding (Task 1.16b): the /api/v1 verifier admits a client_credentials token with aud=aep-bff and an ouHandle claim; " +
		"refusing it needs a verifier-semantics decision (no grant-type/subject-kind check exists today)")
	h, sign := newVerifiedStudioStack(t)
	w := getAEStudioAs(h, sign(studioTokenClaims{
		Sub: "some-m2m-client", ClientID: "some-m2m-client", OuHandle: "acme",
		RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{studioAudience}},
	}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", w.Code, w.Body)
	}
}
