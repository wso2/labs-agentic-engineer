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

// newRawPublisherVerifier is newPublisherVerifier with a signer that takes
// the IdP's org claims whole, for the cases about how they are read.
func newRawPublisherVerifier(t *testing.T) (*PublisherTokenVerifier, func(aud string, raw dialect.Raw) string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "raw-kid"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwtassertion.JWKS{Keys: []jwtassertion.JSONWebKey{{
			Kty: "RSA", Kid: kid, Use: "sig", Alg: "RS256",
			N: base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	v := NewPublisherTokenVerifier(jwtassertion.NewJWKSCache(srv.URL), pubIssuer, pubAudPrefix, thunderDialect)
	if v == nil {
		t.Fatal("NewPublisherTokenVerifier returned nil")
	}
	sign := func(aud string, raw dialect.Raw) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, PublisherClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    pubIssuer,
				Audience:  jwt.ClaimStrings{aud},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			Raw: raw,
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

// The org a publisher token binds is the one in its audience, and it must be
// the org handle the dialect reads from the token's own claims, declared
// outright: the user path's fallback to ouName or ouId for legacy tokens does
// not apply to a client aep-api registered itself.
func TestPublisherTokenVerifier_ReadsTheOrgThroughTheDialect(t *testing.T) {
	v, sign := newRawPublisherVerifier(t)
	for name, tc := range map[string]struct {
		raw     dialect.Raw
		wantErr string
	}{
		"ouHandle matches":                {raw: dialect.Raw{OuHandle: "acme", OuName: "Globex"}},
		"ouName alone is refused":         {raw: dialect.Raw{OuName: "acme"}, wantErr: "org handle claim missing"},
		"ouId alone is refused":           {raw: dialect.Raw{OuID: "acme"}, wantErr: "org handle claim missing"},
		"ouHandle names another org":      {raw: dialect.Raw{OuHandle: "globex"}, wantErr: `org "globex" does not match audience org "acme"`},
		"ouHandle wins over a name match": {raw: dialect.Raw{OuHandle: "globex", OuName: "acme"}, wantErr: `does not match audience org`},
		"no org claim at all":             {raw: dialect.Raw{GrantType: "client_credentials"}, wantErr: "org handle claim missing"},
	} {
		t.Run(name, func(t *testing.T) {
			claims, err := v.Verify(sign(pubAudPrefix+"acme", tc.raw))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Verify: %v", err)
				}
				if claims.OrgHandle != "acme" {
					t.Fatalf("OrgHandle = %q, want acme", claims.OrgHandle)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Verify: err=%v, want containing %q", err, tc.wantErr)
			}
		})
	}
}
