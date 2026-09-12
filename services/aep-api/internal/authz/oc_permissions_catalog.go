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

// OcActionCatalog is the AE-permission -> OpenChoreo AuthzRole action mapping
// fed to NewAuthZBridge. PLACEHOLDER (issue #743 decision) for
// PermissionSkillConfig/PermissionBuild: both resolve to the same starter
// action pair — component:view, component:create — pending a real
// per-permission OC action design. Revisit before this grants anything
// beyond the coding agent's own component read/create needs.
//
// PermissionBuild is included so ae-developer (whose other permissions —
// requirement-view, design-view — are unmapped) resolves to a non-empty
// action set: OC's AuthzRole CRD rejects spec.actions with zero entries, so
// an ae-developer role/binding could not be created at all without it.
//
// PermissionRequirementUpdate is traced to the CreateProject flow
// (internal/projects/project_service.go): project:create/delete for the
// project itself (delete covers the cell-provisioning and repo-name-conflict
// compensating rollbacks), deploymentpipeline:view for resolving the
// pipeline's promotion environments, and projectreleasebinding:create for
// the per-environment cell namespace each of those environments gets. Per
// OpenChoreo's own action catalog, project:create and deploymentpipeline:view
// have LowestScope=namespace, so the AuthzRoleBinding granting this must be
// bound at namespace scope — a binding narrowed to an existing project
// rejects project:create outright, since no project exists yet at create
// time.
//
// PermissionGitHubConfig is traced to the GitHub PAT connect/disconnect flow
// (internal/organization/secret_ref_writer.go's WriteGitHubPAT/
// DeleteGitHubPAT): in OSS OpenBao-direct mode (SecretReferenceManager not
// managing refs itself), aep-api's own OC client upserts the PAT's
// SecretReference CR directly, authenticated as the forwarding user's JWT —
// secretreference:view for the pre-write existence check,
// secretreference:create/update for the write, secretreference:delete for
// DisconnectGitProvider. Namespace-scoped, same as PermissionRequirementUpdate
// above.
//
// PermissionModelConfig is traced to the Anthropic-key connect flow
// (internal/organization/anthropic_credential_service.go's Connect ->
// secretRefWriter.WriteAnthropic): same upsertSecretReference path as
// PermissionGitHubConfig, so the same view/create/update trio — but no
// secretreference:delete, unlike GitHub. Disconnect
// (anthropic_credential_service.go's Disconnect) only deletes the local
// org_secrets row; it never calls secretRefWriter.DeleteAnthropic (which has
// no callers anywhere in the codebase), so the OC SecretReference is
// presently orphaned on disconnect rather than deleted — a gap in the
// disconnect flow itself, not something this permission mapping should paper
// over by granting delete for a call that's never made.
//
// PermissionRequirementView carries project:view. GetProject is a
// permissionGateCarveOuts entry (edge/permission_gate.go) — readable by any
// authenticated org member, no ae:* permission required at the BFF gate —
// but OC's own AuthzRole still needs the action regardless, and it's granted
// nowhere else. PermissionRequirementView is the mapping every role that
// should be able to view a project actually holds (both ae-admin and
// ae-developer), matching project:create's placement under
// PermissionRequirementUpdate above.
var OcActionCatalog = map[string][]string{
	string(PermissionModelConfig): {
		"secretreference:view",
		"secretreference:create",
		"secretreference:update",
	},
	string(PermissionSkillConfig): {"component:view", "component:create"},
	string(PermissionBuild):       {"component:view", "component:create"},
	string(PermissionRequirementUpdate): {
		"project:create",
		"project:delete",
		"deploymentpipeline:view",
		"projectreleasebinding:create",
	},
	string(PermissionRequirementView): {
		"project:view",
	},
	string(PermissionGitHubConfig): {
		"secretreference:view",
		"secretreference:create",
		"secretreference:update",
		"secretreference:delete",
	},
}
