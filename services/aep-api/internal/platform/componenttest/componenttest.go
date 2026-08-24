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

// Package componenttest is the in-process component-tier harness. It
// assembles the REAL BFF /api handler via edge.NewHandlerForTest — the same
// mountSurfaces assembly
// production uses — with exactly one seam swapped: the JWKS verifier is
// replaced by fakeInboundAuth, so the real tenant gate runs in ENFORCE with no
// Thunder/JWKS. A test supplies the feature's real service (with its
// out-of-process clients mocked) via Options.Deps and drives the handler with
// httptest, asserting the HTTP contract — validation, status codes, error
// mapping, and the no-claims 401 — in milliseconds, no infrastructure.
//
// NOTE on org scope: the public edge derives the active org SOLELY from the
// verified token (the deny-by-default tenant gate — there is no {orgHandle}
// path param anywhere in the contract, locked by the arch guard in
// api/tenant_gate_test.go), so a cross-org request is unrepresentable by
// construction. The runtime assertion this harness adds over the arch-lock is
// therefore the gate's ENFORCE no-claims 401 (and that authed requests reach
// the real handler); there is no path-based cross-org 404 to sweep in the
// token-only model.
package componenttest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// Options configures the harness. Fill only what the feature under test needs.
type Options struct {
	// Deps carries the feature services for the generated strict-server
	// (internal/api/gen, generated from packages/contracts/api/v1) — the REAL
	// service under test, with its out-of-process clients mocked. Fields left
	// zero register nothing for that feature (its routes 404/nil-guard).
	Deps edge.Deps

	// DB is optional and passed through to AppParams.DB. Note orgensure is a
	// no-op in the harness REGARDLESS of DB: NewHandlerForTest never sets
	// AppParams.OrganizationService, and orgensure.Middleware(nil) is an
	// unconditional passthrough. So the Component+DB flavor is achieved by the
	// CALLER building its feature service against dbtest.New and putting it in
	// Deps — not by this field, which today only feeds the dev SM-API resync
	// path (itself unreachable here without CredService/AnthropicCredService).
	DB *gorm.DB
}

// Harness is the assembled real handler plus request builders.
type Harness struct {
	// Handler is the production chain: fakeInboundAuth → orgensure →
	// contract validation → tenant gate (ENFORCE) → strict handlers.
	Handler http.Handler
	t       testing.TB
}

// New assembles the harness. It builds the real handler once via
// edge.NewHandlerForTest with fakeInboundAuth in place of the JWKS verifier.
func New(t testing.TB, opt Options) *Harness {
	t.Helper()
	return &Harness{
		Handler: edge.NewHandlerForTest(opt.Deps, fakeInboundAuth, opt.DB),
		t:       t,
	}
}

// Req is a fluent single-request builder. claims == nil means "no token"
// (NoAuth); a non-nil value is injected as a verified Thunder token would be.
type Req struct {
	h      *Harness
	claims *auth.Claims
}

// AsOrg returns a request builder authenticated as a verified token for org.
// The claims mirror what Thunder would issue (OuHandle drives ResolveOuHandle).
func (h *Harness) AsOrg(org string) *Req {
	return &Req{h: h, claims: &auth.Claims{
		OuHandle: org,
		OuId:     org + "-ouid",
		Subject:  "componenttest-user",
	}}
}

// NoAuth returns a request builder with no claims — an org-scoped op 401s at the
// gate's no-claims branch (the tier's ENFORCE proof).
func (h *Harness) NoAuth() *Req { return &Req{h: h, claims: nil} }

// ClaimsHeader returns the header key/value that authenticates a RAW HTTP
// request as a verified token for org — for tests that wrap Harness.Handler in
// an httptest.Server because they need a real streaming client (SSE live-tail)
// instead of the recorder. Mirrors AsOrg's claims exactly.
func ClaimsHeader(t testing.TB, org string) (key, value string) {
	t.Helper()
	raw, err := json.Marshal(&auth.Claims{OuHandle: org, OuId: org + "-ouid", Subject: "componenttest-user"})
	if err != nil {
		t.Fatalf("componenttest: marshal claims: %v", err)
	}
	return hdrClaims, string(raw)
}

// With customizes the claims (sub/OuId/…) when a handler reads more than
// OuHandle. No-op on a NoAuth request.
func (r *Req) With(mut func(*auth.Claims)) *Req {
	if r.claims != nil && mut != nil {
		mut(r.claims)
	}
	return r
}

// Get / Delete / Post / Put issue the request through the real handler and
// return the recorder. Post/Put take a JSON body (required for required-body
// ops — the strict wrapper 400s an undecodable/empty body before handlers).
func (r *Req) Get(path string) *httptest.ResponseRecorder { return r.do(http.MethodGet, path, "") }
func (r *Req) Delete(path string) *httptest.ResponseRecorder {
	return r.do(http.MethodDelete, path, "")
}
func (r *Req) Post(path, jsonBody string) *httptest.ResponseRecorder {
	return r.do(http.MethodPost, path, jsonBody)
}
func (r *Req) Put(path, jsonBody string) *httptest.ResponseRecorder {
	return r.do(http.MethodPut, path, jsonBody)
}
func (r *Req) Patch(path, jsonBody string) *httptest.ResponseRecorder {
	return r.do(http.MethodPatch, path, jsonBody)
}

// PostRaw issues a POST with a caller-chosen Content-Type and raw bytes, for
// the operations whose body is not JSON (multipart uploads).
func (r *Req) PostRaw(path, contentType string, body []byte) *httptest.ResponseRecorder {
	r.h.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	r.stampClaims(req)
	rec := httptest.NewRecorder()
	r.h.Handler.ServeHTTP(rec, req)
	return rec
}

// stampClaims attaches the request's tenant claims header, if any.
func (r *Req) stampClaims(req *http.Request) {
	if r.claims == nil {
		return
	}
	raw, err := json.Marshal(r.claims)
	if err != nil {
		r.h.t.Fatalf("componenttest: marshal claims: %v", err)
	}
	req.Header.Set(hdrClaims, string(raw))
}

func (r *Req) do(method, path, body string) *httptest.ResponseRecorder {
	r.h.t.Helper()
	var reqBody *strings.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	} else {
		reqBody = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reqBody)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	r.stampClaims(req)
	rec := httptest.NewRecorder()
	r.h.Handler.ServeHTTP(rec, req)
	return rec
}
