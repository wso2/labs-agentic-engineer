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

// openapi_security_gate.go — the SECURITY half of a component openapi.yaml's
// gate, the platform-side twin of packages/agent-stream/src/openapi-security.ts.
//
// The structural half (`openapi: 3.x`, has paths, parses) is owned elsewhere:
// the agent's `checkOpenapiSpec` upstream, `NormalizeOpenAPIYAML` here. This
// file judges the document against the two files that give its `security` block
// meaning:
//
//   - the component's design.json — whether the component sits behind end-user
//     sign-in at all;
//   - specs/design/security.json — the permission catalog. Operations REFERENCE
//     handles; they never define them, and a handle whose resource another
//     component owns is that component's to enforce.
//
// Why it is worth mirroring rather than trusting the agent's write gate: every
// rule here is a SILENT failure downstream. A scope the catalog does not
// declare is a scope the identity provider never puts on a token, so every call
// 401s at the gateway and the SPA restarts sign-in — an infinite login loop with
// nothing in any log. An OIDC scope emitted as an API scope (`openid`,
// `profile`, `email`, `group`, `ou`) is worse: the operation looks guarded and
// admits every signed-in account in the org. `X-User-Id` declared
// `required: true` makes the generated server answer 400 from the parameter
// binder before the auth middleware runs, so the design's "no identity → 401"
// is unreachable.
//
// # Rule ORDER is part of the contract
//
// Both gates must name the SAME first violation for the same document, or the
// model fixing one refusal meets a different one from the other side and the
// two gates read as two rule sets. So the order below is the TypeScript order,
// and the document is read in DOCUMENT order (yaml.Node, not a Go map) — a map's
// random iteration would make the first violation of a two-defect document a
// coin toss.
//
// # The one deliberate divergence: how "protected" is decided
//
// The agent bundle keys protectedness on the LITERAL resourceType `thunder-app`
// in the component's design.json dependencies. The platform must not: ADR-0007
// says membership is the resource catalog's `aep.wso2.com/role: end-user-auth`
// CRT marker, never a hardcoded type name, so a new sign-in flavour is a cluster
// install rather than an app-factory release. derive_auth.go resolves that
// marker at design-save and stamps its consequence —
// `exposesAPI.auth = end-user-required` — into the committed design.json, which
// is what this gate reads (the same committed-truth signal build_gate.go's
// hasEndUserSignIn uses, and the reason neither gate needs a cluster round-trip).
//
// PARITY GAP, recorded rather than papered over: a sign-in CRT that is renamed
// or aliased (any labelled type that is not literally `thunder-app`) is seen as
// PROTECTED here and UNPROTECTED by the agent's gate. The agent would then
// refuse the oauth2 scheme this gate requires, and the component could not be
// written at all. Closing it means teaching the agent bundle the marker instead
// of the name — it needs a catalog read the FileBundle does not have today, so
// it is a task for the phase that gives the bundle a platform fact channel.

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
)

// componentSpecPathRE matches a component's own spec by REPO-RELATIVE path, the
// same shape the agent's gate matches. A dependency's spec
// (specs/design/dependencies/…) is somebody else's API and is never judged.
var componentSpecPathRE = regexp.MustCompile(`^specs/design/components/([^/]+)/openapi\.ya?ml$`)

// securityDesignPath is the one authored permission catalog.
const securityDesignPath = "specs/design/" + securityspec.BundleKey

// oauth2SchemeName is the scheme a generated component declares — the only one.
const oauth2SchemeName = "oauth2"

// openapiMethods are the operation keys of a path item (OpenAPI 3.x).
var openapiMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// -------------------------------------------------------------------------
// Reading YAML in document order
// -------------------------------------------------------------------------

// yamlMap is a YAML mapping that remembers the order its keys were written in.
// The gate returns the FIRST violation, so "first" has to mean the same thing
// on both sides of the language boundary; a Go map cannot say that.
type yamlMap struct {
	keys  []string
	byKey map[string]any
}

func (m *yamlMap) get(key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	value, ok := m.byKey[key]
	return value, ok
}

func (m *yamlMap) has(key string) bool {
	_, ok := m.get(key)
	return ok
}

// record returns the value at key as a mapping, or nil when it is absent or is
// not one — the Go shape of the TypeScript `asRecord(x?.[k])` chain.
func (m *yamlMap) record(key string) *yamlMap {
	value, _ := m.get(key)
	nested, _ := value.(*yamlMap)
	return nested
}

// maxYAMLDepth bounds decoding. A YAML alias can be made to point back into its
// own subtree; the gate must refuse to recurse rather than exhaust the stack on
// a document a user can author.
const maxYAMLDepth = 100

// maxYAMLNodes bounds the TOTAL number of values one document may expand to.
// Depth alone does not bound an alias bomb: ten nesting levels of nine aliases
// each is a 500-byte document that expands to 9^10 values, and every one of
// them is produced by the walker below, not by the YAML library. The budget is
// shared across the whole walk (not per branch), so a document that blows it
// stops immediately whatever shape it used to get there. 100k values is far
// beyond any real OpenAPI spec and is reached in milliseconds.
const maxYAMLNodes = 100_000

// yamlWalk carries the node budget across one document's decode.
type yamlWalk struct{ remaining int }

// decode turns a parsed node tree into ordered Go values: *yamlMap for
// mappings, []any for sequences, and the scalar's natural Go type otherwise.
// A document that exhausts the depth or node budget decodes to nil from that
// point down — the gate then simply finds nothing to judge, which is the same
// verdict an unparseable document gets.
func (w *yamlWalk) decode(node *yaml.Node, depth int) any {
	if node == nil || depth > maxYAMLDepth || w.remaining <= 0 {
		return nil
	}
	w.remaining--
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil
		}
		return w.decode(node.Content[0], depth+1)
	case yaml.MappingNode:
		out := &yamlMap{byKey: make(map[string]any, len(node.Content)/2)}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if _, seen := out.byKey[key]; !seen {
				out.keys = append(out.keys, key)
			}
			out.byKey[key] = w.decode(node.Content[i+1], depth+1)
		}
		return out
	case yaml.SequenceNode:
		out := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			out = append(out, w.decode(child, depth+1))
		}
		return out
	case yaml.AliasNode:
		return w.decode(node.Alias, depth+1)
	default:
		var scalar any
		if err := node.Decode(&scalar); err != nil {
			return node.Value
		}
		return scalar
	}
}

// parseOpenAPIOrdered parses a spec body into an ordered mapping, or nil when
// the document is not a YAML mapping. A parse failure is NOT this gate's
// business — the structural gate refuses it first and one defect must produce
// one message.
//
// The document is unmarshalled TWICE on purpose. Decoding into a *yaml.Node
// only builds the parse tree: it never resolves an alias, so it also never
// reaches yaml.v3's "excessive aliasing" guard — the guard lives in the value
// decoder. Probing into an `any` first inherits that guard, and an alias bomb
// is rejected there as a parse failure before the ordered walk below ever
// expands it. The walk's own node budget is the second line of defence.
func parseOpenAPIOrdered(content string) *yamlMap {
	var probe any
	if err := yaml.Unmarshal([]byte(content), &probe); err != nil {
		return nil
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(content), &node); err != nil {
		return nil
	}
	walk := &yamlWalk{remaining: maxYAMLNodes}
	root, _ := walk.decode(&node, 0).(*yamlMap)
	return root
}

// -------------------------------------------------------------------------
// Inputs: the sign-in premise, and the catalog
// -------------------------------------------------------------------------

// componentSignIn reports whether the component sits behind end-user sign-in,
// and whether the question is answerable at all.
//
// known=false — design.json absent, or not a JSON object — is NOT a rejection:
// the design lineup writes design.json before openapi.yaml, so the only way to
// get here is an out-of-order write, and refusing this file for a fact about
// ANOTHER file would be unfixable in one edit.
//
// See the file header for why the signal is `exposesAPI.auth` (derive_auth's
// committed output) rather than the dependency's resourceType.
func componentSignIn(src securityspec.FileSource, component string) (protected bool, known bool) {
	raw, present := src.Read("specs/design/components/" + component + "/design.json")
	if !present {
		return false, false
	}
	var doc any
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return false, false
	}
	root, ok := doc.(map[string]any)
	if !ok {
		return false, false
	}
	exposes, ok := root["exposesAPI"].(map[string]any)
	if !ok {
		return false, true
	}
	auth, _ := exposes["auth"].(string)
	return auth == authEndUserRequired, true
}

// catalogOwners maps `<resource>:<action>` to the component the catalog says
// owns that resource, or nil when security.json is not in the bundle (or does
// not parse). Deliberately lenient, and read structurally rather than through
// securityspec.Parse: the catalog has its own gate, which ran when it was
// written, and re-judging it here would refuse an openapi.yaml for a defect in
// a different file.
func catalogOwners(src securityspec.FileSource) map[string]string {
	raw, present := src.Read(securityDesignPath)
	if !present {
		return nil
	}
	var doc any
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return nil
	}
	root, ok := doc.(map[string]any)
	if !ok {
		return nil
	}
	permissions, ok := root["permissions"].([]any)
	if !ok {
		return nil
	}
	owners := map[string]string{}
	for _, entry := range permissions {
		permission, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		resource, resourceOK := permission["resource"].(string)
		component, componentOK := permission["component"].(string)
		actions, actionsOK := permission["actions"].([]any)
		if !resourceOK || !componentOK || !actionsOK {
			continue
		}
		for _, raw := range actions {
			action, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if handle, ok := action["handle"].(string); ok {
				owners[resource+":"+handle] = component
			}
		}
	}
	return owners
}

// -------------------------------------------------------------------------
// Reading the document
// -------------------------------------------------------------------------

// specOperationNode is one operation with everything the security rules read.
type specOperationNode struct {
	// Method is uppercased, as the message and the gateway spell it.
	Method string
	Path   string
	Op     *yamlMap
	// Parameters are the path item's parameters followed by the operation's
	// own, `$ref`s resolved — the effective set the generated server binds.
	Parameters []*yamlMap
}

// resolveParameter resolves a local `#/components/parameters/<name>`; anything
// else (a remote or non-parameter ref) stays unresolved and is dropped, exactly
// as the agent's gate drops it — a header the gate cannot see is one it must
// not claim a rule about.
func resolveParameter(value any, root *yamlMap) *yamlMap {
	param, ok := value.(*yamlMap)
	if !ok {
		return nil
	}
	rawRef, present := param.get("$ref")
	if !present {
		return param
	}
	ref, ok := rawRef.(string)
	if !ok {
		return param
	}
	const prefix = "#/components/parameters/"
	name, found := strings.CutPrefix(ref, prefix)
	if !found {
		return nil
	}
	return root.record("components").record("parameters").record(name)
}

func parameterList(value any, root *yamlMap) []*yamlMap {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	var out []*yamlMap
	for _, entry := range list {
		if param := resolveParameter(entry, root); param != nil {
			out = append(out, param)
		}
	}
	return out
}

// specOperationNodes returns every operation the document declares, in document
// order.
func specOperationNodes(root *yamlMap) []specOperationNode {
	paths := root.record("paths")
	if paths == nil {
		return nil
	}
	var out []specOperationNode
	for _, path := range paths.keys {
		value, _ := paths.get(path)
		pathItem, ok := value.(*yamlMap)
		if !ok {
			continue
		}
		shared := parameterList(pathItem.byKey["parameters"], root)
		for _, key := range pathItem.keys {
			if !openapiMethods[strings.ToLower(key)] {
				continue
			}
			op, ok := pathItem.byKey[key].(*yamlMap)
			if !ok {
				continue
			}
			params := make([]*yamlMap, 0, len(shared))
			params = append(params, shared...)
			params = append(params, parameterList(op.byKey["parameters"], root)...)
			out = append(out, specOperationNode{
				Method: strings.ToUpper(key), Path: path, Op: op, Parameters: params,
			})
		}
	}
	return out
}

// identityHeaders are the `X-User-*` header parameters an operation carries,
// under the name it declares them with.
func identityHeaders(op specOperationNode) []*yamlMap {
	var out []*yamlMap
	for _, param := range op.Parameters {
		in, _ := param.get("in")
		name, _ := param.get("name")
		header, isString := name.(string)
		if in == "header" && isString && strings.HasPrefix(strings.ToLower(header), "x-user-") {
			out = append(out, param)
		}
	}
	return out
}

// flowScope is one scope the oauth2 scheme advertises, with the flow that does.
type flowScope struct {
	Scope string
	Where string
}

// flowScopes reads `securitySchemes.oauth2.flows.*.scopes`, in document order.
func flowScopes(root *yamlMap) []flowScope {
	flows := root.record("components").record("securitySchemes").record(oauth2SchemeName).record("flows")
	if flows == nil {
		return nil
	}
	var out []flowScope
	for _, flow := range flows.keys {
		scopes := flows.record(flow).record("scopes")
		if scopes == nil {
			continue
		}
		for _, scope := range scopes.keys {
			out = append(out, flowScope{
				Scope: scope,
				Where: "components.securitySchemes.oauth2.flows." + flow + ".scopes",
			})
		}
	}
	return out
}

// isDocumentSecurityDefault reports the document default the design fixes:
// exactly `security: [ { oauth2: [] } ]`.
//
// The scope list must be EMPTY, not merely a list. A default naming a scope
// reads as "everything needs this permission", but nothing enforces that: the
// projection (OpenAPIOperations) hands every operation that declares no
// security of its own a plain signed-in requirement, so the named scope is
// silently dropped and the operation ships open to any signed-in caller. The
// only safe default is the one that says exactly what is enforced.
func isDocumentSecurityDefault(value any) bool {
	list, ok := value.([]any)
	if !ok || len(list) != 1 {
		return false
	}
	requirement, ok := list[0].(*yamlMap)
	if !ok || len(requirement.keys) != 1 || requirement.keys[0] != oauth2SchemeName {
		return false
	}
	scopes, isList := requirement.byKey[oauth2SchemeName].([]any)
	return isList && len(scopes) == 0
}

// isReservedOIDCScope reports whether scope is one of the five scopes that ride
// every access token the identity provider issues.
func isReservedOIDCScope(scope string) bool {
	for _, reserved := range securityspec.OIDCScopes {
		if scope == reserved {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------------
// The gate
// -------------------------------------------------------------------------

// checkOpenapiSecurity judges the security of one component openapi.yaml.
//
// path is REPO-RELATIVE (`specs/design/components/<id>/openapi.yaml`), the same
// vocabulary securityspec.FileSource reads in, so both gates name files the same
// way. It returns "" when the path is not a component spec, when the document is
// sound, or when the premise cannot be read (design.json absent).
//
// security.json absent NARROWS the check rather than skipping it: the structural
// rules (the scheme, the document default, one requirement object, one scope,
// the identity headers) all still run, and only catalog membership and ownership
// wait for the file that defines them.
func checkOpenapiSecurity(path, content string, src securityspec.FileSource) string {
	match := componentSpecPathRE.FindStringSubmatch(path)
	if match == nil {
		return ""
	}
	component := match[1]

	protected, known := componentSignIn(src, component)
	if !known {
		return ""
	}
	root := parseOpenAPIOrdered(content)
	if root == nil {
		return ""
	}

	owners := catalogOwners(src)
	ops := specOperationNodes(root)
	if protected {
		return checkProtectedComponent(component, root, ops, owners)
	}
	return checkUnprotectedComponent(component, root, ops)
}

// checkUnprotectedComponent: a component with no sign-in dependency declares no
// scheme and no security, anywhere. Every one of its operations is public, and a
// `security` block on a policy-free operation is a promise the gateway does not
// keep.
func checkUnprotectedComponent(component string, root *yamlMap, ops []specOperationNode) string {
	if schemes := root.record("components").record("securitySchemes"); schemes != nil && len(schemes.keys) > 0 {
		return securityspec.Msg(securityspec.MsgSchemeWithoutDependency, "component", component)
	}
	if root.has("security") {
		return securityspec.Msg(securityspec.MsgDocumentSecurityWithoutDependency, "component", component)
	}
	for _, op := range ops {
		if op.Op.has("security") {
			return securityspec.Msg(securityspec.MsgSecurityWithoutDependency,
				"component", component, "method", op.Method, "path", op.Path)
		}
	}
	return identityHeaderProblem(ops)
}

// checkProtectedComponent: the scheme, the document default, the advertised
// flow scopes, then every operation.
func checkProtectedComponent(
	component string, root *yamlMap, ops []specOperationNode, owners map[string]string,
) string {
	scheme := root.record("components").record("securitySchemes").record(oauth2SchemeName)
	if scheme == nil {
		return securityspec.Msg(securityspec.MsgMissingOAuth2Scheme, "component", component)
	}
	schemeType, declared := scheme.get("type")
	if schemeType != oauth2SchemeName {
		return securityspec.Msg(securityspec.MsgSchemeWrongType,
			"component", component, "type", describeValue(schemeType, declared))
	}
	if !isDocumentSecurityDefault(root.byKey["security"]) {
		return securityspec.Msg(securityspec.MsgMissingDocumentSecurity, "component", component)
	}

	for _, advertised := range flowScopes(root) {
		if isReservedOIDCScope(advertised.Scope) {
			return securityspec.Msg(securityspec.MsgReservedOIDCScope,
				"scope", advertised.Scope, "where", advertised.Where)
		}
		if owners == nil {
			continue
		}
		owner, inCatalog := owners[advertised.Scope]
		if !inCatalog {
			return securityspec.Msg(securityspec.MsgFlowScopeNotInCatalog, "scope", advertised.Scope)
		}
		if owner != component {
			return securityspec.Msg(securityspec.MsgFlowScopeNotOwned, "scope", advertised.Scope, "owner", owner)
		}
	}

	for _, op := range ops {
		if problem := checkOperationSecurity(component, op, owners); problem != "" {
			return problem
		}
	}
	return identityHeaderProblem(ops)
}

// checkOperationSecurity accepts absent, `[]`, or ONE requirement object naming
// oauth2 with at most one scope (decision B1).
//
// The structural half is operationRequirement (openapi_operations.go) — the
// SAME classifier the gateway projection reads the block with, so a document
// this gate passes cannot be projected as something else. What is left here is
// the catalog half, which needs security.json: the two rules about the handle
// an operation names, rather than about the shape it names it in.
func checkOperationSecurity(component string, op specOperationNode, owners map[string]string) string {
	requirement, problem := operationRequirement(op, signedInRequirement)
	if problem != "" {
		return problem
	}
	if requirement.Kind != RequirementScope || owners == nil {
		return ""
	}
	owner, inCatalog := owners[requirement.Scope]
	if !inCatalog {
		return securityspec.Msg(securityspec.MsgScopeNotInCatalog,
			"method", op.Method, "path", op.Path, "scope", requirement.Scope)
	}
	if owner != component {
		return securityspec.Msg(securityspec.MsgScopeNotOwned,
			"method", op.Method, "path", op.Path, "scope", requirement.Scope, "owner", owner)
	}
	return ""
}

// identityHeaderProblem applies the two identity-header rules. Both are about
// what happens OUTSIDE the document — the parameter binder runs before the
// middleware, and the gateway does not strip inbound headers on an operation it
// applies no policy to — so neither shows up in the spec as anything but a
// plausible line.
//
// PARITY NOTE: on a component with no sign-in at all, an operation is public
// without saying `security: []`, so a `required: true` identity header there is
// reported as identity_header_required rather than as
// public_operation_declares_identity_header. That is the agent gate's wording
// too (packages/agent-stream/src/openapi-security.ts) and the two must name the
// same defect the same way, so it is kept deliberately.
func identityHeaderProblem(ops []specOperationNode) string {
	for _, op := range ops {
		headers := identityHeaders(op)
		if len(headers) == 0 {
			continue
		}
		security, isList := op.Op.byKey["security"].([]any)
		isPublic := isList && len(security) == 0
		for _, header := range headers {
			name, _ := header.byKey["name"].(string)
			if isPublic {
				return securityspec.Msg(securityspec.MsgPublicOperationDeclaresIdentityHeader,
					"method", op.Method, "path", op.Path, "header", name)
			}
			if required, _ := header.byKey["required"].(bool); required {
				return securityspec.Msg(securityspec.MsgIdentityHeaderRequired,
					"method", op.Method, "path", op.Path, "header", name)
			}
		}
	}
	return ""
}

// describeValue renders a scheme's `type` for the refusal: the string as
// written, "absent" when the key is missing, and the JSON form of anything else
// — the same three shapes the agent's message produces.
func describeValue(value any, declared bool) string {
	if text, ok := value.(string); ok {
		return text
	}
	if !declared {
		return "absent"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "absent"
	}
	return string(encoded)
}

// -------------------------------------------------------------------------
// Bundle-wide application
// -------------------------------------------------------------------------

// componentSpecKeys returns the DESIGN-BUNDLE keys (relative to specs/design/)
// of every component openapi.yaml/.yml in files, sorted — so a bundle with two
// broken specs reports them in the same order every run.
func componentSpecKeys(files map[string]string) []string {
	var keys []string
	for rel := range files {
		if componentSpecPathRE.MatchString("specs/design/" + rel) {
			keys = append(keys, rel)
		}
	}
	sort.Strings(keys)
	return keys
}

// openapiSecurityFindings runs the security gate over every component spec in a
// design bundle (keys relative to specs/design/), returning one FileValidationError
// per offending spec — the first violation of each, since the model fixes one
// thing per round trip.
func openapiSecurityFindings(files map[string]string) []FileValidationError {
	src := securityspec.DesignBundle(files)
	var out []FileValidationError
	for _, rel := range componentSpecKeys(files) {
		content := files[rel]
		if strings.TrimSpace(content) == "" {
			continue
		}
		if problem := checkOpenapiSecurity("specs/design/"+rel, content, src); problem != "" {
			out = append(out, FileValidationError{Path: rel, Code: codeInvalidOpenAPI, Message: problem})
		}
	}
	return out
}
