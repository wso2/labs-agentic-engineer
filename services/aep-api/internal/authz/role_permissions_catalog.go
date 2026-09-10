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

var rolePermissionsCatalog = map[string][]string{
	"ae-admin": {
		"ae:build",
		"ae:build-view",
		"ae:design-view",
		"ae:github-config",
		"ae:model-config",
		"ae:requirement-update",
		"ae:skill-config",
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
