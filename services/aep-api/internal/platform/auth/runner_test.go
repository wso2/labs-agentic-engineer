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
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// newRunnerTaskManager builds a TaskTokenManager backed by a freshly generated
// RSA key so tests can mint real BFF-signed identity JWTs.
func newRunnerTaskManager(t *testing.T) *TaskTokenManager {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	mgr, err := NewTaskTokenManager(TaskTokenConfig{
		PrivateKey: string(pemKey),
		Issuer:     "aep-bff",
		Audience:   "git-service",
		TTL:        time.Hour,
	})
	if err != nil {
		t.Fatalf("NewTaskTokenManager: %v", err)
	}
	return mgr
}

// statusOf extracts the HTTP status an *HTTPError carries (0 if it is not
// one).
func statusOf(err error) int {
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status
	}
	return 0
}

// Task JWTs are not a runner-callback credential. A still-valid BFF-signed
// token must 401 the same as garbage — otherwise an old Job silently keeps
// working after publisher CC became the only path.
func TestRunnerAuthorizer_TaskJWTRejected(t *testing.T) {
	mgr := newRunnerTaskManager(t)
	verifier, _ := newPublisherVerifier(t)
	a := NewRunnerAuthorizer(verifier, func(context.Context, string) (string, error) {
		t.Fatal("cycle lookup must not run for a Task JWT")
		return "", nil
	})

	tok, err := mgr.IssueServiceToken("git-service", "org-a", time.Hour)
	if err != nil {
		t.Fatalf("IssueServiceToken: %v", err)
	}
	_, err = a.Authorize(context.Background(), "Bearer "+tok, "task-1")
	if got := statusOf(err); got != 401 {
		t.Fatalf("status = %d, want 401 (err=%v)", got, err)
	}
}

func TestRunnerAuthorizer_BadBearer(t *testing.T) {
	a := NewRunnerAuthorizer(nil, func(context.Context, string) (string, error) {
		return "", nil
	})

	cases := map[string]string{
		"absent":       "",
		"wrong scheme": "Token abc.def.ghi",
		"too short":    "Bearer",
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := a.Authorize(context.Background(), header, "task-1")
			if got := statusOf(err); got != 401 {
				t.Fatalf("status = %d, want 401 (err=%v)", got, err)
			}
		})
	}
}

func TestRunnerAuthorizer_GarbageJWT(t *testing.T) {
	verifier, _ := newPublisherVerifier(t)
	a := NewRunnerAuthorizer(verifier, func(context.Context, string) (string, error) {
		t.Fatal("cycle lookup must not run for garbage jwt")
		return "", nil
	})
	_, err := a.Authorize(context.Background(), "Bearer not-a-real-token", "task-1")
	if got := statusOf(err); got != 401 {
		t.Fatalf("status = %d, want 401 (err=%v)", got, err)
	}
}

// newPublisherVerifier stands up an httptest JWKS server backed by a fresh RSA
// key and returns a verifier plus a mint func that signs publisher-cc tokens
// (aud = "aep-publisher-<org>", ouHandle = <ouHandle>) the verifier accepts.
const pubIssuer, pubAudPrefix = "platform-idp", "aep-publisher-"

func newPublisherVerifier(t *testing.T) (*PublisherTokenVerifier, func(org, ouHandle string) string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "test-kid"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwtassertion.JWKS{Keys: []jwtassertion.JSONWebKey{{
			Kty: "RSA", Kid: kid, Use: "sig", Alg: "RS256",
			N: base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)

	v := NewPublisherTokenVerifier(jwtassertion.NewJWKSCache(srv.URL), pubIssuer, pubAudPrefix)
	if v == nil {
		t.Fatal("NewPublisherTokenVerifier returned nil")
	}
	mint := func(org, ouHandle string) string {
		claims := PublisherClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    pubIssuer,
				Audience:  jwt.ClaimStrings{pubAudPrefix + org},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			OuHandle: ouHandle,
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = kid
		signed, err := tok.SignedString(priv)
		if err != nil {
			t.Fatalf("sign publisher token: %v", err)
		}
		return signed
	}
	return v, mint
}

// Publisher CC is the only runner-callback credential. The token's org MUST
// match the cycle's owning org (runner.go's cross-org fence) — org-A's token
// cannot refresh an org-B cycle it names.
func TestRunnerAuthorizer_PublisherCC(t *testing.T) {
	verifier, mint := newPublisherVerifier(t)

	t.Run("valid + task-org matches → ok, publisher source", func(t *testing.T) {
		a := NewRunnerAuthorizer(verifier,
			func(context.Context, string) (string, error) { return "org-a", nil })
		caller, err := a.Authorize(context.Background(), "Bearer "+mint("org-a", "org-a"), "task-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if caller.Org != tenant.OrgHandle("org-a") {
			t.Errorf("org = %q, want org-a", caller.Org)
		}
		if caller.Source != tenant.SourcePublisherCC {
			t.Errorf("source = %v, want SourcePublisherCC", caller.Source)
		}
	})

	t.Run("token org ≠ task org → 403 (cross-org fence)", func(t *testing.T) {
		a := NewRunnerAuthorizer(verifier,
			func(context.Context, string) (string, error) { return "org-b", nil })
		_, err := a.Authorize(context.Background(), "Bearer "+mint("org-a", "org-a"), "task-1")
		if got := statusOf(err); got != 403 {
			t.Fatalf("status = %d, want 403 (err=%v)", got, err)
		}
	})

	t.Run("task lookup fails → 403", func(t *testing.T) {
		a := NewRunnerAuthorizer(verifier,
			func(context.Context, string) (string, error) { return "", errors.New("not found") })
		_, err := a.Authorize(context.Background(), "Bearer "+mint("org-a", "org-a"), "task-1")
		if got := statusOf(err); got != 403 {
			t.Fatalf("status = %d, want 403 (err=%v)", got, err)
		}
	})

	// A cycle that belongs to ANOTHER org and a cycle that does not exist must be
	// indistinguishable to the caller. A different message on the cross-org arm
	// turns a valid org-A token into an oracle for whether any given cycle id
	// exists on the platform — the id is all the prober has to supply, and a
	// distinguishable answer is the whole exploit. Same status AND same message;
	// only the log may tell them apart.
	t.Run("another org's cycle is indistinguishable from a missing one", func(t *testing.T) {
		answer := func(lookup CycleOrgLookup) (int, string) {
			a := NewRunnerAuthorizer(verifier, lookup)
			_, err := a.Authorize(context.Background(), "Bearer "+mint("org-a", "org-a"), "cycle-1")
			var he *HTTPError
			if !errors.As(err, &he) {
				t.Fatalf("want an *HTTPError, got %v", err)
			}
			return he.Status, he.Message
		}
		missingStatus, missingMsg := answer(
			func(context.Context, string) (string, error) { return "", errors.New("no such cycle") })
		otherStatus, otherMsg := answer(
			func(context.Context, string) (string, error) { return "org-b", nil })

		if missingStatus != otherStatus || missingMsg != otherMsg {
			t.Fatalf("the two answers leak which cycles exist: missing=%d/%q, other-org=%d/%q",
				missingStatus, missingMsg, otherStatus, otherMsg)
		}
	})
}

// Org is taken from the verified publisher token — never from anything the
// caller could spoof in the request.
func TestRunnerAuthorizer_OrgFromClaimNotInput(t *testing.T) {
	verifier, mint := newPublisherVerifier(t)
	a := NewRunnerAuthorizer(verifier, func(context.Context, string) (string, error) { return "org-zed", nil })
	caller, err := a.Authorize(context.Background(), "Bearer "+mint("org-zed", "org-zed"), "task-9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if caller.Org != tenant.OrgHandle("org-zed") {
		t.Errorf("org = %q, want org-zed (from the signed claim)", caller.Org)
	}
	if caller.Source != tenant.SourcePublisherCC {
		t.Errorf("source = %v, want SourcePublisherCC", caller.Source)
	}
}
