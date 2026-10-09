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
	"iter"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/reqspec"
	"github.com/wso2/aep/aep-api/internal/platform/taskplan"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// planDrainIdleTimeout aborts a Plan turn's stream when no event arrives for
// this long. The pod sends a keep-alive every 15 s while the turn runs, so a
// longer silence means a hung pod, and the planning activity would otherwise
// hold its 30-minute timeout for a turn that is not coming back. Keep-alives
// reset it like any other event: a turn reading files for minutes between
// two Tasks is alive.
const planDrainIdleTimeout = 90 * time.Second

// errPlanTurnSilent is a Plan turn whose stream sent nothing, not even a
// keep-alive, for planDrainIdleTimeout. Retryable: the next attempt reattaches
// or starts afresh.
var errPlanTurnSilent = errors.New("plan: the turn sent nothing for the idle deadline")

// planTap reads a Plan turn's event stream from the org's AE Studio pod and
// performs the GitHub writes for planTask/updateTask as they pass. The
// pod projects the agent's successful task tool results to `task-op` events
// the tap decodes each with the same taskplan decoders the raw
// tool results used.
//
// Every issue it mints is PROSE in a MILESTONE: the milestone number rides the
// create call (so a plan costs 1+N calls, never create-then-patch), the `aep`
// label marks it as the run's working set, and nothing in the body is ever
// parsed back. Re-plan reconcile is therefore ADDITIVE-ONLY, deduped on the
// title slug against the milestone's existing issues plus this run's creations —
// which is also what makes a crash re-run land no duplicates.
type planTap struct {
	// ctx carries the writes. Detached from the caller's cancellation, so a
	// write in flight when the activity is cancelled still lands; it keeps
	// the values, among them the activity's progress beat.
	ctx       context.Context
	orgID     string
	projectID string
	issues    IssueClient
	// writer mints the Task issues. The planner's own updateTask edits (title,
	// body, the write-failure comment) stay on `issues`: those are the agent's
	// tool surface acting on an issue that already exists, not platform mints,
	// and they carry none of the label or dedupe policy the writer owns.
	writer *delivery.IssueWriter

	// milestone is the version's milestone NUMBER, assigned at issue creation.
	// Zero leaves creations unassigned (no milestone was minted for this turn).
	milestone int
	// appPaths maps a design component name (lowercased) to its source directory,
	// componentStories maps a component id (lowercased) to its IN-SCOPE story
	// citations (#369) — the source of the platform-stamped Serves-stories
	// block. Nil on a scope-less legacy plan.
	componentStories map[string][]string
	// rendered into the body as the App Path the agent works in. Empty when no
	// design reader is wired.
	appPaths map[string]string

	// state carries BOTH pre-existing Tasks (preloaded by the plan assembler,
	// addressable by updateTask{issueNumber}) and this-run creations.
	state map[int]plannedTask

	// contextNumbers is the frozen set of issue numbers preloaded into the turn's
	// context. An updateTask{issueNumber} ref MUST target a member: the agent only
	// ever sees the numbers the assembler pushed, so an out-of-context number is a
	// hallucination (or a human bug report that happens to share the id space) and
	// must NEVER be written — doing so would clobber an unrelated issue's body.
	contextNumbers map[int]bool

	titleToNumber map[string]int // normalized this-run title → created issue number
	// existingSlugs is the titleSlug of every issue already in the milestone;
	// createdSlugs the ones minted this run. Together they are the whole dedupe
	// primitive — there is no machine-block key to compare any more.
	existingSlugs map[string]bool
	createdSlugs  map[string]bool
	// componentToNumber resolves a "Depends on" component name to the issue this
	// run planned for it, so dependency lines carry real issue numbers. With
	// per-feature Tasks it holds the component's latest, and only says the
	// component has Tasks this run; taskFor is the lookup.
	componentToNumber map[string]int
	// taskFor resolves a component's Task for one feature (or its foundation)
	// to the issue this run planned for it (B3).
	taskFor map[taskKey]int

	// features are the version's carried features by ID, and productWide its
	// carried product-wide items: what a Task's feature waits on, and which
	// product-wide requirements its body names. Empty on a scope-less plan.
	features    map[string]spec.ScopeFeature
	productWide []spec.ScopeItem

	// failures counts the GitHub writes the tap could not land.
	failures int
}

// taskKey names one component's Task for one feature.
type taskKey struct{ component, feature string }

func keyOf(component, feature string) taskKey {
	return taskKey{component: strings.ToLower(strings.TrimSpace(component)), feature: feature}
}

// withScope gives the tap the version's features and product-wide items.
func (t *planTap) withScope(scope spec.BuildScope) {
	t.componentStories = scope.ComponentStories
	t.features = map[string]spec.ScopeFeature{}
	for _, f := range scope.Features {
		t.features[f.ID] = f
	}
	t.productWide = scope.ProductWide
}

// storiesFor resolves a Task's in-scope story citations for the stamp: the
// component's stories of the Task's feature, none for a foundation Task, and
// all of them when the planner named no feature. nil when the scope carries
// none for it.
func (t *planTap) storiesFor(component, feature string) []string {
	all := t.componentStories[strings.ToLower(strings.TrimSpace(component))]
	switch feature {
	case "":
		return all
	case foundation:
		return nil
	}
	var out []string
	for _, id := range all {
		if strings.HasPrefix(id, feature+".") {
			out = append(out, id)
		}
	}
	return out
}

// briefFor is what a Task's body states from the version's scope: its
// feature's name, and the product-wide requirements it honours — every
// carried one for a foundation Task, those that apply to the feature for a
// feature's Task.
func (t *planTap) briefFor(feature string) taskBrief {
	brief := taskBrief{FeatureName: t.features[feature].Name}
	for _, it := range t.productWide {
		item := reqspec.Item{ID: it.ID, AppliesTo: it.AppliesTo}
		if feature == foundation || (feature != "" && item.Reaches(feature)) {
			brief.ProductWide = append(brief.ProductWide, it.ID)
		}
	}
	return brief
}

// linksFor resolves a Task's dependencies to issues, in the order a reader
// takes them: its component's foundation, its component's Tasks for the
// features its own waits on, then each component it depends on — that
// component's Task for the same feature, else its foundation. A dependency
// on a component that has Tasks this run but none for the feature is left
// out: the work it relies on belongs to another feature, which the needs
// already order. One with no Tasks yet is named, for a forward reference.
func (t *planTap) linksFor(p plannedTask) []taskLink {
	var out []taskLink
	seen := map[int]bool{}
	add := func(n int) {
		if n > 0 && !seen[n] {
			seen[n] = true
			out = append(out, taskLink{Number: n})
		}
	}
	if p.Feature != "" && p.Feature != foundation {
		add(t.taskFor[keyOf(p.Component, foundation)])
		for _, need := range t.features[p.Feature].Needs {
			add(t.taskFor[keyOf(p.Component, need)])
		}
	}
	for _, dep := range p.DependsOn {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if p.Feature == "" {
			if n, ok := t.issueForComponent(dep); ok && n > 0 {
				add(n)
			} else {
				out = append(out, taskLink{Name: "`" + dep + "`"})
			}
			continue
		}
		if n := t.taskFor[keyOf(dep, p.Feature)]; n > 0 {
			add(n)
		} else if n := t.taskFor[keyOf(dep, foundation)]; n > 0 {
			add(n)
		} else if _, planned := t.issueForComponent(dep); !planned {
			out = append(out, taskLink{Name: "`" + dep + "`"})
		}
	}
	return out
}

// newPlanTap builds a tap with every map initialised. Callers set the milestone,
// the preloaded state and the app paths.
func newPlanTap(ctx context.Context, orgID, projectID string, issues IssueClient, writer *delivery.IssueWriter) *planTap {
	return &planTap{
		ctx:               ctx,
		orgID:             orgID,
		projectID:         projectID,
		issues:            issues,
		writer:            writer,
		state:             map[int]plannedTask{},
		contextNumbers:    map[int]bool{},
		titleToNumber:     map[string]int{},
		existingSlugs:     map[string]bool{},
		createdSlugs:      map[string]bool{},
		componentToNumber: map[string]int{},
		taskFor:           map[taskKey]int{},
	}
}

// Stream reads the turn's events to the end, performing each task-op's
// GitHub write as it passes, and reports how the turn ended: nil for a
// completed turn, an aestudiotools.TurnFailedError carrying the pod's code
// (and a provider_limit's reset time) for a failed one (the planning
// activity's retry policy reads it), the stream's own error when it broke
// off, or errPlanTurnSilent when the idle watchdog fired.
//
// Every event, keep-alives included, resets the watchdog and reports progress
// on t.ctx, which the planning activity turns into a heartbeat.
// On expiry the watchdog calls abort, which must end the stream (cancel the
// context the stream was started under, closing its body). GitHub write
// failures are not Stream's answer; they are counted in t.failures.
func (t *planTap) Stream(events iter.Seq2[aestudiotools.TurnEvent, error], abort func()) error {
	var silent atomic.Bool
	alive := make(chan struct{}, 1)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		timer := time.NewTimer(planDrainIdleTimeout)
		defer timer.Stop()
		for {
			select {
			case <-stop:
				return
			case <-alive:
				timer.Reset(planDrainIdleTimeout)
			case <-timer.C:
				silent.Store(true)
				abort()
				return
			}
		}
	}()

	for ev, err := range events {
		if silent.Load() {
			return errPlanTurnSilent
		}
		if err != nil {
			return err
		}
		select {
		case alive <- struct{}{}:
		default:
		}
		delivery.ReportProgress(t.ctx)
		switch ev.Type {
		case aestudiotools.EventTaskOp:
			t.consume(ev)
		case aestudiotools.EventResult:
			if ev.Status != "completed" {
				return fmt.Errorf("plan: %w", &aestudiotools.TurnFailedError{Code: ev.Code, ResetAt: ev.ResetAt})
			}
			return nil
		}
	}
	if silent.Load() {
		return errPlanTurnSilent
	}
	return errors.New("plan: the turn stream ended without a result")
}

// consume performs the GitHub write a task-op describes. The pod sends only
// the agent's successful planTask/updateTask results; the decoders still
// refuse anything that is not one, so a malformed op mints nothing.
func (t *planTap) consume(ev aestudiotools.TurnEvent) {
	switch ev.Op {
	case "plan":
		if out, err := taskplan.DecodePlanTaskOk(ev.Output); err == nil {
			t.handlePlan(out)
		} else {
			slog.WarnContext(t.ctx, "plan tap: undecodable planTask op skipped", "error", err)
		}
	case "update":
		if out, err := taskplan.DecodeUpdateTaskOk(ev.Output); err == nil {
			t.handleUpdate(out)
		} else {
			slog.WarnContext(t.ctx, "plan tap: undecodable updateTask op skipped", "error", err)
		}
	}
}

// handlePlan mints one Task issue into the version's milestone for a planTask
// result. Dedupe is the titleSlug of the title against the milestone's existing
// issues and this run's creations — so a re-plan is additive-only and a crash
// re-run converges to no-ops.
func (t *planTap) handlePlan(out *taskplan.PlanTaskOk) {
	slug := titleSlug(out.Title)
	norm := normalizeTitle(out.Title)
	if slug != "" && (t.existingSlugs[slug] || t.createdSlugs[slug]) {
		return
	}
	if _, dup := t.titleToNumber[norm]; dup {
		return
	}
	planned := plannedTask{
		Component: out.Component,
		Feature:   out.Feature,
		AppPath:   t.appPathFor(out.Component),
		DependsOn: out.DependsOn,
		Rationale: out.Rationale,
	}
	// Milestone assignment RIDES the create — one call, which is what keeps the
	// plan's API cost at 1+N rather than 1+2N. No DedupeKey, deliberately: this
	// is the ONE mint in the domain that dedupes client-side, on the title slug
	// above, because a re-plan legitimately re-proposes a Task under a title the
	// agent may have reworded and the milestone's own membership is the set to
	// reconcile against.
	number, _, err := t.writer.Mint(t.ctx, t.orgID, t.projectID, delivery.IssueSpec{
		Title: out.Title,
		// The Serves-stories stamp is platform-authored from the design's
		// citations (#369) — the planner has zero discretion over it.
		Body: delivery.StampServesStories(composeTaskBody(planned, t.briefFor(planned.Feature), t.linksFor(planned)), t.storiesFor(out.Component, out.Feature)),
		// Armed, and PLANNED work: a Task is what the spec asked for. The kind is
		// what keeps a bug-fix run off it — that loop works the deployed version
		// and must never pick up planned work for a version still being built.
		// Gates and the validation task are minted elsewhere and are deliberately
		// not this population.
		Labels:    []string{delivery.LabelAgentWork, delivery.KindDevelopment},
		Milestone: t.milestone,
	})
	if err != nil || number == 0 {
		// No issue exists to flag — record for the terminal in-band surface.
		//
		// A zero number counts as a failure for the same reason an error does:
		// nothing was filed that a later `updateTask` could address. Recording it
		// would put issue 0 in the slug and component maps, which is worse than
		// recording nothing — the next Task naming the same component would then
		// render a "Depends on #0" line into an agent's prose.
		t.failures++
		slog.WarnContext(t.ctx, "plan tap: create issue filed nothing", "title", out.Title, "error", err)
		return
	}
	if slug != "" {
		t.createdSlugs[slug] = true
	}
	t.titleToNumber[norm] = number
	if c := strings.ToLower(strings.TrimSpace(out.Component)); c != "" {
		t.componentToNumber[c] = number
		if out.Feature != "" {
			t.taskFor[keyOf(c, out.Feature)] = number
		}
	}
	t.state[number] = planned
}

// appPathFor resolves a component's source directory from the design, or ""
// when the design pins none (the component builds from the repo root) or no
// design reader is wired.
func (t *planTap) appPathFor(component string) string {
	if len(t.appPaths) == 0 {
		return ""
	}
	return t.appPaths[strings.ToLower(strings.TrimSpace(component))]
}

// issueForComponent resolves a dependency's component name to the issue this
// run planned for it. A forward reference (the dependency's own Task has not
// been minted yet) misses, and the body names the component instead.
func (t *planTap) issueForComponent(component string) (int, bool) {
	n, ok := t.componentToNumber[strings.ToLower(strings.TrimSpace(component))]
	return n, ok
}

// handleUpdate patches an existing or this-run Task for an updateTask result.
func (t *planTap) handleUpdate(out *taskplan.UpdateTaskOk) {
	number, ok := t.resolveRef(out.Ref)
	if !ok {
		// An out-of-context {issueNumber} is a real hazard, not a benign miss: it
		// would clobber an unrelated issue. Skip the op with NO GitHub write and
		// record it in the write-failure accounting (same surface as other tap
		// failures). A {title} miss is a benign skip (unknown this-run title).
		if out.Ref.IssueNumber != nil {
			t.failures++
			slog.WarnContext(t.ctx, "plan tap: updateTask ref out of context — skipped (would clobber an unrelated issue)", "issue", *out.Ref.IssueNumber)
		}
		return
	}
	st := t.state[number] // zero value is a benign empty state for a pre-existing miss
	prior := delivery.ParseServesStories(st.Body)

	set := out.Set
	if set.Title != nil && strings.TrimSpace(*set.Title) != "" {
		if err := t.issues.EditIssueTitle(t.ctx, t.orgID, t.projectID, number, *set.Title); err != nil {
			t.recordFlag(number, err)
		}
		// Remap the this-run title index: the echoed ref.title is the canonical
		// pre-rename title (phase-1); future {title} refs use the new title.
		if out.Ref.Title != nil {
			delete(t.titleToNumber, normalizeTitle(*out.Ref.Title))
		}
		t.titleToNumber[normalizeTitle(*set.Title)] = number
		if s := titleSlug(*set.Title); s != "" {
			t.createdSlugs[s] = true
		}
	}
	if set.DependsOn != nil {
		st.DependsOn = *set.DependsOn
	}
	if set.Rationale != nil {
		st.Rationale = *set.Rationale
	}
	if set.Body != nil {
		st.Body = *set.Body
	}
	// The whole body is re-rendered from the current facts, so a patch never
	// accumulates: the dependency lines are re-resolved too, which is how a
	// forward reference planned before its dependency picks up the real issue
	// number once updateTask touches it. The Serves-stories stamp is restamped
	// from the design's citations, falling back to the stamp the body carried
	// (a pre-existing task whose component the scope no longer names must not
	// lose its lineage).
	stories := t.storiesFor(st.Component, st.Feature)
	if len(stories) == 0 {
		stories = prior
	}
	body := delivery.StampServesStories(composeTaskBody(st, t.briefFor(st.Feature), t.linksFor(st)), stories)
	if err := t.issues.EditIssueBody(t.ctx, t.orgID, t.projectID, number, body); err != nil {
		t.recordFlag(number, err)
	}
	t.state[number] = st
}

// resolveRef resolves an updateTask ref to an issue number: {issueNumber} for a
// pre-existing Task, {title} for one planned earlier this run (§9.3). The agent
// never sees issue numbers it did not receive, so an {issueNumber} that is not a
// member of the preloaded context set is rejected (ok=false) — the tenant-scoped
// fence against clobbering an unrelated issue — and a {title} miss is skipped.
func (t *planTap) resolveRef(ref taskplan.TaskRef) (int, bool) {
	if ref.IssueNumber != nil {
		n := *ref.IssueNumber
		return n, t.contextNumbers[n]
	}
	if ref.Title != nil {
		n, ok := t.titleToNumber[normalizeTitle(*ref.Title)]
		return n, ok
	}
	return 0, false
}

// recordFlag records a mid-stream write failure and says so on the affected
// issue, so a human reading the milestone can see that this Task's brief is
// incomplete. The count also reaches the caller: a plan whose writes did not
// all land settles the run it was filling rather than supervising a short
// milestone.
func (t *planTap) recordFlag(number int, err error) {
	t.failures++
	slog.WarnContext(t.ctx, "plan tap: update issue failed", "issue", number, "error", err)
	if cerr := t.issues.CommentIssue(t.ctx, t.orgID, t.projectID, number,
		"⚠️ A plan update to this Task failed to apply. Re-run the build's planning pass, or edit it by hand."); cerr != nil {
		slog.WarnContext(t.ctx, "plan tap: write-failure comment failed", "issue", number, "error", cerr)
	}
}

// normalizeTitle matches the agents-service title normalization (trim + lower)
// so this side's title→issue map keys identically to the echoed refs (§9.3).
func normalizeTitle(title string) string {
	return strings.ToLower(strings.TrimSpace(title))
}
