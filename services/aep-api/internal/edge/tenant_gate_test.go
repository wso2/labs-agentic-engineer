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
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/contracttest"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// TestTenantGateCarveOuts_NameContractOperations is the arch-lock on the
// enumerated carve-out set: every key must be a method of the generated
// strict interface (i.e. a real contract operationID). A contract rename or
// removal that orphans a carve-out fails here instead of silently leaving a
// dead entry that looks like an un-gated operation.
func TestTenantGateCarveOuts_NameContractOperations(t *testing.T) {
	t.Parallel()
	iface := reflect.TypeOf((*gen.StrictServerInterface)(nil)).Elem()
	for op := range tenantGateCarveOuts {
		if _, ok := iface.MethodByName(op); !ok {
			t.Errorf("carve-out %q is not an operation of the generated strict interface", op)
		}
	}
	// The set is deliberately tiny; growing it is a security decision. Force
	// the diff (and this comment) into any PR that adds one.
	if len(tenantGateCarveOuts) != 1 {
		t.Errorf("carve-out set changed size (%d) — review deny-by-default posture", len(tenantGateCarveOuts))
	}
}

// TestNoClientSuppliedOrg is the IDOR arch-lock, contract edition (successor
// of the retired *_huma.go tag scan): the active org is derived SOLELY from
// the verified JWT — the committed contract must never declare a parameter
// that would let a request name an org. Re-adding one would reintroduce the
// IDOR class the token-only model closed.
func TestNoClientSuppliedOrg(t *testing.T) {
	t.Parallel()
	banned := []string{"orgHandle", "orgId", "organizationId"}
	raw := contracttest.SourceYAML(t)
	for _, name := range banned {
		if bytes.Contains(raw, []byte("name: "+name)) {
			t.Errorf("contract declares a client-supplied org parameter %q", name)
		}
	}
}

// TestTenantGate_DenyByDefault unit-tests the strict middleware itself:
// claimless requests are denied 401 in ENFORCE, pass with a canary in LOG,
// carve-outs bypass entirely, and a token org is bound into the context the
// handler sees.
func TestTenantGate_DenyByDefault(t *testing.T) {
	t.Parallel()

	seen := ""
	next := func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		seen = tenant.BoundOrgFromContext(ctx)
		return "ok", nil
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)

	t.Run("claimless ENFORCE → 401 apiError", func(t *testing.T) {
		ctx := tenant.WithGateMode(context.Background(), tenant.GateModeEnforce)
		_, err := tenantGate(next, "ListProjects")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusUnauthorized {
			t.Fatalf("want 401 apiError, got %v", err)
		}
	})

	t.Run("claimless LOG → passes unbound", func(t *testing.T) {
		seen = "sentinel"
		ctx := tenant.WithGateMode(context.Background(), tenant.GateModeLog)
		if _, err := tenantGate(next, "ListProjects")(ctx, nil, req, nil); err != nil {
			t.Fatalf("LOG mode must pass through, got %v", err)
		}
		if seen != "" {
			t.Fatalf("LOG pass-through must not bind an org, got %q", seen)
		}
	})

	t.Run("token org → bound into handler ctx", func(t *testing.T) {
		ctx := auth.WithClaims(context.Background(), &auth.Claims{OrgHandle: "acme"})
		if _, err := tenantGate(next, "ListProjects")(ctx, nil, req, nil); err != nil {
			t.Fatalf("authed call: %v", err)
		}
		if seen != "acme" {
			t.Fatalf("bound org: got %q want acme", seen)
		}
	})

	t.Run("carve-out bypasses the gate", func(t *testing.T) {
		seen = "sentinel"
		ctx := tenant.WithGateMode(context.Background(), tenant.GateModeEnforce)
		if _, err := tenantGate(next, "ListOrganizations")(ctx, nil, req, nil); err != nil {
			t.Fatalf("carve-out must not be denied, got %v", err)
		}
	})
}
