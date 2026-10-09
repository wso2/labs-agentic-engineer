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

// Component-tier coverage for the contract-first internal S2S route group: the
// runner callbacks through the REAL handler graph (mountRoutes → internalGate →
// strict handler), with a Thunder publisher-cc token.

package edge

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

	"github.com/wso2/aep/aep-api/internal/delivery/validation"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// fakeValidationContext records the cycle id and org the route group hands it, which
// is what proves the path parameter reaches the service intact.
type fakeValidationContext struct {
	gotCycle, gotOrg string
}

func (f *fakeValidationContext) ValidationContext(_ context.Context, cycleID, orgHandle string) (*validation.ValidationContextResponse, error) {
	f.gotCycle, f.gotOrg = cycleID, orgHandle
	return &validation.ValidationContextResponse{
		Endpoints: []validation.ComponentEndpoint{{Component: "hello-webapp", URL: "https://hello.example"}},
	}, nil
}

type internalStack struct {
	handler http.Handler
	// deps is what handler was built from, so a test can extend the runner
	// wiring and build its own handler.
	deps InternalDeps
	// mint signs an org's publisher client token (the runner's credential).
	mint func(org string) string
	// mintStudio signs an org's ae-studio-<org> client token (the AE Studio
	// tools pod's credential, the only one the ae-studio/ ops accept).
	mintStudio func(org string) string
	// sign signs any claims with the Thunder test key, for tokens that are not
	// a well-formed publisher token (user JWTs, other clients' tokens).
	sign    func(claims jwt.Claims) string
	context *fakeValidationContext
	// fenced records every cycle id the runner authorizer looked up, which
	// is the id internalGate fenced the request on.
	fenced *[]string
}

func newInternalTestStack(t *testing.T) (http.Handler, func(org string) string) {
	t.Helper()
	s := newInternalStack(t)
	return s.handler, s.mint
}

const pubIssuer, pubAudPrefix = "platform-idp", "aep-publisher-"

// orgWithoutStudioClient is the one org recordedStudioClients has no
// ae-studio client recorded for.
const orgWithoutStudioClient = "no-client"

// sreHandoffKey is the stack's install-time SRE handoff key: it opens
// sre-handoff/mcp and nothing else.
const sreHandoffKey = "s3cr3t-sre-handoff-key-of-32-chars"

// sreHandoffMCPReached stands in for the SRE MCP handler: a 200 here means the
// request got past the handoff verifier.
var sreHandoffMCPReached = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// recordedStudioClients records ae-studio-<org> for every org but
// orgWithoutStudioClient.
type recordedStudioClients struct{}

func (recordedStudioClients) StudioClientID(_ context.Context, org string) (string, error) {
	if org == orgWithoutStudioClient {
		return "", nil
	}
	return "ae-studio-" + org, nil
}

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
	sign := func(claims jwt.Claims) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = kid
		signed, err := tok.SignedString(priv)
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		return signed
	}
	mint := func(org string) string {
		return sign(auth.PublisherClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    pubIssuer,
				Audience:  jwt.ClaimStrings{pubAudPrefix + org},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			OuHandle: org,
		})
	}
	mintStudio := func(org string) string {
		return sign(auth.PublisherClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    pubIssuer,
				Audience:  jwt.ClaimStrings{"ae-studio-" + org},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			OuHandle: org,
		})
	}
	studioClients := auth.NewStudioClientVerifier(jwtassertion.NewJWKSCache(jwks.URL), pubIssuer, recordedStudioClients{})
	if studioClients == nil {
		t.Fatal("NewStudioClientVerifier returned nil")
	}
	fenced := &[]string{}
	lookup := func(_ context.Context, cycleID string) (auth.RunnerCycle, error) {
		*fenced = append(*fenced, cycleID)
		if strings.HasPrefix(cycleID, "other-org-") {
			return auth.RunnerCycle{OrgHandle: "org-other", Open: true}, nil
		}
		// A closed cycle of the token's own org: its runner has nothing left to do.
		if strings.HasPrefix(cycleID, "closed-") {
			return auth.RunnerCycle{OrgHandle: "org-acme"}, nil
		}
		return auth.RunnerCycle{OrgHandle: "org-acme", Open: true}, nil
	}
	stack := internalStack{
		mint:       mint,
		mintStudio: mintStudio,
		sign:       sign,
		context:    &fakeValidationContext{},
		fenced:     fenced,
	}
	stack.deps = InternalDeps{
		RunnerAuth:        auth.NewRunnerAuthorizer(verifier, lookup),
		ValidationContext: stack.context,
		StudioClients:     studioClients,
		SREHandoffAuth:    auth.NewSREHandoffVerifier(sreHandoffKey),
		SREHandoffMCP:     sreHandoffMCPReached,
	}
	stack.handler = NewHandler(AppParams{InternalDeps: stack.deps})
	return stack
}

// The validation callback lives under the runs/ prefix, so the edge must MOUNT
// that prefix — the inner mux registers the contract's full paths, and a prefix
// missing from the outer mux 404s before any handler or auth gate runs. That is a
// silent break the contract test cannot see, so it is asserted through real HTTP.
func TestInternalRoutes_ValidationCallbackIsRoutedAndCycleKeyed(t *testing.T) {
	t.Parallel()
	s := newInternalStack(t)
	const cycle = "9d90f001-67bb-4c51-a5f3-7fd808c06c36"

	tok := s.mint("org-acme")

	t.Run("context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/runs/"+cycle+"/validation-context", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		s.handler.ServeHTTP(rec, req)

		if rec.Code == 404 {
			t.Fatalf("404 — the /internal/v1/runs/ prefix is not mounted on the edge mux")
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

	// Org fence: a publisher token for another org cannot read this cycle.
	t.Run("bearer bound to another org → 403", func(t *testing.T) {
		other := s.mint("org-other")
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/runs/"+cycle+"/validation-context", nil)
		req.Header.Set("Authorization", "Bearer "+other)
		rec := httptest.NewRecorder()
		s.handler.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatalf("want 403, got %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

// A validation runner whose cycle has closed is refused at the gate with the
// same 403 an unknown cycle gets — before the service is asked anything. The
// coding runner has no cycle-scoped callback; for it the Job suspend is the
// fence (codingagent design, oc-job-dispatch.md).
func TestInternalRoutes_ClosedCycleIsRefusedAtTheGate(t *testing.T) {
	t.Parallel()
	s := newInternalStack(t)
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/runs/closed-9d90f001/validation-context", nil)
	req.Header.Set("Authorization", "Bearer "+s.mint("org-acme"))
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "cycle not found") {
		t.Fatalf("want 403 cycle not found, got %d body=%s", rec.Code, rec.Body.String())
	}
	if s.context.gotCycle != "" {
		t.Fatalf("the service must not be reached for a closed cycle (saw %q)", s.context.gotCycle)
	}
}

func TestInternalRoutes_AuthPosture(t *testing.T) {
	t.Parallel()
	h, mint := newInternalTestStack(t)

	// No bearer → 401 envelope.
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/runs/cyc-42/validation-context", strings.NewReader(""))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"code"`) {
		t.Fatalf("no bearer: want 401 envelope, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Publisher token for another org → 403 (org fence).
	tok := mint("org-other")
	req = httptest.NewRequest(http.MethodGet, "/internal/v1/runs/cyc-42/validation-context", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("other org: want 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// credentials/refresh is gone: the coding Job mounts the org's gitpat, so there
// is nothing for a runner to exchange. The route must not exist for the
// publisher token that used to open it.
func TestInternal_CredentialsRefreshIs404(t *testing.T) {
	t.Parallel()
	h, mint := newInternalTestStack(t)

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/executions/x/credentials/refresh", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+mint("org-acme"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("refresh: want 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}
