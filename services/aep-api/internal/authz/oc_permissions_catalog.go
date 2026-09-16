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

// OcActionCatalog maps each AE permission to the OpenChoreo AuthzRole actions
// its operations need. AuthZBridge resolves a role's AE permissions through
// this table when it provisions the org's OC AuthzRole, so a permission whose
// handlers reach OC must appear here — otherwise the BFF's own gate admits a
// caller that OC then refuses, and the failure surfaces one layer away from
// its cause.
//
// Two things the table deliberately does NOT contain:
//
//   - Permissions whose operations never reach OC. The design tree, skills,
//     build logs and run streams are git- or database-backed, so ae:design,
//     ae:design-view, ae:skill-view, ae:usage-view and ae:observability-view
//     have no entry and need none.
//
//   - Actions for an OR branch no role exercises. Read permissions are
//     exact-matched at the gate (permission_gate.go, rule 2), and no built-in
//     role holds a config permission without its view sibling, so granting the
//     view actions to the config permission as well would widen the OC role to
//     cover a caller shape that does not exist.
//
// Scope matters for the write permissions: project:create and
// deploymentpipeline:view have LowestScope=namespace in OpenChoreo's own
// catalog, so the AuthzRoleBinding granting ae:requirement-update must be bound
// at namespace scope. A binding narrowed to an existing project cannot satisfy
// project:create, since at create time no project exists to narrow to.
//
// PermissionSkillConfig and PermissionBuild share a placeholder pair
// (component:view + component:create) pending a real per-permission action
// design — issue #743. PermissionBuild also has to resolve to something
// non-empty for ae-developer to exist at all: OC's AuthzRole CRD rejects
// spec.actions with zero entries, and the role's other permissions are all
// unmapped by design.
var OcActionCatalog = map[string][]string{
	// The org's git PAT: upserted as a SecretReference CR by aep-api's own OC
	// client in OpenBao-direct mode. Delete is here and absent from the model
	// credential below because only this flow has a disconnect that removes the
	// CR.
	string(PermissionGitHubConfig): {
		"secretreference:view",
		"secretreference:create",
		"secretreference:update",
		"secretreference:delete",
	},
	// Same upsert path for the Anthropic key. Disconnect only deletes the local
	// org_secrets row and never calls DeleteAnthropic, so the OC SecretReference
	// is presently orphaned rather than removed — a gap in that flow, not one to
	// paper over by granting a delete nothing calls.
	string(PermissionModelConfig): {
		"secretreference:view",
		"secretreference:create",
		"secretreference:update",
	},

	string(PermissionSkillConfig): {"component:view", "component:create"},
	string(PermissionBuild):       {"component:view", "component:create"},

	// The project-creation flow: the project itself (delete covers the
	// cell-provisioning and repo-name-conflict rollbacks), the pipeline read
	// that resolves its promotion environments, and the per-environment cell
	// namespace each of those gets.
	string(PermissionRequirementUpdate): {
		"project:create",
		"project:delete",
		"deploymentpipeline:view",
		"projectreleasebinding:create",
	},
	// Every read the project shell and its non-resource sections make.
	string(PermissionRequirementView): {
		"project:view",
		"workload:view",
		"releasebinding:view",
		"component:view",
	},
	// BuildDetailPage's External Resources panel reads a ResourceReleaseBinding
	// — distinct from PermissionRequirementView's ProjectReleaseBinding above.
	// The other build-view operations never reach OC.
	string(PermissionBuildView): {
		"resourcereleasebinding:view",
	},

	string(PermissionResourceView): {
		"workload:view",
		"resource:view",
		"clusterresourcetype:view",
		"resourcetype:view",
	},
	// Ensure is a get-or-create, hence create rather than a separate get.
	// environment:view belongs here rather than with the view permission
	// because its only caller is the registration form's environment picker,
	// which is itself config-only (permission_gate.go).
	string(PermissionResourceConfig): {
		"resourcetype:create",
		"resourcetype:update",
		"resourcetype:delete",
		"environment:view",
	},
}
