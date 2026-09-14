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

package agentfold

// designgate.go — the component design.json write-gate, an EXACT
// accept/reject port of packages/agent-stream/src/component-design-schema.ts
// checkComponentDesign (the zod gate the TS FileBundle runs on every write).
//
// Deliberately NOT delegated to internal/platform/designspec: for the fold,
// parity with the agent's gate wins — a write the agent applied must fold
// here too, or the manifest check fails a healthy turn. The historic
// divergence (zod accepted unknown CONNECTION properties while the published
// component-design.schema.json pins `additionalProperties: false`) was
// RECONCILED in Phase 5: the zod gate is strict now, and this port matches.
// Messages are designspec-flavored; message text is log-only.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// componentDesignRe is COMPONENT_DESIGN_JSON_RE.
var componentDesignRe = regexp.MustCompile(`^specs/design/components/([^/]+)/design\.json$`)

type designProblem struct {
	code    ErrCode
	message string
}

var (
	designStringFields = []string{"name", "type", "version", "language", "buildpack", "appPath", "entrypoint", "description"}
	exposureValues     = map[string]bool{"internet": true, "intranet": true}
	// webAppTypeAliases are the wrong spellings of the canonical "web-application"
	// component kind that the zod gate rejects (component-design-schema.ts); the
	// fold gate mirrors that rejection so a design carrying "webapp"/"web-app"
	// (which silently breaks deploy + runtime-config) never folds.
	webAppTypeAliases   = map[string]bool{"webapp": true, "web-app": true, "webapplication": true, "web application": true}
	dependencyKinds     = map[string]bool{"component": true, "org-service": true, "external": true, "platform-resource": true}
	dependencyKnownKeys = map[string]bool{
		"kind": true, "name": true, "description": true,
		"resourceType": true, "parameters": true,
		// wiring is platform-stamped, not agent-authored — but it IS persisted in
		// design.json, so it must fold. Design save re-derives and overwrites it,
		// so an agent echoing back what it read is harmless (see the zod gate's
		// dependencyWiringSchema for why this is accepted rather than rejected).
		"wiring": true,
	}
	// movedDependencyKeys are the fields an external dependency's definition
	// used to carry on the component. They live in
	// specs/design/dependencies/<name>/dependency.json now (one dependency, one
	// definition); a component that still writes them gets a message naming
	// that file, not a bare "unknown property". Mirrors MOVED_DEPENDENCY_FIELDS
	// in component-design-schema.ts.
	movedDependencyKeys = []string{"style", "package", "specPath", "candidates", "suggestions", "config"}
	designKnownKeys     = map[string]bool{
		"name": true, "type": true, "version": true, "language": true,
		"buildpack": true, "appPath": true, "entrypoint": true,
		"exposure": true, "dependencies": true, "description": true,
		"endpoint": true, "exposesAPI": true, "componentAgentInstructions": true,
		"skillsPinned": true, "stories": true,
	}
)

// validateComponentDesign mirrors the zod componentDesignSchema.safeParse
// (dependencies[] — the kind-discriminated successor to the legacy connections[])
// plus the name-equals-directory rule. Only the FIRST problem is reported (the
// accept/reject outcome is what parity needs). It is deliberately AT MOST as
// strict as the TS zod gate (which the FileBundle runs first), so a zod-passing
// write always folds here: the top-level shape + each dependency's kind/name and
// strict-key set are enforced (the retired specUrl/sources fields are no longer
// in that set, so — like status/reason — they reject as unknown keys); the
// optional platform-owned blocks (exposesAPI /
// componentAgentInstructions) are type-checked only.
func validateComponentDesign(content, dirName string) *designProblem {
	var parsed any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return &designProblem{code: ErrInvalidJSON, message: "content is not valid JSON: " + err.Error()}
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "must be an object"}
	}
	// z.strictObject: unknown TOP-LEVEL properties reject.
	for k := range obj {
		if !designKnownKeys[k] {
			return &designProblem{code: ErrSchemaViolation, message: "unknown property " + k}
		}
	}
	for _, field := range designStringFields {
		s, ok := obj[field].(string)
		if !ok {
			return &designProblem{code: ErrSchemaViolation, message: field + ": must be a string"}
		}
		if s == "" {
			return &designProblem{code: ErrSchemaViolation, message: field + ": must be at least 1 characters"}
		}
	}
	if t, _ := obj["type"].(string); webAppTypeAliases[strings.ToLower(strings.TrimSpace(t))] {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("type %q is not a canonical kind — use \"web-application\" for a browser app", t)}
	}
	exposure, ok := obj["exposure"].(string)
	if !ok || !exposureValues[exposure] {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("exposure: %q is not an allowed value", obj["exposure"])}
	}
	deps, ok := obj["dependencies"].([]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "dependencies: must be an array"}
	}
	for i, d := range deps {
		if p := validateDependency(i, d); p != nil {
			return p
		}
	}
	if ep, present := obj["endpoint"]; present {
		if p := validateEndpoint(ep); p != nil {
			return p
		}
	}
	if sp, present := obj["skillsPinned"]; present {
		if p := validateSkillsPinned(sp); p != nil {
			return p
		}
	}
	if st, present := obj["stories"]; present {
		if p := validateStories(st); p != nil {
			return p
		}
	}
	if name := obj["name"].(string); name != dirName {
		return &designProblem{
			code:    ErrSchemaViolation,
			message: fmt.Sprintf("name %q must equal the component directory name %q", name, dirName),
		}
	}
	return nil
}

// validateEndpoint mirrors the zod endpointSchema.strictObject: an optional
// block whose only key is `name` (a non-empty string). The port must accept
// exactly what the zod gate accepts — a design.json with an `endpoint` block
// passes the FileBundle's write-gate, so it MUST fold here too or the manifest
// check fails a healthy turn (the endpoint field was added by the endpoint-name
// single-source change; component-design-schema.ts is the source of truth).
func validateEndpoint(v any) *designProblem {
	ep, ok := v.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "endpoint: must be an object"}
	}
	for k := range ep {
		if k != "name" {
			return &designProblem{code: ErrSchemaViolation, message: "endpoint: unknown property " + k}
		}
	}
	name, ok := ep["name"].(string)
	if !ok || name == "" {
		return &designProblem{code: ErrSchemaViolation, message: "endpoint.name: must be at least 1 characters"}
	}
	return nil
}

// validateStories mirrors the zod `stories` field (platform-recomputed from
// the cell's citations): when present, an array of positive integers.
func validateStories(raw any) *designProblem {
	arr, ok := raw.([]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "stories: must be an array"}
	}
	for i, v := range arr {
		n, ok := v.(float64)
		if !ok || n != float64(int(n)) || n <= 0 {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("stories[%d]: must be a positive integer", i)}
		}
	}
	return nil
}

// validateSkillsPinned mirrors the zod `skillsPinned` field
// (`z.array(z.string())`): when present it must be an array whose every element
// is a string. Parity with the agent's zod gate — without it the Go fold would
// accept a shape the agent rejected (or vice versa), diverging the fold. (JSON
// arrays unmarshal to []any.)
func validateSkillsPinned(raw any) *designProblem {
	arr, ok := raw.([]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "skillsPinned: must be an array"}
	}
	for i, v := range arr {
		if _, ok := v.(string); !ok {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("skillsPinned[%d]: must be a string", i)}
		}
	}
	return nil
}

// validateDependency mirrors the zod dependencySchema.strictObject: a
// kind-discriminated edge whose kind + name are required and whose keys are a
// closed set (unknown keys — notably the read-time-computed status/reason, which
// the agent must NEVER author — reject). It also mirrors the zod gate's
// superRefine: candidates/style/package/specPath are rejected on any kind
// other than "external".
func validateDependency(i int, d any) *designProblem {
	dep, ok := d.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("dependencies[%d]: must be an object", i)}
	}
	if kind, _ := dep["kind"].(string); kind == "external" {
		var moved []string
		for _, k := range movedDependencyKeys {
			if _, present := dep[k]; present {
				moved = append(moved, fmt.Sprintf("%q", k))
			}
		}
		if len(moved) > 0 {
			name, _ := dep["name"].(string)
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf(
				"dependencies[%d] (%q) carries %s — an external dependency's definition lives in specs/design/dependencies/%s/dependency.json "+
					"(provider, style, contract file, config keys, suggestions), written once and shared by every component that uses it. "+
					"Keep only { \"kind\": \"external\", \"name\": %q } here and put those fields in that file.",
				i, name, strings.Join(moved, ", "), name, name)}
		}
	}
	for k := range dep {
		if !dependencyKnownKeys[k] {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("dependencies[%d]: unknown property %s", i, k)}
		}
	}
	kind, ok := dep["kind"].(string)
	if !ok || !dependencyKinds[kind] {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("dependencies[%d].kind: %q is not an allowed value", i, dep["kind"])}
	}
	name, ok := dep["name"].(string)
	if !ok || name == "" {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("dependencies[%d].name: must be a non-empty string", i)}
	}
	if p := validateDependencyParameters(i, dep["parameters"]); p != nil {
		return p
	}
	if p := validateDependencyWiring(i, dep["wiring"]); p != nil {
		return p
	}
	return nil
}

// validateDependencyWiring mirrors the zod dependencyWiringSchema union: when
// present, `wiring` is EXACTLY ONE of two variants, each carrying only its own
// keys —
//
//	{ref, envBindings}  — one workload dependencies.resources[] entry
//	{endpoint}          — one workload dependencies.endpoints[] entry
//
// Every field of a variant is required together. A half-stamped wiring (a ref with
// no bindings, an endpoint with no component) renders a workload.yaml entry
// OpenChoreo silently ignores, which is worse than an absent wiring the agent
// reports as a platform fault.
func validateDependencyWiring(i int, raw any) *designProblem {
	if raw == nil {
		return nil // absent (optional) — not yet derivable
	}
	violation := func(format string, args ...any) *designProblem {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("dependencies[%d].wiring"+format, append([]any{i}, args...)...)}
	}
	wiring, ok := raw.(map[string]any)
	if !ok {
		return violation(": must be an object")
	}
	_, hasEndpoint := wiring["endpoint"]
	if hasEndpoint {
		// The variants are exclusive: one dependency resolves to a resource or to
		// an endpoint, never both, so a mixed object is a defect and not a
		// tolerable superset.
		for k := range wiring {
			if k != "endpoint" {
				return violation(": %s cannot be combined with endpoint — a dependency wires to a resource or to an endpoint, not both", k)
			}
		}
		return validateEndpointWiring(violation, wiring["endpoint"])
	}
	for k := range wiring {
		if k != "ref" && k != "envBindings" {
			return violation(": unknown property %s", k)
		}
	}
	if ref, ok := wiring["ref"].(string); !ok || ref == "" {
		return violation(".ref: must be a non-empty string")
	}
	return validateEnvBindings(violation, ".envBindings", wiring["envBindings"])
}

// validateEndpointWiring mirrors the zod endpointWiringSchema.strictObject: the
// scoped provider component, its endpoint name, the target's visibility, and the
// address binding — all required, nothing else.
func validateEndpointWiring(violation func(string, ...any) *designProblem, raw any) *designProblem {
	endpoint, ok := raw.(map[string]any)
	if !ok {
		return violation(".endpoint: must be an object")
	}
	for k := range endpoint {
		switch k {
		case "component", "name", "visibility", "envBindings":
		default:
			return violation(".endpoint: unknown property %s", k)
		}
	}
	for _, field := range []string{"component", "name", "visibility"} {
		if v, ok := endpoint[field].(string); !ok || v == "" {
			return violation(".endpoint.%s: must be a non-empty string", field)
		}
	}
	return validateEnvBindings(violation, ".endpoint.envBindings", endpoint["envBindings"])
}

// validateEnvBindings is the output→env-var map both variants carry: an object of
// string values. Shared so the two variants cannot drift on what a binding is.
func validateEnvBindings(violation func(string, ...any) *designProblem, path string, raw any) *designProblem {
	bindings, ok := raw.(map[string]any)
	if !ok {
		return violation("%s: must be an object", path)
	}
	for k, v := range bindings {
		if _, ok := v.(string); !ok {
			return violation("%s.%s: must be a string", path, k)
		}
	}
	return nil
}

// validateDependencyParameters mirrors the zod
// `parameters: z.record(z.string(), z.union([z.string(), z.number(), z.boolean()]))`:
// when present, parameters must be an object whose values are scalar (string,
// number, or boolean). Objects/arrays/null values reject. Enforcing this keeps
// the write-gate in parity with the agent's zod gate — WITHOUT it the Go fold
// would accept a parameter shape the agent rejected (or vice versa), diverging
// the fold. (JSON numbers unmarshal to float64 through encoding/json.)
func validateDependencyParameters(i int, raw any) *designProblem {
	if raw == nil {
		return nil // absent (optional)
	}
	params, ok := raw.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("dependencies[%d].parameters: must be an object", i)}
	}
	for k, v := range params {
		switch v.(type) {
		case string, float64, bool:
			// scalar — allowed
		default:
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("dependencies[%d].parameters.%s: must be a string, number, or boolean", i, k)}
		}
	}
	return nil
}
