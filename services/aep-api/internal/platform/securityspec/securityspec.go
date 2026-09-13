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

// Package securityspec parses and validates `specs/design/security.json`,
// version 2 — the one spec file the platform acts on deterministically at build
// time. There is no prose companion: this document is the whole security
// design.
//
// v2 puts the PERMISSION CATALOG at the centre. A project owns one OAuth
// resource server; `permissions[]` declares its resources and the actions on
// them, and everything else in the document — and in `openapi.yaml` and the
// wireframes — references those `<resource>:<action>` handles rather than
// restating prose. Roles grant handles; screens require one; operations name
// one in their security block.
//
// It is the sibling of designspec, and for the same reason: the single schema
// definition is packages/contracts/schemas/security-design.schema.json
// (generated from the Zod `securityDesignSchema` the agent's FileBundle
// write-gate uses), vendored here as an embed because go:embed cannot cross the
// aep-api module boundary. Agent and BFF therefore validate ONE definition.
//
// Beyond the schema, this package owns the two things a standalone JSON Schema
// cannot express and the build absolutely depends on:
//
//   - the referential rules (references.go) — the same list as the agent's
//     `checkSecurityReferences`, phrased from the same vendored message table
//     (messages.go) so the model never meets one rule in two wordings. Parse
//     applies the half that one file can answer; the half that needs a sibling
//     spec file (a component the design cell declares, a screen the wireframe
//     declares, the operation behind a screen) is applied by the build gate,
//     which is the only caller that holds the whole bundle;
//   - `Plan`, the deterministic expansion of the document into the exact set of
//     org groups, project roles and test accounts the build must ensure.
//     Making that expansion a pure function here — rather than a loop inside
//     the ensure — is what lets "every operation's scope is granted by a role
//     that has a login" be a tested property rather than an integration-test
//     hope.
package securityspec

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/jsonschema"
)

//go:embed security-design.schema.json
var schemaJSON []byte

// Error codes (mirroring the agent's write gate and designspec's vocabulary, so
// the console renders one set of codes across every gated artifact).
const (
	CodeInvalidJSON     = "INVALID_JSON"
	CodeSchemaViolation = "SCHEMA_VIOLATION"
)

// Path is where the security document lives, repo-relative.
const Path = "specs/design/security.json"

// BundleKey is the same file's key inside the design bundle (paths there are
// relative to specs/design/).
const BundleKey = "security.json"

// Document vocabulary. `Ownership` says which ROWS an action reaches; `Kind`
// says what a role is assigned TO; `Enrolment` says how somebody comes to hold
// it. The zero value of the two optional ones is the default, which is why
// Role.RoleKind and Role.EnrolmentKind exist rather than callers comparing "".
const (
	OwnershipOwn = "own"
	OwnershipAny = "any"

	KindUser    = "user"
	KindService = "service"

	EnrolmentAdmin       = "admin"
	EnrolmentSelfService = "self-service"

	// PublicScreen is `screens[].requires` for a screen shown before sign-in.
	// A handle always carries a colon, so the literal cannot collide with one.
	PublicScreen = "public"
)

// OIDCScopes are the scopes the identity provider puts on every access token.
// They are NOT catalog handles: an operation guarded on `openid` admits every
// signed-in account in the org and the gateway reports nothing, so a projection
// bug that emits one fails wide open, silently. They are listed here because
// the client's allowlist is "these plus every catalog handle".
var OIDCScopes = []string{"openid", "profile", "email", "group", "ou"}

// ValidationError carries a stable code + human message for a rejected
// security document.
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string { return e.Code + ": " + e.Message }

var securitySchema = jsonschema.MustParse(schemaJSON)

// usernameRE mirrors TEST_USERNAME_RE in the TS gate. Lowercase-only so an
// authored username and a platform-generated `test-<role-slug>` cannot collide
// by case alone.
var usernameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// handleSegmentRE mirrors HANDLE_SEGMENT_RE in the TS gate: one half of a
// `<resource>:<action>` handle. Lowercase because the handle reaches an access
// token's `scope` claim verbatim, and a space-separated claim has no room for a
// quoting convention.
//
// It is enforced in CODE, not as a schema `pattern`: the Go schema interpreter
// does not implement `pattern` and ignores what it does not implement, so a
// pattern in the contract would leave this gate validating less than the
// agent's. The TS side expresses it as a Zod refinement for exactly the same
// reason.
var handleSegmentRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Document is the parsed security.json, version 2.
type Document struct {
	Version     int          `json:"version"`
	Permissions []Permission `json:"permissions"`
	Groups      []Group      `json:"groups"`
	Roles       []Role       `json:"roles"`
	Screens     []Screen     `json:"screens"`
	TestUsers   []TestUser   `json:"testUsers"`
}

// Permission is one resource in the catalog and the actions callers may take on
// it. Component is the service that OWNS the resource, as design.cell names it.
type Permission struct {
	Resource    string   `json:"resource"`
	Component   string   `json:"component"`
	Description string   `json:"description,omitempty"`
	Actions     []Action `json:"actions"`
}

// Action is one action on a resource; `<resource>:<handle>` is the scope handle.
// Ownership is required — an unstated ownership is how a prose-only permission
// turns into a list-everything endpoint.
type Action struct {
	Handle      string `json:"handle"`
	Ownership   string `json:"ownership"`
	Description string `json:"description,omitempty"`
}

// Group is an org group this project INTRODUCES. A group the directory already
// holds is reused by naming it in a role's assignTo and is not redeclared.
type Group struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Role is one project role and everything it may do. Its name becomes
// `<project>/<name>` on the directory, so it is project-scoped and must never
// equal an org group's name.
type Role struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Stories      []int    `json:"stories"`
	Grants       []string `json:"grants"`
	AssignTo     []string `json:"assignTo,omitempty"`
	Enrolment    string   `json:"enrolment,omitempty"`
	AssignableBy []string `json:"assignableBy,omitempty"`
	Kind         string   `json:"kind,omitempty"`
}

// RoleKind is the role's kind with the default applied (`user`).
func (r Role) RoleKind() string {
	if r.Kind == "" {
		return KindUser
	}
	return r.Kind
}

// EnrolmentKind is how somebody comes to hold the role, with the default
// applied (`admin` — somebody puts them in an assignTo group).
func (r Role) EnrolmentKind() string {
	if r.Enrolment == "" {
		return EnrolmentAdmin
	}
	return r.Enrolment
}

// NeedsTestUser reports whether the build owes this role a login. Only an
// admin-enrolment user role: a self-service role's accounts are made by the
// app's registration flow, and a service role is held by an app principal.
func (r Role) NeedsTestUser() bool {
	return r.RoleKind() == KindUser && r.EnrolmentKind() == EnrolmentAdmin
}

// Screen is one screen of one web application and what it takes to reach it.
// Requires is one catalog handle, nil for any signed-in user, or PublicScreen.
type Screen struct {
	Component string  `json:"component"`
	Screen    string  `json:"screen"`
	Requires  *string `json:"requires"`
}

// RequiresHandle returns the catalog handle this screen requires, and whether
// it requires one at all (a public or signed-in-baseline screen does not).
func (s Screen) RequiresHandle() (string, bool) {
	if s.Requires == nil || *s.Requires == PublicScreen {
		return "", false
	}
	return *s.Requires, true
}

// TestUser is one account that exists so a role's behaviour can be exercised. A
// username and role names, and nothing else, ever — a password here would be
// committed to git and pinned into the version tag.
type TestUser struct {
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
}

// Parse validates raw security.json bytes against the embedded schema and the
// referential rules, returning the parsed document. Returns a *ValidationError
// on any failure.
func Parse(raw []byte) (*Document, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, &ValidationError{Code: CodeInvalidJSON, Message: "content is not valid JSON: " + err.Error()}
	}
	// BEFORE the schema: a v1 document gets one sentence about the migration
	// instead of a const-mismatch plus five unknown-key issues in which the
	// real cause is one line among six.
	if msg := v1Refusal(v); msg != "" {
		return nil, &ValidationError{Code: CodeSchemaViolation, Message: msg}
	}
	if msgs := jsonschema.Validate(v, securitySchema); len(msgs) > 0 {
		return nil, &ValidationError{Code: CodeSchemaViolation, Message: msgs[0]}
	}
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, &ValidationError{Code: CodeInvalidJSON, Message: err.Error()}
	}
	if msg := checkRefinements(&doc); msg != "" {
		return nil, &ValidationError{Code: CodeSchemaViolation, Message: msg}
	}
	if msg := checkReferences(&doc); msg != "" {
		return nil, &ValidationError{Code: CodeSchemaViolation, Message: msg}
	}
	return &doc, nil
}

// checkRefinements mirrors the Zod REFINEMENTS of the agent's schema — the
// rules that sit between the object shape and the referential checks: handle
// spelling, and the `screens[].requires` literal.
//
// They are refinements rather than schema keywords on purpose. The Go schema
// interpreter (internal/platform/jsonschema) does not implement `pattern` and
// IGNORES what it does not implement, so a pattern in the published contract
// would leave this gate quietly validating LESS than the agent's. Expressed as
// refinements they are invisible to the JSON Schema, and this function is how
// the platform keeps them — in code, like every referential rule.
//
// The message shape copies Zod's (`path: what is wrong`), because that is what
// the model sees from the write gate for the very same document.
func checkRefinements(doc *Document) string {
	const segmentHint = `must be lowercase letters, digits or "-", starting with a letter`
	for i, permission := range doc.Permissions {
		if !handleSegmentRE.MatchString(permission.Resource) {
			return fmt.Sprintf("permissions.%d.resource: %s", i, segmentHint)
		}
		for j, action := range permission.Actions {
			if !handleSegmentRE.MatchString(action.Handle) {
				return fmt.Sprintf("permissions.%d.actions.%d.handle: %s", i, j, segmentHint)
			}
		}
	}
	for i, role := range doc.Roles {
		for j, handle := range role.Grants {
			if !IsHandle(handle) {
				return fmt.Sprintf("roles.%d.grants.%d: %s", i, j, handleHint)
			}
		}
	}
	for i, screen := range doc.Screens {
		if screen.Requires == nil || *screen.Requires == PublicScreen {
			continue
		}
		if !IsHandle(*screen.Requires) {
			return fmt.Sprintf("screens.%d.requires: must be one catalog handle, null for any signed-in user, or the literal %q", i, PublicScreen)
		}
	}
	return ""
}

// handleHint is the agent gate's wording for a malformed grant, verbatim.
const handleHint = `must be a catalog handle "<resource>:<action>", each half lowercase letters, digits or "-" starting with a letter`

// IsHandle reports whether value is a full `<resource>:<action>` catalog
// handle. It is the Go twin of isHandle in the agent's schema module, and it is
// what makes the `screens[].requires` literal unambiguous: a handle always
// carries exactly one colon, so "public" cannot collide with one.
func IsHandle(value string) bool {
	resource, action, ok := strings.Cut(value, ":")
	if !ok || strings.Contains(action, ":") {
		return false
	}
	return handleSegmentRE.MatchString(resource) && handleSegmentRE.MatchString(action)
}

// removedV1Fields are the fields v2 removed, in the order the refusal lists
// them — the Go twin of REMOVED_V1_FIELDS in the TS gate.
var removedV1Fields = []struct {
	path    string
	present func(doc map[string]any) bool
}{
	{"coldStartRole", func(d map[string]any) bool { _, ok := d["coldStartRole"]; return ok }},
	{"publicComponents", func(d map[string]any) bool { _, ok := d["publicComponents"]; return ok }},
	{"thunder", func(d map[string]any) bool { _, ok := d["thunder"]; return ok }},
	{"roles[].grantedBy", func(d map[string]any) bool { return someEntryHas(d["roles"], "grantedBy") }},
	{"roles[].permissions", func(d map[string]any) bool { return someEntryHas(d["roles"], "permissions") }},
	{"testUsers[].role", func(d map[string]any) bool { return someEntryHas(d["testUsers"], "role") }},
}

func someEntryHas(value any, key string) bool {
	entries, ok := value.([]any)
	if !ok {
		return false
	}
	for _, e := range entries {
		if obj, ok := e.(map[string]any); ok {
			if _, has := obj[key]; has {
				return true
			}
		}
	}
	return false
}

// v1Refusal is the one-line refusal for a version-1 document, or "" when the
// document is not v1. "Is v1" means it SAYS so, or it still carries a field
// only v1 had — a half-migrated file is v1 too.
func v1Refusal(parsed any) string {
	doc, ok := parsed.(map[string]any)
	if !ok {
		return ""
	}
	var removed []string
	for _, f := range removedV1Fields {
		if f.present(doc) {
			removed = append(removed, f.path)
		}
	}
	version, _ := doc["version"].(float64)
	if version != 1 && len(removed) == 0 {
		return ""
	}
	// The slot carries the whole clause, exactly as the agent's v1Refusal builds
	// it: what to remove, or — for a document that only SAYS 1 — what to do
	// instead.
	carries := "re-author it against version 2"
	if len(removed) > 0 {
		carries = "remove " + strings.Join(removed, ", ")
	}
	return Msg(MsgV1Document, "fields", carries)
}

// CatalogHandles returns every `<resource>:<action>` handle the document
// declares, in declaration order. It is the client's scope allowlist (with the
// OIDC scopes), the set every grant and every operation scope is checked
// against, and the rows of the console's Security matrix.
func CatalogHandles(doc *Document) []string {
	var handles []string
	for _, perm := range doc.Permissions {
		for _, action := range perm.Actions {
			handles = append(handles, perm.Resource+":"+action.Handle)
		}
	}
	return handles
}

// ClientScopes is what the project's OAuth client is allowed to ask for: the
// OIDC scopes every access token carries plus every catalog handle,
// space-joined, exactly as the `thunder-app` CRT's `scopes` parameter wants it.
func ClientScopes(doc *Document) string {
	return strings.Join(append(slices.Clone(OIDCScopes), CatalogHandles(doc)...), " ")
}

// PlannedGroup is one org group the build must ensure exists.
//
// Declared says the document's own groups[] introduces it, which is the only
// case that carries a description: a group named only in an assignTo is one the
// org already has, and the platform never rewrites somebody else's description.
type PlannedGroup struct {
	Name        string
	Description string
	Declared    bool
}

// PlannedUser is one test account the build must ensure exists, with everything
// about it already resolved: the roles it holds, the groups those roles put it
// in, and the scopes its token will carry.
type PlannedUser struct {
	Username string
	// Roles are the project roles this account holds, in declaration order.
	Roles []string
	// Groups is the union of the assignTo lists of Roles — the org groups the
	// account is enrolled in. Empty for an account that holds only
	// self-service roles, which nothing enrols through a group.
	Groups []string
	// Scopes is the union of the grants of Roles: what this login may do, and
	// what the gate ticket publishes so the validation agent can ask for a
	// properly scoped token.
	Scopes []string
	// Supplied is true when the design named no test user for a role and the
	// platform generated this account. The console badges these so the user can
	// see which account names they did not choose.
	Supplied bool
}

// EnsurePlan is the deterministic expansion of a security document into the
// work the build must do. Everything comes out in declaration order, so two
// runs of the same tag plan byte-identical work.
type EnsurePlan struct {
	// Handles is the permission catalog, flattened.
	Handles []string
	// Roles are the declared roles, verbatim and in order (service roles
	// included — phase 2's role pass needs them).
	Roles []Role
	// Grants is role name → the handles it grants, for the gate's cross-checks
	// and the ticket's Scopes column.
	Grants map[string][]string
	// Groups are the org groups to ensure: groups[] plus every name any role
	// assigns to.
	Groups []PlannedGroup
	// Users are the accounts to ensure, authored ones first in document order,
	// then the platform's supplied ones in role order.
	Users []PlannedUser
}

// Plan expands doc into the exact set of groups, roles and test accounts to
// ensure.
//
// The mandatory-test-user rule lives here: every admin-enrolment USER role that
// the design gave no test user gets one named `test-<role-slug>`, marked
// Supplied. A build is never refused for the omission — refusing would trade a
// real blocked build for a documentation nicety the platform can obviously
// handle itself. A self-service role gets none (its accounts come from the
// app's registration flow) and neither does a service role (its principal is an
// application, not a person).
//
// A generated name that collides with an authored username in the same document
// is disambiguated by suffixing the role's ordinal, so the plan can never carry
// two entries for one account.
func Plan(doc *Document) EnsurePlan {
	plan := EnsurePlan{
		Handles: CatalogHandles(doc),
		Roles:   doc.Roles,
		Grants:  make(map[string][]string, len(doc.Roles)),
	}
	byName := make(map[string]Role, len(doc.Roles))
	for _, role := range doc.Roles {
		plan.Grants[role.Name] = slices.Clone(role.Grants)
		byName[strings.ToLower(role.Name)] = role
	}

	// Groups: the document's own declarations first (they carry the seed
	// description), then every name a role assigns to that is not one of them.
	seenGroup := map[string]bool{}
	for _, group := range doc.Groups {
		key := strings.ToLower(group.Name)
		if seenGroup[key] {
			continue
		}
		seenGroup[key] = true
		plan.Groups = append(plan.Groups, PlannedGroup{
			Name: group.Name, Description: group.Description, Declared: true,
		})
	}
	for _, role := range doc.Roles {
		for _, name := range role.AssignTo {
			key := strings.ToLower(name)
			if seenGroup[key] {
				continue
			}
			seenGroup[key] = true
			plan.Groups = append(plan.Groups, PlannedGroup{Name: name})
		}
	}

	taken := map[string]bool{}
	for _, u := range doc.TestUsers {
		taken[u.Username] = true
	}
	// Authored accounts, in document order.
	covered := map[string]bool{}
	for _, u := range doc.TestUsers {
		planned := PlannedUser{Username: u.Username, Roles: slices.Clone(u.Roles)}
		planned.Groups, planned.Scopes = expand(u.Roles, byName)
		plan.Users = append(plan.Users, planned)
		for _, roleName := range u.Roles {
			covered[strings.ToLower(roleName)] = true
		}
	}
	// …then one supplied account for every role that owes a login and has none.
	for i, role := range doc.Roles {
		if !role.NeedsTestUser() || covered[strings.ToLower(role.Name)] {
			continue
		}
		name := supplyUsername(role.Name, i, taken)
		taken[name] = true
		planned := PlannedUser{Username: name, Roles: []string{role.Name}, Supplied: true}
		planned.Groups, planned.Scopes = expand(planned.Roles, byName)
		plan.Users = append(plan.Users, planned)
	}
	return plan
}

// expand folds a list of role names into the groups those roles are assigned to
// and the handles they grant — each deduplicated, each in first-seen order, so
// one account holding two roles reads as one union rather than a concatenation.
func expand(roleNames []string, byName map[string]Role) (groups, scopes []string) {
	seenGroup, seenScope := map[string]bool{}, map[string]bool{}
	for _, name := range roleNames {
		role, ok := byName[strings.ToLower(name)]
		if !ok {
			continue
		}
		for _, group := range role.AssignTo {
			if key := strings.ToLower(group); !seenGroup[key] {
				seenGroup[key] = true
				groups = append(groups, group)
			}
		}
		for _, handle := range role.Grants {
			if !seenScope[handle] {
				seenScope[handle] = true
				scopes = append(scopes, handle)
			}
		}
	}
	return groups, scopes
}

// supplyUsername generates the platform's name for a role with no authored test
// user. ordinal disambiguates the (rare) case where the natural name is already
// taken by an authored user of a DIFFERENT role.
func supplyUsername(roleName string, ordinal int, taken map[string]bool) string {
	base := "test-" + RoleSlug(roleName)
	if !taken[base] {
		return base
	}
	return fmt.Sprintf("%s-%d", base, ordinal+1)
}

// slugUnsafeRE is every run of characters a directory username may not hold.
var slugUnsafeRE = regexp.MustCompile(`[^a-z0-9]+`)

// RoleSlug lowercases a role name into the username-safe form the platform's
// generated test-user names are built from ("Compliance Admin" →
// "compliance-admin"). The console has its own copy of this slug for the
// pre-build preview; both must stay in lockstep.
func RoleSlug(name string) string {
	s := slugUnsafeRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "role"
	}
	return s
}
