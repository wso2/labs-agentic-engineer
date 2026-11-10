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
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/aep-api/internal/platform/auth/dialect"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// The tokens here carry the claim set a real Asgardeo access token does
// (claim names and types recorded from a test organization; every value is
// made up): the org in org_handle / org_id / org_name, aut for who the token
// was issued to, and aud as a single string.
const (
	asgIssuer     = "https://api.asgardeo.io/t/acme/oauth2/token"
	asgOrgID      = "22222222-2222-2222-2222-222222222222"
	asgConsoleApp = "aep-console-client"
)

var asgardeoDialect = dialect.ForKind(dialect.KindAsgardeo)

// asgardeoUserClaims is a signed-in user's access token from the console app,
// with the attributes the app lists under Access Token Attributes. Asgardeo
// sends no name claim.
func asgardeoUserClaims() jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"sub": "7c1f0c52-0d6a-4a8e-9a55-3f3c1a2b4d5e", "aut": "APPLICATION_USER",
		"iss": asgIssuer, "aud": asgConsoleApp, "azp": asgConsoleApp, "client_id": asgConsoleApp,
		"org_id": asgOrgID, "org_name": "Acme Inc", "org_handle": "acme",
		"scope": "email openid profile", "binding_type": "sso-session", "binding_ref": "b1",
		"email": "ada@acme.example", "given_name": "Ada", "family_name": "Lovelace",
		"username": "ada@acme.example", "groups": []string{"ae-users"},
		"exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "nbf": now.Unix(), "jti": "j1",
	}
}

// asgardeoMachineClaims is a client_credentials token of the app clientID:
// sub, aud, azp and client_id all hold the client id.
func asgardeoMachineClaims(clientID string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"sub": clientID, "aud": clientID, "azp": clientID, "client_id": clientID, "aut": "APPLICATION",
		"iss": asgIssuer, "org_id": asgOrgID, "org_name": "Acme Inc", "org_handle": "acme",
		"exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "nbf": now.Unix(), "jti": "j2",
	}
}

// with returns claims with the given changes; a nil value drops the claim.
func with(claims jwt.MapClaims, changes map[string]any) jwt.MapClaims {
	for k, v := range changes {
		if v == nil {
			delete(claims, k)
			continue
		}
		claims[k] = v
	}
	return claims
}

// asgardeoSigner serves a JWKS for a fresh key and signs claims with it.
func asgardeoSigner(t *testing.T) (*jwtassertion.JWKSCache, func(jwt.MapClaims) string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "asg-kid"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwtassertion.JWKS{Keys: []jwtassertion.JSONWebKey{{
			Kty: "RSA", Kid: kid, Use: "sig", Alg: "RS256",
			N: base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	sign := func(claims jwt.MapClaims) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = kid
		tok.Header["typ"] = "at+jwt"
		signed, err := tok.SignedString(priv)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		return signed
	}
	return jwtassertion.NewJWKSCache(srv.URL), sign
}

// The user edge under the Asgardeo dialect: a user's token is admitted with
// its org under the neutral names and its display claims; a machine's token,
// one without aut and one whose aut is neither value are refused.
func TestJWTMiddleware_Asgardeo(t *testing.T) {
	jwks, sign := asgardeoSigner(t)
	var got *Claims
	h := JWTMiddleware(JWTConfig{
		JWKS: jwks, AllowedIssuers: []string{asgIssuer},
		// The machine client's audience is allowed too, so the cases below
		// are refused by the machine-token rule and not by the audience.
		AllowedAudiences: []string{asgConsoleApp, "aep-api-client"},
		Dialect:          asgardeoDialect,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	call := func(claims jwt.MapClaims) int {
		got = nil
		r := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
		r.Header.Set("Authorization", "Bearer "+sign(claims))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}

	if code := call(asgardeoUserClaims()); code != http.StatusOK {
		t.Fatalf("user token: status %d, want 200", code)
	}
	want := Claims{
		Subject: "7c1f0c52-0d6a-4a8e-9a55-3f3c1a2b4d5e", ClientID: asgConsoleApp,
		OrgHandle: "acme", OrgID: asgOrgID, OrgName: "Acme Inc",
		Email: "ada@acme.example", GivenName: "Ada", FamilyName: "Lovelace",
	}
	if got == nil || *got != want {
		t.Fatalf("claims = %+v, want %+v", got, want)
	}

	for name, claims := range map[string]jwt.MapClaims{
		"machine token":           asgardeoMachineClaims("aep-api-client"),
		"no aut":                  with(asgardeoUserClaims(), map[string]any{"aut": nil}),
		"aut neither value":       with(asgardeoUserClaims(), map[string]any{"aut": "DEVICE"}),
		"a Thunder user's claims": with(asgardeoUserClaims(), map[string]any{"aut": nil, "grant_type": "authorization_code"}),
	} {
		if code := call(claims); code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, code)
		}
	}
}

// A per-org machine client under the Asgardeo dialect: the client is the
// audience (a single string on Asgardeo), and the org in that audience must
// be the org_handle the token declares.
func TestPublisherTokenVerifier_Asgardeo(t *testing.T) {
	jwks, sign := asgardeoSigner(t)
	v := NewPublisherTokenVerifier(jwks, asgIssuer, pubAudPrefix, asgardeoDialect)
	if v == nil {
		t.Fatal("NewPublisherTokenVerifier returned nil")
	}
	client := pubAudPrefix + "acme"

	claims, err := v.Verify(sign(asgardeoMachineClaims(client)))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.OrgHandle != "acme" {
		t.Fatalf("OrgHandle = %q, want acme", claims.OrgHandle)
	}

	for name, tc := range map[string]struct {
		claims  jwt.MapClaims
		wantErr string
	}{
		"org_handle names another org": {
			claims:  with(asgardeoMachineClaims(client), map[string]any{"org_handle": "globex"}),
			wantErr: `org "globex" does not match audience org "acme"`,
		},
		"no org_handle": {
			claims:  with(asgardeoMachineClaims(client), map[string]any{"org_handle": nil}),
			wantErr: "org handle claim missing",
		},
		"Thunder's ouHandle is not read": {
			claims:  with(asgardeoMachineClaims(client), map[string]any{"org_handle": nil, "ouHandle": "acme"}),
			wantErr: "org handle claim missing",
		},
		"the console app's user token": {
			claims:  asgardeoUserClaims(),
			wantErr: "no publisher audience present",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := v.Verify(sign(tc.claims)); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Verify: err=%v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

// The org's AE Studio client under the Asgardeo dialect verifies when it is
// the one recorded for the org its token declares.
func TestStudioClientVerifier_Asgardeo(t *testing.T) {
	jwks, sign := asgardeoSigner(t)
	v := NewStudioClientVerifier(jwks, asgIssuer, asgardeoDialect, storedStudioClients{"acme": "ae-studio-acme"})
	if v == nil {
		t.Fatal("NewStudioClientVerifier returned nil")
	}
	org, err := v.Verify(context.Background(), sign(asgardeoMachineClaims("ae-studio-acme")))
	if err != nil || org != "acme" {
		t.Fatalf("own client: org=%q err=%v, want acme", org, err)
	}
	for name, claims := range map[string]jwt.MapClaims{
		"the org's publisher client": asgardeoMachineClaims("aep-publisher-acme"),
		"another org's handle":       with(asgardeoMachineClaims("ae-studio-acme"), map[string]any{"org_handle": "globex"}),
		"the install-wide client":    asgardeoMachineClaims("ae-studio-internal-client"),
	} {
		if org, err := v.Verify(context.Background(), sign(claims)); err == nil {
			t.Errorf("%s: verified as %q, want refused", name, org)
		}
	}
}
