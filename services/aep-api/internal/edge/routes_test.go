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
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// removedRoute is a route a phase deleted. A valid user JWT must get the
// listed status: never 2xx, 401 or 5xx (scenario 1.5).
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
}

func TestRemovedRoutes(t *testing.T) {
	asUser := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), &auth.Claims{Subject: "u", OuHandle: "acme"})))
		})
	}
	h := NewHandlerForTest(Deps{}, asUser, nil)
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
// that admits it, in one place (03 §5). Rows a later phase deletes say so.
func TestRouteTable(t *testing.T) {
	got := map[string]route{}
	for _, r := range routes(AppParams{}) {
		if r.caller == "" || r.gate == "" {
			t.Errorf("row %q has no caller or gate", r.pattern)
		}
		got[r.pattern] = r
	}
	for _, p := range []string{
		"GET /healthz", "GET /readyz", "GET /auth/external/jwks.json",
		"POST /api/v1/webhooks/github", "/api/",
		"/internal/v1/",
		"POST /_dev/v1/secret-ref-resync",
	} {
		if _, ok := got[p]; !ok {
			t.Errorf("mount table lacks %q", p)
		}
	}
	if len(got) != 7 {
		t.Errorf("mount table has %d rows, want 7", len(got))
	}
}
