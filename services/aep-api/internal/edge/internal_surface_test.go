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

// Component-tier coverage for the contract-first internal S2S surface: the
// runner credentials-refresh exchange through the REAL handler graph
// (mountSurfaces → runnerAuthGate → strict handler), with a Thunder
// publisher-cc token. Pins the RUNNER-LOCKSTEP wire shape: exact top-level
// body keys and the capitalized Identity keys — the runner must work unchanged
// against this surface.

package edge

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/aep-api/internal/delivery/validation"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

type fakeCredsRefresh struct {
	gotExecution, gotOrg string
}

func (f *fakeCredsRefresh) Refresh(_ context.Context, executionID, orgHandle string) (*organization.RefreshResponse, error) {
	f.gotExecution, f.gotOrg = executionID, orgHandle
	return &organization.RefreshResponse{
		Token:     "ghs_fresh",
		ExpiresAt: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
		Identity:  secrets.Identity{Name: "AEP Bot", Email: "bot@aep.dev", Login: "aep-bot"},
		TaskID:    executionID,
	}, nil
}

// fakeValidationContext records the cycle id and org the surface hands it, which
// is what proves the path parameter reaches the service intact.
type fakeValidationContext struct {
	gotCycle, gotOrg string
}

func (f *fakeValidationContext) ValidationContext(_ context.Context, cycleID, orgHandle string) (*validation.ValidationContextResponse, error) {
	f.gotCycle, f.gotOrg = cycleID, orgHandle
	return &validation.ValidationContextResponse{
		Endpoints:    []validation.ComponentEndpoint{{Component: "hello-webapp", URL: "https://hello.example"}},
		CriteriaPath: "specs/validation/validation-criteria.json",
	}, nil
}

type fakeValidationCreds struct {
	gotCycle, gotOrg string
}

func (f *fakeValidationCreds) RequestCredentials(_ context.Context, cycleID, orgHandle string, _ validation.CredentialRequest) (*validation.TestCredential, error) {
	f.gotCycle, f.gotOrg = cycleID, orgHandle
	return &validation.TestCredential{Username: "admin", Password: "admin", Mock: true}, nil
}

type internalStack struct {
	handler http.Handler
	mint    func(org string) string
	refresh *fakeCredsRefresh
	context *fakeValidationContext
	creds   *fakeValidationCreds
}

func newInternalTestStack(t *testing.T) (http.Handler, func(org string) string, *fakeCredsRefresh) {
	t.Helper()
	s := newInternalStack(t)
	return s.handler, s.mint, s.refresh
}

const pubIssuer, pubAudPrefix = "platform-idp", "aep-publisher-"

func newInternalStack(t *testing.T) internalStack {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "test-kid"
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwtassertion.JWKS{Keys: []jwtassertion.JSONWebKey{{
			Kty: "RSA", Kid: kid, Use: "sig", Alg: "RS256",
			N: base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
		}}})
	}))
	t.Cleanup(jwks.Close)
	verifier := auth.NewPublisherTokenVerifier(jwtassertion.NewJWKSCache(jwks.URL), pubIssuer, pubAudPrefix)
	if verifier == nil {
		t.Fatal("NewPublisherTokenVerifier returned nil")
	}
	mint := func(org string) string {
		claims := auth.PublisherClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    pubIssuer,
				Audience:  jwt.ClaimStrings{pubAudPrefix + org},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			OuHandle: org,
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = kid
		signed, err := tok.SignedString(priv)
		if err != nil {
			t.Fatalf("sign publisher token: %v", err)
		}
		return signed
	}
	lookup := func(_ context.Context, cycleID string) (string, error) {
		if strings.HasPrefix(cycleID, "other-org-") {
			return "org-other", nil
		}
		return "org-acme", nil
	}
	stack := internalStack{
		mint:    mint,
		refresh: &fakeCredsRefresh{},
		context: &fakeValidationContext{},
		creds:   &fakeValidationCreds{},
	}
	stack.handler = NewHandler(AppParams{
		InternalDeps: InternalDeps{
			CredsRefresh:          stack.refresh,
			RunnerAuth:            auth.NewRunnerAuthorizer(verifier, lookup),
			ValidationContext:     stack.context,
			ValidationCredentials: stack.creds,
		},
	})
	return stack
}

func TestInternalSurface_RunnerRefresh_Lockstep(t *testing.T) {
	t.Parallel()
	h, mint, svc := newInternalTestStack(t)

	tok := mint("org-acme")
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/executions/exec-42/credentials/refresh", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("refresh: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if svc.gotExecution != "exec-42" || svc.gotOrg != "org-acme" {
		t.Fatalf("service saw execution=%q org=%q — org must come from the verified token", svc.gotExecution, svc.gotOrg)
	}

	// RUNNER LOCKSTEP: exact field sets, including the capitalized Identity keys.
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v\n%s", err, rec.Body.String())
	}
	if got, want := slices.Sorted(maps.Keys(body)), []string{"expiresAt", "identity", "taskId", "token"}; !slices.Equal(got, want) {
		t.Fatalf("top-level keys drifted: got %v want %v", got, want)
	}
	var identity map[string]json.RawMessage
	if err := json.Unmarshal(body["identity"], &identity); err != nil {
		t.Fatalf("identity: %v", err)
	}
	if got, want := slices.Sorted(maps.Keys(identity)), []string{"Email", "Login", "Name"}; !slices.Equal(got, want) {
		t.Fatalf("identity keys drifted (capitalized, runner lockstep): got %v want %v", got, want)
	}
}

// The validation callbacks live under their own prefix, so the edge must MOUNT
// that prefix — the inner mux registers the contract's full paths, and a prefix
// missing from the outer mux 404s before any handler or auth gate runs. That is a
// silent break the contract test cannot see, so it is asserted through real HTTP.
func TestInternalSurface_ValidationCallbacksAreRoutedAndCycleKeyed(t *testing.T) {
	t.Parallel()
	s := newInternalStack(t)
	const cycle = "9d90f001-67bb-4c51-a5f3-7fd808c06c36"

	tok := s.mint("org-acme")

	t.Run("context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/validation/"+cycle+"/context", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		s.handler.ServeHTTP(rec, req)

		if rec.Code == 404 {
			t.Fatalf("404 — the /internal/v1/validation/ prefix is not mounted on the edge mux")
		}
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		// The CYCLE id must arrive intact, and the org must come from the verified
		// token rather than anything in the request.
		if s.context.gotCycle != cycle || s.context.gotOrg != "org-acme" {
			t.Fatalf("service saw cycle=%q org=%q; want %q / org-acme", s.context.gotCycle, s.context.gotOrg, cycle)
		}
		if !strings.Contains(rec.Body.String(), "hello.example") {
			t.Errorf("endpoints missing from the body: %s", rec.Body.String())
		}
	})

	t.Run("test-credentials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/validation/"+cycle+"/test-credentials",
			strings.NewReader(`{"role":"admin"}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.handler.ServeHTTP(rec, req)

		if rec.Code == 404 {
			t.Fatalf("404 — the /internal/v1/validation/ prefix is not mounted on the edge mux")
		}
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		if s.creds.gotCycle != cycle || s.creds.gotOrg != "org-acme" {
			t.Fatalf("service saw cycle=%q org=%q; want %q / org-acme", s.creds.gotCycle, s.creds.gotOrg, cycle)
		}
	})

	// Org fence: a publisher token for another org cannot read this cycle.
	t.Run("bearer bound to another org → 403", func(t *testing.T) {
		other := s.mint("org-other")
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/validation/"+cycle+"/context", nil)
		req.Header.Set("Authorization", "Bearer "+other)
		rec := httptest.NewRecorder()
		s.handler.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatalf("want 403, got %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestInternalSurface_AuthPosture(t *testing.T) {
	t.Parallel()
	h, mint, _ := newInternalTestStack(t)

	// No bearer → 401 envelope.
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/executions/exec-42/credentials/refresh", strings.NewReader(""))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"code"`) {
		t.Fatalf("no bearer: want 401 envelope, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Publisher token for another org → 403 (org fence).
	tok := mint("org-other")
	req = httptest.NewRequest(http.MethodPost, "/internal/v1/executions/exec-42/credentials/refresh", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("other org: want 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
