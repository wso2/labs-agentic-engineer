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

package jwtassertion

import (
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
)

// signedTokenFor returns a JWKS cache trusting a fresh key, and a signer for
// tokens from issuer "thunder" to audience "aep-console-client".
func signedTokenFor(t *testing.T) (*JWKSCache, func(grantType string) string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(JWKS{Keys: []JSONWebKey{{
			Kty: "RSA", Kid: "k1", Use: "sig", Alg: "RS256",
			N: base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	sign := func(grantType string) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, TokenClaims{
			Sub: "entity-1", ClientID: "aep-console-client", OuHandle: "acme", GrantType: grantType,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer: "thunder", Audience: jwt.ClaimStrings{"aep-console-client"},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
		})
		tok.Header["kid"] = "k1"
		s, err := tok.SignedString(priv)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	return NewJWKSCache(srv.URL), sign
}

// A user-only verifier refuses a client_credentials token with the same 401
// invalid_token challenge as any other refusal, and admits user grants.
func TestAuthenticator_UserTokensOnlyRefusesClientCredentials(t *testing.T) {
	jwks, sign := signedTokenFor(t)
	mw := Authenticator(Config{
		JWKS: jwks, AllowedIssuers: []string{"thunder"}, AllowedAudiences: []string{"aep-console-client"},
		UserTokensOnly: true,
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	call := func(tok string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
		r.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	cc := call(sign("client_credentials"))
	if cc.Code != http.StatusUnauthorized {
		t.Fatalf("client_credentials: status %d, want 401", cc.Code)
	}
	if got, want := cc.Header().Get("WWW-Authenticate"), `Bearer realm="aep", error="invalid_token"`; got != want {
		t.Fatalf("client_credentials: challenge %q, want %q", got, want)
	}
	for _, grant := range []string{"authorization_code", "refresh_token"} {
		if w := call(sign(grant)); w.Code != http.StatusOK {
			t.Fatalf("%s: status %d, want 200", grant, w.Code)
		}
	}
}

// Without UserTokensOnly the grant is not the verifier's business: service and
// task tokens keep verifying on the routes that take them.
func TestAuthenticator_ClientCredentialsAdmittedWhenNotUserOnly(t *testing.T) {
	jwks, sign := signedTokenFor(t)
	h := Authenticator(Config{JWKS: jwks, AllowedIssuers: []string{"thunder"}, AllowedAudiences: []string{"aep-console-client"}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Authorization", "Bearer "+sign("client_credentials"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}
}
