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

// Permission is an AE-level permission key, e.g. "ae:build". It is the unit
// both this role catalog and the OC action catalog
// (oc_permissions_catalog.go) resolve, and the same string arrives on a
// caller's JWT as an OAuth scope entry.
type Permission string

const (
	PermissionBuild     Permission = "ae:build"
	PermissionBuildView Permission = "ae:build-view"
	// PermissionDesign is the console-side gate for the Overview track's Spec
	// leg (apps/console's OverviewTrack), paired with PermissionDesignView the
	// same way every other surface pairs a write permission with its view-only
	// sibling. No OC action maps to it (yet): the spec editor's actual write
	// path (ApplyFiles/ReadFileBundle, services/collab) is still a
	// permission_gate.go carve-out pending a permission design for
	// non-console callers, so this permission currently backs UI gating only.
	PermissionDesign            Permission = "ae:design"
	PermissionDesignView        Permission = "ae:design-view"
	PermissionGitHubConfig      Permission = "ae:github-config"
	PermissionModelConfig       Permission = "ae:model-config"
	PermissionRequirementUpdate Permission = "ae:requirement-update"
	PermissionRequirementView   Permission = "ae:requirement-view"
	PermissionSkillConfig       Permission = "ae:skill-config"
	PermissionSkillView         Permission = "ae:skill-view"
	PermissionUsageView         Permission = "ae:usage-view"
	PermissionObservabilityView Permission = "ae:observability-view"
	PermissionResourceView      Permission = "ae:resource-view"
	PermissionResourceConfig    Permission = "ae:resource-config"
)

// AllPermissions is every AE permission key the platform recognizes.
var AllPermissions = []Permission{
	PermissionBuild,
	PermissionBuildView,
	PermissionDesign,
	PermissionDesignView,
	PermissionGitHubConfig,
	PermissionModelConfig,
	PermissionRequirementUpdate,
	PermissionRequirementView,
	PermissionSkillConfig,
	PermissionSkillView,
	PermissionUsageView,
	PermissionObservabilityView,
	PermissionResourceView,
	PermissionResourceConfig,
}

var rolePermissionsCatalog = map[string][]string{
	"ae-admin": {
		string(PermissionBuild),
		string(PermissionBuildView),
		string(PermissionDesign),
		string(PermissionDesignView),
		string(PermissionGitHubConfig),
		string(PermissionModelConfig),
		string(PermissionRequirementUpdate),
		string(PermissionRequirementView),
		string(PermissionSkillConfig),
		string(PermissionSkillView),
		string(PermissionUsageView),
		string(PermissionObservabilityView),
		string(PermissionResourceView),
		string(PermissionResourceConfig),
	},
	// ae:usage-view/ae:observability-view are NOT included — those read org
	// spend and incident/alert reports, which reads as an admin-facing
	// concern absent a decision to extend it. ae:design pairs with the
	// ae:design-view already held here — same full write/view pair as
	// ae:build/ae:build-view below, since ae-developer is the role that
	// actually authors specs; it's also what keeps this role able to use
	// the AI chat panel (permission_gate.go's CreateTurn/GetActiveTurn/etc.
	// gate on ae:design, not a dedicated chat permission — the panel is a
	// facet of the design workspace, not a separate feature).
	"ae-developer": {
		string(PermissionRequirementView),
		string(PermissionDesign),
		string(PermissionDesignView),
		string(PermissionBuild),
		string(PermissionBuildView),
	},
}

// RolePermissions returns the AE permissions for the given role, or nil if the
// role has no entry in the catalog. The result is a defensive copy: the
// catalog itself is not exported, so nothing outside this function can
// mutate it.
func RolePermissions(role string) []string {
	perms, ok := rolePermissionsCatalog[role]
	if !ok {
		return nil
	}
	cp := make([]string, len(perms))
	copy(cp, perms)
	return cp
}

func Roles() []string {
	roles := make([]string, 0, len(rolePermissionsCatalog))
	for role := range rolePermissionsCatalog {
		roles = append(roles, role)
	}
	return roles
}
