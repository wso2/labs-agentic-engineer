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
	// The list itself — a separate permission from ae:skill-config, since
	// viewing the skills panel shouldn't require the edit permission every
	// mutation above needs. Exact-match ae:skill-view, NOT OR'd with
	// ae:skill-config: a write permission gates mutations, never page entry
	// on its own — a config-only holder (no view grant) is a real, if
	// unusual, role shape and must be blocked from the panel the same as
	// every other write-without-view case in this file. No OC dependency
	// (reads the org's git-backed skills repo via h.skills, not OpenChoreo),
	// so no OcActionCatalog entry is needed for it.
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
	// Settings > Credentials (both cards, read from the same GET /config).
	// OR here only decides whether the call is answered AT ALL: a caller
	// holding just one of the two still gets the OTHER section redacted to
	// null inside the handler itself (getconfig.Handler.GetConfig) — the
	// gate can't express "half a response", only a request-level allow/deny.
	// GetConfigStatus (permissionGateCarveOuts) is the permission-free
	// sibling for OnboardingGate's own bootstrap need.
	"GetConfig": {authz.PermissionGitHubConfig, authz.PermissionModelConfig},

	// Project lifecycle & requirements authoring.
	"CreateProject":        {authz.PermissionRequirementUpdate},
	"DeleteProject":        {authz.PermissionRequirementUpdate},
	"PutProjectReferences": {authz.PermissionRequirementUpdate},
	// Matches the OcActionCatalog mapping backing OC's own project:view
	// action — the BFF gate and OC's AuthzRole agree on who can view a
	// project instead of the BFF allowing anyone through and OC silently
	// narrowing it. Exact-match ae:requirement-view, not an OR with
	// ae:requirement-update: same call as ae:resource-view/ae:resource-config
	// below — no role in rolePermissionsCatalog holds ae:requirement-update
	// without also holding ae:requirement-view (only ae-admin holds either,
	// and it holds both), so an OR here would narrow nothing a real caller
	// has, only add a branch nothing exercises. ProjectOverview's whole shell
	// (ProjectLayout, the track, every section) reads through this one call,
	// the same surface GetProjectStatus/ListComponents/ListDeployments/
	// GetComponentOpenapi below now share.
	"GetProject": {authz.PermissionRequirementView},
	// The projects grid and the header's project switcher — same exact-match
	// reasoning as GetProject above.
	"ListProjects": {authz.PermissionRequirementView},
	// Endpoints page — reaches OC (ListWorkloadEndpoints -> GET /workloads;
	// see OcActionCatalog's PermissionRequirementView entry for the
	// matching workload:view action).
	"ListOrgEndpoints": {authz.PermissionRequirementView},
	// The rest of ProjectOverview's own reads (its status poll, its
	// Components section, a component's Deployments/OpenAPI drill-downs) —
	// exact-match ae:requirement-view, same as GetProject above, not the
	// borrowed ae:build/ae:build-view or ae:design-view gates they used
	// before. GetProjectStatus/ListDeployments reach OC
	// (ListProjectReleaseBindings -> GET /releasebindings; see
	// OcActionCatalog's PermissionRequirementView entry for the matching
	// releasebinding:view action) and ListComponents reaches OC too
	// (ListComponents -> GET /components; component:view, same entry).
	// GetComponentOpenapi does not reach OC at all — it reads a git-backed
	// design.json via the ArtifactStore — so it carries no OC action either
	// way; it moves here because ProjectOverview and ComponentsList are its
	// only console callers, both already requirement-view-gated for
	// everything else on the page.
	"GetProjectStatus":    {authz.PermissionRequirementView},
	"ListComponents":      {authz.PermissionRequirementView},
	"ListDeployments":     {authz.PermissionRequirementView},
	"GetComponentOpenapi": {authz.PermissionRequirementView},
	// SpecView's version/tag references — git-backed (ArtifactService), no
	// OC. Same exact-match reasoning as the rows above.
	"ListProjectTags": {authz.PermissionRequirementView},
	// Resource-registration form's environment picker — reaches OC
	// (EnvironmentClient.ListNames -> GET /environments; OcActionCatalog gets
	// a new environment:view entry under this permission for it). Its only
	// console caller is RegisterFormPage (the Resources register/edit form),
	// whose content only renders once its own outer wrapper confirms
	// ae:resource-config — no path reaches this holding ae:requirement-view
	// alone, so that permission doesn't belong in the OR (a past over-grant,
	// caught with the same shared-read reasoning as ListExternalResources).
	"ListOrgEnvironments": {authz.PermissionResourceConfig},
	// ProjectOverview's Dependencies section — exact-match ae:resource-view
	// now, not ae:requirement-view: it's resource data, gated the same as
	// every other resource read on this page (ListPlatformResourceTypes/
	// ListExternalResources below), not folded into the page's own
	// requirement-view shell permission. Reaches OC three ways
	// (ListWorkloadConsumerDeps/GetResource/GetResourceType — see
	// OcActionCatalog's PermissionResourceView entry for the matching
	// workload:view/resource:view/resourcetype:view actions).
	"ListWorkloadDependencies": {authz.PermissionResourceView},
	// Read from two console call sites — Project Overview's Dependencies
	// section and the org Resources catalog page — exact-match
	// ae:resource-view now, not an OR with ae:resource-config: no role in
	// rolePermissionsCatalog holds ae:resource-config without also holding
	// ae:resource-view (only ae-admin holds either, and it holds both), so
	// dropping the config branch narrows nothing a real caller has today.
	// ResourcesCatalog's own read (usePlatformResourceTypes) and
	// RegisterFormPage's edit-mode prefill (useExternalResources, on the
	// matching entry below) both still assume a config-only holder can list
	// — degrading gracefully to an empty/unprefilled state rather than
	// erroring, same as DeploymentsPage's own supporting-lookup pattern —
	// should a config-without-view role ever get introduced. Reaches OC
	// (catalog.List -> ClusterResourceType read; see OcActionCatalog's
	// PermissionResourceView entry).
	"ListPlatformResourceTypes": {authz.PermissionResourceView},

	// Org resource catalog (settings > Resources, register/edit flow) —
	// exclusively RegisterFormPage/ResourcesCatalog's own mutations, no
	// other console caller, so these are ae:resource-config only, not an OR
	// with ae:build (their previous, borrowed gate).
	"RegisterExternalResource": {authz.PermissionResourceConfig},
	"UpdateExternalResource":   {authz.PermissionResourceConfig},
	"DeleteExternalResource":   {authz.PermissionResourceConfig},

	// Build execution (write).
	"BuildProject":                  {authz.PermissionBuild},
	"CollectExternalResourceValues": {authz.PermissionBuild},
	"CancelRun":                     {authz.PermissionBuild},
	// Discloses a live test-user credential — deliberately requires the
	// stronger ae:build rather than mirroring the console's current
	// ae:build/ae:build-view page-level gate (DeploymentsPage), which reads
	// as an unintentional scoping gap rather than a deliberate design choice.
	"RevealTestUserPassword": {authz.PermissionBuild},
	"RotateTestUserPassword": {authz.PermissionBuild},
	"DeleteTestUser":         {authz.PermissionBuild},

	// Build/deployment read surfaces. Exact-match ae:build-view, NOT OR'd
	// with ae:build: a write permission gates mutations (BuildProject,
	// CancelRun, … above), never page entry on its own — same rule as
	// ListSkills/GetSkill above and ListFiles/ReadFile below. OC-level
	// access is unaffected: it's a property of the caller's full permission
	// set, not which permission gates a given aep-api operation, and both
	// built-in roles that hold ae:build-view already hold ae:build too
	// (role_permissions_catalog.go) — this only closes the gap for a
	// build-only role that was never supposed to see build state anyway.
	"ListProjectBuilds": {authz.PermissionBuildView},
	"ListBuildRuns":     {authz.PermissionBuildView},
	"ListTasks":         {authz.PermissionBuildView},
	"GetTask":           {authz.PermissionBuildView},
	"ListCycleBuilds":   {authz.PermissionBuildView},
	// Read from two console call sites — DeploymentsPage and the org
	// Resources catalog page — exact-match ae:resource-view now, not an OR
	// with ae:resource-config: no role holds the config permission without
	// the view one (see ListPlatformResourceTypes above for the same
	// reasoning), so this narrows nothing a real caller has today.
	"ListExternalResources": {authz.PermissionResourceView},
	"GetProjectRoles":       {authz.PermissionBuildView},
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
	// pages (DeploymentsPage also loads it) — a genuine cross-category OR
	// (either page's own view permission satisfies it), unlike the
	// same-category write/view ORs eliminated elsewhere in this file:
	// ae:build (the write permission) is deliberately absent here, since
	// this is a read and neither page's own entry gate accepts its write
	// permission alone either.
	"ListDesignDependencies": {authz.PermissionDesignView, authz.PermissionBuildView},
	// DependencyView/ProvideInterfaceDialog's two writes into
	// specs/design/dependencies/<name>/ — committing a contract, or signing
	// off on the one the design agent wrote. Same reasoning as GenerateDesign:
	// exact-match ae:design, not OR'd with ae:design-view — a caller who can
	// only VIEW the design tree does not get to commit into it or accept an
	// assumption on the project's behalf just because the page renders.
	"ProvideDependencyContract":  {authz.PermissionDesign},
	"AcceptDependencyAssumption": {authz.PermissionDesign},

	// services/collab (the spec editor's Yjs collaboration server) calls all
	// three of these server-to-server, forwarding the connecting console
	// user's own JWT — so this gate sees exactly the same claims it would for
	// a direct console call, and denies/allows on the same basis.
	//
	// ValidateCollabAccess (room join, every joiner) and ReadFileBundle (the
	// room's seed read, first joiner only) both need ae:design-view: without
	// it on ValidateCollabAccess specifically, a caller could join an
	// already-warm room and read its full live document via Yjs sync without
	// ReadFileBundle ever running for them (ReadFileBundle only fires once,
	// for whoever seeds the room) — so the join-time check is the one that
	// actually closes the read gap, not the seed-read check alone.
	//
	// ApplyFiles (the room's git-commit flush) needs the stronger ae:design,
	// exact-match like ProvideDependencyContract/AcceptDependencyAssumption
	// above and GenerateDesign below — viewing the live room must not imply
	// permission to commit into it. Deliberately NOT OR'd with
	// ae:resource-config the way CreateTurn/StreamTurn's shared chat panel
	// is: the spec collab room is design-track content, and resource
	// registration (a separate feature/route) has no legitimate reason to
	// write here.
	"ValidateCollabAccess": {authz.PermissionDesignView},
	"ReadFileBundle":       {authz.PermissionDesignView},
	"ApplyFiles":           {authz.PermissionDesign},

	// Org usage/spend (settings > Usage page). New permission, no existing
	// analog — this is an org-financial view, not a project one.
	"ListProjectUsage": {authz.PermissionUsageView},

	// Alerts/RCA reports: header notification bell (global) + the Alerts list
	// and detail pages. New permission — no existing alert/incident concept.
	"ListRcaAgentReports": {authz.PermissionObservabilityView},
	"GetRcaAgentReport":   {authz.PermissionObservabilityView},

	// AI chat panel (AgentChatPanel) — two distinct console callers share
	// this same component and these same seven operations, each reached
	// through its own permission rather than a cross-project ae:ai-chat
	// permission (retired): AppLayout mounts it for the project spec chat
	// (gated on ae:design — the panel is a write surface, so ae:design-view
	// alone does not satisfy it there, same read/write split as every other
	// permission pair in this console) and RegisterFormPage mounts it for
	// the marketplace registration-form assistant, pointed at a separate
	// MARKETPLACE_CHAT_PROJECT pseudo-project (gated on ae:resource-config,
	// its own page's existing permission — nothing to do with design). All
	// seven gate on the same OR pair (no read/write split within either
	// context) since the panel is one feature either way.
	"CreateTurn":         {authz.PermissionDesign, authz.PermissionResourceConfig},
	"GetActiveTurn":      {authz.PermissionDesign, authz.PermissionResourceConfig},
	"GetConversation":    {authz.PermissionDesign, authz.PermissionResourceConfig},
	"GetTurn":            {authz.PermissionDesign, authz.PermissionResourceConfig},
	"ListConversations":  {authz.PermissionDesign, authz.PermissionResourceConfig},
	"RotateConversation": {authz.PermissionDesign, authz.PermissionResourceConfig},
	"StreamTurn":         {authz.PermissionDesign, authz.PermissionResourceConfig},
	// GenerateDesign is NOT part of the OR above, deliberately: it's the one
	// operation whose entire content is fixed server-side (the `/design`
	// command — see spec.StartDesignTurn), so it's the one place "may this
	// caller trigger design generation" can be asked exactly, without the
	// marketplace assistant's ae:resource-config riding along. Exact-match
	// ae:design — no other permission satisfies it.
	"GenerateDesign": {authz.PermissionDesign},
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

	// GetConfigStatus is the permission-free sibling GetConfig itself no
	// longer is (see operationPermissions below): just the two "is it
	// connected" booleans, with none of GetConfig's identity/key detail —
	// safe at app bootstrap (OnboardingGate), before a caller's AE
	// permissions are necessarily provisioned. Console caller: queries.ts's
	// GET /config/status.
	"GetConfigStatus": {},

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
	// StartGitProviderConnect serves the GitHub App OAuth connect-session
	// flow. The console connects with a PAT instead (PATCH /config), so
	// nothing calls this — but GitProviderWrite schema-rejects mode=app
	// precisely to point App-mode callers here, so the route stays.
	"StartGitProviderConnect": {},
	"ProvisionPlatformResource": {},
	"RequestOrgServiceAccess":   {},
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

// updateConfigPermissions resolves the permission(s) UpdateConfig requires from
// which section(s) of the patch body are actually populated — gitProvider needs
// ae:github-config, llm/codingLlm/codingAgent need ae:model-config (the
// runtime/model pair CodingAgentCard writes is grouped with the credential it
// bills, per ADR-0016). A patch touching both needs BOTH: unlike
// operationPermissions' OR semantics, permissionGate requires every permission
// this function returns.
//
// An idp section is refused outright rather than mapped to a permission. It
// repoints the issuer the org's protected APIs pin JWT validation to (the
// deploy path reads it through DeploymentService.resolveIssuers) and, on a kind
// switch, deletes the org's Thunder publisher app — but no AE permission
// describes identity configuration, and nothing in the platform writes the
// section today, so there is no grant to check and no caller to break. A
// permission belongs here when the surface that writes it is designed; until
// then the gate refuses rather than waves it through.
//
// A request that is not an UpdateConfig body at all is likewise an error, not a
// free pass: this file's whole posture is that an unrecognized shape denies.
func updateConfigPermissions(request any) ([]authz.Permission, error) {
	req, ok := request.(gen.UpdateConfigRequestObject)
	if !ok || req.Body == nil {
		return nil, errForbidden("malformed config patch")
	}
	if req.Body.IDP.Sent {
		return nil, errForbidden("the idp section cannot be changed through this API")
	}
	var required []authz.Permission
	if req.Body.GitProvider.Sent {
		required = append(required, authz.PermissionGitHubConfig)
	}
	if req.Body.LLM.Sent || req.Body.CodingLLM.Sent || req.Body.CodingAgent.Sent {
		required = append(required, authz.PermissionModelConfig)
	}
	return required, nil
}
