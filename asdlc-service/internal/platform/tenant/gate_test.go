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

package tenant_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/shared/tenancytest"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/tenant"
)

// newGated returns a ServeMux with one org-scoped route guarded by BindUserOrg
// in the given mode. The leaf echoes the bound Caller.Org so tests can assert
// what scope was established.
func newGated(mode tenant.GateMode) http.Handler {
	mux := http.NewServeMux()
	gate := tenant.BindUserOrg("orgHandle", mode)
	leaf := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := tenant.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, string(c.Org))
	})
	mux.Handle("GET /api/v1/organizations/{orgHandle}/projects", gate(leaf))
	return mux
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestGate_Enforce_MatchingOrgPasses(t *testing.T) {
	h := newGated(tenant.GateModeEnforce)
	rec := serve(h, tenancytest.UserRequest("GET", "/api/v1/organizations/acme/projects", "acme"))
	if rec.Code != http.StatusOK {
		t.Fatalf("matching org: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "acme" {
		t.Fatalf("expected Caller.Org=acme bound, got %q", rec.Body.String())
	}
}

func TestGate_Enforce_CaseInsensitiveMatch(t *testing.T) {
	h := newGated(tenant.GateModeEnforce)
	rec := serve(h, tenancytest.UserRequest("GET", "/api/v1/organizations/acme/projects", "Acme"))
	if rec.Code != http.StatusOK {
		t.Fatalf("case-insensitive match: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestGate_Enforce_CrossOrgDenied(t *testing.T) {
	h := newGated(tenant.GateModeEnforce)
	// JWT org "acme" reaching for org "evilcorp" on the path → 404, no leak.
	tenancytest.AssertCrossOrgDenied(t, h, "GET", "/api/v1/organizations/evilcorp/projects", "acme")
}

func TestGate_Enforce_NoOrgClaimUnauthorized(t *testing.T) {
	h := newGated(tenant.GateModeEnforce)
	rec := serve(h, tenancytest.UserRequest("GET", "/api/v1/organizations/acme/projects", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing org claim: want 401, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestGate_Log_CrossOrgPassesThrough(t *testing.T) {
	// In log mode the gate observes the mismatch (emits would-deny) but does
	// NOT deny — so the canary can be measured against real traffic before
	// enforcement is enabled.
	h := newGated(tenant.GateModeLog)
	rec := serve(h, tenancytest.UserRequest("GET", "/api/v1/organizations/evilcorp/projects", "acme"))
	if rec.Code != http.StatusOK {
		t.Fatalf("log mode mismatch: want pass-through 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestParseGateMode(t *testing.T) {
	// Enforce by default: only an explicit "log" downgrades; unset / unknown
	// values enforce (fail-secure).
	cases := map[string]tenant.GateMode{
		"":          tenant.GateModeEnforce,
		"garbage":   tenant.GateModeEnforce,
		"enforce":   tenant.GateModeEnforce,
		"ENFORCE":   tenant.GateModeEnforce,
		" enforce ": tenant.GateModeEnforce,
		"log":       tenant.GateModeLog,
		"LOG":       tenant.GateModeLog,
		" log ":     tenant.GateModeLog,
	}
	for in, want := range cases {
		if got := tenant.ParseGateMode(in); got != want {
			t.Errorf("ParseGateMode(%q) = %q, want %q", in, got, want)
		}
	}
}
