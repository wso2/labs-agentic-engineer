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
	"github.com/wso2/aep/aep-api/internal/spec"
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

// chatRequestFor builds the real generated request object for a chat
// operation, carrying the given project name. The concrete types matter:
// chatPermissions reads the project off them by type switch, so a stand-in
// would test nothing.
func chatRequestFor(op, project string) any {
	switch op {
	case "CreateTurn":
		return gen.CreateTurnRequestObject{ProjectName: project}
	case "GetActiveTurn":
		return gen.GetActiveTurnRequestObject{ProjectName: project}
	case "GetConversation":
		return gen.GetConversationRequestObject{ProjectName: project}
	case "GetTurn":
		return gen.GetTurnRequestObject{ProjectName: project}
	case "ListConversations":
		return gen.ListConversationsRequestObject{ProjectName: project}
	case "RotateConversation":
		return gen.RotateConversationRequestObject{ProjectName: project}
	case "StreamTurn":
		return gen.StreamTurnRequestObject{ProjectName: project}
	}
	panic("chatRequestFor: unknown chat operation " + op)
}

// TestChatOperationsCoverTheirRows keeps the project-scoped set and the rows it
// applies to from drifting apart: a chat operation added to one and not the
// other would either lose its marketplace branch or silently keep the old OR.
func TestChatOperationsCoverTheirRows(t *testing.T) {
	t.Parallel()
	for op := range chatOperations {
		perms, ok := operationPermissions[op]
		if !ok {
			t.Errorf("chat operation %q has no operationPermissions row", op)
			continue
		}
		if len(perms) != 1 || perms[0] != authz.PermissionDesign {
			t.Errorf("chat operation %q must declare ae:design as its real-project fallback, got %v", op, perms)
		}
		if chatRequestFor(op, "x") == nil {
			t.Errorf("chat operation %q has no request shape in chatRequestFor", op)
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

	t.Run("ListProjectBuilds: ae:build-view alone satisfies it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:build-view"})
		if _, err := permissionGate(next, "ListProjectBuilds")(ctx, nil, req, nil); err != nil {
			t.Fatalf("ae:build-view alone should satisfy ListProjectBuilds, got %v", err)
		}
		if !called {
			t.Fatal("handler must run")
		}
	})

	// Exact-match, not OR'd: a write permission gates mutations (BuildProject,
	// CancelRun, …), never page entry on its own — a build-only caller (no
	// view grant) must be blocked from every build/deployment read surface.
	for _, op := range []string{
		"ListProjectBuilds", "ListBuildRuns", "ListTasks", "GetTask",
		"ListCycleBuilds", "GetProjectRoles",
	} {
		t.Run(op+": ae:build alone does NOT satisfy it", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:build"})
			_, err := permissionGate(next, op)(ctx, nil, req, nil)
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("want 403 holding only ae:build, got %v", err)
			}
			if called {
				t.Fatal("handler must not run")
			}
		})

		t.Run(op+": ae:build-view alone satisfies it", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:build-view"})
			if _, err := permissionGate(next, op)(ctx, nil, req, nil); err != nil {
				t.Fatalf("ae:build-view alone should satisfy %s, got %v", op, err)
			}
			if !called {
				t.Fatal("handler must run")
			}
		})
	}

	// ListDesignDependencies: a genuine cross-category OR (either page's own
	// view permission), unlike the same-category write/view ORs eliminated
	// above — ae:build (the write permission) must NOT satisfy it on its own.
	t.Run("ListDesignDependencies: ae:build alone does NOT satisfy it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:build"})
		_, err := permissionGate(next, "ListDesignDependencies")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:build, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
		}
	})

	for _, perm := range []string{"ae:design-view", "ae:build-view"} {
		t.Run("ListDesignDependencies: "+perm+" alone satisfies it", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: perm})
			if _, err := permissionGate(next, "ListDesignDependencies")(ctx, nil, req, nil); err != nil {
				t.Fatalf("%s alone should satisfy ListDesignDependencies, got %v", perm, err)
			}
			if !called {
				t.Fatal("handler must run")
			}
		})
	}

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

	// Exact-match, not OR'd: a write permission gates mutations, never page
	// entry on its own — a skill-config-only caller (no view grant) must be
	// blocked from GetSkill the same as any other write-without-view case.
	t.Run("GetSkill: ae:skill-config alone does NOT satisfy it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:skill-config"})
		_, err := permissionGate(next, "GetSkill")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:skill-config, got %v", err)
		}
		if called {
			t.Fatal("handler must not run")
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

	t.Run("ListSkills: ae:skill-view alone satisfies it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:skill-view"})
		if _, err := permissionGate(next, "ListSkills")(ctx, nil, req, nil); err != nil {
			t.Fatalf("ae:skill-view alone should satisfy ListSkills, got %v", err)
		}
		if !called {
			t.Fatal("handler must run")
		}
	})

	t.Run("ListSkills: ae:skill-config alone does NOT satisfy it", func(t *testing.T) {
		called = false
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:skill-config"})
		_, err := permissionGate(next, "ListSkills")(ctx, nil, req, nil)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 holding only ae:skill-config, got %v", err)
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

	// AI chat panel operations. One panel, two mount points, and the
	// requirement follows WHICH project the request addresses — because the
	// two mount points differ by path, not by operation.
	//
	// ae:resource-config satisfies these only for the marketplace registration
	// assistant's synthetic project. It must NOT satisfy them for a real one:
	// a turn's instruction carries `/<skill>` flow commands verbatim for the
	// server to expand, so a caller who could send any turn at a real project
	// could send `/design` — which is exactly what generate-design's ae:design
	// gate exists to control.
	//
	// ae:design-view satisfies neither: the panel sends turns, a write action.
	for _, op := range []string{
		"CreateTurn", "GetActiveTurn", "GetConversation", "GetTurn",
		"ListConversations", "RotateConversation", "StreamTurn",
	} {
		t.Run(op+": ae:design satisfies it on a real project", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:design"})
			if _, err := permissionGate(next, op)(ctx, nil, req, chatRequestFor(op, "shop")); err != nil {
				t.Fatalf("%s: ae:design should satisfy a real project, got %v", op, err)
			}
			if !called {
				t.Fatalf("%s: handler must run", op)
			}
		})

		t.Run(op+": ae:resource-config does NOT satisfy it on a real project", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:resource-config"})
			_, err := permissionGate(next, op)(ctx, nil, req, chatRequestFor(op, "shop"))
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("%s: want 403 for ae:resource-config on a real project, got %v", op, err)
			}
			if called {
				t.Fatalf("%s: handler must not run", op)
			}
		})

		t.Run(op+": ae:resource-config satisfies it on the marketplace project", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:resource-config"})
			marketplace := chatRequestFor(op, spec.MarketplaceRegisterProjectID)
			if _, err := permissionGate(next, op)(ctx, nil, req, marketplace); err != nil {
				t.Fatalf("%s: the marketplace assistant must still work, got %v", op, err)
			}
			if !called {
				t.Fatalf("%s: handler must run", op)
			}
		})

		t.Run(op+": ae:design-view alone does NOT satisfy it", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:design-view"})
			_, err := permissionGate(next, op)(ctx, nil, req, chatRequestFor(op, "shop"))
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("%s: want 403 holding only ae:design-view, got %v", op, err)
			}
			if called {
				t.Fatalf("%s: handler must not run", op)
			}
		})

		// An unrecognized request shape must not widen the requirement to the
		// marketplace's permission — the gate falls back to the stricter one.
		t.Run(op+": an unreadable request denies ae:resource-config", func(t *testing.T) {
			called = false
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:resource-config"})
			_, err := permissionGate(next, op)(ctx, nil, req, nil)
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("%s: want 403 for an unreadable request, got %v", op, err)
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

	// The idp section repoints the issuer the org's protected APIs pin JWT
	// validation to. No AE permission describes identity config, so the gate
	// refuses the section outright — including for a caller holding every
	// permission there is, since no grant can express consent to this.
	t.Run("idp section is refused, however privileged the caller", func(t *testing.T) {
		for _, scope := range []string{"", "ae:github-config ae:model-config"} {
			ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: scope})
			_, err := permissionGate(next, "UpdateConfig")(ctx, nil, req, idpOnly)
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("scope %q: want 403 for an idp section, got %v", scope, err)
			}
		}
	})

	// An idp section refuses the WHOLE patch, not just its own half: sections
	// are applied together, so letting the rest through would half-apply a
	// request the caller may not have wanted split.
	t.Run("idp alongside a permitted section still refuses", func(t *testing.T) {
		mixed := gen.UpdateConfigRequestObject{Body: &gen.ConfigPatch{}}
		mixed.Body.GitProvider.Sent = true
		mixed.Body.IDP.Sent = true
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:github-config ae:model-config"})
		_, err := permissionGate(next, "UpdateConfig")(ctx, nil, req, mixed)
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
			t.Fatalf("want 403 when idp rides along with gitProvider, got %v", err)
		}
	})

	// Deny-by-default extends to the request shape itself: a body the gate
	// cannot read is refused rather than treated as "no sections, nothing to
	// check". The route's validator rejects a bodyless PATCH first, so this
	// pins the posture, not a reachable path.
	t.Run("an unreadable request body is refused", func(t *testing.T) {
		ctx := auth.WithClaims(context.Background(), &auth.Claims{Scope: "ae:github-config ae:model-config"})
		for name, bad := range map[string]any{
			"nil body":    gen.UpdateConfigRequestObject{Body: nil},
			"wrong type":  "not an UpdateConfigRequestObject",
			"nil request": nil,
		} {
			_, err := permissionGate(next, "UpdateConfig")(ctx, nil, req, bad)
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
				t.Fatalf("%s: want 403, got %v", name, err)
			}
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
