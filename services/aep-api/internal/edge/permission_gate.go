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
	"log/slog"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/authz"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// operationPermissions maps each contract operationID to the AE permission(s)
// that satisfy it. A caller holding ANY listed permission may call the
// operation — this mirrors the console's useHasAnyPermission semantics for
// read surfaces shared by a write and a view-only permission (e.g. a build
// page readable by either ae:build or ae:build-view).
//
// Every operationID is required to appear in EITHER this map OR
// permissionGateCarveOuts — TestPermissionGateCoverage pins that — so a new
// contract operation can never ship silently ungated.
var operationPermissions = map[string][]authz.Permission{
	// Skills (settings > Skills panel).
	"SyncSkills":      {authz.PermissionSkillConfig},
	"SetSkillEnabled": {authz.PermissionSkillConfig},
	"DeleteSkill":     {authz.PermissionSkillConfig},
	"UpdateSkill":     {authz.PermissionSkillConfig},
	"ImportSkill":     {authz.PermissionSkillConfig},
	"CreateSkill":     {authz.PermissionSkillConfig},
	// The list itself — new permission, distinct from ae:skill-config, since
	// viewing the skills panel shouldn't require the edit permission every
	// mutation above needs. No OC dependency (reads the org's git-backed
	// skills repo via h.skills, not OpenChoreo), so no OcActionCatalog entry
	// is needed for it.
	"ListSkills": {authz.PermissionSkillView},
	// Same reasoning — SkillViewerDialog/EditSkillDialog, both reachable only
	// from the same Skills panel ListSkills backs.
	"GetSkill": {authz.PermissionSkillView},
	// Platform-update status badges on each skill row — gated by the config
	// permission rather than skill-view, matching every mutation above.
	"ListSkillUpdates": {authz.PermissionSkillConfig},

	// GitHub connection (settings > GitHubCredentialCard). UpdateConfig also
	// requires this permission for its gitProvider section — see
	// updateConfigPermissions, not this map; UpdateConfig itself is a
	// permissionGateCarveOuts entry.
	"DisconnectGitProvider": {authz.PermissionGitHubConfig},

	// Project lifecycle & requirements authoring.
	"CreateProject":        {authz.PermissionRequirementUpdate},
	"DeleteProject":        {authz.PermissionRequirementUpdate},
	"PutProjectReferences": {authz.PermissionRequirementUpdate},
	// Matches the OcActionCatalog mapping backing OC's own project:view
	// action — both grant on the same permission, so the BFF gate and OC's
	// AuthzRole agree on who can view a project instead of the BFF allowing
	// anyone through and OC silently narrowing it.
	"GetProject": {authz.PermissionRequirementView},
	// Same reasoning as GetProject — backs the console's projects grid and
	// the header's project switcher, both of which read the same "which
	// projects can I see" question.
	"ListProjects": {authz.PermissionRequirementView},
	// ProjectOverview's Dependencies section. Reaches OC three ways
	// (ListWorkloadConsumerDeps/GetResource/GetResourceType — see
	// OcActionCatalog's PermissionRequirementView entry for the matching
	// workload:view/resource:view/resourcetype:view actions).
	"ListWorkloadDependencies": {authz.PermissionRequirementView},
	// Endpoints page — reaches OC (ListWorkloadEndpoints -> GET /workloads,
	// same workload:view action ListWorkloadDependencies already needs).
	"ListOrgEndpoints": {authz.PermissionRequirementView},
	// Resource-registration form's environment picker — reaches OC
	// (EnvironmentClient.ListNames -> GET /environments; OcActionCatalog gets
	// a new environment:view entry under this permission for it).
	"ListOrgEnvironments": {authz.PermissionRequirementView},
	// Project overview's Dependencies section AND the org Resources catalog
	// page — reaches OC (catalog.List -> ClusterResourceType read;
	// OcActionCatalog gets a new clusterresourcetype:view entry).
	"ListPlatformResourceTypes": {authz.PermissionRequirementView},
	// SpecView's version/tag references — git-backed (ArtifactService), no OC.
	"ListProjectTags": {authz.PermissionRequirementView},

	// Build execution (write).
	"BuildProject":                  {authz.PermissionBuild},
	"CollectExternalResourceValues": {authz.PermissionBuild},
	"CancelRun":                     {authz.PermissionBuild},
	"RegisterExternalResource":      {authz.PermissionBuild},
	"UpdateExternalResource":        {authz.PermissionBuild},
	"DeleteExternalResource":        {authz.PermissionBuild},
	// Discloses a live test-user credential — deliberately requires the
	// stronger ae:build rather than mirroring the console's current
	// ae:build/ae:build-view page-level gate (DeploymentsPage), which reads
	// as an unintentional scoping gap rather than a deliberate design choice.
	"RevealTestUserPassword": {authz.PermissionBuild},
	"RotateTestUserPassword": {authz.PermissionBuild},
	"DeleteTestUser":         {authz.PermissionBuild},

	// Build/deployment read surfaces: viewable with either the write or the
	// view-only permission.
	"ListProjectBuilds":     {authz.PermissionBuild, authz.PermissionBuildView},
	"ListBuildRuns":         {authz.PermissionBuild, authz.PermissionBuildView},
	"ListTasks":             {authz.PermissionBuild, authz.PermissionBuildView},
	"GetTask":               {authz.PermissionBuild, authz.PermissionBuildView},
	"GetProjectStatus":      {authz.PermissionBuild, authz.PermissionBuildView},
	"ListCycleBuilds":       {authz.PermissionBuild, authz.PermissionBuildView},
	"ListComponents":        {authz.PermissionBuild, authz.PermissionBuildView},
	"ListDeployments":       {authz.PermissionBuild, authz.PermissionBuildView},
	"ListExternalResources": {authz.PermissionBuild, authz.PermissionBuildView},
	"GetProjectRoles":       {authz.PermissionBuild, authz.PermissionBuildView},
	// Gates the pre-build dependency/approval check (SpecView's "Build"
	// action) — write-only, not the view permission, since this is part of
	// triggering a build rather than reading its state.
	"GetBuildPreflight": {authz.PermissionBuild},
	// Exact-match ae:build-view, not the OR pair the rows above use — a pure
	// read, so the view-only permission alone is the correct gate. ae-developer
	// didn't hold ae:build-view before this change (role_permissions_catalog.go);
	// it's added there alongside this so the role isn't newly locked out of
	// build logs it already had access to via ae:build's OR semantics elsewhere.
	"GetBuildLogs": {authz.PermissionBuildView},
	// Same read-only reasoning as GetBuildLogs — the live agent-run progress
	// feed on BuildDetailPage/ValidationPage (RunFeed/RunStory/RunSpine and
	// useValidationLive all wrap this one stream).
	"StreamRunProgress": {authz.PermissionBuildView},
	// Same reasoning again — TaskPage's log viewer, no OC dependency.
	"StreamTaskLog": {authz.PermissionBuildView},
	// BuildDetailPage's External Resources panel (ExternalResources.tsx) —
	// unlike the three rows above, this DOES reach OC (GetBinding reads a
	// ResourceReleaseBinding), so it needs a matching OcActionCatalog entry
	// (see PermissionBuildView there) for resourcereleasebinding:view.
	"GetProjectDependencyReadiness": {authz.PermissionBuildView},

	// Design/spec workspace.
	"ListFiles": {authz.PermissionDesignView},
	"ReadFile":  {authz.PermissionDesignView},
	// Reachable from both the design workspace and the build/deployment
	// pages (DeploymentsPage also loads it); any of the three satisfies it.
	"ListDesignDependencies": {authz.PermissionDesignView, authz.PermissionBuild, authz.PermissionBuildView},
	// ComponentOpenApiDialog — reads a git-backed design.json, no OC.
	"GetComponentOpenapi": {authz.PermissionDesignView},

	// Org usage/spend (settings > Usage page). New permission, no existing
	// analog — this is an org-financial view, not a project one.
	"ListProjectUsage": {authz.PermissionUsageView},

	// Alerts/RCA reports: header notification bell (global) + the Alerts list
	// and detail pages. New permission — no existing alert/incident concept.
	"ListRcaAgentReports": {authz.PermissionObservabilityView},
	"GetRcaAgentReport":   {authz.PermissionObservabilityView},

	// AI chat panel — mounted globally in AppLayout whenever a project is
	// open, not a separate page. New permission: none of the existing ones
	// fit a cross-project chat surface. All seven gate on the same
	// permission (no read/write split) since the panel is one feature.
	"CreateTurn":         {authz.PermissionAiChat},
	"GetActiveTurn":      {authz.PermissionAiChat},
	"GetConversation":    {authz.PermissionAiChat},
	"GetTurn":            {authz.PermissionAiChat},
	"ListConversations":  {authz.PermissionAiChat},
	"RotateConversation": {authz.PermissionAiChat},
	"StreamTurn":         {authz.PermissionAiChat},
}

// permissionGateCarveOuts enumerates operations that run WITHOUT a specific
// AE permission requirement, grouped below by reason. Every operationID not
// in operationPermissions must appear here — TestPermissionGateCoverage pins
// that — so leaving a new operation unclassified fails the build rather than
// shipping silently ungated (still requires a valid JWT + tenant binding from
// tenantGate).
var permissionGateCarveOuts = map[string]struct{}{
	// --- Deliberate carve-outs: correct as-is, not gaps ------------------
	// Every entry here is confirmed console-reachable (verified against
	// apps/console/src, not assumed from the original comment framing).

	// Pre-org-selection / bootstrap, mirrors tenantGateCarveOuts.
	"ListOrganizations": {},

	// The AE->OC RBAC bridge's own onboarding surface (internal/authz):
	// gating this behind an AE permission would be circular, since the
	// org's AuthzRole may not exist yet when it runs. Console caller:
	// features/settings/api/queries.ts's GET /authz/ensure.
	"EnsureAuthzRole": {},

	// UpdateConfig is gated by field-aware logic in permissionGate
	// (updateConfigPermissions), checked BEFORE this carve-out map is even
	// consulted. This entry exists only so TestPermissionGateCoverage
	// doesn't also demand a flat operationPermissions row for it. Console
	// caller: queries.ts's PATCH /config (settings' credential/model cards).
	"UpdateConfig": {},

	// Read at app bootstrap (OnboardingGate), before a user's AE permissions
	// are necessarily provisioned. Console caller: queries.ts's GET /config.
	"GetConfig": {},

	// --- Has a real caller, just not the console -------------------------
	// Each of these is genuinely exercised in production — verified against
	// apps/console/src, services/aep-mcp-server, services/collab,
	// services/agents, tools/aectl, and internal Go call graphs — but the
	// caller is a server-to-server or agent-tool path, not a console
	// component. A permission decision is deferred pending a design for
	// what "authorized" means for a non-console caller (e.g. should the
	// forwarded user JWT's own ae:* permissions gate these, or is a
	// different model needed for agent/S2S callers).
	//
	// CreateIssue, ListIssues, PromoteTaskFromIssue: services/aep-mcp-server's
	// ae_create_issue / ae_search_related_issues / ae_dispatch_coding_agent
	// tools (used by the SRE agent), forwarding the caller's bearer as-is.
	"CreateIssue":          {},
	"ListIssues":           {},
	"PromoteTaskFromIssue": {},
	// ReadFileBundle, ApplyFiles, ValidateCollabAccess: services/collab
	// (the spec editor's Yjs collaboration server), forwarding the
	// console user's own JWT server-to-server on every room load
	// (ReadFileBundle), edit flush (ApplyFiles), and room join
	// (ValidateCollabAccess). ValidateCollabAccess's handler already does
	// its own org+ownership check inline (spec/collab/handler.go) — same
	// circularity reasoning as EnsureAuthzRole above — so its carve-out is
	// deliberate, not deferred, unlike the other two here.
	"ReadFileBundle":       {},
	"ApplyFiles":           {},
	"ValidateCollabAccess": {},

	// --- Zero callers anywhere: fully dead, not just console-unused ------
	// Verified with the same rigor as the group above — checked every
	// caller category and found NONE: no console component, no MCP tool
	// (aep-mcp-server or aep-api's own dependency-discovery server), no
	// services/collab or services/agents caller, no webhook handler, no
	// aectl reference, and — one level below the HTTP handler — no other
	// internal Go caller of the service method it calls either. These are
	// either built ahead of a frontend feature that hasn't shipped, or
	// genuinely orphaned; a permission decision (or removal) is deferred
	// until a real caller exists.
	// TODO(authz): assign a permission, wire a real caller, or remove.
	//
	// StartGitProviderConnect/RotateIdpClientSecret/DiscoverIdp were
	// originally filed as a deliberate "runs before permissions exist"
	// carve-out (by analogy with EnsureAuthzRole), but that was never
	// actually verified — the console's GitHub connection uses the PAT flow
	// (PATCH /config) exclusively, not this OAuth-App connect-session flow,
	// and no IdP-related UI exists in the console at all. Confirmed no
	// caller anywhere (console, MCP, collab, agents, aectl, internal Go).
	"StartGitProviderConnect": {},
	"RotateIdpClientSecret":   {},
	"DiscoverIdp":             {},
	// Same correction: assumed deliberate (paired with EnsureAuthzRole's
	// circularity reasoning) but never independently verified. No caller
	// anywhere, including no console call to POST /authz/role-permissions.
	"ModifyAuthzRolePermissions": {},
	"ProvisionPlatformResource":  {},
	"RequestOrgServiceAccess":    {},
	// GetDependencyStatus's HTTP route has no direct caller, but its
	// underlying service method (provisioning.Service.Status) is reused
	// internally by GetBuildPreflight's dependency-readiness check
	// (internal/app/build_adapters.go's buildProvisionStatus.Ready) — the
	// logic runs constantly, just never through this route.
	"GetDependencyStatus":   {},
	"ListAccessRequests":    {},
	"CreateRcaAgentReport":  {},
	"TriggerBuild":          {},
	"RevalidateBuild":       {},
	"UpdateComponentConfig": {},
	"GetComponent":          {},
	"GetComponentConfig":    {},
	"ListBuilds":            {},
	"GetSpecCollabSession":  {},
	"ListActivity":          {},
	"StreamActivity":        {},
	"StreamBuildProgress":   {},
}

// permissionGate is the deny-by-default AE-permission gate, applied to every
// strict operation alongside tenantGate. The caller's permissions are derived
// from auth.Claims.Permissions() — the ae:* entries in the verified JWT's
// scope claim. An operation not present in operationPermissions or
// permissionGateCarveOuts fails TestPermissionGateCoverage, so it can never
// ship silently ungated.
func permissionGate(f gen.StrictHandlerFunc, operationID string) gen.StrictHandlerFunc {
	// UpdateConfig is checked before the carve-out map even though it also
	// has an entry there (for TestPermissionGateCoverage bookkeeping) — its
	// entry exists so the coverage test doesn't also demand a flat
	// operationPermissions row, not to bypass enforcement.
	if operationID == "UpdateConfig" {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			required, err := updateConfigPermissions(request)
			if err != nil {
				return nil, err
			}
			held := auth.ClaimsFromContext(ctx).Permissions()
			for _, perm := range required {
				if !containsPermission(held, perm) {
					logMissingPermission(ctx, operationID, held, []authz.Permission{perm})
					return nil, errForbidden("missing required permission: " + string(perm))
				}
			}
			return f(ctx, w, r, request)
		}
	}
	if _, ok := permissionGateCarveOuts[operationID]; ok {
		return f
	}
	required := operationPermissions[operationID]
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		claims := auth.ClaimsFromContext(ctx)
		if !hasAnyPermission(claims, required) {
			logMissingPermission(ctx, operationID, claims.Permissions(), required)
			return nil, errForbidden("missing required permission")
		}
		return f(ctx, w, r, request)
	}
}

// logMissingPermission logs the AE-permission-gate denial: the caller reached
// operationID without holding any of required. This is the "request never
// left the BFF" case — distinct from an OC-side rejection (logged separately
// where the OC client surfaces ErrForbidden), so reading the logs tells you
// whether the AE gate or the downstream OC call is where a permission
// mismatch actually happened.
func logMissingPermission(ctx context.Context, operationID string, held, required []authz.Permission) {
	slog.WarnContext(ctx, "authz: request denied — caller missing required AE permission",
		"operationID", operationID, "required", required, "held", held)
}

// hasAnyPermission reports whether claims holds at least one of required. An
// empty/nil required list denies by construction — an operation reaching
// here with no declared permission is a TestPermissionGateCoverage failure,
// not a silent allow.
func hasAnyPermission(claims *auth.Claims, required []authz.Permission) bool {
	if len(required) == 0 {
		return false
	}
	held := claims.Permissions()
	for _, want := range required {
		if containsPermission(held, want) {
			return true
		}
	}
	return false
}

func containsPermission(held []authz.Permission, want authz.Permission) bool {
	for _, h := range held {
		if h == want {
			return true
		}
	}
	return false
}

// updateConfigPermissions resolves the permission(s) UpdateConfig requires
// from which section(s) of the patch body are actually populated —
// gitProvider needs ae:github-config, llm/codingLlm need ae:model-config. A
// patch touching both sections needs BOTH permissions (unlike
// operationPermissions' OR semantics, permissionGate requires every entry
// this function returns).
//
// idp is NOT yet mapped to a permission — no AE permission concept for IdP
// config exists today — so an idp-only patch is presently allowed to any
// authenticated org member.
// TODO(authz): decide idp's permission before relying on this gate for IdP
// config protection.
func updateConfigPermissions(request any) ([]authz.Permission, error) {
	req, ok := request.(gen.UpdateConfigRequestObject)
	if !ok || req.Body == nil {
		return nil, nil
	}
	var required []authz.Permission
	if req.Body.GitProvider.Sent {
		required = append(required, authz.PermissionGitHubConfig)
	}
	if req.Body.LLM.Sent || req.Body.CodingLLM.Sent {
		required = append(required, authz.PermissionModelConfig)
	}
	return required, nil
}
