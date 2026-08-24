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

package provisioning

// DECLARATIVE ENDPOINT WIRING (ADR-0004, narrowed).
//
// The coding agent authors every line of a component's workload.yaml and the
// platform never patches a deployed Workload CR — so the platform's only way to
// tell the agent where a dependency lives is to SAY so. This file resolves the
// `endpoints:` half of that block and posts it as the "Platform-resolved
// dependencies" comment.
//
// The `resources:` half is NOT here any more. A resource's ref and env-var names
// are pure functions of the design plus the resource type's declared outputs, so
// design save stamps them into design.json itself (spec/derive_wiring.go) and the
// agent reads them out of its own tree. What is left here is only what genuinely
// needs live resolution: a cross-project org-service endpoint (the provider may
// not have published it yet) and a same-project sibling's endpoint name (which
// comes from a workload.yaml nobody has written yet).
//
// WHEN it posts is at CYCLE DISPATCH, and that is the whole point of the design.
// It used to post at gate resolution, which had a snapshot audience: a project
// whose gates closed before its implementation issues existed told nobody, and
// nothing ever re-drove it. Dispatch is instead provably the right moment — the
// dispatch predicate (delivery's Dispatchable) requires that NO gate is open in
// the milestone, so by then every dependency that can resolve has, and the
// working set is non-empty by construction rather than by luck. It also covers an
// issue adopted mid-run (a fix or conflict issue), which the gate trigger could
// never reach.
//
// WHO it posts to is the run's WORKING SET: the project's open `aep` issues,
// minus the gates and the validation issue. Nothing platform-side attributes an
// ISSUE to a component — bodies are prose and a title is renamable — so the
// recipient set is the whole working set and the CONTENT is keyed by component
// instead: one block per design component that has endpoints to wire, each naming
// the workload.yaml it belongs in. One agent works the whole milestone in a
// cycle, so that reaches the reader either way; the cost is a comment on a
// sibling issue whose component has no block.
//
// Posting is IDEMPOTENT on the aep:wired label (gate_labels.go), stamped only
// when the block was COMPLETE. A partial block — a sibling endpoint that had not
// resolved yet — goes up unlabelled so the next dispatch supersedes it with the
// fuller version.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/ocname"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// workloadDeps is the `endpoints:` half of the OC WorkloadDescriptor
// `dependencies:` block, rendered to the YAML the coding agent merges into its
// component's workload.yaml. Emitted only when non-empty (omitempty).
//
// There is deliberately no `resources:` field: that half is stamped into each
// dependency's `wiring` block in design.json at design save, and an always-empty
// field here would suggest this comment can carry it.
type workloadDeps struct {
	Endpoints []workloadEndpointDepYAML `yaml:"endpoints,omitempty"`
}

type workloadEndpointDepYAML struct {
	Project     string            `yaml:"project,omitempty"` // omit if same project
	Component   string            `yaml:"component"`
	Name        string            `yaml:"name"`
	Visibility  string            `yaml:"visibility"`
	EnvBindings map[string]string `yaml:"envBindings"` // {address: <ENV>}
}

// PublishResolvedWiring posts the ADR-0004 endpoint-wiring comment on every
// working-set issue that has not had a complete one yet. It is called once per
// cycle dispatch, through delivery's WiringPublisher port.
//
// Best-effort throughout: a GitHub hiccup here must not block a dispatch. Every
// failure is logged and the next issue is still attempted — and because the
// aep:wired marker is only stamped on a complete post, a failure simply means the
// next dispatch tries again.
func (s *Service) PublishResolvedWiring(ctx context.Context, orgID, projectID string) {
	if s.issues == nil || s.design == nil {
		return
	}
	// The audience first: it is one list call, and a dispatch with nothing open to
	// work has nobody to tell.
	targets, err := s.openWorkingSet(ctx, orgID, projectID)
	if err != nil {
		slog.WarnContext(ctx, "provisioning: list working set for wiring comment failed",
			"project", projectID, "error", err)
		return
	}
	pending := targets[:0:0]
	for _, issue := range targets {
		if !delivery.HasLabel(issue.Labels, wiredLabel) {
			pending = append(pending, issue)
		}
	}
	if len(pending) == 0 {
		return
	}

	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		slog.WarnContext(ctx, "provisioning: read design for wiring comment failed",
			"project", projectID, "error", err)
		return
	}
	body, complete := s.resolvedWiringComment(ctx, orgID, projectID, comps)
	if body == "" {
		return // no component has consumer-side endpoints, or none resolve yet
	}

	for _, issue := range pending {
		if cerr := s.issues.CommentIssue(ctx, orgID, projectID, issue.Number, body); cerr != nil {
			slog.WarnContext(ctx, "provisioning: post wiring comment failed",
				"issue", issue.Number, "error", cerr)
			continue // no marker: the next dispatch retries this issue
		}
		if !complete {
			// Something could not be resolved yet. Leaving the marker off is what
			// makes the next dispatch supersede this with the fuller block instead
			// of treating a partial answer as final.
			continue
		}
		if lerr := s.issues.AddLabels(ctx, orgID, projectID, issue.Number, []string{wiredLabel}); lerr != nil {
			// The comment landed but the marker did not, so the next dispatch may
			// repeat it. A duplicate comment is noise; a missing one used to be a
			// CrashLoopBackOff — it no longer is, since the resource half the agent
			// cannot invent now travels in design.json.
			slog.WarnContext(ctx, "provisioning: stamp wiring marker failed — the next dispatch may repeat this comment",
				"issue", issue.Number, "error", lerr)
		}
	}
	slog.InfoContext(ctx, "provisioning: posted resolved endpoint wiring",
		"project", projectID, "issues", len(pending), "complete", complete)
}

// openWorkingSet is the run's working set: the project's OPEN issues that a
// coding cycle works — armed, and of a kind the dev loop picks up.
//
// It is not milestone-scoped, and does not need to be: cutting a version closes
// the previous milestone's still-open issues, so the project's open armed issues
// ARE the current increment's. The `aep` label rides the host's AND-semantics
// ?labels= filter to narrow the fetch, and membership is then decided by
// delivery.InDevWorkingSet — the one place that rule lives — because a label
// filter is the server's promise, not this code's, and because the kind test the
// filter cannot express is the half that matters.
func (s *Service) openWorkingSet(ctx context.Context, orgID, projectID string) ([]sourcecontrol.IssueInfo, error) {
	issues, err := s.issues.ListIssues(ctx, orgID, projectID, []string{delivery.LabelAgentWork})
	if err != nil {
		return nil, fmt.Errorf("provisioning: list working set: %w", err)
	}
	out := make([]sourcecontrol.IssueInfo, 0, len(issues))
	for _, issue := range issues {
		if !strings.EqualFold(issue.State, "open") {
			continue
		}
		if !delivery.InDevWorkingSet(issue.Labels) {
			continue
		}
		out = append(out, issue)
	}
	return out, nil
}

// resolvedWiringComment renders the comment body: a preamble plus one section per
// component whose endpoint wiring resolves to something. Returns "" when nothing
// resolves, so the caller posts nothing rather than an empty block.
//
// complete is false when any component had a consumer-side endpoint dependency
// that could not be resolved yet — the signal that keeps the caller from stamping
// the idempotency marker on a partial answer.
func (s *Service) resolvedWiringComment(ctx context.Context, orgID, projectID string, comps []spec.DesignComponent) (body string, complete bool) {
	var sections []string
	complete = true
	for _, comp := range comps {
		block, contracts, resolvedAll, err := s.resolveDependenciesYAML(ctx, orgID, projectID, comp)
		if err != nil {
			slog.WarnContext(ctx, "provisioning: resolve wiring for component failed",
				"project", projectID, "component", comp.Name, "error", err)
			complete = false
			continue
		}
		if !resolvedAll {
			complete = false
		}
		if block == "" {
			continue
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "## Component `%s`\n\nAdd this to `%s`'s `workload.yaml`:\n\n```yaml\n%s```",
			comp.Name, comp.Name, block)
		if contracts != "" {
			sb.WriteString("\n\n**Consumed API contracts** — before writing any client code, fetch the " +
				"exact contract for each provider below. Do not guess at request/response shapes or " +
				"endpoint paths:\n\n")
			sb.WriteString(contracts)
		}
		sections = append(sections, sb.String())
	}
	if len(sections) == 0 {
		return "", complete
	}
	header := "**Platform-resolved dependencies** — the service addresses your components consume now " +
		"exist.\n\nEach block below belongs to ONE component, named in its heading. Add the block to that " +
		"component's `workload.yaml` (merging into any existing `dependencies:`) **verbatim** — the platform " +
		"has already resolved the targets and the env-var bindings. OpenChoreo injects the resolved " +
		"addresses into your pod env at runtime; never hardcode them.\n\nThis comment carries `endpoints:` " +
		"only. A platform resource's `resources:` entry is in its own dependency's `wiring` block in " +
		"`design.json` — read it from there, and never invent one."
	return header + "\n\n" + strings.Join(sections, "\n\n"), complete
}

// resolveDependenciesYAML resolves one component's consumer ENDPOINT deps —
// cross-project org-service endpoints and same-project component siblings — into
// the workload.yaml dependencies block, plus a "Consumed API contract" section
// (one per endpoint dep, plus one per `external` dep with a design-time-collected
// spec) instructing the coding agent to fetch or read the provider's real
// contract instead of guessing at endpoints.
//
// It does NOT resolve the `resources:` half. A resource's ref and env-var names
// need no binding to compute, so design save stamps them into the dependency's
// own `wiring` block in design.json (spec/derive_wiring.go) and the agent reads
// them from its own tree — which is what removed this comment's ordering
// dependency, and with it the whole class of silent misses.
//
// Anything not yet resolvable is OMITTED and reported through resolvedAll=false,
// so the caller withholds the idempotency marker and the next dispatch re-posts
// the fuller block. Returns "" for the yaml block when nothing resolves. orgID is
// the OC namespace (orgHandle).
func (s *Service) resolveDependenciesYAML(ctx context.Context, orgID, projectID string, comp spec.DesignComponent) (yamlBlock, contracts string, resolvedAll bool, err error) {
	var deps workloadDeps
	var contractSections []string
	resolvedAll = true

	if s.providers != nil {
		// org-service endpoints (cross-project, visibility namespace). Skip any
		// provider not yet published namespace-visible — the next dispatch re-drives.
		for _, name := range comp.OrgServiceDependsOn() {
			target, ok, rerr := s.providers.ResolveNamespaceVisible(ctx, orgID, name)
			if rerr != nil {
				return "", "", false, fmt.Errorf("resolve org-service %q: %w", name, rerr)
			}
			if !ok {
				resolvedAll = false
				continue
			}
			deps.Endpoints = append(deps.Endpoints, workloadEndpointDepYAML{
				Project:     target.Project,
				Component:   target.Component,
				Name:        target.Name,
				Visibility:  "namespace",
				EnvBindings: map[string]string{spec.EndpointAddressOutput: orgServiceURLEnv(name)},
			})
			contractSections = append(contractSections, orgServiceContractSection(name, target))
		}
		// same-project component siblings (visibility project). The sibling's OC
		// component name is the SCOPED one; the env var keys on the LOGICAL dep
		// name. Project is omitted (same project).
		//
		// Both values are now stamped into design.json at design save
		// (spec/derive_wiring.go), so this path is no longer the only source for
		// them — it re-states them for a reader of the issue thread, and still
		// carries the consumed-contract guidance below, which is not derivable.
		for _, depName := range comp.ComponentDependsOn() {
			ocComponent := ocname.ScopedComponentName(projectID, depName)
			target, ok, rerr := s.providers.ResolveProjectEndpoint(ctx, orgID, projectID, ocComponent)
			if rerr != nil {
				return "", "", false, fmt.Errorf("resolve same-project component %q: %w", depName, rerr)
			}
			if !ok {
				resolvedAll = false
				continue
			}
			deps.Endpoints = append(deps.Endpoints, workloadEndpointDepYAML{
				Component:   target.Component,
				Name:        target.Name,
				Visibility:  spec.EndpointVisibilityProject,
				EnvBindings: map[string]string{spec.EndpointAddressOutput: orgServiceURLEnv(depName)},
			})
			contractSections = append(contractSections, localComponentContractSection(depName))
		}
	}

	// external deps with a design-time-collected spec (specPath set): tell the
	// coding agent to implement the client against that EXACT stored contract.
	// This is independent of the external resource's binding/provisioning state
	// below — the spec is a static repo artifact from design save, not a runtime
	// resolution, so it applies whether or not the connection is bound yet.
	for _, d := range comp.Dependencies {
		if d.Kind == spec.DependencyKindExternal && d.SpecPath != "" {
			contractSections = append(contractSections, externalSpecContractSection(d.Name, d.SpecPath))
		}
	}

	if len(deps.Endpoints) == 0 {
		return "", "", resolvedAll, nil
	}
	out, err := yaml.Marshal(map[string]workloadDeps{"dependencies": deps})
	if err != nil {
		return "", "", false, fmt.Errorf("marshal dependencies yaml: %w", err)
	}
	return string(out), strings.Join(contractSections, "\n\n"), resolvedAll, nil
}

// orgServiceContractSection renders the "Consumed API contract" guidance for a
// resolved cross-project org-service dependency: it names the provider (from the
// already-resolved openchoreo.WorkloadEndpointInfo) and tells the coding agent to
// fetch the real contract via MCP rather than guessing at endpoints.
//
// Owner/repo/subdir are intentionally omitted: WorkloadEndpointInfo carries only
// Project/Component/Name/Type/Port/BasePath/Schema. Those coordinates live on the
// separate endpoints.OrgComponentEndpoint DTO behind list_org_component_endpoints,
// which the agent calls anyway and which returns them directly — resolving them
// here too would duplicate a lookup the agent is about to make itself.
func orgServiceContractSection(depName string, target openchoreo.WorkloadEndpointInfo) string {
	return fmt.Sprintf(
		"### Consumed API contract — %s\n"+
			"Provider: project `%s`, component `%s`, endpoint `%s`.\n"+
			"Call the `list_org_component_endpoints` MCP tool to get this provider's API "+
			"contract (inline spec, or via `get_remote_git_file_contents`/`search_remote_git_code` "+
			"when the spec lives in the repo). Implement the client against the EXACT operations. "+
			"Do NOT invent endpoints.",
		depName, target.Project, target.Component, target.Name,
	)
}

// localComponentContractSection renders the "local" variant of the "Consumed API
// contract" guidance for a same-project component dependency: the sibling's
// OpenAPI contract is checked out alongside this component's own code (same
// repo), so no MCP round-trip is needed.
func localComponentContractSection(depName string) string {
	return fmt.Sprintf(
		"### Consumed API contract — %s (local)\n"+
			"Provider: sibling component `%s` in this same project — no MCP call needed, its "+
			"contract is in your own checked-out repo.\n"+
			"Read `specs/design/components/%s/openapi.yaml` and implement the client against the "+
			"EXACT operations. Do NOT invent endpoints.",
		depName, depName, depName,
	)
}

// externalSpecContractSection renders the contract note for an `external`
// dependency that has a `specPath` — which is EITHER a URL or a repo-relative
// file path. It is the authoritative contract when present; the coding agent
// fetches it (URL) or reads it (file) and researches the API's own docs for
// anything the contract doesn't cover.
func externalSpecContractSection(depName, specPath string) string {
	return fmt.Sprintf(
		"External API contract for `%s`: `%s` — if this is a URL, fetch it; if a path, "+
			"it is a file in your checked-out repo. Use it as the source of truth for the "+
			"API's operations, and research the provider's docs for anything it doesn't cover.",
		depName, specPath,
	)
}

// orgServiceURLEnv is <UPPER_SNAKE>_URL — the env var a consumer reads a
// provider's base URL from. Delegates to ocname.ServiceURLEnvName so this comment
// and the `address` binding spec stamps into design.json cannot drift apart.
// e.g. "employee-api" → "EMPLOYEE_API_URL".
func orgServiceURLEnv(name string) string {
	return ocname.ServiceURLEnvName(name)
}
