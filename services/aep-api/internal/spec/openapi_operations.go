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

package spec

// openapi_operations.go — WHAT a component's openapi.yaml demands of a caller,
// operation by operation, as data rather than as a refusal message.
//
// The gate next door (openapi_security_gate.go) answers "is this document
// sound"; the gateway projection (internal/projects/api_operations.go) needs
// the same reading of the same block to answer "what do I tell the gateway".
// Both go through the ORDERED reader and the ONE classifier below, because two
// readings of `security` that can disagree are a silent authorization bug: the
// gate would pass a document the projection then renders as something else, and
// the difference is only visible as a 401 nobody can explain.
//
// So `checkOperationSecurity` is this file's classifier plus the catalog rules,
// and nothing else parses an OpenAPI `security` block anywhere in aep-api.

import (
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
)

// OperationRequirementKind is what an operation demands of its caller. The
// three values are the whole vocabulary: the design fixed "at most one scope,
// at most one requirement object" (decision B1), so there is no "any of these
// two scopes" case to represent.
type OperationRequirementKind string

const (
	// RequirementPublic — `security: []`. No token is checked, and the gateway
	// is a two-way pass-through: a caller's `x-user-*` headers reach the
	// handler untouched.
	RequirementPublic OperationRequirementKind = "public"
	// RequirementSignedIn — the document default, or `security: [{oauth2: []}]`
	// spelled out. A valid token from the pinned issuer with the pinned
	// audience, and no particular permission. It is NEVER expressed as the
	// `openid` scope: OIDC scopes ride every access token, so requiring one
	// admits every signed-in account in the org while looking guarded.
	RequirementSignedIn OperationRequirementKind = "signedIn"
	// RequirementScope — `security: [{oauth2: [<handle>]}]`. Exactly one
	// catalog handle, compared by the gateway as a whole string.
	RequirementScope OperationRequirementKind = "scope"
)

// OperationRequirement is one operation's demand. Scope is set only when Kind
// is RequirementScope.
type OperationRequirement struct {
	Kind  OperationRequirementKind
	Scope string
}

// OpenAPIOperation is one (method, path) the document declares, with what it
// demands. Method is uppercase, as the gateway and every message spell it.
type OpenAPIOperation struct {
	Method      string
	Path        string
	Requirement OperationRequirement
}

// signedInRequirement is the document default a protected component must
// declare (`security: [{oauth2: []}]`), and therefore the requirement of every
// operation that declares no `security` of its own.
var signedInRequirement = OperationRequirement{Kind: RequirementSignedIn}

// ErrNoOperations is returned when the document declares no operation at all —
// there is nothing to project, and an empty operation table would take an API
// off the air rather than leave it as it was.
var ErrNoOperations = errors.New("openapi: the document declares no operations")

// OpenAPIOperations reads a PROTECTED component's spec into its operation
// table, refusing anything the security gate refuses about the document itself.
//
// It is the gate's structural half, minus the two rules that need files this
// function is not given: catalog membership and resource ownership (those are
// the build gate's, and they cannot change what an operation DEMANDS — only
// whether the handle it names is one the identity provider will ever mint).
//
// An error here means the caller must not render an operation table. It is
// unreachable for a document that passed the gate, so a caller's fallback is
// for the spec that was never gated (absent, empty, or written before the gate
// existed), not for a routine condition.
func OpenAPIOperations(content string) ([]OpenAPIOperation, error) {
	root := parseOpenAPIOrdered(content)
	if root == nil {
		return nil, errors.New("openapi: the spec is not a YAML mapping")
	}
	scheme := root.record("components").record("securitySchemes").record(oauth2SchemeName)
	if scheme == nil {
		return nil, errors.New("openapi: components.securitySchemes.oauth2 is absent, " +
			"so no operation's security block can be read against a scheme")
	}
	if schemeType, declared := scheme.get("type"); schemeType != oauth2SchemeName {
		return nil, fmt.Errorf("openapi: components.securitySchemes.oauth2 is type %s, want oauth2",
			describeValue(schemeType, declared))
	}
	// The document default is not a nicety: it is what makes an operation whose
	// `security` block was forgotten fail CLOSED. Without it, "absent" would
	// mean public, and the projection would render a wide-open operation from a
	// document that never said so.
	if !isDocumentSecurityDefault(root.byKey["security"]) {
		return nil, errors.New("openapi: the document declares no `security: [{oauth2: []}]` default, " +
			"so an operation that declares no security of its own cannot be read as signed-in")
	}
	// An OIDC scope advertised in the flows is refused even though the
	// projection reads the flows for nothing: the document is then one edit away
	// from an operation that requires it, and the gate refuses it too.
	for _, advertised := range flowScopes(root) {
		if isReservedOIDCScope(advertised.Scope) {
			return nil, errors.New(securityspec.Msg(securityspec.MsgReservedOIDCScope,
				"scope", advertised.Scope, "where", advertised.Where))
		}
	}

	nodes := specOperationNodes(root)
	out := make([]OpenAPIOperation, 0, len(nodes))
	for _, node := range nodes {
		requirement, problem := operationRequirement(node, signedInRequirement)
		if problem != "" {
			return nil, errors.New(problem)
		}
		out = append(out, OpenAPIOperation{Method: node.Method, Path: node.Path, Requirement: requirement})
	}
	if len(out) == 0 {
		return nil, ErrNoOperations
	}
	return out, nil
}

// operationRequirement classifies ONE operation's `security` block: the whole
// structural rule set, in the order the gate reports it, returning the first
// refusal message or the requirement the operation expresses.
//
// The order is load-bearing twice over. It is the TypeScript gate's order, so
// both gates name the same first violation of a two-defect document; and it is
// now also the order the projection refuses in, so a model fixing a refusal
// never meets a different one from a third direction.
func operationRequirement(op specOperationNode, docDefault OperationRequirement) (OperationRequirement, string) {
	raw, declared := op.Op.get("security")
	if !declared {
		return docDefault, ""
	}
	security, isList := raw.([]any)
	if !isList {
		return docDefault, securityspec.Msg(securityspec.MsgOperationSecurityNotAList,
			"method", op.Method, "path", op.Path)
	}
	if len(security) == 0 {
		return OperationRequirement{Kind: RequirementPublic}, ""
	}
	if len(security) > 1 {
		return docDefault, securityspec.Msg(securityspec.MsgOperationMultipleRequirements,
			"method", op.Method, "path", op.Path)
	}

	requirement, isMap := security[0].(*yamlMap)
	if !isMap {
		return docDefault, securityspec.Msg(securityspec.MsgOperationSecurityNotAList,
			"method", op.Method, "path", op.Path)
	}
	// Two schemes in ONE object mean "both", which the gateway cannot express
	// and the generated server does not read — the same disagreement as two
	// objects.
	if len(requirement.keys) != 1 {
		return docDefault, securityspec.Msg(securityspec.MsgOperationMultipleRequirements,
			"method", op.Method, "path", op.Path)
	}
	name := requirement.keys[0]
	if name != oauth2SchemeName {
		return docDefault, securityspec.Msg(securityspec.MsgOperationUnknownScheme,
			"method", op.Method, "path", op.Path, "scheme", name)
	}

	scopes, isList := requirement.byKey[name].([]any)
	if !isList {
		return docDefault, securityspec.Msg(securityspec.MsgOperationSecurityNotAList,
			"method", op.Method, "path", op.Path)
	}
	if len(scopes) > 1 {
		return docDefault, securityspec.Msg(securityspec.MsgOperationMultipleScopes,
			"method", op.Method, "path", op.Path)
	}
	if len(scopes) == 0 {
		// The document default, spelled out.
		return OperationRequirement{Kind: RequirementSignedIn}, ""
	}
	scope, isString := scopes[0].(string)
	if !isString {
		return docDefault, securityspec.Msg(securityspec.MsgOperationSecurityNotAList,
			"method", op.Method, "path", op.Path)
	}
	if isReservedOIDCScope(scope) {
		return docDefault, securityspec.Msg(securityspec.MsgReservedOIDCScope,
			"scope", scope, "where", "the security of "+op.Method+" "+op.Path)
	}
	return OperationRequirement{Kind: RequirementScope, Scope: scope}, ""
}
