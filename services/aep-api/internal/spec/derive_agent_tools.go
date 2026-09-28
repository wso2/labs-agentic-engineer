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

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// AGENT TOOL RESOLUTION AT DESIGN READ TIME.
//
// afmgate.go (internal/platform/agentfold) gates an agent.afm.md document on
// its OWN shape at write time — it cannot check whether a
// `x-aep.tools.openapi[].component` names a real dependency or whether an
// `allow` entry is a real operationId, because that provider's openapi.yaml
// may not exist yet: skills/design writes per-component artifacts in no
// guaranteed order, so an agent's document can legitimately land before its
// provider's contract does. Rejecting on write would fail a correct write.
//
// So this file resolves it here instead, on every design READ (called from
// AssembleDesignFrom, exactly where org-service/external Status/Reason are
// computed), over the WHOLE design (every component's files are already
// assembled into one []DesignComponent by the time derive.go runs) —
// reported as a status on the Go-side design model, never as a write
// rejection. It reuses ComputeAgentToolStatus (agent_tools.go), the pure
// precedence table; this file's job is only to gather that function's
// arguments with no I/O of its own (both its inputs, AgentAFM and
// OpenAPISpec, are already loaded onto DesignComponent by AssembleDesign).
// The result rides two consumers: ComponentDependencies.Dependency.Operations
// (the console's read model, edge/handlers_design.go) and the build-time
// version-cut hard gate (delivery/build/dependency_gate.go), which blocks on
// any DependencyStatusUnresolved entry — never on AgentToolStatusUnchecked.
//
// This intentionally does not import internal/platform/agentfold: everything
// afmgate.go exposes for reading front matter is unexported, and
// reimplementing its full accept/reject rule set here to get a shared type
// would silently drift from it. So a SECOND, narrower AFM reader exists in
// this package (afmFrontMatterPattern below), deliberately: it does not
// validate — afmgate.go's write-time gate already guarantees a committed
// document is well-formed — it only extracts the one thing this precedence
// table reads, `x-aep.tools.openapi[].{component,allow}`. Two parsers is a
// recorded decision, not an accident: one enforces shape on write, the other
// extracts a slice of already-valid content at read/derive time, and they
// live in different packages that cannot share code without exporting
// afmgate.go's internals.

// hasAIAgentComponent reports whether any component in the design is
// ComponentTypeAIAgent — the gate on whether deriveAgentToolStatuses does any
// work at all. Mirrors hasPlatformResourceDependency (derive_auth.go): a
// design with no ai-agent component costs nothing.
func hasAIAgentComponent(components []DesignComponent) bool {
	for i := range components {
		if components[i].ComponentType == ComponentTypeAIAgent {
			return true
		}
	}
	return false
}

// deriveAgentToolStatuses computes AgentToolStatuses for every ai-agent
// component in components and stamps it in place — called from
// AssembleDesignFrom on every design read, alongside
// resolveOrgServices/resolveExternalDependencies, except its result is NEVER
// persisted to design.json (it is a read-time status, not an authored or
// platform-owned design fact, exactly like Dependency.Status). A design with
// no ai-agent component does no work at all. An ai-agent component with no
// agent.afm.md yet (AssembleDesign left AgentAFM empty — the document hasn't
// landed) is left with a nil AgentToolStatuses rather than an error: there is
// nothing to check yet, and that is not a fault.
//
// Every unresolved entry is logged as a single structured line — the check
// must be observable and demonstrably running, on top of its two real
// consumers: it rides the wire on ComponentDependencies (Dependency.Operations,
// edge/handlers_design.go's withAgentToolOperations) and blocks the tag-cut
// (delivery/build/dependency_gate.go's agentToolGateFailures) exactly like an
// unresolved external/org-service dependency does.
func deriveAgentToolStatuses(ctx context.Context, components []DesignComponent) {
	if !hasAIAgentComponent(components) {
		return
	}
	contractOps := buildContractOperationIDs(components)
	for i := range components {
		comp := &components[i]
		if comp.ComponentType != ComponentTypeAIAgent {
			continue
		}
		if strings.TrimSpace(comp.AgentAFM) == "" {
			continue
		}
		afm, err := parseAFMToolEntries(comp.AgentAFM)
		if err != nil {
			slog.WarnContext(ctx, "design read: agent.afm.md front matter unreadable, skipping tool resolution",
				"component", comp.Name, "error", err)
			continue
		}
		statuses := ComputeAgentToolStatus(afm, comp.Dependencies, contractOps)
		comp.AgentToolStatuses = statuses
		for _, st := range statuses {
			if st.Status == DependencyStatusUnresolved {
				slog.WarnContext(ctx, "design read: agent tool allow-list entry unresolved",
					"component", comp.Name, "provider", st.Component, "operation", st.Operation, "reason", st.Reason)
			}
		}
	}
}

// buildContractOperationIDs collects, for every component in the design that
// carries a non-empty OpenAPISpec, the operationIds its openapi.yaml
// declares — the contractOps ComputeAgentToolStatus needs. A component with
// no openapi.yaml at all is simply absent from the returned map (the
// comma-ok "no contract for that component" case in ComputeAgentToolStatus's
// precedence table); a component whose spec parses but declares no
// operationId at all gets an entry mapping to an empty/nil slice, which is
// NOT the same thing — its contract IS in the tree, so an allow entry
// against it is a real "not an operation of" mismatch, not "unchecked".
func buildContractOperationIDs(components []DesignComponent) map[string][]string {
	out := make(map[string][]string, len(components))
	for i := range components {
		spec := components[i].OpenAPISpec
		if strings.TrimSpace(spec) == "" {
			continue
		}
		out[components[i].Name] = operationIDsFromOpenAPISpec(spec)
	}
	return out
}

// openAPIHTTPMethods is the closed set of OpenAPI path-item keys that carry
// an Operation Object (and so may carry an operationId). Everything else
// under a path item (parameters, summary, description, servers, …) is not a
// method and is skipped.
var openAPIHTTPMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// operationIDsFromOpenAPISpec extracts every `operationId` under `paths` in
// an OpenAPI YAML document. Deliberately minimal (a generic YAML walk, not a
// full OpenAPI schema decode): NormalizeOpenAPIYAML/save_gate.go already
// gate parseability + shape on write, so by the time this runs the content
// is a valid, normalized spec — this only needs the operationId list out of
// it. Returns nil on anything that fails to parse as a mapping (fails open:
// an unreadable spec yields no known operations, which
// ComputeAgentToolStatus reports as "not an operation of", never a crash).
func operationIDsFromOpenAPISpec(spec string) []string {
	var raw any
	if err := yaml.Unmarshal([]byte(spec), &raw); err != nil {
		return nil
	}
	root, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	paths, ok := root["paths"].(map[string]any)
	if !ok {
		return nil
	}
	var ids []string
	for _, pathItemRaw := range paths {
		pathItem, ok := pathItemRaw.(map[string]any)
		if !ok {
			continue
		}
		for method, opRaw := range pathItem {
			if !openAPIHTTPMethods[strings.ToLower(method)] {
				continue
			}
			op, ok := opRaw.(map[string]any)
			if !ok {
				continue
			}
			if id, ok := op["operationId"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// afmFrontMatterPattern is a narrower, LOCAL copy of the fence
// afmgate.go matches (afmFrontMatterRe in
// internal/platform/agentfold/afmgate.go) — see the file doc comment above
// for why this package cannot import that one.
var afmFrontMatterPattern = regexp.MustCompile(`^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$`)

// parseAFMToolEntries extracts the `x-aep.tools.openapi[]` entries from an
// agent.afm.md document's YAML front matter into the minimal afmFrontMatter
// shape ComputeAgentToolStatus reads. Lenient by design: afmgate.go's
// write-time gate already guarantees a committed document's shape, so this
// does not re-validate it — a field of the wrong type is simply skipped
// rather than erroring, and a document with no x-aep.tools.openapi at all
// (an ai-agent component that calls nothing) returns a zero-value
// afmFrontMatter with no error. Returns an error only when the document
// itself is not parseable as AFM front matter at all (the fence is missing,
// or the block is not valid YAML) — a state afmgate.go should already have
// prevented from being committed, so this is a defensive fallback, not the
// primary line of defense.
func parseAFMToolEntries(content string) (afmFrontMatter, error) {
	m := afmFrontMatterPattern.FindStringSubmatch(content)
	if m == nil {
		return afmFrontMatter{}, errNoAFMFrontMatter
	}
	var raw any
	if err := yaml.Unmarshal([]byte(m[1]), &raw); err != nil {
		return afmFrontMatter{}, err
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return afmFrontMatter{}, errAFMFrontMatterNotObject
	}
	xaep, ok := obj["x-aep"].(map[string]any)
	if !ok {
		return afmFrontMatter{}, nil
	}
	toolsObj, ok := xaep["tools"].(map[string]any)
	if !ok {
		return afmFrontMatter{}, nil
	}
	openapiList, ok := toolsObj["openapi"].([]any)
	if !ok {
		return afmFrontMatter{}, nil
	}
	var tools []afmOpenAPITool
	for _, entryRaw := range openapiList {
		entry, ok := entryRaw.(map[string]any)
		if !ok {
			continue
		}
		component, _ := entry["component"].(string)
		if component == "" {
			continue
		}
		allowRaw, _ := entry["allow"].([]any)
		var allow []string
		for _, a := range allowRaw {
			if s, ok := a.(string); ok && s != "" {
				allow = append(allow, s)
			}
		}
		if len(allow) == 0 {
			continue
		}
		tools = append(tools, afmOpenAPITool{Component: component, Allow: allow})
	}
	return afmFrontMatter{Tools: tools}, nil
}

// errNoAFMFrontMatter and errAFMFrontMatterNotObject name the two ways
// parseAFMToolEntries' defensive fallback can fail to even start reading —
// see its doc comment.
var (
	errNoAFMFrontMatter        = errors.New("no parseable YAML front matter — not an AFM document")
	errAFMFrontMatterNotObject = errors.New("front matter must be an object")
)
