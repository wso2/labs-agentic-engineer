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

// AuthZBridge implements PermissionResolver. It holds the full AE→OC
// permission mapping and applies it on demand.
type AuthZBridge struct {
	// permissionMap is the authoritative AE→OC translation table.
	// Each key is an AE permission string; each value is the set of OC
	// permissions that must be granted to satisfy it.
	permissionMap map[string][]string
}

// NewAuthZBridge constructs an AuthZBridge from the given AE→OC mapping.
//
//deadcode:keep the AuthzRoles this resolves are installed by the platform
//chart (templates/authz/ae-roles.yaml), not created at runtime, so nothing on
//the request path calls it. It stays because it DEFINES the list the chart
//must carry — TestChartAuthzRolesMatchCatalog resolves it and fails when the
//YAML disagrees, which is the only thing keeping the two in step.
func NewAuthZBridge(permissionMap map[string][]string) *AuthZBridge {
	// Defensive copy: the caller retains their map reference and could mutate
	// it after construction. The bridge's mapping must not change after init.
	m := make(map[string][]string, len(permissionMap))
	for k, v := range permissionMap {
		cp := make([]string, len(v))
		copy(cp, v)
		m[k] = cp
	}
	return &AuthZBridge{permissionMap: m}
}

// ResolveOcPermissions returns the deduplicated set of OC actions that satisfy
// the given AE permissions — a role's `spec.actions` in the chart.
//
//deadcode:keep see NewAuthZBridge.
func (b *AuthZBridge) ResolveOcPermissions(aePermissions []string) []string {
	ocSet := make(map[string]struct{})
	for _, ae := range aePermissions {
		for _, oc := range b.permissionMap[ae] {
			ocSet[oc] = struct{}{}
		}
	}

	result := make([]string, 0, len(ocSet))
	for oc := range ocSet {
		result = append(result, oc)
	}
	return result
}
