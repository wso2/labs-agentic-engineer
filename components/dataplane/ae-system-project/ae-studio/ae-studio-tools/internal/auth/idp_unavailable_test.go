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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// switchableJWKS serves kp's key set until down is set, then answers 503.
func switchableJWKS(t *testing.T, kp keyPair) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	var down atomic.Bool
	jwks := JWKS{Keys: []JSONWebKey{{
		Kty: "RSA", Kid: kp.kid, Use: "sig", Alg: "RS256",
		N: base64.RawURLEncoding.EncodeToString(kp.key.N.Bytes()),
		E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(kp.key.E)).Bytes()),
	}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)
	return srv, &down
}

func captureAuthLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func serve(gate func(http.Handler) http.Handler, token, org string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if org != "" {
		r.Header.Set("X-Impersonate-Org", org)
	}
	rec := httptest.NewRecorder()
	gate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(rec, r)
	return rec
}

// An IdP whose keys cannot be fetched is no verdict on the token: Verify says
// so with ErrIdPUnavailable, never ErrUnauthenticated, and both gates answer
// 503 idp_unavailable with Retry-After, logged without a value.
func TestIdPUnavailable_IsRetryNotDenied(t *testing.T) {
	kp := newKeyPair(t)
	srv, down := switchableJWKS(t, kp)
	down.Store(true)
	v := NewVerifier("http://idp", NewJWKSCache(srv.URL))
	user := kp.sign(t, jwt.MapClaims{"iss": "http://idp", "aud": "aep-console-client", "sub": "u1",
		"ouId": "ou-1", "ouHandle": "default", "exp": time.Now().Add(time.Hour).Unix()})
	m2m := kp.sign(t, jwt.MapClaims{"iss": "http://idp", "aud": "ae-studio-internal-client", "client_id": "ae-studio-internal-client",
		"grant_type": "client_credentials", "exp": time.Now().Add(time.Hour).Unix()})

	_, err := v.Verify(user, []string{"aep-console-client"})
	if !errors.Is(err, ErrIdPUnavailable) || errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Verify err = %v, want ErrIdPUnavailable only", err)
	}

	for name, rec := range map[string]func() *httptest.ResponseRecorder{
		"user gate": func() *httptest.ResponseRecorder {
			return serve(UserGate(v, []string{"aep-console-client"}, "ou-1", "default"), user, "")
		},
		"m2m gate": func() *httptest.ResponseRecorder {
			return serve(M2MGate(v, "ae-studio-internal-client", "ou-1"), m2m, "ou-1")
		},
	} {
		t.Run(name, func(t *testing.T) {
			logs := captureAuthLogs(t)
			got := rec()
			if got.Code != http.StatusServiceUnavailable || !strings.Contains(got.Body.String(), `"code":"idp_unavailable"`) {
				t.Fatalf("got %d %s", got.Code, got.Body.String())
			}
			if got.Header().Get("Retry-After") != "5" || got.Header().Get("WWW-Authenticate") != "" {
				t.Fatalf("headers = %v", got.Header())
			}
			if got.Header().Get("Content-Type") != "application/problem+json" {
				t.Fatalf("content-type %q", got.Header().Get("Content-Type"))
			}
			line := logs.String()
			if !strings.Contains(line, `"msg":"auth.idp_unavailable"`) {
				t.Fatalf("no auth.idp_unavailable line in %s", line)
			}
			if strings.Contains(line, srv.URL) || strings.Contains(line, "eyJ") {
				t.Fatalf("log line carries a value: %s", line)
			}
		})
	}
}

// What a caller controls never turns into a 503: a malformed token, one with
// no kid, or one naming a kid the (cached) key set lacks is a 401 even while
// the IdP is down; only a key set that cannot be had at all is a 503.
func TestIdPUnavailable_NotReachableByTheToken(t *testing.T) {
	kp := newKeyPair(t)
	srv, down := switchableJWKS(t, kp)
	v := NewVerifier("http://idp", NewJWKSCache(srv.URL))
	gate := UserGate(v, []string{"aep-console-client"}, "ou-1", "default")
	claims := jwt.MapClaims{"iss": "http://idp", "aud": "aep-console-client", "sub": "u1",
		"ouId": "ou-1", "ouHandle": "default", "exp": time.Now().Add(time.Hour).Unix()}
	good := kp.sign(t, claims)
	// Warm the cache while the IdP is up.
	if rec := serve(gate, good, ""); rec.Code != 204 {
		t.Fatalf("warm-up: %d %s", rec.Code, rec.Body.String())
	}
	down.Store(true)

	unknownKid := keyPair{key: kp.key, kid: "attacker-kid"}.sign(t, claims)
	noKid := func() string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		s, err := tok.SignedString(kp.key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}()
	otherKey := newKeyPair(t)
	badSig := otherKey.sign(t, claims) // kid k1, signed by another key

	for name, token := range map[string]string{
		"unknown kid":   unknownKid,
		"no kid":        noKid,
		"bad signature": badSig,
		"malformed":     "not.a.jwt",
	} {
		t.Run(name, func(t *testing.T) {
			if rec := serve(gate, token, ""); rec.Code != http.StatusUnauthorized {
				t.Fatalf("got %d %s, want 401", rec.Code, rec.Body.String())
			}
		})
	}
	// A key the cache holds still verifies while the IdP is down.
	if rec := serve(gate, good, ""); rec.Code != 204 {
		t.Fatalf("cached key: %d %s", rec.Code, rec.Body.String())
	}

	// A cold cache with the IdP down: even a malformed token stays 401 (it
	// never reaches the key lookup).
	cold := UserGate(NewVerifier("http://idp", NewJWKSCache(srv.URL)), []string{"aep-console-client"}, "ou-1", "default")
	if rec := serve(cold, "not.a.jwt", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("malformed on a cold cache: %d", rec.Code)
	}
}
