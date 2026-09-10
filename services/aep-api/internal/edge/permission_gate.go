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

	// GitHub connection (settings > GitHubCredentialCard). UpdateConfig also
	// requires this permission for its gitProvider section — see
	// updateConfigPermissions, not this map; UpdateConfig itself is a
	// permissionGateCarveOuts entry.
	"DisconnectGitProvider": {authz.PermissionGitHubConfig},

	// Project lifecycle & requirements authoring.
	"CreateProject":        {authz.PermissionRequirementUpdate},
	"DeleteProject":        {authz.PermissionRequirementUpdate},
	"PutProjectReferences": {authz.PermissionRequirementUpdate},

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

	// Design/spec workspace.
	"ListFiles": {authz.PermissionDesignView},
	"ReadFile":  {authz.PermissionDesignView},
	// Reachable from both the design workspace and the build/deployment
	// pages (DeploymentsPage also loads it); any of the three satisfies it.
	"ListDesignDependencies": {authz.PermissionDesignView, authz.PermissionBuild, authz.PermissionBuildView},
}

// permissionGateCarveOuts enumerates operations that run WITHOUT a specific
// AE permission requirement, grouped below by reason. Every operationID not
// in operationPermissions must appear here — TestPermissionGateCoverage pins
// that — so leaving a new operation unclassified fails the build rather than
// shipping silently ungated (still requires a valid JWT + tenant binding from
// tenantGate).
var permissionGateCarveOuts = map[string]struct{}{
	// Pre-org-selection / bootstrap, mirrors tenantGateCarveOuts.
	"ListOrganizations": {},

	// The AE->OC RBAC bridge's own onboarding surface (internal/authz):
	// gating these behind an AE permission would be circular, since the
	// org's AuthzRole may not exist yet when they run.
	"EnsureAuthzRole":            {},
	"ModifyAuthzRolePermissions": {},

	// Onboarding / IdP connection flow: runs before a user's AE
	// role/permissions are necessarily provisioned.
	"StartGitProviderConnect": {},
	"RotateIdpClientSecret":   {},
	"DiscoverIdp":             {},

	// UpdateConfig is gated by field-aware logic in permissionGate
	// (updateConfigPermissions), checked BEFORE this carve-out map is even
	// consulted. This entry exists only so TestPermissionGateCoverage
	// doesn't also demand a flat operationPermissions row for it.
	"UpdateConfig": {},

	// No console caller exists for any of these yet (verified by a full-repo
	// search); a permission decision is deferred until a real caller and an
	// access model exist together.
	// TODO(authz): assign a permission before wiring a frontend surface to
	// any of these.
	"ProvisionPlatformResource": {},
	"RequestOrgServiceAccess":   {},
	"GetDependencyStatus":       {},
	"ListAccessRequests":        {},
	"CreateIssue":               {},
	"PromoteTaskFromIssue":      {},
	"CreateRcaAgentReport":      {},

	// Mutations with no traced frontend caller either — flagged individually
	// (rather than folded into the blanket group below) because they write
	// state and deserve a reviewer's attention before this gate is trusted
	// as a complete authorization boundary.
	// TODO(authz): assign a permission or confirm the carve-out deliberately.
	"TriggerBuild":          {},
	"RevalidateBuild":       {},
	"UpdateComponentConfig": {},
	"ApplyFiles":            {},

	// Blanket carve-out: reads and streaming surfaces with no evidence, from
	// either traced console usage or by name, of needing restriction beyond
	// tenant scoping. Not audited permission-by-permission.
	// TODO(authz): audit before relying on this gate as a complete
	// authorization boundary.
	"CreateTurn":                    {},
	"GetActiveTurn":                 {},
	"GetBuildLogs":                  {},
	"GetBuildPreflight":             {},
	"GetComponent":                  {},
	"GetComponentConfig":            {},
	"GetComponentOpenapi":           {},
	"GetConfig":                     {},
	"GetConversation":               {},
	"GetProject":                    {},
	"GetProjectDependencyReadiness": {},
	"GetRcaAgentReport":             {},
	"GetSkill":                      {},
	"GetSpecCollabSession":          {},
	"GetTurn":                       {},
	"ListActivity":                  {},
	"ListBuilds":                    {},
	"ListConversations":             {},
	"ListIssues":                    {},
	"ListOrgEndpoints":              {},
	"ListOrgEnvironments":           {},
	"ListPlatformResourceTypes":     {},
	"ListProjectTags":               {},
	"ListProjectUsage":              {},
	"ListProjects":                  {},
	"ListRcaAgentReports":           {},
	"ListSkillUpdates":              {},
	"ListSkills":                    {},
	"ListWorkloadDependencies":      {},
	"ReadFileBundle":                {},
	"RotateConversation":            {},
	"StreamActivity":                {},
	"StreamBuildProgress":           {},
	"StreamRunProgress":             {},
	"StreamTaskLog":                 {},
	"StreamTurn":                    {},
	"ValidateCollabAccess":          {},
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
		if !hasAnyPermission(auth.ClaimsFromContext(ctx), required) {
			return nil, errForbidden("missing required permission")
		}
		return f(ctx, w, r, request)
	}
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
