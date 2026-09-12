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

package projects

// api_operations.go — the component's OpenAPI contract, read as the API
// gateway's operation table.
//
// The split with internal/spec is deliberate: `spec.OpenAPIOperations` says what
// the DOCUMENT demands (and refuses a document it cannot read), while this file
// says what the GATEWAY must be told about it. The two gateway facts live here
// because neither is in any OpenAPI document:
//
//   - an OPTIONS row per path, because routing runs before policy and an
//     undeclared method+path is a 404 the CORS policy never sees (P2-live-1
//     h1/h2);
//   - a refusal to emit a method or a path shape the RestApi CRD has not been
//     measured to accept, because ONE rejected operation leaves the whole
//     RestApi `Programmed=False` and then EVERY path on it 404s — policy-free
//     siblings included — which is byte-identical to "this API was never
//     deployed" (P2-live-1 d′).
//
// The second is why this file refuses rather than renders-and-hopes: the cost
// of emitting a shape we cannot vouch for is not that one operation misbehaves,
// it is that the component disappears with no error anyone can see.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/wso2/aep/aep-api/internal/spec"
)

// Requirement is what one operation demands of its caller — public, signed-in,
// or one named scope. Aliased rather than redeclared: the gate and the
// projection must not be able to grow two vocabularies for the same block.
type Requirement = spec.OperationRequirement

// The three requirement kinds, re-spelled locally so the projection reads in
// one vocabulary. Same constants, one authority (internal/spec).
const (
	RequirementPublic   = spec.RequirementPublic
	RequirementSignedIn = spec.RequirementSignedIn
	RequirementScope    = spec.RequirementScope
)

// Operation is one row of the `operations` trait parameter, before rendering.
type Operation struct {
	// Method is uppercase, as the RestApi CRD spells it.
	Method string
	// Path is relative to the API context, with `{param}` placeholders.
	Path string
	// Requirement is what the gateway must enforce on this row.
	Requirement Requirement
}

// optionsMethod is synthesised, never read from a document: OpenAPI has no way
// to declare a preflight, and a CORS preflight to an undeclared method+path is
// answered 404 before any policy runs.
const optionsMethod = "OPTIONS"

// renderableMethods is the method set the RestApi CRD was MEASURED to accept —
// the six the trait's own `/*` default enumerates. HEAD and TRACE are legal
// OpenAPI operation keys and are refused here: they have never been rendered
// into a RestApi on the pinned stack, and an operation the CRD rejects takes
// the whole API off the air rather than just itself.
var renderableMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, optionsMethod: true,
}

// pathSegmentRE is one path segment: a literal, or a single `{name}`
// placeholder. Deliberately narrower than RFC 3986 — a segment carrying `*`,
// whitespace, or an unbalanced brace is either a wildcard the contract never
// asked for or a shape the CRD may reject, and both are refusals rather than
// guesses.
var pathSegmentRE = regexp.MustCompile(`^(?:\{[A-Za-z_][A-Za-z0-9_.-]*\}|[A-Za-z0-9._~%!$&'()+,;=:@-]+)$`)

// OperationsFromSpec projects a protected component's OpenAPI contract onto the
// gateway's operation table.
//
// It returns an error rather than a partial table for anything the security
// gate refuses (two scopes, two requirement objects, an OIDC scope as a
// permission, a scheme that is not oauth2, a missing document default) and for
// anything the gateway cannot be told safely (an unrenderable method, a path
// this file cannot vouch for). The caller's only correct response to that error
// is to leave the trait's `/*` default in place: the component then behaves
// exactly as it did before scopes — every operation needs a token, none needs a
// permission — which is loud (a public health check starts answering 401) and
// never wide open.
func OperationsFromSpec(openapiYAML []byte) ([]Operation, error) {
	declared, err := spec.OpenAPIOperations(string(openapiYAML))
	if err != nil {
		return nil, err
	}

	ops := make([]Operation, 0, len(declared)*2)
	// firstOnPath is the requirement the synthesised OPTIONS row inherits: the
	// first operation declared on that path. A preflight short-circuits BEFORE
	// jwt-auth (204, no token) even when the OPTIONS row carries the policy, so
	// which sibling it copies changes nothing a caller can observe — copying one
	// keeps the table readable and keeps `public` off a row that has no reason
	// to be a two-way pass-through.
	firstOnPath := map[string]Requirement{}
	var pathOrder []string
	declaresOptions := map[string]bool{}

	for _, op := range declared {
		method := strings.ToUpper(strings.TrimSpace(op.Method))
		if !renderableMethods[method] {
			return nil, fmt.Errorf(
				"openapi: %s %s cannot be rendered into a RestApi operation: the gateway has been "+
					"measured only for GET, POST, PUT, PATCH, DELETE and OPTIONS, and one operation "+
					"the CRD rejects leaves every path on the API unserved", method, op.Path)
		}
		if err := validateOperationPath(op.Path); err != nil {
			return nil, err
		}
		if _, seen := firstOnPath[op.Path]; !seen {
			firstOnPath[op.Path] = op.Requirement
			pathOrder = append(pathOrder, op.Path)
		}
		if method == optionsMethod {
			declaresOptions[op.Path] = true
		}
		ops = append(ops, Operation{Method: method, Path: op.Path, Requirement: op.Requirement})
	}

	// Appended after the declared rows rather than interleaved: the table is
	// read by humans against the contract it came from, and a contract has no
	// OPTIONS to line up against.
	for _, path := range pathOrder {
		if declaresOptions[path] {
			continue
		}
		ops = append(ops, Operation{Method: optionsMethod, Path: path, Requirement: firstOnPath[path]})
	}
	return ops, nil
}

// validateOperationPath refuses a path template the projection cannot vouch
// for. See the file header: the blast radius of a rejected operation is the
// whole API, so "probably fine" is not good enough.
func validateOperationPath(path string) error {
	refuse := func(why string) error {
		return fmt.Errorf("openapi: path %q cannot be rendered into a RestApi operation: %s", path, why)
	}
	if path == "" || !strings.HasPrefix(path, "/") {
		return refuse("an OpenAPI path is absolute and starts with `/`")
	}
	if path == "/" {
		return nil
	}
	if strings.ContainsAny(path, "?# \t\n") {
		return refuse("a path template carries no query, fragment or whitespace")
	}
	for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if segment == "" {
			return refuse("it has an empty segment")
		}
		if !pathSegmentRE.MatchString(segment) {
			return refuse(fmt.Sprintf("the segment %q is neither a literal nor a single `{param}` placeholder",
				segment))
		}
	}
	return nil
}
