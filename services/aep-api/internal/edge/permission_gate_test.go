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

	t.Run("GetSkill: ae:skill-view alone satisfies it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:skill-view"})
		if _, err := permissionGate(next, "GetSkill")(ctx, nil, req, nil); err != nil {
			t.Fatalf("ae:skill-view alone should satisfy GetSkill, got %v", err)
		}
		if !called {
			t.Fatal("handler must run")
		}
	})

	t.Run("GetSkill: ae:skill-config alone also satisfies it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:skill-config"})
		if _, err := permissionGate(next, "GetSkill")(ctx, nil, req, nil); err != nil {
			t.Fatalf("ae:skill-config alone should satisfy GetSkill (config implies view), got %v", err)
		}
		if !called {
			t.Fatal("handler must run")
		}
	})

	t.Run("GetSkill: neither permission denies", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "openid profile"})
		_, err := permissionGate(next, "GetSkill")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding neither ae:skill-view nor ae:skill-config, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
		}
	})

	t.Run("ListOrgEnvironments: ae:resource-config alone satisfies it (RegisterFormPage, no ae:requirement-view)", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:resource-config"})
		if _, err := permissionGate(next, "ListOrgEnvironments")(ctx, nil, req, nil); err != nil {
			t.Fatalf("ae:resource-config alone should satisfy ListOrgEnvironments, got %v", err)
		}
		if !called {
			t.Fatal("handler must run")
		}
	})

	t.Run("ListOrgEnvironments: ae:requirement-view alone does NOT satisfy it (its only caller, RegisterFormPage, requires ae:resource-config)", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:requirement-view"})
		_, err := permissionGate(next, "ListOrgEnvironments")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:requirement-view, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
		}
	})

	t.Run("ListPlatformResourceTypes: ae:resource-view alone satisfies it (org Resources catalog, no ae:requirement-view)", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:resource-view"})
		if _, err := permissionGate(next, "ListPlatformResourceTypes")(ctx, nil, req, nil); err != nil {
			t.Fatalf("ae:resource-view alone should satisfy ListPlatformResourceTypes, got %v", err)
		}
		if !called {
			t.Fatal("handler must run")
		}
	})

	t.Run("ListProjects: ae:requirement-update alone does NOT satisfy it (exact-match ae:requirement-view, same call as ae:resource-view/ae:resource-config)", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:requirement-update"})
		_, err := permissionGate(next, "ListProjects")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:requirement-update, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
		}
	})

	t.Run("ListProjects: ae:requirement-view alone still satisfies it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:requirement-view"})
		if _, err := permissionGate(next, "ListProjects")(ctx, nil, req, nil); err != nil {
			t.Fatalf("ae:requirement-view alone should still satisfy ListProjects, got %v", err)
		}
		if !called {
			t.Fatal("handler must run")
		}
	})

	t.Run("ListProjects: neither permission denies", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "openid profile"})
		_, err := permissionGate(next, "ListProjects")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding neither permission, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
		}
	})

	// GetProject (ProjectOverview's own shell read) is exact-match
	// ae:requirement-view, same call as ListProjects above: no role holds
	// ae:requirement-update without ae:requirement-view, so an OR here would
	// narrow nothing a real caller has.
	t.Run("GetProject: ae:requirement-view alone satisfies it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:requirement-view"})
		if _, err := permissionGate(next, "GetProject")(ctx, nil, req, nil); err != nil {
			t.Fatalf("ae:requirement-view alone should satisfy GetProject, got %v", err)
		}
		if !called {
			t.Fatal("handler must run")
		}
	})

	t.Run("GetProject: ae:requirement-update alone does NOT satisfy it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:requirement-update"})
		_, err := permissionGate(next, "GetProject")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:requirement-update, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
		}
	})

	t.Run("GetProject: neither permission denies", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "openid profile"})
		_, err := permissionGate(next, "GetProject")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding neither permission, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
		}
	})

	// ProjectOverview's remaining own reads — its status poll, its
	// Components section, and a component's Deployments/OpenAPI
	// drill-downs — moved off their previous borrowed gates
	// (ae:build/ae:build-view, or ae:design-view for GetComponentOpenapi)
	// onto the same exact-match ae:requirement-view GetProject uses.
	for _, op := range []string{"GetProjectStatus", "ListComponents", "ListDeployments", "GetComponentOpenapi"} {
		t.Run(op+": ae:requirement-view alone satisfies it", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:requirement-view"})
			if _, err := permissionGate(next, op)(ctx, nil, req, nil); err != nil {
				t.Fatalf("ae:requirement-view alone should satisfy %s, got %v", op, err)
			}
			if !called {
				t.Fatalf("%s: handler must run", op)
			}
		})

		for _, perm := range []string{"ae:requirement-update", "ae:build", "ae:build-view"} {
			t.Run(op+": "+perm+" alone does NOT satisfy it", func(t *testing.T) {
				called = false
				ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: perm})
				_, err := permissionGate(next, op)(ctx, nil, req, nil)
				var ae *apiError
				if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
					t.Fatalf("%s: want 403 holding only %s, got %v", op, perm, err)
				}
				if called {
					t.Fatalf("%s: handler must not run holding only %s", op, perm)
				}
			})
		}
	}

	t.Run("GetComponentOpenapi: ae:design-view alone no longer satisfies it (moved off the design-workspace gate)", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:design-view"})
		_, err := permissionGate(next, "GetComponentOpenapi")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:design-view, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
		}
	})

	// ProjectOverview's Dependencies section moved off ae:requirement-view
	// onto the same ae:resource-view every other resource read on the page
	// uses (ListPlatformResourceTypes/ListExternalResources below).
	t.Run("ListWorkloadDependencies: ae:resource-view alone satisfies it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:resource-view"})
		if _, err := permissionGate(next, "ListWorkloadDependencies")(ctx, nil, req, nil); err != nil {
			t.Fatalf("ae:resource-view alone should satisfy ListWorkloadDependencies, got %v", err)
		}
		if !called {
			t.Fatal("handler must run")
		}
	})

	t.Run("ListWorkloadDependencies: ae:requirement-view alone no longer satisfies it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:requirement-view"})
		_, err := permissionGate(next, "ListWorkloadDependencies")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:requirement-view, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
		}
	})

	// ListOrgEndpoints and ListProjectTags are the other two plain
	// exact-match ae:requirement-view reads on the Overview surface, same as
	// GetProject and the four above.
	for _, op := range []string{"ListOrgEndpoints", "ListProjectTags"} {
		t.Run(op+": ae:requirement-view alone satisfies it", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:requirement-view"})
			if _, err := permissionGate(next, op)(ctx, nil, req, nil); err != nil {
				t.Fatalf("ae:requirement-view alone should satisfy %s, got %v", op, err)
			}
			if !called {
				t.Fatalf("%s: handler must run", op)
			}
		})

		t.Run(op+": ae:requirement-update alone does NOT satisfy it", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:requirement-update"})
			_, err := permissionGate(next, op)(ctx, nil, req, nil)
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("%s: want 403 holding only ae:requirement-update, got %v", op, err)
			}
			if called {
				t.Fatalf("%s: handler must not run", op)
			}
		})
	}

	// ListExternalResources is read by four console call sites
	// (DeploymentsPage, ProjectOverview's Dependencies section, the org
	// Resources catalog page, and RegisterFormPage's edit-mode prefill), but
	// it's now exact-match ae:resource-view — ae:resource-config no longer
	// satisfies it on its own (dropped alongside ListPlatformResourceTypes'
	// identical OR, since no role holds config without view).
	t.Run("ListExternalResources: ae:resource-view alone satisfies it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:resource-view"})
		if _, err := permissionGate(next, "ListExternalResources")(ctx, nil, req, nil); err != nil {
			t.Fatalf("ae:resource-view alone should satisfy ListExternalResources, got %v", err)
		}
		if !called {
			t.Fatal("handler must run")
		}
	})

	for _, perm := range []string{"ae:build", "ae:build-view", "ae:resource-config"} {
		t.Run("ListExternalResources: "+perm+" alone does NOT satisfy it", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: perm})
			_, err := permissionGate(next, "ListExternalResources")(ctx, nil, req, nil)
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("want 403 holding only %s, got %v", perm, err)
			}
			if called {
				t.Fatal("handler must not run")
			}
		})
	}

	t.Run("ListExternalResources: no permission denies", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "openid profile"})
		_, err := permissionGate(next, "ListExternalResources")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding no resource permission, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
		}
	})

	t.Run("ListPlatformResourceTypes: ae:requirement-view alone does NOT satisfy it (Dependencies section now also requires a resource permission)", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:requirement-view"})
		_, err := permissionGate(next, "ListPlatformResourceTypes")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:requirement-view, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
		}
	})

	t.Run("ListPlatformResourceTypes: ae:resource-config alone does NOT satisfy it (view-only gate now, config no longer borrows an OR)", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:resource-config"})
		_, err := permissionGate(next, "ListPlatformResourceTypes")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:resource-config, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
		}
	})

	t.Run("RegisterExternalResource/UpdateExternalResource/DeleteExternalResource require ae:resource-config, not ae:build", func(t *testing.T) {
		for _, op := range []string{"RegisterExternalResource", "UpdateExternalResource", "DeleteExternalResource"} {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:build"})
			_, err := permissionGate(next, op)(ctx, nil, req, nil)
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("%s: ae:build alone must no longer satisfy it (moved off the borrowed build gate), got %v", op, err)
			}
			if called {
				t.Fatalf("%s: handler must not run holding only ae:build", op)
			}

			called = false
			ctx = auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:resource-config"})
			if _, err := permissionGate(next, op)(ctx, nil, req, nil); err != nil {
				t.Fatalf("%s: ae:resource-config should satisfy it, got %v", op, err)
			}
			if !called {
				t.Fatalf("%s: handler must run holding ae:resource-config", op)
			}
		}
	})

	t.Run("RegisterExternalResource: ae:resource-view alone (view, not config) denies", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:resource-view"})
		_, err := permissionGate(next, "RegisterExternalResource")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:resource-view, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
		}
	})

	// AI chat panel operations: OR-gated on ae:design (the project spec
	// chat, mounted by AppLayout) and ae:resource-config (the marketplace
	// registration-form assistant, mounted by RegisterFormPage) — two
	// unrelated console callers sharing one set of BFF operations, each
	// satisfied through its own page's own permission. ae:design-view alone
	// must NOT satisfy it: the panel sends turns, a write action, so the
	// weaker view permission doesn't cover it (unlike the Overview track's
	// Spec leg, which only needs to open a read surface).
	for _, op := range []string{
		"CreateTurn", "GetActiveTurn", "GetConversation", "GetTurn",
		"ListConversations", "RotateConversation", "StreamTurn",
	} {
		for _, perm := range []string{"ae:design", "ae:resource-config"} {
			t.Run(op+": "+perm+" alone satisfies it", func(t *testing.T) {
				called = false
				ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: perm})
				if _, err := permissionGate(next, op)(ctx, nil, req, nil); err != nil {
					t.Fatalf("%s alone should satisfy %s, got %v", perm, op, err)
				}
				if !called {
					t.Fatalf("%s: handler must run", op)
				}
			})
		}

		t.Run(op+": ae:design-view alone does NOT satisfy it", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:design-view"})
			_, err := permissionGate(next, op)(ctx, nil, req, nil)
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("%s: want 403 holding only ae:design-view, got %v", op, err)
			}
			if called {
				t.Fatalf("%s: handler must not run", op)
			}
		})
	}

	// GenerateDesign is deliberately NOT in the OR above: unlike CreateTurn,
	// it has no marketplace-assistant caller to accommodate, so it's
	// exact-match ae:design — the one place "may this caller trigger design
	// generation" is asked without ae:resource-config riding along too.
	t.Run("GenerateDesign: ae:design alone satisfies it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:design"})
		if _, err := permissionGate(next, "GenerateDesign")(ctx, nil, req, nil); err != nil {
			t.Fatalf("ae:design alone should satisfy GenerateDesign, got %v", err)
		}
		if !called {
			t.Fatal("handler must run")
		}
	})

	for _, perm := range []string{"ae:design-view", "ae:resource-config"} {
		t.Run("GenerateDesign: "+perm+" alone does NOT satisfy it", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: perm})
			_, err := permissionGate(next, "GenerateDesign")(ctx, nil, req, nil)
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("want 403 holding only %s, got %v", perm, err)
			}
			if called {
				t.Fatal("handler must not run")
			}
		})
	}

	// services/collab's three server-to-server operations: ValidateCollabAccess
	// (room join) and ReadFileBundle (seed read) need ae:design-view; ApplyFiles
	// (git-commit flush) needs the stronger ae:design, and none of the three are
	// OR'd with ae:resource-config — that OR belongs to the unrelated
	// Resources-registration chat sharing CreateTurn/StreamTurn, not to this room.
	for _, op := range []string{"ValidateCollabAccess", "ReadFileBundle"} {
		t.Run(op+": ae:design-view alone satisfies it", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:design-view"})
			if _, err := permissionGate(next, op)(ctx, nil, req, nil); err != nil {
				t.Fatalf("ae:design-view alone should satisfy %s, got %v", op, err)
			}
			if !called {
				t.Fatal("handler must run")
			}
		})

		t.Run(op+": ae:resource-config alone does NOT satisfy it", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:resource-config"})
			_, err := permissionGate(next, op)(ctx, nil, req, nil)
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("want 403 holding only ae:resource-config, got %v", err)
			}
			if called {
				t.Fatal("handler must not run")
			}
		})

		t.Run(op+": no claims at all → 403", func(t *testing.T) {
			called = false
			_, err := permissionGate(next, op)(context.Background(), nil, req, nil)
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("want 403, got %v", err)
			}
			if called {
				t.Fatal("handler must not run")
			}
		})
	}

	t.Run("ApplyFiles: ae:design alone satisfies it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:design"})
		if _, err := permissionGate(next, "ApplyFiles")(ctx, nil, req, nil); err != nil {
			t.Fatalf("ae:design alone should satisfy ApplyFiles, got %v", err)
		}
		if !called {
			t.Fatal("handler must run")
		}
	})

	for _, perm := range []string{"ae:design-view", "ae:resource-config"} {
		t.Run("ApplyFiles: "+perm+" alone does NOT satisfy it (view must not imply commit)", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: perm})
			_, err := permissionGate(next, "ApplyFiles")(ctx, nil, req, nil)
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("want 403 holding only %s, got %v", perm, err)
			}
			if called {
				t.Fatal("handler must not run")
			}
		})
	}
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

	codingAgentOnly := gen.UpdateConfigRequestObject{Body: &gen.ConfigPatch{}}
	codingAgentOnly.Body.CodingAgent.Sent = true

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

	t.Run("codingAgent section requires ae:model-config", func(t *testing.T) {
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:github-config"})
		_, err := permissionGate(next, "UpdateConfig")(ctx, nil, req, codingAgentOnly)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:github-config, got %v", err)
		}

		ctx = auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:model-config"})
		if _, err := permissionGate(next, "UpdateConfig")(ctx, nil, req, codingAgentOnly); err != nil {
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
