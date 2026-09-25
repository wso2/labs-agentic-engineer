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

import "fmt"

// afmFrontMatter is the minimal shape ComputeAgentToolStatus needs out of an
// `agent.afm.md` document's YAML front matter: just the `x-aep.tools.openapi`
// entries an ai-agent component declares. It intentionally does NOT reuse a
// type from internal/platform/agentfold (afmgate.go): that package's AFM
// parsing (validateAgentAfm and friends) works over map[string]any with
// unexported known-key sets and returns an accept/reject *designProblem, not
// a decoded value — nothing there is exported, so nothing is importable
// across the package boundary. Reimplementing that whole accept/reject gate
// here to get a typed struct would duplicate afmgate.go's rules and drift
// from them silently, which is worse than this narrow, single-purpose type:
// the caller (deriveAgentToolStatuses / parseAFMToolEntries in
// derive_agent_tools.go) parses the YAML front matter itself — leniently,
// since afmgate.go's write-time gate already guarantees a committed
// document's shape — and fills this in with only the field this file's
// precedence table reads. This is a deliberately recorded decision: TWO AFM
// front-matter readers now exist in this codebase (afmgate.go's, strict and
// accept/reject, on write; parseAFMToolEntries', lenient and
// extraction-only, at design read time) because they live in different packages
// with no shared exported surface, not because either was written without
// noticing the other.
type afmFrontMatter struct {
	Tools []afmOpenAPITool
}

// afmOpenAPITool mirrors one `x-aep.tools.openapi[]` entry: the provider
// component and the operationIds the agent may call on it. Only Component and
// Allow are read here — baseUrl is irrelevant to resolution.
type afmOpenAPITool struct {
	Component string
	Allow     []string
}

// AgentToolStatus is the read-time-reportable outcome for ONE allowed
// operation on ONE `x-aep.tools.openapi[]` entry of an ai-agent component's
// agent.afm.md. Component/Operation identify what was checked; Status is one
// of DependencyStatusResolved, DependencyStatusUnresolved, or
// AgentToolStatusUnchecked; Reason is empty on resolved, human-readable
// otherwise.
type AgentToolStatus struct {
	Component string
	Operation string
	Status    string
	Reason    string
}

// AgentToolStatusUnchecked marks an allow-list entry the platform could not
// evaluate because the named component's contract is not in the tree yet (no
// declared component dependency of that kind, or no openapi.yaml alongside
// it) — never persistent evidence of a broken reference, since
// skills/design's per-component writes have no guaranteed order and a
// provider's openapi.yaml can legitimately land after the agent's
// agent.afm.md that references it.
const AgentToolStatusUnchecked = "unchecked"

// ComputeAgentToolStatus is the single authority for resolving an agent's
// `x-aep.tools.openapi[].allow` entries against its declared dependencies and
// each named component's OpenAPI contract — the cross-file checks
// afmgate.go's write-time gate cannot make (another component's files may not
// exist yet). Pure and table-testable, mirroring ComputeDependencyStatus:
// deps is the agent component's OWN Dependencies (already read), contractOps
// maps a component dependency's name to the operationIds its openapi.yaml
// declares (already parsed) — this function performs no I/O and calls no
// resolver itself.
//
// Precedence per (component, operation) pair, first match wins:
//  1. component is not a `kind: component` dependency of the agent at all →
//     unresolved (no address is injected for it — the higher-consequence
//     failure, since a bad operationId costs one tool but an undeclared
//     component costs the whole upstream).
//  2. component IS declared but as a non-component kind (org-service,
//     external) → unchecked: only a component-kind dependency's contract is
//     ever in the tree, so this is never treated as invalid.
//  3. no contract recorded for that component in contractOps (its
//     openapi.yaml is not in this design yet) → unchecked.
//  4. operation absent from that contract's operationIds → unresolved.
//  5. else → resolved.
func ComputeAgentToolStatus(afm afmFrontMatter, deps []Dependency, contractOps map[string][]string) []AgentToolStatus {
	var out []AgentToolStatus
	for _, tool := range afm.Tools {
		dep, declared := componentDependency(deps, tool.Component)
		for _, op := range tool.Allow {
			out = append(out, resolveAgentTool(tool.Component, op, dep, declared, contractOps))
		}
	}
	return out
}

// resolveAgentTool applies the ComputeAgentToolStatus precedence table to one
// (component, operation) pair. dep/declared come from a single
// componentDependency lookup per component (hoisted by the caller) rather
// than repeating the scan per operation.
func resolveAgentTool(component, operation string, dep Dependency, declared bool, contractOps map[string][]string) AgentToolStatus {
	if !declared {
		return AgentToolStatus{
			Component: component,
			Operation: operation,
			Status:    DependencyStatusUnresolved,
			Reason:    fmt.Sprintf("%s is not a declared component dependency — no address is injected for it", component),
		}
	}
	if dep.Kind != DependencyKindComponent {
		return AgentToolStatus{
			Component: component,
			Operation: operation,
			Status:    AgentToolStatusUnchecked,
			Reason:    fmt.Sprintf("%s is a %s dependency, not a component — its contract is never in the tree", component, dep.Kind),
		}
	}
	ops, haveContract := contractOps[component]
	if !haveContract {
		return AgentToolStatus{
			Component: component,
			Operation: operation,
			Status:    AgentToolStatusUnchecked,
			Reason:    fmt.Sprintf("no openapi.yaml for %s in this design", component),
		}
	}
	if !containsString(ops, operation) {
		return AgentToolStatus{
			Component: component,
			Operation: operation,
			Status:    DependencyStatusUnresolved,
			Reason:    fmt.Sprintf("%s is not an operation of %s", operation, component),
		}
	}
	return AgentToolStatus{Component: component, Operation: operation, Status: DependencyStatusResolved}
}

// componentDependency returns the dependency named name from deps, and
// whether one was found at all (regardless of its Kind — the caller
// distinguishes "not declared" from "declared as a non-component kind").
func componentDependency(deps []Dependency, name string) (Dependency, bool) {
	for _, d := range deps {
		if d.Name == name {
			return d, true
		}
	}
	return Dependency{}, false
}

// containsString reports whether s appears in list.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
