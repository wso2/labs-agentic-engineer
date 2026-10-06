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
package edge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// removedRoute is a route a phase deleted. A valid user JWT must get the
// listed status: never 2xx, 401 or 5xx.
type removedRoute struct {
	method, path string
	want         int
}

// removedRoutes grows with each phase that deletes a route.
var removedRoutes = []removedRoute{
	{http.MethodGet, "/api/v1/projects/p/activity", http.StatusNotFound},
	{http.MethodGet, "/api/v1/projects/p/activity/stream", http.StatusNotFound},
	// Regression row: the callback never mounted in this harness, so it passes
	// before and after; its removal is proven by AppParams losing the controller.
	{http.MethodGet, "/api/v1/org/credentials/github/connect/callback", http.StatusNotFound},
	{http.MethodPost, "/api/v1/config/git-provider/connect-sessions", http.StatusNotFound},
	// Token minting is gone: the JWKS of BFF-signed tokens and the playground
	// mint. TestRemovedTokenRoutesAre404 repeats them with the old flag set.
	{http.MethodGet, "/auth/external/jwks.json", http.StatusNotFound},
	{http.MethodPost, "/internal/v1/mcp/playground-token", http.StatusNotFound},
	// The validation-context callback moved to runs/{cycleId}/validation-context.
	{http.MethodGet, "/internal/v1/validation/c/context", http.StatusNotFound},
	// The GitHub webhook receiver moved to the AE Studio tools pod.
	{http.MethodPost, "/api/v1/webhooks/github", http.StatusNotFound},
	// Regression row: the dev group never mounted in this harness; its removal
	// is proven by TestRouteTable.
	{http.MethodPost, "/_dev/v1/secret-ref-resync", http.StatusNotFound},
	// No user rotation of the publisher client secret: it lives only in
	// vault, written by the gitpat submit's client ensure.
	{http.MethodPost, "/api/v1/config/idp/client-secret", http.StatusNotFound},
}

func TestRemovedRoutes(t *testing.T) {
	asUser := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), &auth.Claims{Subject: "u", OuHandle: "acme"})))
		})
	}
	h := NewHandlerForTest(Deps{}, asUser)
	for _, rr := range removedRoutes {
		t.Run(rr.method+" "+rr.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(rr.method, rr.path, nil))
			if w.Code != rr.want {
				t.Fatalf("status = %d, want %d", w.Code, rr.want)
			}
		})
	}
}

// TestRouteTable pins the mount table: every caller of aep-api and the gate
// that admits it, in one place. Rows a later phase deletes say so.
func TestRouteTable(t *testing.T) {
	got := map[string]route{}
	for _, r := range routes(AppParams{}) {
		if r.caller == "" || r.gate == "" {
			t.Errorf("row %q has no caller or gate", r.pattern)
		}
		got[r.pattern] = r
	}
	for _, p := range []string{
		"GET /healthz", "GET /readyz",
		"/api/",
		"/internal/v1/",
	} {
		if _, ok := got[p]; !ok {
			t.Errorf("mount table lacks %q", p)
		}
	}
	if len(got) != 4 {
		t.Errorf("mount table has %d rows, want 4", len(got))
	}
	// The dev secret resync is gone with the stored values it re-pushed.
	if _, ok := got["POST /_dev/v1/secret-ref-resync"]; ok {
		t.Error("mount table still has the dev secret resync")
	}
	// The SRE handoff is an internal caller: /api/ admits user JWTs only.
	if c := got["/internal/v1/"].caller; !strings.Contains(c, "aep-mcp-server (SRE handoff)") {
		t.Errorf("/internal/v1/ caller %q does not name the SRE handoff", c)
	}
	if c := got["/api/"].caller; strings.Contains(c, "SRE") || strings.Contains(c, "aep-mcp-server") {
		t.Errorf("/api/ caller %q still names the SRE handoff", c)
	}
}

// TestRemovedTokenRoutesAre404: the JWKS and the playground mint stay gone even
// with the retired PLAYGROUND_TOKEN_ENABLED flag set in the environment.
func TestRemovedTokenRoutesAre404(t *testing.T) {
	t.Setenv("PLAYGROUND_TOKEN_ENABLED", "true")
	h := NewHandlerForTest(Deps{}, nil)
	for _, rr := range []removedRoute{
		{http.MethodGet, "/auth/external/jwks.json", http.StatusNotFound},
		{http.MethodPost, "/internal/v1/mcp/playground-token", http.StatusNotFound},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(rr.method, rr.path, nil))
		if w.Code != rr.want {
			t.Errorf("%s %s = %d, want %d", rr.method, rr.path, w.Code, rr.want)
		}
	}
}
