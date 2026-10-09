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

// Moved from services/aep-api/internal/platform/securityspec/securityspec.go;
// the aep-api copy is deleted in phase 4.

// Package securityspec parses and validates `specs/design/security.json`,
// version 3 — the one spec file the platform acts on deterministically at build
// time. There is no prose companion: this document is the whole security
// design.
//
// The PERMISSION CATALOG is at the centre. A project owns one OAuth
// resource server; `permissions[]` declares its resources and the actions on
// them, and everything else in the document — and in `openapi.yaml` —
// references those `<resource>:<action>` handles rather than restating prose.
// Roles grant handles; operations name one in their security block. Screens are
// NOT in this document: a screen's gate is the scope of the operation that
// LOADS it, and that scope is already in openapi.yaml (ADR-0033).
//
// It is the sibling of designspec, and for the same reason: the single schema
// definition is packages/contracts/schemas/security-design.schema.json
// (generated from the Zod `securityDesignSchema` the agent's FileBundle
// write-gate uses), vendored here as an embed because go:embed cannot cross the
// aep-api module boundary. Agent and BFF therefore validate ONE definition.
//
// Beyond the schema, this package owns the referential rules (references.go)
// — the same list as the agent's `checkSecurityReferences`, phrased from the
// same vendored message table (messages.go) so the model never meets one rule
// in two wordings. Parse applies the half that one file can answer; the half
// that needs a sibling spec file (a component the design cell declares, the
// operations the owner's openapi.yaml declares) is ReferenceFindings, applied
// by a caller that holds the whole bundle. In this module that caller is the
// Files apply's coverage notices; aep-api's copy also carries the build's
// `Plan`, which this copy does not need.
package securityspec

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/jsonschema"
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

// Document vocabulary. `Kind` says what a role is assigned TO; `Enrolment` says
// how somebody comes to hold it. The zero value of each is the default, which
// is why Role.RoleKind and Role.EnrolmentKind exist rather than callers
// comparing "". There is no row vocabulary: which rows an operation reaches is
// its path in openapi.yaml (ADR-0031), not a property of a handle.
const (
	KindUser    = "user"
	KindService = "service"

	EnrolmentAdmin       = "admin"
	EnrolmentSelfService = "self-service"
)

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

// Document is the parsed security.json, version 3.
type Document struct {
	Version     int          `json:"version"`
	Permissions []Permission `json:"permissions"`
	Groups      []Group      `json:"groups"`
	Roles       []Role       `json:"roles"`
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
// It carries no row axis: `read` and `read-all` are two actions because they
// guard two operations, and the path of each says which rows it reaches.
type Action struct {
	Handle      string `json:"handle"`
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
	Stories      []string `json:"stories"`
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

// NeedsTestUser reports whether the build owes this role a login. Every user
// role does, whatever its enrolment — a test user is a disposable agent
// account, not a model of how a person signs up, and a role nobody can sign in
// as cannot be validated. Only a service role gets none: its principal is an
// application.
//
// How the login HOLDS the role still follows enrolment: an admin role's account
// joins an assignTo group, a self-service role's binds straight to the role as
// a user principal (it has no assignTo, and the gate refuses one).
func (r Role) NeedsTestUser() bool {
	return r.RoleKind() == KindUser
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
	if msg := v2Refusal(v); msg != "" {
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
// spelling.
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
	return ""
}

// handleHint is the agent gate's wording for a malformed grant, verbatim.
const handleHint = `must be a catalog handle "<resource>:<action>", each half lowercase letters, digits or "-" starting with a letter`

// IsHandle reports whether value is a full `<resource>:<action>` catalog
// handle. It is the Go twin of isHandle in the agent's schema module: a handle
// carries exactly one colon, each half spelled as handleSegmentRE says.
func IsHandle(value string) bool {
	resource, action, ok := strings.Cut(value, ":")
	if !ok || strings.Contains(action, ":") {
		return false
	}
	return handleSegmentRE.MatchString(resource) && handleSegmentRE.MatchString(action)
}

// removedV1Fields are the fields v2 removed from v1, in the order the refusal lists
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
	carries := "re-author it against version 3"
	if len(removed) > 0 {
		carries = "remove " + strings.Join(removed, ", ")
	}
	return Msg(MsgV1Document, "fields", carries)
}

// v2Refusal is the one-line refusal for a version-2 document, or "" when the
// document is not v2. "Is v2" means it SAYS so, or an action still carries
// `ownership` — the one field v3 removed. Checked AFTER v1Refusal so a v1
// document is told about v1, not about a field it never had. The Go twin of
// v2Refusal in the TS gate.
func v2Refusal(parsed any) string {
	doc, ok := parsed.(map[string]any)
	if !ok {
		return ""
	}
	version, _ := doc["version"].(float64)
	if version != 2 && !someActionHasOwnership(doc) {
		return ""
	}
	return Msg(MsgV2Document)
}

// someActionHasOwnership reports whether any permissions[].actions[] entry still
// carries v2's `ownership`.
func someActionHasOwnership(doc map[string]any) bool {
	permissions, ok := doc["permissions"].([]any)
	if !ok {
		return false
	}
	for _, p := range permissions {
		if perm, ok := p.(map[string]any); ok && someEntryHas(perm["actions"], "ownership") {
			return true
		}
	}
	return false
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
