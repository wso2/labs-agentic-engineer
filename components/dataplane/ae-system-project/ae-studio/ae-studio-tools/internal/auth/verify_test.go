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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type keyPair struct {
	key *rsa.PrivateKey
	kid string
}

func newKeyPair(t *testing.T) keyPair {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return keyPair{key: k, kid: "k1"}
}

func (kp keyPair) serveJWKS(t *testing.T) *httptest.Server {
	t.Helper()
	jwks := JWKS{Keys: []JSONWebKey{{
		Kty: "RSA", Kid: kp.kid, Use: "sig", Alg: "RS256",
		N: base64.RawURLEncoding.EncodeToString(kp.key.N.Bytes()),
		E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(kp.key.E)).Bytes()),
	}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (kp keyPair) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kp.kid
	s, err := tok.SignedString(kp.key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGate_Table(t *testing.T) {
	kp := newKeyPair(t)
	srv := kp.serveJWKS(t)
	v := NewVerifier("http://idp", NewJWKSCache(srv.URL))
	user := func(h, id string) string {
		return kp.sign(t, jwt.MapClaims{"iss": "http://idp", "aud": "aep-console-client", "sub": "u1",
			"ouId": id, "ouHandle": h, "exp": time.Now().Add(time.Hour).Unix()})
	}
	m2m := kp.sign(t, jwt.MapClaims{"iss": "http://idp", "aud": "ae-studio-internal-client", "client_id": "ae-studio-internal-client",
		"grant_type": "client_credentials", "sub": "app-1", "exp": time.Now().Add(time.Hour).Unix()})
	pub := kp.sign(t, jwt.MapClaims{"iss": "http://idp", "aud": "aep-publisher-default", "client_id": "aep-publisher-default",
		"grant_type": "client_credentials", "ouId": "ou-1", "ouHandle": "default", "exp": time.Now().Add(time.Hour).Unix()})
	expired := kp.sign(t, jwt.MapClaims{"iss": "http://idp", "aud": "aep-console-client", "ouId": "ou-1", "ouHandle": "default", "exp": time.Now().Add(-time.Minute).Unix()})
	otherIss := kp.sign(t, jwt.MapClaims{"iss": "http://evil", "aud": "aep-console-client", "ouId": "ou-1", "ouHandle": "default", "exp": time.Now().Add(time.Hour).Unix()})
	// Hardening rows beyond the plan's table: a user-audience token minted by
	// client_credentials, an M2M token whose aud matches but whose client_id
	// does not, one carrying an org claim, a token with no exp, and an
	// unsigned (alg none) token.
	m2mUserAud := kp.sign(t, jwt.MapClaims{"iss": "http://idp", "aud": "aep-console-client", "client_id": "aep-console-client",
		"grant_type": "client_credentials", "ouId": "ou-1", "ouHandle": "default", "exp": time.Now().Add(time.Hour).Unix()})
	m2mOtherClient := kp.sign(t, jwt.MapClaims{"iss": "http://idp", "aud": "ae-studio-internal-client", "client_id": "someone-else",
		"grant_type": "client_credentials", "exp": time.Now().Add(time.Hour).Unix()})
	m2mWithOrg := kp.sign(t, jwt.MapClaims{"iss": "http://idp", "aud": "ae-studio-internal-client", "client_id": "ae-studio-internal-client",
		"grant_type": "client_credentials", "ouId": "ou-1", "exp": time.Now().Add(time.Hour).Unix()})
	noExp := kp.sign(t, jwt.MapClaims{"iss": "http://idp", "aud": "aep-console-client", "ouId": "ou-1", "ouHandle": "default"})
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"iss": "http://idp", "aud": "aep-console-client",
		"ouId": "ou-1", "ouHandle": "default", "exp": time.Now().Add(time.Hour).Unix()}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	// Algorithm confusion: HS256 keyed with the RSA public key bytes.
	hs256, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": "http://idp", "aud": "aep-console-client",
		"ouId": "ou-1", "ouHandle": "default", "exp": time.Now().Add(time.Hour).Unix()}).SignedString(x509.MarshalPKCS1PublicKey(&kp.key.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	futureNbf := kp.sign(t, jwt.MapClaims{"iss": "http://idp", "aud": "aep-console-client", "ouId": "ou-1", "ouHandle": "default",
		"nbf": time.Now().Add(time.Hour).Unix(), "exp": time.Now().Add(2 * time.Hour).Unix()})

	userGate := UserGate(v, []string{"aep-console-client"}, "ou-1", "default")
	m2mGate := M2MGate(v, "ae-studio-internal-client", "ou-1")
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })

	cases := []struct {
		name  string
		gate  func(http.Handler) http.Handler
		token string
		org   string
		want  int
	}{
		{"user ok", userGate, user("default", "ou-1"), "", 204},
		{"user other OU", userGate, user("e2e-other", "ou-2"), "", 403},
		{"user handle matches id differs", userGate, user("default", "ou-2"), "", 403},
		{"no token on v1", userGate, "", "", 401},
		{"expired", userGate, expired, "", 401},
		{"foreign issuer", userGate, otherIss, "", 401},
		{"AE-only M2M on v1", userGate, m2m, "", 401},
		{"publisher on v1", userGate, pub, "", 401},
		{"client_credentials with user aud on v1", userGate, m2mUserAud, "", 401},
		{"no exp on v1", userGate, noExp, "", 401},
		{"alg none on v1", userGate, unsigned, "", 401},
		{"HS256 keyed with RSA public key on v1", userGate, hs256, "", 401},
		{"future nbf on v1", userGate, futureNbf, "", 401},
		{"m2m ok", m2mGate, m2m, "ou-1", 204},
		{"m2m no impersonation", m2mGate, m2m, "", 403},
		{"m2m foreign impersonation", m2mGate, m2m, "ou-2", 403},
		{"user on internal", m2mGate, user("default", "ou-1"), "ou-1", 401},
		{"publisher on internal", m2mGate, pub, "ou-1", 401},
		{"other client id on internal", m2mGate, m2mOtherClient, "ou-1", 401},
		{"m2m with org claim on internal", m2mGate, m2mWithOrg, "ou-1", 401},
		{"no token on internal", m2mGate, "", "ou-1", 401},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/x", nil)
			if c.token != "" {
				r.Header.Set("Authorization", "Bearer "+c.token)
			}
			if c.org != "" {
				r.Header.Set("X-Impersonate-Org", c.org)
			}
			rec := httptest.NewRecorder()
			c.gate(ok).ServeHTTP(rec, r)
			if rec.Code != c.want {
				t.Fatalf("status %d, want %d (%s)", rec.Code, c.want, rec.Body.String())
			}
			if c.want >= 400 && rec.Header().Get("Content-Type") != "application/problem+json" {
				t.Fatalf("content-type %q", rec.Header().Get("Content-Type"))
			}
			if c.want == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") != `Bearer realm="ae-studio-tools"` {
				t.Fatalf("WWW-Authenticate %q", rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestConstructors_PanicOnEmptySecurityParams(t *testing.T) {
	jwks := NewJWKSCache("http://idp/jwks")
	v := NewVerifier("http://idp", jwks)
	cases := []struct {
		name string
		fn   func()
	}{
		{"verifier empty issuer", func() { NewVerifier("", jwks) }},
		{"verifier nil jwks", func() { NewVerifier("http://idp", nil) }},
		{"user gate nil verifier", func() { UserGate(nil, []string{"a"}, "ou-1", "default") }},
		{"user gate no audiences", func() { UserGate(v, nil, "ou-1", "default") }},
		{"user gate empty audience entry", func() { UserGate(v, []string{""}, "ou-1", "default") }},
		{"user gate empty org id", func() { UserGate(v, []string{"a"}, "", "default") }},
		{"user gate empty org handle", func() { UserGate(v, []string{"a"}, "ou-1", "") }},
		{"m2m gate nil verifier", func() { M2MGate(nil, "c", "ou-1") }},
		{"m2m gate empty client id", func() { M2MGate(v, "", "ou-1") }},
		{"m2m gate empty org id", func() { M2MGate(v, "c", "") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			c.fn()
		})
	}
}
