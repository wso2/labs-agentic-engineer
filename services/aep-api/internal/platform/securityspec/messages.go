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

package securityspec

// messages.go — the ONE message table the security-design checks phrase their
// refusals from.
//
// The agent's write gate (TypeScript) and this package both refuse the same
// documents, and a refusal the model reads twice in two different wordings is
// two rules as far as the model is concerned. So the wording lives in ONE data
// file — `packages/agent-stream/src/security-design-messages.json` — vendored
// here (go:embed cannot cross the aep-api module boundary) and diffed by
// messages_vendor_test.go, exactly as the JSON Schema is.
//
// Keys are the stable identity; the template text is free to be edited. Every
// key the Go code uses is declared as a constant below and the vendored file
// must hold exactly that set — both directions, so neither a Go check without
// a message nor a message no check emits survives a test run.
//
// Task 1.6 (the OpenAPI security gate) extended this table. Its sentences are
// authored in the SAME package on the agent side but in a SECOND file
// (`packages/agent-stream/src/openapi-security-messages.json`), because the two
// rule sets are two gates over two different documents. That split is kept here:
// two embedded artifacts, two vendor diffs, ONE Go table — so `Msg` stays one
// function and a key can only mean one thing whichever gate renders it.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// placeholderRE is what counts as a `{slot}`. The v1 refusal's own text quotes
// the v2 shape (`actions[{handle, ownership}]`), which is prose, not a slot.
var placeholderRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)

//go:embed security-design-messages.json
var messagesJSON []byte

//go:embed openapi-security-messages.json
var openapiMessagesJSON []byte

// Message keys. The placeholders each template carries are named in the
// comment; Msg is given them as name/value pairs.
const (
	// MsgGrantUnknownHandle {role} {handle} — a grant naming no catalog handle.
	MsgGrantUnknownHandle = "grant_unknown_handle"
	// MsgAssignableByUnknownRole {role} {ref}
	MsgAssignableByUnknownRole = "assignable_by_unknown_role"
	// MsgAdminRoleNeedsAssignTo {role}
	MsgAdminRoleNeedsAssignTo = "admin_role_needs_assign_to"
	// MsgNonAdminRoleHasAssignTo {role} {kind}
	MsgNonAdminRoleHasAssignTo = "non_admin_role_has_assign_to"
	// MsgTestUserUnknownRole {username} {role}
	MsgTestUserUnknownRole = "test_user_unknown_role"
	// MsgTestUserRoleNotUserKind {username} {role}
	MsgTestUserRoleNotUserKind = "test_user_role_not_user_kind"
	// MsgDuplicateTestUser {username}
	MsgDuplicateTestUser = "duplicate_test_user"
	// MsgDuplicateResource {resource}
	MsgDuplicateResource = "duplicate_resource"
	// MsgDuplicateAction {resource} {handle}
	MsgDuplicateAction = "duplicate_action"
	// MsgResourceComponentUnknown {resource} {component} — needs design.cell.
	MsgResourceComponentUnknown = "resource_component_unknown"
	// MsgInvalidTestUsername {username}
	MsgInvalidTestUsername = "invalid_test_username"
	// MsgRoleNameWhitespace {role}
	MsgRoleNameWhitespace = "role_name_whitespace"
	// MsgRoleNameInvalid {role}
	MsgRoleNameInvalid = "role_name_invalid"
	// MsgAssignToDirectoryChecked {role} {group} — INFO, never a refusal: a
	// group the org directory already holds is deliberately not redeclared.
	MsgAssignToDirectoryChecked = "assign_to_directory_checked"
	// MsgDuplicateRoleName {role}
	MsgDuplicateRoleName = "duplicate_role_name"
	// MsgRoleNameIsGroupName {role}
	MsgRoleNameIsGroupName = "role_name_is_group_name"
	// MsgScreenComponentUnknown {component} {screen} — needs design.cell.
	MsgScreenComponentUnknown = "screen_component_unknown"
	// MsgScreenUnknown {component} {screen} — needs the component's wireframes.dsl.
	MsgScreenUnknown = "screen_unknown"
	// MsgScreenRequiresUnknownHandle {component} {screen} {handle}
	MsgScreenRequiresUnknownHandle = "screen_requires_unknown_handle"
	// MsgScreenOperationNotGranted {role} {screen} {handle} — needs the owning
	// component's openapi.yaml.
	MsgScreenOperationNotGranted = "screen_operation_not_granted"
	// MsgReadAllWithoutRead {role} {allHandle} {readHandle} — decision B1.
	MsgReadAllWithoutRead = "read_all_without_read"
	// MsgV1Document {fields}
	MsgV1Document = "v1_document"
	// MsgHandleUsedNowhere {handle} — build-gate WARNING, never a refusal.
	MsgHandleUsedNowhere = "handle_used_nowhere"
	// MsgHandleUnreachable {handle} — build-gate WARNING, never a refusal.
	MsgHandleUnreachable = "handle_unreachable"
)

// The openapi.yaml SECURITY gate's keys (task 1.6). Same table, second
// artifact: these are rendered by internal/spec's gate over a component spec,
// not by the referential checks over security.json.
const (
	// MsgMissingOAuth2Scheme {component}
	MsgMissingOAuth2Scheme = "missing_oauth2_scheme"
	// MsgSchemeWrongType {component} {type}
	MsgSchemeWrongType = "scheme_wrong_type"
	// MsgMissingDocumentSecurity {component} — its text also QUOTES the literal
	// `{oauth2: []}` the model must write, which is prose, not a slot.
	MsgMissingDocumentSecurity = "missing_document_security"
	// MsgSchemeWithoutDependency {component}
	MsgSchemeWithoutDependency = "scheme_without_dependency"
	// MsgDocumentSecurityWithoutDependency {component}
	MsgDocumentSecurityWithoutDependency = "document_security_without_dependency"
	// MsgSecurityWithoutDependency {component} {method} {path}
	MsgSecurityWithoutDependency = "security_without_dependency"
	// MsgOperationSecurityNotAList {method} {path}
	MsgOperationSecurityNotAList = "operation_security_not_a_list"
	// MsgOperationMultipleRequirements {method} {path} — decision B1.
	MsgOperationMultipleRequirements = "operation_multiple_requirements"
	// MsgOperationMultipleScopes {method} {path} — decision B1.
	MsgOperationMultipleScopes = "operation_multiple_scopes"
	// MsgOperationUnknownScheme {method} {path} {scheme}
	MsgOperationUnknownScheme = "operation_unknown_scheme"
	// MsgScopeNotInCatalog {method} {path} {scope}
	MsgScopeNotInCatalog = "scope_not_in_catalog"
	// MsgScopeNotOwned {method} {path} {scope} {owner}
	MsgScopeNotOwned = "scope_not_owned"
	// MsgFlowScopeNotInCatalog {scope}
	MsgFlowScopeNotInCatalog = "flow_scope_not_in_catalog"
	// MsgFlowScopeNotOwned {scope} {owner}
	MsgFlowScopeNotOwned = "flow_scope_not_owned"
	// MsgReservedOIDCScope {scope} {where} — Δ P2-live-2 §2c: an OIDC scope
	// emitted as an API scope fails wide open, silently.
	MsgReservedOIDCScope = "reserved_oidc_scope"
	// MsgIdentityHeaderRequired {method} {path} {header} — Δ P6 §7.2.
	MsgIdentityHeaderRequired = "identity_header_required"
	// MsgPublicOperationDeclaresIdentityHeader {method} {path} {header}
	MsgPublicOperationDeclaresIdentityHeader = "public_operation_declares_identity_header"
)

// MessageKeys is every key this codebase renders, in no particular order. The
// vendored file must hold exactly these — see messages_vendor_test.go.
var MessageKeys = []string{
	MsgGrantUnknownHandle,
	MsgAssignableByUnknownRole,
	MsgAdminRoleNeedsAssignTo,
	MsgNonAdminRoleHasAssignTo,
	MsgTestUserUnknownRole,
	MsgTestUserRoleNotUserKind,
	MsgDuplicateTestUser,
	MsgDuplicateResource,
	MsgDuplicateAction,
	MsgResourceComponentUnknown,
	MsgInvalidTestUsername,
	MsgRoleNameWhitespace,
	MsgRoleNameInvalid,
	MsgAssignToDirectoryChecked,
	MsgDuplicateRoleName,
	MsgRoleNameIsGroupName,
	MsgScreenComponentUnknown,
	MsgScreenUnknown,
	MsgScreenRequiresUnknownHandle,
	MsgScreenOperationNotGranted,
	MsgReadAllWithoutRead,
	MsgV1Document,
	MsgHandleUsedNowhere,
	MsgHandleUnreachable,
	MsgMissingOAuth2Scheme,
	MsgSchemeWrongType,
	MsgMissingDocumentSecurity,
	MsgSchemeWithoutDependency,
	MsgDocumentSecurityWithoutDependency,
	MsgSecurityWithoutDependency,
	MsgOperationSecurityNotAList,
	MsgOperationMultipleRequirements,
	MsgOperationMultipleScopes,
	MsgOperationUnknownScheme,
	MsgScopeNotInCatalog,
	MsgScopeNotOwned,
	MsgFlowScopeNotInCatalog,
	MsgFlowScopeNotOwned,
	MsgReservedOIDCScope,
	MsgIdentityHeaderRequired,
	MsgPublicOperationDeclaresIdentityHeader,
}

// messages is the vendored table, parsed once. A malformed vendored file is a
// build-time defect in a checked-in artifact, so it panics rather than
// degrading every refusal into a placeholder.
var messages = mustParseMessages(messagesJSON, openapiMessagesJSON)

// mustParseMessages folds every vendored artifact into ONE table. A key that
// appears in two artifacts would make a rendered sentence depend on embed
// order, so it panics — the two files describe two gates and their vocabularies
// are disjoint by construction.
func mustParseMessages(raws ...[]byte) map[string]string {
	merged := map[string]string{}
	for _, raw := range raws {
		var table map[string]string
		if err := json.Unmarshal(raw, &table); err != nil {
			panic("securityspec: cannot parse the embedded message table: " + err.Error())
		}
		for key, text := range table {
			if _, clash := merged[key]; clash {
				panic("securityspec: message key " + key + " is declared by two vendored tables")
			}
			merged[key] = text
		}
	}
	return merged
}

// Msg renders the message for key, substituting `{name}` placeholders from
// name/value pairs.
//
// It never panics and never returns "": a missing key or an odd argument list
// is a defect in THIS package, and a build refused with a mangled sentence is
// still better than one refused with an empty one. Both are caught by
// messages_vendor_test.go long before they ship.
func Msg(key string, kv ...string) string {
	template, ok := messages[key]
	if !ok {
		return fmt.Sprintf("(no message text for %q — the vendored table is out of date)", key)
	}
	if len(kv)%2 != 0 {
		return template + fmt.Sprintf(" (message %q rendered with an odd argument list)", key)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		template = strings.ReplaceAll(template, "{"+kv[i]+"}", kv[i+1])
	}
	return template
}

// MessagePlaceholders returns the distinct `{name}` placeholders a template
// carries, sorted. The vendor test uses it to prove every placeholder is one the
// callers actually supply — a template whose slot nobody fills reaches the model
// as a literal `{role}`, which reads as a platform defect rather than as a rule.
//
//deadcode:keep test seam — the message table's placeholder guard, called only by messages_vendor_test.go
func MessagePlaceholders(key string) []string {
	template, ok := messages[key]
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for {
		open := strings.Index(template, "{")
		if open < 0 {
			break
		}
		rest := template[open+1:]
		end := strings.Index(rest, "}")
		if end < 0 {
			break
		}
		if name := rest[:end]; placeholderRE.MatchString(name) && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
		template = rest[end+1:]
	}
	sort.Strings(names)
	return names
}
