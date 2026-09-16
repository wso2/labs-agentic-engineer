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
	"github.com/wso2/aep/aep-api/internal/spec"
)

// This file is the AE permission gate's decision table. Every contract
// operationID appears in exactly one of the two maps below, and
// TestPermissionGateCoverage fails the build otherwise — so an operation can
// never ship without a permission decision having been made about it.
//
// Five rules govern the table; entry comments record only what a row adds to
// them.
//
//  1. A row lists the permission(s) that satisfy the operation. Several are
//     OR'd: holding any one suffices.
//
//  2. Entry to a surface is gated on its VIEW permission exactly. A write
//     permission (ae:build, ae:design, ae:skill-config, …) authorizes
//     mutations; it never admits its holder to a page on its own. So a row
//     reading ae:build-view is NOT satisfied by ae:build, even though every
//     built-in role that holds one holds the other — the gate describes what a
//     permission means, not which roles happen to exist.
//
//  3. An OR therefore never pairs a write permission with its own view
//     sibling. Where one appears it spans FEATURES: one surface legitimately
//     reached from two places, each bringing its own permission.
//
//  4. An OR is also the wrong tool when the two features differ by PATH rather
//     than by operation, because it grants each one's permission everywhere the
//     other reaches. The chat panel is that case and resolves its requirement
//     per request instead (chatPermissions); UpdateConfig is the other, keyed
//     on body sections.
//
//  5. A permission whose operations reach OpenChoreo needs matching actions in
//     authz.OcActionCatalog, or the BFF admits a caller that OC then refuses.
var operationPermissions = map[string][]authz.Permission{
	// --- Skills (Settings › Skills) -------------------------------------
	"SyncSkills":       {authz.PermissionSkillConfig},
	"SetSkillEnabled":  {authz.PermissionSkillConfig},
	"DeleteSkill":      {authz.PermissionSkillConfig},
	"UpdateSkill":      {authz.PermissionSkillConfig},
	"ImportSkill":      {authz.PermissionSkillConfig},
	"CreateSkill":      {authz.PermissionSkillConfig},
	"ListSkillUpdates": {authz.PermissionSkillConfig},
	"ListSkills":       {authz.PermissionSkillView},
	"GetSkill":         {authz.PermissionSkillView},

	// --- Org config (Settings › Credentials) ----------------------------
	"DisconnectGitProvider": {authz.PermissionGitHubConfig},
	// Both credential cards read this one response, so either permission is
	// answered — and the handler then redacts the half the caller does not
	// hold (organization.RedactConfigForPermissions). The gate can only allow
	// or deny a request; it cannot return half of one.
	"GetConfig": {authz.PermissionGitHubConfig, authz.PermissionModelConfig},

	// --- Projects & requirements ----------------------------------------
	"CreateProject":        {authz.PermissionRequirementUpdate},
	"DeleteProject":        {authz.PermissionRequirementUpdate},
	"PutProjectReferences": {authz.PermissionRequirementUpdate},
	// The project shell and everything that hangs off it: the projects grid,
	// the header switcher, the overview's own reads, and a component's
	// drill-downs.
	"GetProject":          {authz.PermissionRequirementView},
	"ListProjects":        {authz.PermissionRequirementView},
	"ListOrgEndpoints":    {authz.PermissionRequirementView},
	"GetProjectStatus":    {authz.PermissionRequirementView},
	"ListComponents":      {authz.PermissionRequirementView},
	"ListDeployments":     {authz.PermissionRequirementView},
	"GetComponentOpenapi": {authz.PermissionRequirementView},
	"ListProjectTags":     {authz.PermissionRequirementView},

	// --- Resources (Settings › Resources, and the overview's Dependencies)
	"ListWorkloadDependencies":  {authz.PermissionResourceView},
	"ListPlatformResourceTypes": {authz.PermissionResourceView},
	"ListExternalResources":     {authz.PermissionResourceView},
	"RegisterExternalResource":  {authz.PermissionResourceConfig},
	"UpdateExternalResource":    {authz.PermissionResourceConfig},
	"DeleteExternalResource":    {authz.PermissionResourceConfig},
	// Config, not view: its only caller is the registration form's environment
	// picker, and that form renders only for a caller who already holds
	// ae:resource-config. Nothing reaches it with the view permission alone.
	"ListOrgEnvironments": {authz.PermissionResourceConfig},

	// --- Builds & deployments -------------------------------------------
	"BuildProject":                  {authz.PermissionBuild},
	"CollectExternalResourceValues": {authz.PermissionBuild},
	"CancelRun":                     {authz.PermissionBuild},
	// Triggering a build, not reading one: the pre-build dependency/approval
	// check is part of starting the build it precedes.
	"GetBuildPreflight": {authz.PermissionBuild},
	// Discloses a live test-user credential, so it takes the write permission
	// rather than the view permission its page is entered on.
	"RevealTestUserPassword": {authz.PermissionBuild},
	"RotateTestUserPassword": {authz.PermissionBuild},
	"DeleteTestUser":         {authz.PermissionBuild},

	"ListProjectBuilds":             {authz.PermissionBuildView},
	"ListBuildRuns":                 {authz.PermissionBuildView},
	"ListTasks":                     {authz.PermissionBuildView},
	"GetTask":                       {authz.PermissionBuildView},
	"ListCycleBuilds":               {authz.PermissionBuildView},
	"GetProjectRoles":               {authz.PermissionBuildView},
	"GetBuildLogs":                  {authz.PermissionBuildView},
	"StreamRunProgress":             {authz.PermissionBuildView},
	"StreamTaskLog":                 {authz.PermissionBuildView},
	"GetProjectDependencyReadiness": {authz.PermissionBuildView},

	// --- Design & spec ---------------------------------------------------
	"ListFiles": {authz.PermissionDesignView},
	"ReadFile":  {authz.PermissionDesignView},
	// Rule 3's cross-feature OR: the dependency list is read from the design
	// workspace AND from the deployments page. ae:build is deliberately absent
	// — this is a read, and neither page is entered on a write permission.
	"ListDesignDependencies": {authz.PermissionDesignView, authz.PermissionBuildView},
	// Committing a contract, or accepting an assumption on the project's
	// behalf. Viewing the design tree does not confer either.
	"ProvideDependencyContract":  {authz.PermissionDesign},
	"AcceptDependencyAssumption": {authz.PermissionDesign},

	// services/collab calls these three server-to-server, forwarding the
	// connecting user's own JWT — so the gate sees the same claims it would
	// for a direct console call.
	//
	// The join check is the one that closes the read: ReadFileBundle seeds a
	// room only for its FIRST joiner, so without a check on the join itself a
	// caller could enter an already-warm room and read the whole live document
	// over Yjs without the seed read ever running for them.
	//
	// Being admitted to a room is not permission to change it, and this gate
	// cannot enforce that alone — it sees the join, never the Yjs updates that
	// follow. validate-collab-access also answers canWrite, and the collab
	// server marks a viewer's socket read-only on the strength of it.
	"ValidateCollabAccess": {authz.PermissionDesignView},
	"ReadFileBundle":       {authz.PermissionDesignView},
	"ApplyFiles":           {authz.PermissionDesign},

	// --- AI chat panel ---------------------------------------------------
	// One panel, two mount points: the project spec chat and the marketplace
	// registration assistant, which runs against its own pseudo-project. The
	// rows below are the FALLBACK for a real project; the marketplace
	// pseudo-project is answered on ae:resource-config instead, resolved per
	// request in chatPermissions because the two differ by path, not by
	// operation.
	//
	// Not an OR — that was the bug. A turn's instruction carries `/<skill>`
	// flow commands verbatim for the server to expand, so an OR let a caller
	// holding only ae:resource-config send `/design` at a real project and get
	// exactly what generate-design's ae:design gate exists to control. Giving
	// the marketplace its own project scope is what lets the real-project case
	// be ae:design alone without taking the assistant's chat away with it.
	"CreateTurn":         {authz.PermissionDesign},
	"GetActiveTurn":      {authz.PermissionDesign},
	"GetConversation":    {authz.PermissionDesign},
	"GetTurn":            {authz.PermissionDesign},
	"ListConversations":  {authz.PermissionDesign},
	"RotateConversation": {authz.PermissionDesign},
	"StreamTurn":         {authz.PermissionDesign},
	// Its content is fixed server-side, so no instruction a caller supplies can
	// redirect it. It needs no project-scoped branch: the marketplace assistant
	// has no design to generate.
	"GenerateDesign": {authz.PermissionDesign},

	// --- Org usage & observability ---------------------------------------
	"ListProjectUsage":    {authz.PermissionUsageView},
	"ListRcaAgentReports": {authz.PermissionObservabilityView},
	"GetRcaAgentReport":   {authz.PermissionObservabilityView},
}

// permissionGateCarveOuts are the operations that run without an AE permission
// requirement. They still require a valid JWT and a bound tenant from
// tenantGate; what they skip is the per-operation check.
//
// An operation belongs here for one of two reasons, and the comment on each
// group says which. Nothing lands here by default: an operation absent from
// both maps fails TestPermissionGateCoverage.
var permissionGateCarveOuts = map[string]struct{}{
	// Runs before the caller's permissions can be known or provisioned.
	// ListOrganizations precedes org selection; GetConfigStatus answers the
	// onboarding gate's two booleans, carrying none of GetConfig's identity or
	// key detail; EnsureAuthzRole provisions the org's OC role, which cannot
	// require a grant that provisioning is what establishes.
	"ListOrganizations": {},
	"GetConfigStatus":   {},
	"EnsureAuthzRole":   {},

	// Gated by field-aware logic in permissionGate rather than a flat row —
	// its permissions depend on which sections the patch touches. This entry
	// exists so the coverage test does not also demand a row above; it is not
	// a bypass. See updateConfigPermissions.
	"UpdateConfig": {},

	// Reached only by a non-console caller: the SRE agent's MCP tools, which
	// forward their caller's bearer as-is. What authorization means for an
	// agent rather than a person is undecided, and guessing a permission here
	// would break a working flow on a role that merely lacks the guess.
	"CreateIssue":          {},
	"ListIssues":           {},
	"PromoteTaskFromIssue": {},

	// No caller anywhere — console, MCP, collab, agents, aectl, or another Go
	// caller of the same service method. Each is either built ahead of a
	// feature that has not shipped or genuinely orphaned, and a permission
	// invented for a surface that does not exist would be a guess about a
	// design nobody has done.
	//
	// StartGitProviderConnect is the one with a known future: GitProviderWrite
	// schema-rejects mode=app precisely to point App-mode callers at it.
	// GetDependencyStatus's route is unused, but the service method behind it
	// runs constantly via GetBuildPreflight's readiness check.
	// TODO(authz): assign a permission, wire a caller, or remove.
	"StartGitProviderConnect":   {},
	"ProvisionPlatformResource": {},
	"RequestOrgServiceAccess":   {},
	"GetDependencyStatus":       {},
	"ListAccessRequests":        {},
	"CreateRcaAgentReport":      {},
	"TriggerBuild":              {},
	"RevalidateBuild":           {},
	"UpdateComponentConfig":     {},
	"GetComponent":              {},
	"GetComponentConfig":        {},
	"ListBuilds":                {},
	"GetSpecCollabSession":      {},
	"ListActivity":              {},
	"StreamActivity":            {},
	"StreamBuildProgress":       {},
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
	declared := operationPermissions[operationID]
	_, projectScoped := chatOperations[operationID]
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		required := declared
		if projectScoped {
			required = chatPermissions(request, declared)
		}
		claims := auth.ClaimsFromContext(ctx)
		if !hasAnyPermission(claims, required) {
			logMissingPermission(ctx, operationID, claims.Permissions(), required)
			return nil, errForbidden("missing required permission")
		}
		return f(ctx, w, r, request)
	}
}

// chatOperations are the turn/conversation operations whose requirement
// depends on WHICH project they address — see operationPermissions' chat
// section. Kept as its own set rather than inferred from the request type so
// that adding an operation to the panel is a deliberate edit here.
var chatOperations = map[string]struct{}{
	"CreateTurn":         {},
	"GetActiveTurn":      {},
	"GetConversation":    {},
	"GetTurn":            {},
	"ListConversations":  {},
	"RotateConversation": {},
	"StreamTurn":         {},
}

// chatPermissions narrows a chat operation to the permission its project
// warrants. The marketplace registration assistant runs against a synthetic
// project with no git repo behind it (spec.MarketplaceRegisterProjectID), and
// that page's own permission is ae:resource-config; every other project is a
// real one, where the panel is the design workspace's write surface.
//
// A request whose type is not listed — a new chat operation, or a changed
// generated shape — falls through to `declared`, the stricter ae:design. An
// omission here costs a legitimate marketplace caller a 403, which is visible
// and reported; the other direction would hand a real project's design
// controls to a permission that should not reach them.
func chatPermissions(request any, declared []authz.Permission) []authz.Permission {
	marketplace := []authz.Permission{authz.PermissionResourceConfig}
	switch req := request.(type) {
	case gen.CreateTurnRequestObject:
		if req.ProjectName == spec.MarketplaceRegisterProjectID {
			return marketplace
		}
	case gen.GetActiveTurnRequestObject:
		if req.ProjectName == spec.MarketplaceRegisterProjectID {
			return marketplace
		}
	case gen.GetConversationRequestObject:
		if req.ProjectName == spec.MarketplaceRegisterProjectID {
			return marketplace
		}
	case gen.GetTurnRequestObject:
		if req.ProjectName == spec.MarketplaceRegisterProjectID {
			return marketplace
		}
	case gen.ListConversationsRequestObject:
		if req.ProjectName == spec.MarketplaceRegisterProjectID {
			return marketplace
		}
	case gen.RotateConversationRequestObject:
		if req.ProjectName == spec.MarketplaceRegisterProjectID {
			return marketplace
		}
	case gen.StreamTurnRequestObject:
		if req.ProjectName == spec.MarketplaceRegisterProjectID {
			return marketplace
		}
	}
	return declared
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
