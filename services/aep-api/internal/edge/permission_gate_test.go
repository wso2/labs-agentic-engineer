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
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/aep/aep-api/internal/authz"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// TestPermissionGateCoverage asserts every operation of the generated strict
// interface is present in EXACTLY ONE of operationPermissions or
// permissionGateCarveOuts. This is the deny-by-default arch-lock: a new
// contract operation that lands without a permission decision fails here
// instead of shipping silently ungated. Mirrors
// TestTenantGateCarveOuts_NameContractOperations's honesty discipline.
func TestPermissionGateCoverage(t *testing.T) {
	t.Parallel()
	for _, op := range contractOps() {
		_, hasPerm := operationPermissions[op]
		_, carvedOut := permissionGateCarveOuts[op]
		switch {
		case hasPerm && carvedOut:
			t.Errorf("op %q is in BOTH operationPermissions and permissionGateCarveOuts — pick one", op)
		case !hasPerm && !carvedOut:
			t.Errorf("op %q has no permission requirement and no carve-out — add an operationPermissions "+
				"entry or an explicit permissionGateCarveOuts entry (with a reason)", op)
		}
	}
}

// TestPermissionGateCarveOutsNameContractOperations is the mirror check: every
// carve-out key must be a real contract operationID, so a contract rename or
// removal can't leave a dead entry masquerading as a deliberate decision.
func TestPermissionGateCarveOutsNameContractOperations(t *testing.T) {
	t.Parallel()
	iface := contractOpsSet()
	for op := range permissionGateCarveOuts {
		if !iface[op] {
			t.Errorf("carve-out %q is not an operation of the generated strict interface", op)
		}
	}
	for op := range operationPermissions {
		if !iface[op] {
			t.Errorf("operationPermissions names %q, which is not an operation of the generated strict interface", op)
		}
	}
}

func contractOpsSet() map[string]bool {
	set := make(map[string]bool)
	for _, op := range contractOps() {
		set[op] = true
	}
	return set
}

// TestPermissionGate_DenyByDefault unit-tests the gate itself: a carve-out
// bypasses entirely, a held permission passes, a missing one 403s, and the
// OR semantics of a multi-permission requirement accept any one of them.
func TestPermissionGate_DenyByDefault(t *testing.T) {
	t.Parallel()

	called := false
	next := func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		called = true
		return "ok", nil
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)

	t.Run("carve-out bypasses the gate", func(t *testing.T) {
		called = false
		ctx := context.Background()
		if _, err := permissionGate(next, "ListOrganizations")(ctx, nil, req, nil); err != nil {
			t.Fatalf("carve-out must not be denied, got %v", err)
		}
		if !called {
			t.Fatal("carve-out must call through to the handler")
		}
	})

	t.Run("missing permission → 403 apiError", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "openid profile"})
		_, err := permissionGate(next, "DisconnectGitProvider")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 apiError, got %v", err)
		}
		if called {
			t.Fatal("handler must not run when the permission is missing")
		}
	})

	t.Run("held permission → passes through", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "openid ae:github-config"})
		if _, err := permissionGate(next, "DisconnectGitProvider")(ctx, nil, req, nil); err != nil {
			t.Fatalf("authed call: %v", err)
		}
		if !called {
			t.Fatal("handler must run once the permission is held")
		}
	})

	t.Run("no claims at all → 403", func(t *testing.T) {
		called = false
		_, err := permissionGate(next, "DisconnectGitProvider")(context.Background(), nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 apiError, got %v", err)
		}
	})

	t.Run("multi-permission requirement: either satisfies (OR)", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:build-view"})
		if _, err := permissionGate(next, "ListProjectBuilds")(ctx, nil, req, nil); err != nil {
			t.Fatalf("ae:build-view alone should satisfy ListProjectBuilds, got %v", err)
		}
		if !called {
			t.Fatal("handler must run")
		}
	})
}

// TestPermissionGate_UpdateConfig unit-tests the field-aware special case:
// each populated section requires its own permission, and a patch touching
// two sections requires BOTH (AND, unlike the OR semantics of a plain
// operationPermissions entry).
func TestPermissionGate_UpdateConfig(t *testing.T) {
	t.Parallel()

	next := func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		return "ok", nil
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/config", nil)

	gitProviderOnly := gen.UpdateConfigRequestObject{Body: &gen.ConfigPatch{}}
	gitProviderOnly.Body.GitProvider.Sent = true

	llmOnly := gen.UpdateConfigRequestObject{Body: &gen.ConfigPatch{}}
	llmOnly.Body.LLM.Sent = true

	both := gen.UpdateConfigRequestObject{Body: &gen.ConfigPatch{}}
	both.Body.GitProvider.Sent = true
	both.Body.CodingLLM.Sent = true

	idpOnly := gen.UpdateConfigRequestObject{Body: &gen.ConfigPatch{}}
	idpOnly.Body.IDP.Sent = true

	t.Run("gitProvider section requires ae:github-config", func(t *testing.T) {
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:model-config"})
		_, err := permissionGate(next, "UpdateConfig")(ctx, nil, req, gitProviderOnly)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:model-config, got %v", err)
		}

		ctx = auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:github-config"})
		if _, err := permissionGate(next, "UpdateConfig")(ctx, nil, req, gitProviderOnly); err != nil {
			t.Fatalf("want pass holding ae:github-config, got %v", err)
		}
	})

	t.Run("llm section requires ae:model-config", func(t *testing.T) {
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:github-config"})
		_, err := permissionGate(next, "UpdateConfig")(ctx, nil, req, llmOnly)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:github-config, got %v", err)
		}

		ctx = auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:model-config"})
		if _, err := permissionGate(next, "UpdateConfig")(ctx, nil, req, llmOnly); err != nil {
			t.Fatalf("want pass holding ae:model-config, got %v", err)
		}
	})

	t.Run("both sections require BOTH permissions", func(t *testing.T) {
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:github-config"})
		_, err := permissionGate(next, "UpdateConfig")(ctx, nil, req, both)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only one of two required permissions, got %v", err)
		}

		ctx = auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:github-config ae:model-config"})
		if _, err := permissionGate(next, "UpdateConfig")(ctx, nil, req, both); err != nil {
			t.Fatalf("want pass holding both permissions, got %v", err)
		}
	})

	t.Run("idp-only patch is presently unrestricted", func(t *testing.T) {
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: ""})
		if _, err := permissionGate(next, "UpdateConfig")(ctx, nil, req, idpOnly); err != nil {
			t.Fatalf("idp section has no assigned permission yet, want pass, got %v", err)
		}
	})
}

// TestContainsPermission and TestHasAnyPermission pin the small helpers
// directly, independent of the gate's HTTP plumbing.
func TestContainsPermission(t *testing.T) {
	t.Parallel()
	held := []authz.Permission{authz.PermissionBuild, authz.PermissionSkillConfig}
	if !containsPermission(held, authz.PermissionBuild) {
		t.Fatal("want true for a held permission")
	}
	if containsPermission(held, authz.PermissionModelConfig) {
		t.Fatal("want false for a permission not held")
	}
}

func TestHasAnyPermission(t *testing.T) {
	t.Parallel()
	claims := &auth.Claims{Scope: "ae:build-view"}
	if !hasAnyPermission(claims, []authz.Permission{authz.PermissionBuild, authz.PermissionBuildView}) {
		t.Fatal("want true: holds one of the two required permissions")
	}
	if hasAnyPermission(claims, []authz.Permission{authz.PermissionModelConfig}) {
		t.Fatal("want false: holds none of the required permissions")
	}
	if hasAnyPermission(claims, nil) {
		t.Fatal("want false: an empty required list denies by construction")
	}
}
