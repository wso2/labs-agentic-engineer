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
