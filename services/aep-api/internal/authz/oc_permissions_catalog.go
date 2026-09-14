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
// It also carries project:view: ListProjects and GetProject (permission_gate.go)
// are both OR-gated on ae:requirement-view OR ae:requirement-update, since a
// requirement-update caller needs to find AND open the project they just
// created — reaching OC via the same Service.ListProjects/GetProject ->
// s.client.ListProjects/GetProject calls, so both need the same action
// GetProject already carries under PermissionRequirementView below.
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
// PermissionRequirementView carries project:view, traced to GetProject
// (permission_gate.go OR-gates it with PermissionRequirementUpdate — both
// ae-admin and ae-developer hold this permission, matching project:create's
// placement under PermissionRequirementUpdate above).
//
// The same permission also carries workload:view/resource:view/
// resourcetype:view, traced to ListWorkloadDependencies
// (internal/dependencies/provisioning/workload_deps.go): ListWorkloadConsumerDeps
// reads OC's Workload list, and resolveResourceRow reads back each dependency's
// Resource and (for org-catalog-typed ones) ResourceType — three separate OC
// reads for ProjectOverview's Dependencies section. ListOrgEndpoints reuses
// workload:view already listed above (it reads the same OC Workload list
// ListWorkloadDependencies does).
//
// It does NOT carry environment:view or clusterresourcetype:view: those once
// backed ListOrgEnvironments/ListPlatformResourceTypes reachability via this
// permission, but both operations dropped ae:requirement-view from their OR
// (permission_gate.go) once their only requirement-view-reached console
// caller — ProjectOverview's Dependencies section — started requiring a
// resource permission too, same as every other caller. Those two actions now
// live only under PermissionResourceView/PermissionResourceConfig below.
//
// PermissionBuildView carries resourcereleasebinding:view, traced to
// GetProjectDependencyReadiness (internal/dependencies/provisioning/
// status_service.go's bindingStatus -> resourceClient.GetBinding), which
// reads a ResourceReleaseBinding to report BuildDetailPage's External
// Resources panel. GetBuildLogs/StreamRunProgress/StreamTaskLog also gate on
// this permission but never reach OC, so they need no matching action here.
//
// PermissionResourceView/PermissionResourceConfig back the org Resources
// catalog page (settings > Resources) and its register/edit flow —
// ListPlatformResourceTypes and ListExternalResources are OR-gated on either
// (see permission_gate.go), so a view-only holder still needs the same OC
// reads a config holder does: clusterresourcetype:view (ListPlatformResourceTypes
// -> the org resource catalog's ClusterResourceType read, shared with
// PermissionRequirementView's own entry above) and resourcetype:view
// (ListExternalResources -> dependencies.ExternalResourceCatalog.List reads
// OC's namespaced ResourceType list). PermissionResourceConfig adds
// resourcetype:create/update/delete, traced to RegisterExternalResource/
// UpdateExternalResource/DeleteExternalResource (internal/dependencies/
// provisioning's ExternalRTCatalog.Ensure/Update/Delete — Ensure is a
// get-or-create, hence :create rather than a separate get action), plus
// environment:view for ListOrgEnvironments (its only console caller,
// RegisterFormPage's environment picker, is itself config-only —
// see permission_gate.go's ListOrgEnvironments entry).
var OcActionCatalog = map[string][]string{
	string(PermissionModelConfig): {
		"secretreference:view",
		"secretreference:create",
		"secretreference:update",
	},
	string(PermissionSkillConfig): {"component:view", "component:create"},
	string(PermissionBuild):       {"component:view", "component:create"},
	string(PermissionBuildView): {
		"resourcereleasebinding:view",
	},
	string(PermissionRequirementUpdate): {
		"project:view",
		"project:create",
		"project:delete",
		"deploymentpipeline:view",
		"projectreleasebinding:create",
	},
	string(PermissionRequirementView): {
		"project:view",
		"workload:view",
		"resource:view",
		"resourcetype:view",
	},
	string(PermissionGitHubConfig): {
		"secretreference:view",
		"secretreference:create",
		"secretreference:update",
		"secretreference:delete",
	},
	string(PermissionResourceView): {
		"clusterresourcetype:view",
		"resourcetype:view",
	},
	string(PermissionResourceConfig): {
		"clusterresourcetype:view",
		"resourcetype:view",
		"resourcetype:create",
		"resourcetype:update",
		"resourcetype:delete",
		"environment:view",
	},
}
