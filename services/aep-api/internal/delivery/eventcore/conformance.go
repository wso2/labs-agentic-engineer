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

package eventcore

// WIRING CONFORMANCE.
//
// A component whose design declares a database, and whose shipped workload.yaml
// declares no resource at all, BUILDS. The image compiles, the pod starts, an
// in-process store works, and nothing fails until someone notices the data does
// not survive a restart. That is exactly how a run once shipped SQLite on a
// container filesystem for a component whose design named postgres-cnpg.
//
// So the platform checks it, deterministically, with no LLM involved: the design
// says what a component consumes (each dependency's stamped `wiring`), the shipped
// workload.yaml says what it declared, and the difference is a defect. Nothing
// about that comparison needs a model, a cluster read or a heuristic.
//
// BOTH sub-blocks of `dependencies:` are checked, and the endpoints half is the
// quieter defect of the two. A missing resource at least leaves data that does not
// survive a restart; a sibling endpoint targeting the FRIENDLY component name
// instead of the scoped one produces nothing observable at all — the release
// renders, the pod runs, the app serves, and the sole symptom is a ReleaseBinding
// parked at Ready=False with the connection unresolved, which surfaces only as a
// project that reads "deploying" forever. Because the value is stamped in
// design.json, the comparison is exact: presence is not enough, the target must be
// the declared one.
//
// WHERE it runs is the merged-PR fan-out, which fits the policy this package
// already follows (see decideAutoMerge): there is no verification before the
// merge, because the merge is what triggers verification — and a defect it finds
// mints a fix issue into the same milestone, which the next cycle works like any
// other. Blocking the merge on this would gate the merge on the thing it exists
// to cause.
//
// It NEVER fails the fan-out. A build that would have run still runs: a missing
// wiring declaration is a defect to fix, not a reason to withhold the build output
// that tells the agent whether its code even compiles.

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// workloadPath is where a component's workload descriptor lives, relative to its
// App Path. The coding agent authors it there; OpenChoreo reads it from there.
const workloadPath = "workload.yaml"

// checkWiringConformance compares one component's design-declared wiring against
// the workload.yaml it shipped, and mints a fix issue naming whatever is missing.
//
// BOTH `dependencies:` sub-blocks are checked, and the endpoints half is checked
// for the VALUE, not just presence — see missingEndpointTargets.
//
// Every failure path is a logged no-op. The check is a safety net; a net that can
// fail a merge fan-out is worse than the hole it covers.
func (e *Events) checkWiringConformance(ctx context.Context, run *delivery.MilestoneRun, component string) {
	if e.p.Design == nil || e.p.Workloads == nil || run == nil {
		return
	}
	declared, err := e.p.Design.DeclaredResources(ctx, run.OrgID, run.ProjectID)
	if err != nil {
		slog.WarnContext(ctx, "eventcore: read declared resources for conformance failed",
			"project", run.ProjectID, "component", component, "error", err)
		return
	}
	want, ok := declared[component]
	if !ok || (len(want.Refs) == 0 && len(want.EndpointTargets) == 0) {
		return // nothing declared ⇒ nothing to conform to
	}

	path := workloadPath
	if want.AppPath != "" {
		path = want.AppPath + "/" + workloadPath
	}
	content, found, err := e.p.Workloads.ReadFile(ctx, run.OrgID, run.ProjectID, path)
	if err != nil {
		slog.WarnContext(ctx, "eventcore: read workload.yaml for conformance failed",
			"project", run.ProjectID, "component", component, "path", path, "error", err)
		return
	}
	if !found {
		// A component with no workload.yaml at all is a separate, louder failure
		// the build itself reports; do not double-report it as a wiring defect.
		slog.WarnContext(ctx, "eventcore: no workload.yaml to check for conformance",
			"project", run.ProjectID, "component", component, "path", path)
		return
	}

	if missing := missingResourceRefs(want.Refs, content); len(missing) > 0 {
		slog.WarnContext(ctx, "eventcore: shipped workload.yaml is missing declared resources",
			"project", run.ProjectID, "component", component, "missing", missing)
		if _, err := e.mintUnwiredResourceIssue(ctx, run, component, path, missing); err != nil {
			slog.WarnContext(ctx, "eventcore: mint unwired-resource issue failed",
				"project", run.ProjectID, "component", component, "error", err)
		}
	}
	if missing := missingEndpointTargets(want.EndpointTargets, content); len(missing) > 0 {
		slog.WarnContext(ctx, "eventcore: shipped workload.yaml does not target the declared sibling endpoints",
			"project", run.ProjectID, "component", component, "missing", missing)
		if _, err := e.mintUnwiredEndpointIssue(ctx, run, component, path, missing); err != nil {
			slog.WarnContext(ctx, "eventcore: mint unwired-endpoint issue failed",
				"project", run.ProjectID, "component", component, "error", err)
		}
	}
}

// missingResourceRefs returns the declared refs absent from the workload
// descriptor's `dependencies.resources[]`, sorted for a stable issue body.
//
// An unparseable descriptor yields every ref as missing rather than none: the
// agent authored a file OpenChoreo cannot read, so no resource is wired,
// whatever the bytes intended.
func missingResourceRefs(declaredRefs []string, workloadYAML string) []string {
	var doc struct {
		Dependencies struct {
			Resources []struct {
				Ref string `yaml:"ref"`
			} `yaml:"resources"`
		} `yaml:"dependencies"`
	}
	shipped := map[string]bool{}
	if err := yaml.Unmarshal([]byte(workloadYAML), &doc); err == nil {
		for _, r := range doc.Dependencies.Resources {
			shipped[strings.TrimSpace(r.Ref)] = true
		}
	}
	var missing []string
	for _, ref := range declaredRefs {
		if !shipped[ref] {
			missing = append(missing, ref)
		}
	}
	sort.Strings(missing)
	return missing
}

// missingEndpointTargets returns the declared sibling endpoint targets absent from
// the workload descriptor's `dependencies.endpoints[]`, sorted for a stable issue
// body.
//
// This catches a WRONG value, not only a missing one, and that is the point: the
// expected `component` is the SCOPED OC name, and an agent guessing writes the
// friendly one. `todo-api` where `todo-api99-todo-api` was declared is a diff, so
// it reports — where before it shipped, built, deployed and served, with the only
// symptom a ReleaseBinding stuck at Ready=False and a project that read
// "deploying" forever.
//
// An unparseable descriptor yields every target as missing, for the same reason
// missingResourceRefs does: OpenChoreo cannot read the file either, so nothing in
// it is wired, whatever the bytes intended.
func missingEndpointTargets(declaredTargets []string, workloadYAML string) []string {
	var doc struct {
		Dependencies struct {
			Endpoints []struct {
				Component string `yaml:"component"`
			} `yaml:"endpoints"`
		} `yaml:"dependencies"`
	}
	shipped := map[string]bool{}
	if err := yaml.Unmarshal([]byte(workloadYAML), &doc); err == nil {
		for _, ep := range doc.Dependencies.Endpoints {
			shipped[strings.TrimSpace(ep.Component)] = true
		}
	}
	var missing []string
	for _, target := range declaredTargets {
		if !shipped[target] {
			missing = append(missing, target)
		}
	}
	sort.Strings(missing)
	return missing
}

// mintUnwiredResourceIssue files the fix issue for a component that shipped
// without declaring resources its design consumes.
//
// The body names the refs and points at where the correct block lives — the
// dependency's own `wiring` in design.json — because the failure mode this
// catches is an agent that could not find that block and invented a substitute.
// It carries the agent-work label so the next cycle picks it up like any other
// work, and dedupes on (component, refs) so a redelivered webhook or a second
// merge touching the same component files nothing new.
func (e *Events) mintUnwiredResourceIssue(ctx context.Context, run *delivery.MilestoneRun,
	component, path string, missing []string) (int, error) {
	list := "`" + strings.Join(missing, "`, `") + "`"
	body := fmt.Sprintf(
		"Component **%s** declares platform/external resources in its design that its shipped `%s` does not consume, so OpenChoreo injects nothing for them and the component cannot be using them.\n\n"+
			"Missing from `dependencies.resources`: %s\n\n"+
			"Each one's exact entry is already resolved for you: read the `wiring` object on that dependency in `specs/design/components/%s/design.json` and copy its `ref` and `envBindings` into `%s` verbatim. "+
			"Then make the code read those env vars — if it currently persists to a local file, an in-process store, or any other substitute technology, replace that with the declared resource.\n\n"+
			"Do not invent a ref or an env-var name, and do not remove the dependency from the design to make this pass.",
		component, path, list, component, path)

	number, _, err := e.p.Writer.Mint(ctx, run.OrgID, run.ProjectID, delivery.IssueSpec{
		Title:     fmt.Sprintf("Wire the declared resources for %s", component),
		Body:      body,
		Labels:    []string{delivery.LabelAgentWork, delivery.KindBug, delivery.SrcBuild},
		Milestone: run.MilestoneNumber,
		DedupeKey: delivery.DedupeKeyUnwiredResources(component, missing),
	})
	return number, err
}

// mintUnwiredEndpointIssue files the fix issue for a component whose shipped
// workload.yaml does not target the sibling endpoints its design declares.
//
// The body leads with WHY nothing looks broken, because that is the trap: unlike a
// missing resource, a wrong endpoint target still builds, deploys and serves. An
// agent asked to "fix the deployment" would find a running pod, conclude the report
// is stale, and close it. The one observable symptom — a ReleaseBinding parked at
// Ready=False with the connection unresolved — is named explicitly so the agent can
// confirm the defect rather than re-diagnose it.
//
// Same policy as the resource half: an armed `bug` sourced `src/build` so the next
// cycle picks it up, dedupe on (component, targets) so a redelivered webhook files
// nothing new.
func (e *Events) mintUnwiredEndpointIssue(ctx context.Context, run *delivery.MilestoneRun,
	component, path string, missing []string) (int, error) {
	list := "`" + strings.Join(missing, "`, `") + "`"
	body := fmt.Sprintf(
		"Component **%s** declares sibling-component dependencies in its design that its shipped `%s` does not target, so OpenChoreo resolves no address for them and injects no env var.\n\n"+
			"Missing from `dependencies.endpoints`: %s\n\n"+
			"**This does not look broken.** The component still builds, deploys and serves — the release renders fine without the address, so the only symptom is the component's ReleaseBinding sitting at `Ready=False` with `ConnectionsPending`, and the platform reporting the project as \"deploying\" indefinitely. Do not close this because the app is running.\n\n"+
			"The usual cause is a `component:` value written as the FRIENDLY component name. OpenChoreo resolves an endpoint dependency by the SCOPED name (`<project>-<component>`), so the friendly name matches no binding, silently.\n\n"+
			"Each entry is already resolved for you: read the `wiring.endpoint` object on that dependency in `specs/design/components/%s/design.json` and copy it into `%s` as one `dependencies.endpoints[]` entry verbatim — it is byte-identical to the entry that belongs there.\n\n"+
			"Do not invent a component name or an env-var name, and do not remove the dependency from the design to make this pass.",
		component, path, list, component, path)

	number, _, err := e.p.Writer.Mint(ctx, run.OrgID, run.ProjectID, delivery.IssueSpec{
		Title:     fmt.Sprintf("Wire the declared sibling endpoints for %s", component),
		Body:      body,
		Labels:    []string{delivery.LabelAgentWork, delivery.KindBug, delivery.SrcBuild},
		Milestone: run.MilestoneNumber,
		DedupeKey: delivery.DedupeKeyUnwiredEndpoints(component, missing),
	})
	return number, err
}
