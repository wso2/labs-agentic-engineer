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

package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/shared/tenancytest"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/tenant"
)

// This file is the COPYABLE TEMPLATE for controller HTTP tests (§8.0). To test
// a feature's controller:
//
//  1. build a Router with the real gate (newGatedRouterForTest);
//  2. register the controller's routes through it — in a real test this is
//     `registerXxxRoutes(rt, fakeController)`, where fakeController is a hand
//     fake of the controller interface backed by a fake service;
//  3. assert same-org requests pass; and
//  4. add ONE tenancytest.AssertCrossOrgDenied per gated route — the structural
//     regression lock that keeps the IDOR class closed (§10).
//
// It runs under plain `go test` (no DB needed); the dbtest tier covers the
// store layer separately.

// newGatedRouterForTest builds a Router over a fresh mux with the real
// BindUserOrg gate in the given mode.
func newGatedRouterForTest(mode tenant.GateMode) (*Router, *http.ServeMux) {
	mux := http.NewServeMux()
	return NewRouter(mux, tenant.BindUserOrg("orgHandle", mode)), mux
}

func TestHTTPTemplate_OrgScopedRoute_Tenancy(t *testing.T) {
	// (1) router + real gate (enforce, so the cross-org assertion bites).
	rt, mux := newGatedRouterForTest(tenant.GateModeEnforce)

	// (2) register an org-scoped route backed by a fake "controller". Swap this
	// for registerXxxRoutes(rt, fakeController) when copying for a real feature.
	rt.OrgScoped("GET /api/v1/organizations/{orgHandle}/widgets", func(w http.ResponseWriter, r *http.Request) {
		c, _ := tenant.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, string(c.Org))
	})

	// (3) same-org request passes and the Caller is bound for the handler.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, tenancytest.UserRequest("GET", "/api/v1/organizations/acme/widgets", "acme"))
	if rec.Code != http.StatusOK || rec.Body.String() != "acme" {
		t.Fatalf("same-org: want 200/acme, got %d/%q", rec.Code, rec.Body.String())
	}

	// (4) cross-org request is denied. Copy this line for EVERY gated route.
	tenancytest.AssertCrossOrgDenied(t, mux, "GET", "/api/v1/organizations/evilcorp/widgets", "acme")
}
