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

// Package rolesspec parses and validates `specs/design/roles.json` — the
// STRUCTURED half of a project's security design, and the ONE spec file the
// platform acts on deterministically at build time.
//
// It is the sibling of designspec, and for the same reason: the single schema
// definition is packages/contracts/schemas/roles-design.schema.json (generated
// from the Zod `rolesDesignSchema` the agent's FileBundle write-gate uses),
// vendored here as an embed because go:embed cannot cross the aep-api module
// boundary. Agent and BFF therefore validate ONE definition.
//
// Beyond the schema, this package owns the two things a standalone JSON Schema
// cannot express and the roles ensure absolutely depends on:
//
//   - the referential rules (every test user's role is declared, coldStartRole
//     is declared or null, names and usernames are unique) — the same list as
//     the agent's `checkRolesReferences`;
//   - `EnsurePlan`, the deterministic expansion of the document into the exact
//     set of roles and test users the build must ensure, INCLUDING the
//     platform-supplied user for a role the design gave none. Making that
//     expansion a pure function here — rather than a loop inside the ensure —
//     is what lets "every role has a working login" be a tested property rather
//     than an integration-test hope.
package rolesspec

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/jsonschema"
)

//go:embed roles-design.schema.json
var schemaJSON []byte

// Error codes (mirroring the agent's write gate and designspec's vocabulary, so
// the console renders one set of codes across every gated artifact).
const (
	CodeInvalidJSON     = "INVALID_JSON"
	CodeSchemaViolation = "SCHEMA_VIOLATION"
)

// Path is where the roles document lives, repo-relative.
const Path = "specs/design/roles.json"

// BundleKey is the same file's key inside the design bundle (paths there are
// relative to specs/design/).
const BundleKey = "roles.json"

// ValidationError carries a stable code + human message for a rejected roles
// document.
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string { return e.Code + ": " + e.Message }

var rolesSchema = jsonschema.MustParse(schemaJSON)

// usernameRE mirrors TEST_USERNAME_RE in the TS gate. Lowercase-only so an
// authored username and a platform-generated `test-<role-slug>` cannot collide
// by case alone.
var usernameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// Document is the parsed roles.json.
type Document struct {
	Version          int      `json:"version"`
	ColdStartRole    *string  `json:"coldStartRole"`
	PublicComponents []string `json:"publicComponents"`
	Roles            []Role   `json:"roles"`
	TestUsers        []User   `json:"testUsers"`
}

// Role is one declared role and what it may do within this project.
type Role struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Stories     []int        `json:"stories"`
	GrantedBy   string       `json:"grantedBy"`
	Permissions []Permission `json:"permissions"`
}

// Permission is what one role may do on one component.
type Permission struct {
	Component string   `json:"component"`
	Actions   []string `json:"actions,omitempty"`
	Screens   []string `json:"screens,omitempty"`
}

// User is one declared test user. Username and role, and nothing else — a
// password here would be committed to git and pinned into the version tag.
type User struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Parse validates raw roles.json bytes against the embedded schema and the
// referential rules, returning the parsed document. Returns a *ValidationError
// on any failure.
func Parse(raw []byte) (*Document, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, &ValidationError{Code: CodeInvalidJSON, Message: "content is not valid JSON: " + err.Error()}
	}
	if msgs := jsonschema.Validate(v, rolesSchema); len(msgs) > 0 {
		return nil, &ValidationError{Code: CodeSchemaViolation, Message: msgs[0]}
	}
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, &ValidationError{Code: CodeInvalidJSON, Message: err.Error()}
	}
	if msg := checkReferences(&doc); msg != "" {
		return nil, &ValidationError{Code: CodeSchemaViolation, Message: msg}
	}
	return &doc, nil
}

// checkReferences applies the rules the schema cannot express. It is the Go
// twin of the agent's checkRolesReferences and returns the same first-violation
// message shape; a document that passes one MUST pass the other.
func checkReferences(doc *Document) string {
	declared := map[string]bool{}
	for _, role := range doc.Roles {
		key := strings.ToLower(role.Name)
		if declared[key] {
			return fmt.Sprintf("role %q is declared twice — a role name is an identity on the shared directory, so it appears once.", role.Name)
		}
		declared[key] = true
		if role.Name != strings.TrimSpace(role.Name) {
			return fmt.Sprintf("role %q has leading or trailing whitespace — the name becomes the directory group name verbatim.", role.Name)
		}
		for _, perm := range role.Permissions {
			if len(perm.Actions) == 0 && len(perm.Screens) == 0 {
				return fmt.Sprintf("role %q grants nothing on component %q — give the entry \"actions\" (a service) or \"screens\" (a web application), or drop it.", role.Name, perm.Component)
			}
		}
	}
	if doc.ColdStartRole != nil && !declared[strings.ToLower(*doc.ColdStartRole)] {
		return fmt.Sprintf("coldStartRole %q is not a declared role — name one of roles[].name, or null when a caller with no role reaches nothing.", *doc.ColdStartRole)
	}
	seen := map[string]bool{}
	for _, user := range doc.TestUsers {
		if !usernameRE.MatchString(user.Username) {
			return fmt.Sprintf("test user %q is not a usable directory username — use lowercase letters, digits, \".\", \"_\" or \"-\", starting with a letter or digit.", user.Username)
		}
		if seen[user.Username] {
			return fmt.Sprintf("test user %q is listed twice.", user.Username)
		}
		seen[user.Username] = true
		if !declared[strings.ToLower(user.Role)] {
			return fmt.Sprintf("test user %q holds role %q, which no roles[] entry declares.", user.Username, user.Role)
		}
	}
	return ""
}

// PlannedUser is one test user the build must ensure exists.
type PlannedUser struct {
	Username string
	Role     string
	// Supplied is true when the design named no test user for this role and the
	// platform generated one. The console badges these so the user can see which
	// accounts they did not choose the name of.
	Supplied bool
	// ColdStart marks the user holding the document's cold-start role. The
	// validation credential provider serves this account when a caller asks for
	// credentials without naming a role.
	ColdStart bool
}

// EnsurePlan is the deterministic expansion of a roles document into the work
// the build must do. Roles come out in declaration order; users come out
// grouped by role in the same order, authored users before any supplied one.
type EnsurePlan struct {
	Roles []Role
	Users []PlannedUser
}

// Plan expands doc into the exact set of roles and test users to ensure.
//
// The mandatory-test-user rule lives here: every declared role that the design
// gave no test user gets one named `test-<role-slug>`, marked Supplied. A build
// is never refused for the omission — refusing would trade a real blocked build
// for a documentation nicety the platform can obviously handle itself.
//
// A generated name that collides with an authored username in the same document
// is disambiguated by suffixing the role's ordinal, so the plan can never carry
// two entries for one account.
func Plan(doc *Document) EnsurePlan {
	plan := EnsurePlan{Roles: doc.Roles}
	taken := map[string]bool{}
	for _, u := range doc.TestUsers {
		taken[u.Username] = true
	}
	cold := ""
	if doc.ColdStartRole != nil {
		cold = strings.ToLower(*doc.ColdStartRole)
	}

	byRole := map[string][]User{}
	for _, u := range doc.TestUsers {
		key := strings.ToLower(u.Role)
		byRole[key] = append(byRole[key], u)
	}

	for i, role := range doc.Roles {
		key := strings.ToLower(role.Name)
		authored := byRole[key]
		for _, u := range authored {
			plan.Users = append(plan.Users, PlannedUser{
				Username: u.Username, Role: role.Name, ColdStart: key == cold,
			})
		}
		if len(authored) > 0 {
			continue
		}
		name := supplyUsername(role.Name, i, taken)
		taken[name] = true
		plan.Users = append(plan.Users, PlannedUser{
			Username: name, Role: role.Name, Supplied: true, ColdStart: key == cold,
		})
	}
	return plan
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
// "compliance-admin"). Exported because the console renders the name the build
// WILL generate, before the build runs.
func RoleSlug(name string) string {
	s := slugUnsafeRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "role"
	}
	return s
}
