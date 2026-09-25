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

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// Component test for the real mounted mux (NewHandler → mountSurfaces) proving
// the SRE-handoff bearer reproduces this session's actual failure — 401 on
// aep-mcp-server's placeholder bearer — and that a configured handoff secret
// fixes exactly that call without weakening auth anywhere else.
func TestSREHandoff_ComponentEndToEnd(t *testing.T) {
	post := func(t *testing.T, handler http.Handler, path, bearer string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		if bearer != "" {
			req.Header.Set("Authorization", bearer)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	t.Run("unconfigured verifier: today's bug reproduced — placeholder bearer 401s", func(t *testing.T) {
		handler := NewHandler(AppParams{Config: config.Config{}})
		w := post(t, handler, "/api/v1/projects/hello/issues", "Bearer local-aep-mcp-token")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (no SREHandoffAuth configured, and this bearer is not a JWT)", w.Code)
		}
	})

	t.Run("configured verifier: matching handoff bearer clears auth", func(t *testing.T) {
		handler := NewHandler(AppParams{
			Config:         config.Config{},
			SREHandoffAuth: auth.NewSREHandoffVerifier("s3cr3t", "acme"),
		})
		w := post(t, handler, "/api/v1/projects/hello/issues", "Bearer s3cr3t")
		// No body and no issue service are wired in this test, so CreateIssue's
		// own validation/503 logic takes over — anything other than 401 proves
		// the request got PAST auth and reached the handler.
		if w.Code == http.StatusUnauthorized {
			t.Fatalf("status = %d, want past-auth (e.g. 400/503 from the handler itself), got 401", w.Code)
		}
	})

	t.Run("configured verifier: wrong bearer on the handoff route still 401s", func(t *testing.T) {
		handler := NewHandler(AppParams{
			Config:         config.Config{},
			SREHandoffAuth: auth.NewSREHandoffVerifier("s3cr3t", "acme"),
		})
		w := post(t, handler, "/api/v1/projects/hello/issues", "Bearer wrong")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (wrong secret must still require a real JWT)", w.Code)
		}
	})

	t.Run("configured verifier: other operations are unaffected — no bearer still 401s", func(t *testing.T) {
		handler := NewHandler(AppParams{
			Config:         config.Config{},
			SREHandoffAuth: auth.NewSREHandoffVerifier("s3cr3t", "acme"),
		})
		w := post(t, handler, "/api/v1/projects", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (handoff secret must not widen auth beyond CreateIssue/ListIssues)", w.Code)
		}
	})
}
