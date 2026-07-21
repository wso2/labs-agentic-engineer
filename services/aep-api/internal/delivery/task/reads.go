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

package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The task read DTOs (Lineage, ExecutionView, TaskView, TaskDetail) live in the
// delivery ROOT (delivery.read_views.go) so the build sub-package can name
// TaskView through its TaskReader port without importing taskflow. This service
// produces them; both it and build import only the root (§10.3.1).

// Reads is the live read path (§8): no cache, no read model.
type Reads struct {
	issues   IssueClient
	repos    RepoResolver
	execs    ExecutionReader
	versions VersionReader // optional (stale-design attention)
	design   DesignReader  // optional (dependency-gated on_hold reconciliation)
	pricer   *modelcost.Pricer
}

// NewReads wires the read path. versions may be nil (then the stale-design
// attention flag is not computed). design may be nil (then the dependency-gated
// on_hold reconciliation degrades to component-dep gating only — provision /
// org-service dep resolution is skipped). pricer derives each task's costUsd
// from its captured tokens at read time (#245, ADR-0011); nil skips the usage
// attach entirely.
func NewReads(issues IssueClient, repos RepoResolver, execs ExecutionReader, versions VersionReader, design DesignReader, pricer *modelcost.Pricer) *Reads {
	return &Reads{issues: issues, repos: repos, execs: execs, versions: versions, design: design, pricer: pricer}
}

// taskUsage fetches the repo's per-issue usage rollup; a load failure (or a
// nil pricer) degrades to no usage captions, mirroring the executions load.
func (r *Reads) taskUsage(ctx context.Context, orgID, repoFullName string) map[int]contracts.TokenUsage {
	if r.pricer == nil {
		return nil
	}
	usage, err := r.execs.SumUsageByIssue(ctx, orgID, repoFullName)
	if err != nil {
		slog.WarnContext(ctx, "reads: load task usage failed", "repo", repoFullName, "error", err)
		return nil
	}
	return usage
}

// attachUsage stamps a task's aggregate usage onto its view (#245); zero
// aggregates stay absent so pre-capture tasks render no caption.
func (r *Reads) attachUsage(view *delivery.TaskView, usage map[int]contracts.TokenUsage) {
	u, ok := usage[view.IssueNumber]
	if !ok || u.IsZero() {
		return
	}
	view.Usage = delivery.NewUsageView(u, r.pricer.Cost(u))
}

// List returns the project's implementation Tasks filtered by state ("open" |
// "closed" | "all"; default "open"). FE groups by derivedStatus client-side
// (§8). The aep:validation Task is excluded (see ListByTag).
func (r *Reads) List(ctx context.Context, orgID, projectID, state string) ([]delivery.TaskView, error) {
	return r.ListByTag(ctx, orgID, projectID, state, "")
}

// ListByTag is List additionally scoped to a single spec/build version tag: it
// returns only Tasks whose machine block carries that specTag. The filter runs
// on the parsed block, NOT the aep:spec/<tag> label — the block is the durable
// truth and the label only its flat mirror, absent on Tasks planned before the
// label existed (#182), which a label-scoped GitHub query would silently drop.
// Same single marker-scoped fetch either way. An empty tag returns every Task
// (== List). This is the read behind GET /tasks?tag=v3 and the build's
// per-version task list.
//
// List reads return implementation Tasks ONLY: the project's aep:validation
// Task is a phase of the dev run, not an implementation task — its state is
// surfaced via /status deploy.validation (+ validationUrl), and it stays
// reachable via Get by issue number. Excluding it here (the read-model
// boundary) keeps every list consumer consistent: the console tasks page, the
// build's per-version task list (whose tally already excludes it), and the
// devflow's planned-task graph.
func (r *Reads) ListByTag(ctx context.Context, orgID, projectID, state, tag string) ([]delivery.TaskView, error) {
	repo, owner, name, err := resolveProjectRepo(ctx, r.repos, orgID, projectID)
	if err != nil {
		return nil, err
	}
	repoFullName := owner + "/" + name
	repoBase := strings.TrimSuffix(repo.RepoURL, ".git")
	issues, err := r.issues.ListIssues(ctx, orgID, projectID, []string{taskmeta.LabelMarker})
	if err != nil {
		return nil, err
	}
	latestSpecTag := r.latestSpecTag(ctx, orgID, projectID)

	// One batch query for the whole repo's latest-per-kind rows (not one per
	// issue); a load failure degrades to empty executions, as before.
	execsByIssue, err := r.execs.LatestPerKindForRepoScoped(ctx, orgID, repoFullName)
	if err != nil {
		slog.WarnContext(ctx, "reads: load executions failed", "repo", repoFullName, "error", err)
		execsByIssue = map[int]map[string]*delivery.Execution{}
	}
	usageByIssue := r.taskUsage(ctx, orgID, repoFullName)

	out := make([]delivery.TaskView, 0, len(issues))
	for _, issue := range issues {
		if !matchesState(issue.State, state) {
			continue
		}
		view, ok := buildView(issue, latestSpecTag, repoBase, execsByIssue[issue.Number])
		if !ok {
			continue
		}
		// Phase, not implementation task (see the doc comment above).
		if view.ExecutorClass == string(taskmeta.ClassValidation) {
			continue
		}
		if tag != "" && view.Lineage.SpecTag != tag {
			continue
		}
		r.attachUsage(&view, usageByIssue)
		out = append(out, view)
	}
	// Second pass: reconcile dependency-gated coding Tasks to on_hold + BlockedBy.
	// This needs every sibling's derived status, so it runs after the per-issue
	// loop over the whole set (issue #164 follow-up).
	r.reconcileBlocked(ctx, orgID, projectID, out)
	return out, nil
}

// reconcileBlocked is the read path's dependency-gated on_hold pass (issue #164
// follow-up). A coding Task waiting on unresolved dependencies today derives a
// misleading status — in_progress (a queued-not-running execution → board
// "Ongoing") or pending (no execution → "Pending") — and records nothing about
// WHY. Gating lives at two write-side layers (the funnel's depsGate for
// provisioning deps, the Temporal task graph for sibling-component deps), neither
// of which annotates the Task, so the read path — which already derived every
// Task's status — is the reliable place to compute "is this waiting, and on
// what". It mirrors funnel.depsGate/depDeployed, but resolves "deployed" from the
// already-derived sibling views instead of re-loading each dep's executions.
//
// It overrides ONLY a not-started coding Task (absent or queued coding
// execution): a genuinely running Task keeps in_progress. A met dep is one whose
// resolving view derives deployed; an unmet dep is one whose resolver is missing
// or not deployed. Provision + org-service deps gate CONDITIONALLY — only when a
// consumer-side aep:provision gate exists for the dep — so a resolved/ready dep
// with no gate never becomes a phantom forever-hold (the same conditional rule
// the funnel applies to org-service deps).
func (r *Reads) reconcileBlocked(ctx context.Context, orgID, projectID string, views []delivery.TaskView) {
	// Resolution maps over the derived views (mirror the funnel's projectView):
	// coding/ops Tasks index by component; aep:provision gates index by dependency
	// name (a provision gate's Component field IS the dep name). Highest issue
	// number wins, matching the funnel's latest-per-component rule.
	latestByComponent := map[string]delivery.TaskView{}
	provisionByDep := map[string]delivery.TaskView{}
	for _, v := range views {
		key := strings.ToLower(v.Component)
		if key == "" {
			continue
		}
		if v.ExecutorClass == string(taskmeta.ClassProvision) {
			if cur, ok := provisionByDep[key]; !ok || v.IssueNumber > cur.IssueNumber {
				provisionByDep[key] = v
			}
			continue
		}
		if cur, ok := latestByComponent[key]; !ok || v.IssueNumber > cur.IssueNumber {
			latestByComponent[key] = v
		}
	}

	// Per-component provision + org-service deps from the design at HEAD. A nil
	// design reader or a read error degrades to component-dep gating only (the
	// block's dependsOn still resolves), never blocks the list.
	var provisionDeps, orgServiceDeps map[string][]string
	if r.design != nil {
		if deps, err := r.design.ProvisionDepNames(ctx, orgID, projectID); err != nil {
			slog.WarnContext(ctx, "reads: read provision deps failed", "project", projectID, "error", err)
		} else {
			provisionDeps = deps
		}
		if deps, err := r.design.OrgServiceDepNames(ctx, orgID, projectID); err != nil {
			slog.WarnContext(ctx, "reads: read org-service deps failed", "project", projectID, "error", err)
		} else {
			orgServiceDeps = deps
		}
	}

	// depMet mirrors funnel.depDeployed: a dep resolves via its component Task,
	// falling back to its aep:provision gate; it is met iff that view derives
	// deployed. An unresolvable name is not met.
	depMet := func(dep string) bool {
		key := strings.ToLower(dep)
		v, ok := latestByComponent[key]
		if !ok {
			v, ok = provisionByDep[key]
		}
		if !ok {
			return false
		}
		return v.DerivedStatus == string(taskmeta.StatusDeployed)
	}

	for i := range views {
		v := &views[i]
		if v.ExecutorClass != string(taskmeta.ClassCoding) {
			continue
		}
		if !nonTerminalForHold(v.DerivedStatus) || !codingNotStarted(v) {
			continue
		}
		compKey := strings.ToLower(v.Component)
		seen := map[string]bool{}
		var unmet []string
		check := func(dep string) {
			key := strings.ToLower(dep)
			if key == "" || seen[key] {
				return
			}
			seen[key] = true
			if !depMet(dep) {
				unmet = append(unmet, dep)
			}
		}
		// Sibling-component deps (the block's dependsOn) gate unconditionally.
		for _, dep := range v.DependsOn {
			check(dep)
		}
		// Provision + org-service deps gate ONLY when an aep:provision gate exists
		// for the dep (indexed in provisionByDep): a resolved/ready dep with no gate
		// is not blocking.
		for _, dep := range provisionDeps[compKey] {
			if _, gated := provisionByDep[strings.ToLower(dep)]; gated {
				check(dep)
			}
		}
		for _, dep := range orgServiceDeps[compKey] {
			if _, gated := provisionByDep[strings.ToLower(dep)]; gated {
				check(dep)
			}
		}
		if len(unmet) > 0 {
			v.DerivedStatus = string(taskmeta.StatusOnHold)
			v.BlockedBy = unmet
		}
	}
}

// nonTerminalForHold reports whether a derived status is one the dependency
// reconciler may override to on_hold: a Task still waiting to run (pending /
// in_progress / on_hold). Every terminal or past-dispatch status (deployed,
// building, merged, ready_for_review, rejected, abandoned, failed) is left
// untouched.
func nonTerminalForHold(status string) bool {
	switch taskmeta.DerivedStatus(status) {
	case taskmeta.StatusPending, taskmeta.StatusInProgress, taskmeta.StatusOnHold:
		return true
	}
	return false
}

// codingNotStarted reports whether a coding Task has NOT begun running: its
// latest coding execution is absent or still queued (a dependency-gated Task
// queues behind the funnel's gate). A running coding execution means the Task was
// dispatched — genuinely in_progress — and must not be overridden to on_hold. The
// view's Executions map carries the latest status per kind.
func codingNotStarted(v *delivery.TaskView) bool {
	e, ok := v.Executions[string(taskmeta.KindCoding)]
	if !ok {
		return true
	}
	return e.Status == string(taskmeta.ExecQueued)
}

// Get returns one Task with its full Execution history. It derives the target's
// status through the SAME whole-project reconcile the list uses (buildView over
// every sibling → reconcileBlocked), not a lone buildView — otherwise a
// dependency-gated coding Task shows the misleading raw status (pending /
// in_progress) on its detail page while the list correctly shows on_hold (issue
// #164 follow-up). The gating overlay needs every sibling's derived status and
// the project's provision gates, so the whole set is built before picking one.
func (r *Reads) Get(ctx context.Context, orgID, projectID string, issueNumber int) (*delivery.TaskDetail, error) {
	repo, owner, name, err := resolveProjectRepo(ctx, r.repos, orgID, projectID)
	if err != nil {
		return nil, err
	}
	repoFullName := owner + "/" + name
	repoBase := strings.TrimSuffix(repo.RepoURL, ".git")
	issues, err := r.issues.ListIssues(ctx, orgID, projectID, []string{taskmeta.LabelMarker})
	if err != nil {
		return nil, err
	}
	if !containsIssue(issues, issueNumber) {
		return nil, ErrTaskNotFound
	}
	latestSpecTag := r.latestSpecTag(ctx, orgID, projectID)

	// One batch query for the whole repo's latest-per-kind rows (mirrors
	// ListByTag); a load failure degrades to empty executions.
	execsByIssue, err := r.execs.LatestPerKindForRepoScoped(ctx, orgID, repoFullName)
	if err != nil {
		slog.WarnContext(ctx, "reads: load executions failed", "repo", repoFullName, "error", err)
		execsByIssue = map[int]map[string]*delivery.Execution{}
	}

	views := make([]delivery.TaskView, 0, len(issues))
	for _, issue := range issues {
		if view, ok := buildView(issue, latestSpecTag, repoBase, execsByIssue[issue.Number]); ok {
			views = append(views, view)
		}
	}
	// Same on_hold + BlockedBy reconcile as the list — the detail page must not
	// disagree with the list on a gated Task's status.
	r.reconcileBlocked(ctx, orgID, projectID, views)

	var view *delivery.TaskView
	for i := range views {
		if views[i].IssueNumber == issueNumber {
			view = &views[i]
			break
		}
	}
	if view == nil {
		return nil, ErrTaskNotFound
	}
	r.attachUsage(view, r.taskUsage(ctx, orgID, repoFullName))

	history, err := r.execs.ListByIssueScoped(ctx, orgID, repoFullName, issueNumber)
	if err != nil {
		return nil, err
	}
	hv := make([]delivery.ExecutionView, 0, len(history))
	for i := range history {
		hv = append(hv, executionView(&history[i]))
	}
	return &delivery.TaskDetail{TaskView: *view, ExecutionHistory: hv}, nil
}

// containsIssue reports whether the task-marker issue set includes issueNumber.
func containsIssue(issues []sourcecontrol.IssueInfo, issueNumber int) bool {
	for i := range issues {
		if issues[i].Number == issueNumber {
			return true
		}
	}
	return false
}

// buildView fuses one live issue with its latest-per-kind executions into a
// TaskView. ok is false when the issue is not a Task (no marker) — the caller
// skips it. repoBase is the repo's HTML URL (clone URL sans ".git"), the base
// the PR link is built from.
func buildView(issue sourcecontrol.IssueInfo, latestSpecTag, repoBase string, execs map[string]*delivery.Execution) (delivery.TaskView, bool) {
	labels := taskmeta.ParseLabels(issue.Labels)
	if !labels.IsTask {
		return delivery.TaskView{}, false
	}
	block, human, blockErr := taskmeta.ParseBody(issue.Body)

	execFacts := delivery.ExecutionFacts(execs)
	facts := taskmeta.GitHubFacts{
		IssueOpen:   strings.EqualFold(issue.State, "open"),
		HoldPresent: labels.Hold,
		PR:          taskmeta.PRStateFromFacts(execFacts),
	}
	derived := taskmeta.Derive(facts, execFacts)

	// Validation Tasks run coding→PR→merge with NO post-merge build (the devflow's
	// validating phase: dispatch → PR → merge, no build/deploy). Derive infers a
	// merge only from a following build Execution, so it never sees a validation
	// merge and reads the closed, merged issue as abandoned → the console renders
	// "Failed" for a validation that actually completed. Surface a completed
	// validation (issue closed with a succeeded coding run) as deployed → "Done".
	// A genuinely failed validation run (coding failed) is untouched, staying Failed.
	if labels.Class == taskmeta.ClassValidation && derived == taskmeta.StatusAbandoned {
		if c := execs[string(taskmeta.KindCoding)]; c != nil && c.Status == string(taskmeta.ExecSucceeded) {
			derived = taskmeta.StatusDeployed
		}
	}

	view := delivery.TaskView{
		IssueNumber:   issue.Number,
		Title:         issue.Title,
		IssueURL:      issue.URL,
		ExecutorClass: string(labels.Class),
		Origin:        string(block.Origin),
		Component:     block.Component,
		Operation:     block.Operation,
		DependsOn:     nonNil(block.DependsOn),
		Rationale:     human.Rationale,
		Body:          human.Body,
		Lineage:       delivery.Lineage{SpecTag: block.SpecTag, DesignTag: block.DesignTag},
		DerivedStatus: string(derived),
		Hold:          labels.Hold,
		Attention:     computeAttention(labels, block, blockErr, latestSpecTag),
		Executions:    latestViews(execs),
	}
	// The PR link rides the succeeded coding row's "pr#N" reason (coding success
	// == PR opened) — a running/failed row's reason is never trusted, mirroring
	// the /status validationUrl recovery.
	if c := execs[string(taskmeta.KindCoding)]; c != nil && c.Status == string(taskmeta.ExecSucceeded) {
		if n := taskmeta.OpenPRNumber(c.Reason); n > 0 && repoBase != "" {
			view.PRURL = fmt.Sprintf("%s/pull/%d", repoBase, n)
		}
	}
	return view, true
}

// computeAttention derives the standing attention flags for a Task: a mangled
// machine block, an ambiguous executor class, a stale lineage vs the current
// spec version, or a platform-set aep:attention with no more-specific reason.
func computeAttention(labels taskmeta.ParsedLabels, block taskmeta.Block, blockErr error, latestSpecTag string) []string {
	// Never nil: TaskView.attention is contractually "[] when clean" (the console
	// maps over it directly). A nil slice would marshal as JSON null and crash a
	// consumer that trusts the contract — which is exactly what the task-log
	// stream's `task` frame does.
	flags := []string{}
	if blockErr != nil && !errors.Is(blockErr, taskmeta.ErrNoBlock) {
		flags = append(flags, "mangled-block")
	}
	if labels.ClassAmbiguous {
		flags = append(flags, "ambiguous-class")
	}
	if latestSpecTag != "" && block.DesignTag != "" && block.DesignTag != latestSpecTag {
		flags = append(flags, "stale-design")
	}
	if labels.Attention && len(flags) == 0 {
		flags = append(flags, "flagged")
	}
	return flags
}

func latestViews(execs map[string]*delivery.Execution) map[string]delivery.ExecutionView {
	out := make(map[string]delivery.ExecutionView, len(execs))
	for kind, e := range execs {
		if e == nil {
			continue
		}
		out[kind] = executionView(e)
	}
	return out
}

func executionView(e *delivery.Execution) delivery.ExecutionView {
	return delivery.ExecutionView{
		ID:        e.ID,
		Kind:      e.Kind,
		Status:    e.Status,
		RunName:   e.RunName,
		Reason:    e.Reason,
		CreatedAt: e.CreatedAt,
		StartedAt: e.StartedAt,
		EndedAt:   e.EndedAt,
	}
}

// latestSpecTag returns the newest spec version tag, or "" when unavailable.
// Best-effort — used only for the stale-design attention flag, so it reads
// the local mirror WITHOUT a fetch (VersionReader.LatestSpecTag) rather than
// forcing a live ListRequirementsVersions round-trip on every task-list call
// (docs/design/gitfs-fetch-on-read-followup.md §2).
func (r *Reads) latestSpecTag(ctx context.Context, orgID, projectID string) string {
	if r.versions == nil {
		return ""
	}
	return r.versions.LatestSpecTag(ctx, orgID, projectID)
}

func matchesState(issueState, filter string) bool {
	open := strings.EqualFold(issueState, "open")
	switch strings.ToLower(filter) {
	case "", "open":
		return open
	case "closed":
		return !open
	case "all":
		return true
	default:
		return open
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
