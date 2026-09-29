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

// Package aeperms is the AE permission vocabulary: the keys themselves, the
// Thunder action each one is declared as, and the roles that hold them.
//
// It is public, and one of the few packages outside aep-api's internal/ tree,
// for the same reason ocauth and secretsprovider are: a second module needs
// it. That module is aectl, which provisions the `ae` resource server, its
// actions, and the ae-admin/ae-developer groups and roles into Thunder at
// install time. The gate in aep-api then reads the very scopes that
// provisioning made grantable.
//
// Both halves MUST agree, and the failure when they do not is silent: an
// action aectl never created is a permission no role can hold and no token can
// carry, so the gate refuses a caller who did everything right, in production,
// with nothing in the logs but a 403. That hazard is why this package exists
// rather than a copy of the list in each module — it was previously a YAML
// bootstrap document carrying a comment asking the reader to keep it in step
// by hand. Sharing the vocabulary in Go makes the agreement a compile-time
// fact instead of a request.
//
// What is NOT here: which operations each permission gates (internal/edge's
// permission gate) and which OpenChoreo actions each one implies
// (internal/authz's OC catalog). Both are aep-api's business alone, and
// neither is something an installer needs to know.
package aeperms

// Permission is an AE-level permission key, e.g. "ae:build". The same string
// is the Thunder action's qualified name, the scope entry on a caller's JWT,
// and the unit aep-api's gate resolves — one spelling at every hop, which is
// what lets a mismatch be a compile error rather than a runtime denial.
type Permission string

const (
	PermissionBuild     Permission = "ae:build"
	PermissionBuildView Permission = "ae:build-view"
	// PermissionDesign authorizes changing a project's design: the spec
	// editor's writes (ApplyFiles, the collab room's git flush), design
	// generation, and dependency-contract commits. Paired with
	// PermissionDesignView the same way every other surface pairs a write
	// permission with its view-only sibling — viewing a spec room never implies
	// committing into it, which is enforced on the socket as well as the route
	// (see the collab handler's canWrite). No OC action maps to it: the design
	// tree is git-backed, so nothing here reaches OpenChoreo.
	PermissionDesign            Permission = "ae:design"
	PermissionDesignView        Permission = "ae:design-view"
	PermissionGitHubConfig      Permission = "ae:github-config"
	PermissionModelConfig       Permission = "ae:model-config"
	PermissionRequirementUpdate Permission = "ae:requirement-update"
	PermissionRequirementView   Permission = "ae:requirement-view"
	PermissionSkillConfig       Permission = "ae:skill-config"
	PermissionSkillView         Permission = "ae:skill-view"
	PermissionUsageView         Permission = "ae:usage-view"
	PermissionObservabilityView Permission = "ae:observability-view"
	PermissionResourceView      Permission = "ae:resource-view"
	PermissionResourceConfig    Permission = "ae:resource-config"
)

// Resource names the single Thunder resource the AE actions hang off, and
// Handle is its handle. Thunder qualifies an action as "<resource handle>:<action
// handle>", which is what makes the handles below spell the `ae:` prefix every
// Permission carries — the prefix is not decoration, it is the resource.
const (
	Resource       = "AE"
	ResourceHandle = "ae"

	// ResourceServerName is the display name of the resource server the
	// actions live on. Its identifier is per-install (Thunder's public URL
	// plus ResourceServerPath) and so is computed by the caller, not fixed
	// here — see ResourceServerIdentifier.
	ResourceServerName = "AEP API"
	// ResourceServerPath is appended to Thunder's public URL to form the
	// resource server's identifier. It MUST be an absolute URI: Thunder's
	// /oauth2/authorize rejects a non-URI `resource` parameter outright with
	// invalid_target, and a client requesting an ae:* scope has to send that
	// identifier as an RFC 8707 resource indicator or Thunder resolves the
	// scope against the platform's DEFAULT resource server instead — silently
	// dropping the scope and rewriting the token's audience, which aep-api's
	// own audience allowlist then rejects.
	ResourceServerPath = "/ae"
)

// Action is one AE permission as Thunder declares it: a display name, the
// handle that composes the qualified permission key, and a description that
// shows in Thunder's own console.
type Action struct {
	Permission  Permission
	Name        string
	Handle      string
	Description string
}

// Actions is every AE permission as a Thunder action, in the order an
// installer should create them. Handle is the Permission with the "ae:" prefix
// stripped, because Thunder re-composes the two.
var Actions = []Action{
	{PermissionBuild, "Build", "build", "Trigger and view builds"},
	{PermissionBuildView, "Build View", "build-view", "View builds without triggering"},
	{PermissionDesign, "Design", "design", "Open/edit the spec workspace"},
	{PermissionDesignView, "Design View", "design-view", "View design/spec artifacts"},
	{PermissionGitHubConfig, "GitHub Config", "github-config", "Configure the GitHub integration"},
	{PermissionModelConfig, "Model Config", "model-config", "Configure the organization's model connection"},
	{PermissionRequirementUpdate, "Requirement Update", "requirement-update", "Author/update requirements"},
	{PermissionRequirementView, "Requirement View", "requirement-view", "View requirements"},
	{PermissionSkillConfig, "Skill Config", "skill-config", "Configure skills"},
	{PermissionSkillView, "Skill View", "skill-view", "View skills without configuring them"},
	{PermissionUsageView, "Usage View", "usage-view", "View org usage/spend"},
	{PermissionObservabilityView, "Observability View", "observability-view", "View alerts and RCA reports"},
	{PermissionResourceView, "Resource View", "resource-view", "View the org resource catalog without configuring it"},
	{PermissionResourceConfig, "Resource Config", "resource-config", "Register/edit resources in the org resource catalog"},
}

// AllPermissions is every AE permission key the platform recognizes, derived
// from Actions so a permission cannot be recognized without being declarable.
var AllPermissions = func() []Permission {
	out := make([]Permission, len(Actions))
	for i, a := range Actions {
		out[i] = a.Permission
	}
	return out
}()

// Role names. Both are Thunder roles AND the group each is assigned to: the
// OC AuthzRoleBinding entitles on the `groups` claim by the same name (see
// internal/authz), so the two share one spelling on purpose.
const (
	RoleAdmin     = "ae-admin"
	RoleDeveloper = "ae-developer"
)

// rolePermissions maps a role to the permissions it holds.
//
// ae-developer holds ae:observability-view: alerts and RCA reports are how a
// developer finds out their own deployed code is misbehaving, and an SRE
// incident arrives as one. Withholding it left the role able to build and
// deploy a thing but not to see it fail, and put the notification bell and the
// Alerts page behind a permission the people acting on them did not have.
//
// It omits ae:usage-view — org spend is a billing concern rather than a working
// one, and nothing a developer does depends on reading it.
//
// It omits ae:design too, holding only the ae:design-view beside it. So this is
// the one place the role does NOT get a write/view pair, and the asymmetry is
// deliberate: the design is what the build is judged against, and a role that
// can run builds is not thereby a role that can change what they are measured
// by. Two consequences follow, both intended rather than tolerated:
//
//   - The spec is READ-ONLY for this role. Not just in the UI — ApplyFiles is
//     gated on ae:design, and validate-collab-access answers canWrite:false, so
//     the collab server marks the socket read-only and Yjs updates from it are
//     dropped. A client that does not run our code cannot edit it either.
//   - The AI chat panel is unavailable. Its turn operations gate on ae:design,
//     because a turn's instruction carries `/<skill>` flow commands the server
//     expands — sending one IS editing the design, by a longer route.
var rolePermissions = map[string][]Permission{
	RoleAdmin: AllPermissions,
	RoleDeveloper: {
		PermissionRequirementView,
		PermissionDesignView,
		PermissionBuild,
		PermissionBuildView,
		PermissionObservabilityView,
	},
}

// RoleDescriptions is what each role is called in Thunder's own console.
var RoleDescriptions = map[string]string{
	RoleAdmin:     "Full access to every AE permission.",
	RoleDeveloper: "Read/build access to AE requirements, design, and builds.",
}

// Roles returns the role names in a stable order — admin first, since it is
// the one an installer seeds an account into.
//
//deadcode:keep aectl's caller is in another module, so aep-api's own main
//cannot reach it — the installer provisions these roles into Thunder.
func Roles() []string { return []string{RoleAdmin, RoleDeveloper} }

// RolePermissions returns the permissions the given role holds, or nil if the
// role is unknown. The result is a defensive copy: the catalog is unexported,
// so nothing outside this function can mutate it.
//
//deadcode:keep same as Roles above — aectl reads it to build each Thunder
//role's permission set at install.
func RolePermissions(role string) []Permission {
	perms, ok := rolePermissions[role]
	if !ok {
		return nil
	}
	cp := make([]Permission, len(perms))
	copy(cp, perms)
	return cp
}

// ResourceServerIdentifier returns the `ae` resource server's identifier for
// an installation whose Thunder is served at publicURL. Mirrors aectl's
// SystemResourceIdentifier, and must agree with the console's
// VITE_THUNDER_RESOURCE and aep-api's JWT_AUDIENCE, both of which the platform
// Helm chart derives the same way.
//
//deadcode:keep aectl's caller is in another module, so aep-api's own main
//cannot reach it — this is the one place that states how the identifier is
//formed, which is the fact the installer, the console and the audience check
//all have to agree on.
func ResourceServerIdentifier(publicURL string) string {
	return trimTrailingSlash(publicURL) + ResourceServerPath
}

//deadcode:keep reached only through ResourceServerIdentifier, above.
func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
