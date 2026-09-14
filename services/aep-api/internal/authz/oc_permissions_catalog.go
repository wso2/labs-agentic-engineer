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
// It carries none of PermissionRequirementView's read actions
// (project:view/workload:view/releasebinding:view/component:view): every
// ae:requirement-view row in permission_gate.go (GetProject, ListProjects,
// ListOrgEndpoints, GetProjectStatus, ListComponents, ListDeployments,
// GetComponentOpenapi, ListProjectTags) is exact-match, not OR-gated with
// ae:requirement-update — same call as ae:resource-view/ae:resource-config
// below, and for the same reason: no role in rolePermissionsCatalog holds
// ae:requirement-update without also holding ae:requirement-view, so an OR
// (and the OC actions it would require here) would grant a branch nothing
// exercises.
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
// PermissionRequirementView is ProjectOverview's own permission: every read
// the page's shell and its non-resource sections make traces here.
//   - project:view, traced to GetProject (the page's own shell read).
//   - workload:view, traced to ListOrgEndpoints (ListWorkloadEndpoints ->
//     GET /workloads).
//   - releasebinding:view, traced to GetProjectStatus and ListDeployments
//     (internal/projects/project_service.go's status aggregate and
//     component_service.go's ListDeployments both read OC's
//     ProjectReleaseBinding list via bindingsReader.ListProjectReleaseBindings
//     -> GET /releasebindings — distinct from the ResourceReleaseBinding CR
//     PermissionBuildView's own entry below reads).
//   - component:view, traced to ListComponents (component_service.go's
//     ListComponents -> s.client.ListComponents -> GET /components).
//
// GetComponentOpenapi/ListProjectTags share this permission too
// (permission_gate.go) but reach no OC action: the former reads a git-backed
// design.json via the ArtifactStore, the latter a git-backed ArtifactService
// tag list, so neither needs an entry here.
//
// It does NOT carry workload:view's siblings resource:view/resourcetype:view,
// or environment:view/clusterresourcetype:view: those back
// ListWorkloadDependencies/ListOrgEnvironments/ListPlatformResourceTypes,
// which all now require a resource permission instead (ProjectOverview's
// Dependencies section is resource data, gated the same as the org Resources
// catalog page rather than folded into the page's own shell permission) —
// see PermissionResourceView/PermissionResourceConfig below.
//
// PermissionBuildView carries resourcereleasebinding:view, traced to
// GetProjectDependencyReadiness (internal/dependencies/provisioning/
// status_service.go's bindingStatus -> resourceClient.GetBinding), which
// reads a ResourceReleaseBinding to report BuildDetailPage's External
// Resources panel. GetBuildLogs/StreamRunProgress/StreamTaskLog also gate on
// this permission but never reach OC, so they need no matching action here.
//
// PermissionResourceView backs every resource read reachable from a
// console page — ProjectOverview's Dependencies section, DeploymentsPage,
// the org Resources catalog page, and RegisterFormPage's edit-mode prefill —
// now that ListWorkloadDependencies/ListPlatformResourceTypes/
// ListExternalResources (permission_gate.go) are all exact-match
// ae:resource-view rather than OR-gated with ae:requirement-view or
// ae:resource-config: workload:view/resource:view (ListWorkloadDependencies
// -> workload_deps.go's ListWorkloadConsumerDeps/resolveResourceRow, three
// OC reads), clusterresourcetype:view (ListPlatformResourceTypes -> the org
// resource catalog's ClusterResourceType read) and resourcetype:view
// (ListExternalResources -> dependencies.ExternalResourceCatalog.List reads
// OC's namespaced ResourceType list).
//
// PermissionResourceConfig no longer duplicates clusterresourcetype:view/
// resourcetype:view: those existed here only for ListPlatformResourceTypes/
// ListExternalResources' now-removed OR branch, and no role in
// rolePermissionsCatalog holds this permission without also holding
// PermissionResourceView, so nothing loses reachability. What remains is
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
		"project:create",
		"project:delete",
		"deploymentpipeline:view",
		"projectreleasebinding:create",
	},
	string(PermissionRequirementView): {
		"project:view",
		"workload:view",
		"releasebinding:view",
		"component:view",
	},
	string(PermissionGitHubConfig): {
		"secretreference:view",
		"secretreference:create",
		"secretreference:update",
		"secretreference:delete",
	},
	string(PermissionResourceView): {
		"workload:view",
		"resource:view",
		"clusterresourcetype:view",
		"resourcetype:view",
	},
	string(PermissionResourceConfig): {
		"resourcetype:create",
		"resourcetype:update",
		"resourcetype:delete",
		"environment:view",
	},
}
