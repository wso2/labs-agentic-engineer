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

package humakit

import (
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/tenant"
	"github.com/wso2/asdlc/asdlc-service/middleware/jwt"
)

// resolveWith runs OrgScopedInput.Resolve with the given path org and JWT claims
// org, returning the resolver errors.
func resolveWith(t *testing.T, pathOrg, claimOrg string) []error {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/v1/organizations/"+pathOrg+"/projects", nil)
	if claimOrg != "" {
		r = r.WithContext(jwt.WithClaims(r.Context(), &jwt.Claims{OuHandle: claimOrg}))
	}
	ctx := humatest.NewContext(&huma.Operation{}, r, httptest.NewRecorder())
	in := &OrgScopedInput{OrgHandle: pathOrg}
	return in.Resolve(ctx)
}

func statusOf(t *testing.T, errs []error) int {
	t.Helper()
	if len(errs) == 0 {
		return 0
	}
	se, ok := errs[0].(huma.StatusError)
	if !ok {
		t.Fatalf("expected a huma.StatusError, got %T (%v)", errs[0], errs[0])
	}
	return se.GetStatus()
}

func TestOrgScopedResolve_Enforce(t *testing.T) {
	SetGateMode(tenant.GateModeEnforce)
	t.Cleanup(func() { SetGateMode(tenant.GateModeEnforce) })

	if errs := resolveWith(t, "acme", "acme"); len(errs) != 0 {
		t.Fatalf("same-org should pass, got %v", errs)
	}
	if got := statusOf(t, resolveWith(t, "acme", "evil")); got != 404 {
		t.Fatalf("cross-org should 404 (no existence leak), got %d", got)
	}
	if got := statusOf(t, resolveWith(t, "acme", "")); got != 401 {
		t.Fatalf("missing org claim should 401, got %d", got)
	}
	if got := statusOf(t, resolveWith(t, "Bad_Slug!", "Bad_Slug!")); got != 400 {
		t.Fatalf("malformed orgHandle should 400, got %d", got)
	}
}

func TestOrgScopedResolve_LogModePassesThrough(t *testing.T) {
	SetGateMode(tenant.GateModeLog)
	t.Cleanup(func() { SetGateMode(tenant.GateModeEnforce) })

	if errs := resolveWith(t, "acme", "evil"); len(errs) != 0 {
		t.Fatalf("log mode must pass cross-org through, got %v", errs)
	}
}
