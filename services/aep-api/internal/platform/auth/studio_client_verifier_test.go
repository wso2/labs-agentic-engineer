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
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// storedStudioClients answers the stored ae-studio client id by org.
type storedStudioClients map[string]string

func (s storedStudioClients) StudioClientID(_ context.Context, org string) (string, error) {
	if id, ok := s["!err"]; ok {
		return "", errors.New(id)
	}
	return s[org], nil
}

// newStudioVerifierStack serves a JWKS for a fresh key and returns a verifier
// over clients plus a signer for (aud, ouHandle) cc tokens.
func newStudioVerifierStack(t *testing.T, clients StudioClientLookup) (*StudioClientVerifier, func(aud, ouHandle string) string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "studio-kid"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwtassertion.JWKS{Keys: []jwtassertion.JSONWebKey{{
			Kty: "RSA", Kid: kid, Use: "sig", Alg: "RS256",
			N: base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	v := NewStudioClientVerifier(jwtassertion.NewJWKSCache(srv.URL), pubIssuer, clients)
	if v == nil {
		t.Fatal("NewStudioClientVerifier returned nil")
	}
	sign := func(aud, ouHandle string) string {
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
	return v, sign
}

// Only the org's own ae-studio client, as recorded for that org, verifies;
// the org comes from that binding. Anything else fails closed.
func TestStudioClientVerifier(t *testing.T) {
	clients := storedStudioClients{"acme": "ae-studio-acme", "globex": "ae-studio-globex", "rotated": "ae-studio-rotated-2"}
	v, sign := newStudioVerifierStack(t, clients)

	org, err := v.Verify(context.Background(), sign("ae-studio-acme", "acme"))
	if err != nil || org != "acme" {
		t.Fatalf("own client: org=%q err=%v, want acme", org, err)
	}
	refused := map[string]string{
		"publisher token of the org":            sign("aep-publisher-acme", "acme"),
		"ae-studio audience naming another org": sign("ae-studio-acme", "globex"),
		"ae-studio audience without an org":     sign("ae-studio-acme", ""),
		"client of an org with none recorded":   sign("ae-studio-initech", "initech"),
		"client other than the recorded one":    sign("ae-studio-rotated", "rotated"),
		"AE-only client (no org claim)":         sign("ae-studio-internal-client", ""),
		"user token":                            sign("aep-console", "acme"),
		"garbage":                               "not-a-jwt",
		"empty":                                 "",
	}
	for name, tok := range refused {
		if org, err := v.Verify(context.Background(), tok); err == nil {
			t.Errorf("%s: verified as %q, want refused", name, org)
		}
	}

	failing, signFailing := newStudioVerifierStack(t, storedStudioClients{"!err": "db down"})
	if _, err := failing.Verify(context.Background(), signFailing("ae-studio-acme", "acme")); !errors.Is(err, ErrStudioClientLookup) {
		t.Errorf("a failed client lookup must refuse with ErrStudioClientLookup, got %v", err)
	}
	var nilVerifier *StudioClientVerifier
	if _, err := nilVerifier.Verify(context.Background(), sign("ae-studio-acme", "acme")); err == nil {
		t.Error("a nil verifier must refuse")
	}
}

func TestNewStudioClientVerifier_NilWithoutInputs(t *testing.T) {
	jwks := jwtassertion.NewJWKSCache("http://127.0.0.1:1/jwks")
	if NewStudioClientVerifier(nil, "iss", storedStudioClients{}) != nil ||
		NewStudioClientVerifier(jwks, "", storedStudioClients{}) != nil ||
		NewStudioClientVerifier(jwks, "iss", nil) != nil {
		t.Fatal("a verifier missing an input must be nil (fails closed at the gate)")
	}
}
