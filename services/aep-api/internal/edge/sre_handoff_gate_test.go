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

func TestIsSREHandoffRoute(t *testing.T) {
	t.Parallel()
	cases := []struct {
		method, path string
		want         bool
	}{
		{http.MethodPost, "/api/v1/projects/hello/issues", true},
		{http.MethodGet, "/api/v1/projects/hello/issues", true},
		{http.MethodDelete, "/api/v1/projects/hello/issues", false},
		{http.MethodPost, "/api/v1/projects/hello/issues/42", false},
		{http.MethodPost, "/api/v1/projects/issues", false},
		{http.MethodPost, "/api/v1/projects/hello/reports", false},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if got := isSREHandoffRoute(r); got != tc.want {
			t.Errorf("isSREHandoffRoute(%s %s) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestSreHandoffOrJWT(t *testing.T) {
	t.Parallel()

	var nextCalled, jwtCalled bool
	var orgSeen string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		orgSeen = auth.ResolveOuHandle(auth.ClaimsFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	})
	jwtMW := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			jwtCalled = true
			h.ServeHTTP(w, r)
		})
	}
	verifier := auth.NewSREHandoffVerifier("s3cr3t", "acme")

	reset := func() { nextCalled, jwtCalled, orgSeen = false, false, "" }

	t.Run("matching route + valid bearer skips jwt and binds org", func(t *testing.T) {
		reset()
		handler := sreHandoffOrJWT(verifier, jwtMW)(next)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/projects/hello/issues", nil)
		r.Header.Set("Authorization", "Bearer s3cr3t")
		handler.ServeHTTP(httptest.NewRecorder(), r)

		if !nextCalled || jwtCalled {
			t.Fatalf("nextCalled=%v jwtCalled=%v, want next only", nextCalled, jwtCalled)
		}
		if orgSeen != "acme" {
			t.Fatalf("orgSeen = %q, want acme", orgSeen)
		}
	})

	t.Run("matching route + wrong bearer falls through to jwt", func(t *testing.T) {
		reset()
		handler := sreHandoffOrJWT(verifier, jwtMW)(next)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/projects/hello/issues", nil)
		r.Header.Set("Authorization", "Bearer nope")
		handler.ServeHTTP(httptest.NewRecorder(), r)

		if !jwtCalled {
			t.Fatal("want jwt middleware invoked on invalid handoff bearer")
		}
	})

	t.Run("other route with valid-looking bearer still goes through jwt", func(t *testing.T) {
		reset()
		handler := sreHandoffOrJWT(verifier, jwtMW)(next)
		r := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
		r.Header.Set("Authorization", "Bearer s3cr3t")
		handler.ServeHTTP(httptest.NewRecorder(), r)

		if !jwtCalled {
			t.Fatal("want jwt middleware invoked for a non-handoff route")
		}
	})

	t.Run("nil verifier always falls through to jwt", func(t *testing.T) {
		reset()
		handler := sreHandoffOrJWT(nil, jwtMW)(next)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/projects/hello/issues", nil)
		r.Header.Set("Authorization", "Bearer s3cr3t")
		handler.ServeHTTP(httptest.NewRecorder(), r)

		if !jwtCalled {
			t.Fatal("want jwt middleware invoked when the verifier is unconfigured")
		}
	})
}
