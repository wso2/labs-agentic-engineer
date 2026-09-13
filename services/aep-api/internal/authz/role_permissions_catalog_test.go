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

package authz

import (
	"sort"
	"testing"
)

func TestAllPermissions_MatchesConstants(t *testing.T) {
	constants := []Permission{
		PermissionBuild,
		PermissionBuildView,
		PermissionDesignView,
		PermissionGitHubConfig,
		PermissionModelConfig,
		PermissionRequirementUpdate,
		PermissionRequirementView,
		PermissionSkillConfig,
		PermissionSkillView,
		PermissionUsageView,
		PermissionObservabilityView,
		PermissionAiChat,
	}

	if len(AllPermissions) != len(constants) {
		t.Fatalf("AllPermissions has %d entries, want %d", len(AllPermissions), len(constants))
	}

	seen := make(map[Permission]int, len(AllPermissions))
	for _, p := range AllPermissions {
		seen[p]++
	}
	for _, c := range constants {
		switch seen[c] {
		case 0:
			t.Errorf("constant %q missing from AllPermissions", c)
		case 1:
			// ok
		default:
			t.Errorf("constant %q appears %d times in AllPermissions", c, seen[c])
		}
	}
}

func TestRolePermissionsCatalog_KeysAreValidPermissions(t *testing.T) {
	valid := make(map[string]struct{}, len(AllPermissions))
	for _, p := range AllPermissions {
		valid[string(p)] = struct{}{}
	}

	for _, role := range Roles() {
		for _, perm := range RolePermissions(role) {
			if _, ok := valid[perm]; !ok {
				t.Errorf("role %q grants %q, which is not in AllPermissions", role, perm)
			}
		}
	}
}

func TestRoles_ReturnsCatalogKeys(t *testing.T) {
	want := []string{"ae-admin", "ae-developer"}
	got := Roles()
	sort.Strings(got)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("Roles() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Roles() = %v, want %v", got, want)
		}
	}
}

func TestRolePermissions_AeAdminHasAllPermissions(t *testing.T) {
	got := RolePermissions("ae-admin")
	sort.Strings(got)

	want := make([]string, len(AllPermissions))
	for i, p := range AllPermissions {
		want[i] = string(p)
	}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("RolePermissions(ae-admin) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RolePermissions(ae-admin) = %v, want %v", got, want)
		}
	}
}

func TestRolePermissions_AeDeveloperHasExactlyFivePermissions(t *testing.T) {
	got := RolePermissions("ae-developer")
	sort.Strings(got)

	want := []string{
		string(PermissionBuild),
		string(PermissionBuildView),
		string(PermissionDesignView),
		string(PermissionRequirementView),
		string(PermissionAiChat),
	}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("RolePermissions(ae-developer) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RolePermissions(ae-developer) = %v, want %v", got, want)
		}
	}
}

func TestRolePermissions_UnknownRoleReturnsNil(t *testing.T) {
	if got := RolePermissions("ae-nonexistent"); got != nil {
		t.Fatalf("RolePermissions(ae-nonexistent) = %v, want nil", got)
	}
}
