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

import "github.com/wso2/aep/aep-api/aeperms"

// The AE permission vocabulary and the roles that hold it live in the public
// aeperms package, not here, because aectl needs the same list: it provisions
// the `ae` resource server, its actions, and these roles into Thunder at
// install time, and a permission Thunder never declares is one no token can
// ever carry. Sharing the vocabulary in Go makes that agreement a compile-time
// fact — see aeperms' own doc comment for the failure it prevents.
//
// This file is the alias layer, so the rest of aep-api goes on saying
// authz.PermissionBuild and authz.Permission with no idea the definitions
// moved. What stays aep-api's own is everything that maps a permission to THIS
// service's behaviour: the operations it gates (internal/edge) and the OC
// actions it implies (oc_permissions_catalog.go).

// Permission is an AE-level permission key, e.g. "ae:build". A type alias, not
// a defined type, so a value crosses between here and aeperms freely.
type Permission = aeperms.Permission

const (
	PermissionBuild             = aeperms.PermissionBuild
	PermissionBuildView         = aeperms.PermissionBuildView
	PermissionDesign            = aeperms.PermissionDesign
	PermissionDesignView        = aeperms.PermissionDesignView
	PermissionGitHubConfig      = aeperms.PermissionGitHubConfig
	PermissionModelConfig       = aeperms.PermissionModelConfig
	PermissionRequirementUpdate = aeperms.PermissionRequirementUpdate
	PermissionRequirementView   = aeperms.PermissionRequirementView
	PermissionSkillConfig       = aeperms.PermissionSkillConfig
	PermissionSkillView         = aeperms.PermissionSkillView
	PermissionUsageView         = aeperms.PermissionUsageView
	PermissionObservabilityView = aeperms.PermissionObservabilityView
	PermissionResourceView      = aeperms.PermissionResourceView
	PermissionResourceConfig    = aeperms.PermissionResourceConfig
)

// AllPermissions is every AE permission key the platform recognizes.
var AllPermissions = aeperms.AllPermissions

// RolePermissions returns the AE permissions for the given role as plain
// strings, or nil if the role is unknown. Strings rather than Permissions
// because the only caller feeds PermissionResolver.ResolveOcPermissions, which
// keys the OC action catalog by the raw key.
func RolePermissions(role string) []string {
	perms := aeperms.RolePermissions(role)
	if perms == nil {
		return nil
	}
	out := make([]string, len(perms))
	for i, p := range perms {
		out[i] = string(p)
	}
	return out
}

// Roles returns the AE role names.
func Roles() []string { return aeperms.Roles() }
