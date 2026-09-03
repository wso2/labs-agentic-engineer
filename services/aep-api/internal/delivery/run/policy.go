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

package run

import (
	"sort"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// The loop's DECISIONS, as pure functions over facts. They are separated from
// the workflow that gathers those facts so each can be read and tested without
// a workflow environment — and so the arithmetic behind a terminal reason is
// one short function rather than a branch buried in a select.

// MilestoneSnapshot is one cycle-boundary poll of the milestone: every
// population the boundary's predicates are computed over, in ONE round trip.
//
// It carries BOTH working sets rather than "the working set", because two
// species of run poll the same milestone through the same activity and they work
// different populations — a dev run works planned work too, a task run must
// never touch it. Answering both here is free: the host returns every count in
// one GraphQL call, so the alternative would be a second activity or a second
// round trip on the loop's hottest read, and a snapshot that answered only the
// caller's own set would have to be told which caller it was.
//
// A working set is deliberately narrower than "some issue is open": a milestone
// holding only ledger issues has nothing to work, and its gates and validation
// task are worked by something other than a coding cycle.
type MilestoneSnapshot struct {
	// DevWork is a DEV run's working set — armed issues of kind development, bug
	// or conflict (delivery.InDevWorkingSet, counted as OpenDevWork).
	//
	// The wire name stays `work`, and that is deliberate rather than lazy. An
	// activity RESULT lives in Temporal history, so a run in flight across an
	// upgrade decodes the snapshot its old worker recorded. The field this
	// replaced held exactly this population — armed, minus gates, minus the
	// validation task — so keeping the name means old history decodes into the
	// right field. Renaming it would have decoded to ZERO, and a boundary that
	// reads an empty working set settles the version DELIVERED without building
	// it. Same rule as RunInput's zero-value fields: a value a replay cannot
	// find must mean the pre-existing behaviour.
	DevWork int `json:"work"`
	// TaskWork is a TASK run's working set — the same minus planned work
	// (delivery.InTaskWorkingSet, counted as OpenTaskWork).
	//
	// New, so it decodes to zero from history a previous worker wrote. That is
	// survivable only because the workflow TYPE and the id grammar changed with
	// the split (ADR-0020): no pre-split execution can be continued at all, so
	// there is no run in flight for which this field could read zero and matter.
	// Drain, not migrate — which is a property of that release, not of this
	// struct, and worth re-checking before the next field lands here.
	TaskWork int `json:"taskWork"`
	Gates    int `json:"gates"`
	Total    int `json:"total"`
	// ValidationRepairs is how many open issues carry `src/validation`: the
	// repair work a failed verdict filed. It is not a working set and nothing is
	// dispatched off it — it answers the ONE question the task run's bookend
	// asks, whether the defects in this milestone came from a verdict, and
	// therefore whether draining the working set should reopen the version's
	// validation task.
	ValidationRepairs int `json:"validationRepairs"`
}

// Dispatchable is the dispatch predicate, and it guards EVERY cycle boundary:
// no gate is open in the milestone, and its working set is non-empty.
//
// A hand-filed mid-run gate is therefore a deliberate human brake — it holds
// the next dispatch, and only the next dispatch. A stray gate never blocks
// settle, because settle is reached through the empty-working-set branch that
// runs before this one: gates hold dispatch, and with nothing to dispatch they
// hold nothing.
//
// The rule is delivery.MilestoneWork.Dispatchable, and this is the supervisor's
// adapter onto it. The event plane asks the same question of the same milestone
// (eventcore.dispatchable) to decide whether a webhook is worth waking a waiting
// run for, and it may not import this package — so the rule lives at the domain
// root both can reach. A run woken by a predicate its own boundary then rejects
// is a wasted cycle; the reverse is a version nobody wakes.
func Dispatchable(s MilestoneSnapshot, work int) bool {
	return delivery.MilestoneWork{Gates: s.Gates, Work: work}.Dispatchable()
}

// The two working-set selectors a run's bookends hand the boundary. They are
// the COUNT half of the rule whose per-issue half is delivery.InDevWorkingSet /
// InTaskWorkingSet, and the two halves are checked against each other by
// delivery.TestWorkingSetsAgreeWithTheHostCounts.
//
// Written as named functions rather than as a `kind` switch inside the loop
// because the loop must not be able to guess: the working set is what settles a
// version and what a dispatch is spent on, so the workflow that owns those
// consequences states it (see bookends).
func devWorkingSet(s MilestoneSnapshot) int { return s.DevWork }

func taskWorkingSet(s MilestoneSnapshot) int { return s.TaskWork }

// nextCycleKind picks what the next cycle is for, from what the previous one
// produced. Recovery cycles are ordinary cycles — the fix or conflict issue is
// in the working set like any other work — so the kind exists to name the
// budget the cycle spends, not to change what the agent does.
func nextCycleKind(previous cycleResult) string {
	switch previous {
	case cycleRed, cycleDeployFailed:
		return delivery.CycleKindFix
	case cycleConflict:
		return delivery.CycleKindConflict
	default:
		return delivery.CycleKindCoding
	}
}

// budgetRefusal reports the terminal reason that forbids starting a cycle of
// this kind, or "" when the run may proceed.
//
// The chain budget is checked BEFORE the ceiling so the reason names the
// immediate cause: a run that has already spent both fix cycles is failing
// because its fix chain ran out, even if it also happens to be at the ceiling.
// Every budget names exactly one failure class — that is the whole point of
// having four of them rather than one attempt counter.
func budgetRefusal(kind string, cyclesTotal, fixCycles, conflictCycles, ceiling int) string {
	switch kind {
	case delivery.CycleKindFix:
		if fixCycles >= delivery.RunMaxFixCycles {
			return delivery.RunReasonFixChainBudget
		}
	case delivery.CycleKindConflict:
		if conflictCycles >= delivery.RunMaxConflictCycles {
			return delivery.RunReasonConflictBudget
		}
	}
	if ceiling > 0 && cyclesTotal >= ceiling {
		return delivery.RunReasonCycleCeiling
	}
	return ""
}

// maxUnreportedDispatches bounds how many validation CYCLES one validation run
// opens after an agent merged its pull request without committing a report.
//
// It is a separate budget from the cycle's re-dispatch allowance
// (RunMaxRedispatchPerCycle) because it answers a different failure: that one is
// an agent that never landed a pull request at all, this one is an agent that
// landed one and broke the report half of the runner contract. The remedy is the
// same — dispatch again — but nothing else can supply it: no criterion asserted,
// so there is no repair issue to file, and nothing was deployed, so there is
// nothing outside the workflow to fix.
//
// Two, because an agent that ignored the contract twice will ignore it a third
// time, and every dispatch is a paid agent run.
const maxUnreportedDispatches = 2

// noProgress is the rule that stops a run looping forever on work it cannot
// finish: a GREEN cycle that closed no issue and minted no platform issue has
// left the milestone exactly as it found it, and the next cycle would be the
// same dispatch against the same working set.
//
// It is deliberately coarse — it compares working-set SIZES, not identities.
// An adoption that lands one issue in the same cycle that closes another reads
// as no progress, which is the safe direction: the human who adopted it gets a
// failed run and a milestone they can start again, rather than a loop.
//
// It applies only after a green cycle. A red or conflicted cycle mints work of
// its own, and its budget (fix chain, conflict chain) is what bounds it.
func noProgress(previous cycleResult, workBefore, workAfter int) bool {
	return previous == cycleGreen && workAfter >= workBefore
}

// redBuildAttempts is how many failed WorkflowRuns at one (component, commit)
// make the component's build a VERDICT rather than a flake: the original
// attempt plus the single automatic re-trigger the model allows.
//
// The supervisor does not enforce this budget — the event plane does, at the
// trigger site. This constant only lets the supervisor READ the same fact off
// the same runs, so the two can never disagree about when red means red.
const redBuildAttempts = 1 + delivery.RunMaxBuildRetriggersPerComponentSHA

// componentBuildState is one component's build verdict at one commit.
type componentBuildState int

const (
	// buildPending — no terminal run yet, or a first failure whose automatic
	// re-trigger has not reported. Waiting is correct: acting on the first red
	// would race the re-trigger the event plane is already making.
	buildPending componentBuildState = iota
	buildGreen
	buildRed
)

// classifyComponentBuild reads a component's verdict at one commit off the
// WorkflowRuns themselves.
//
// Green wins over red: a component whose re-trigger succeeded IS built, whatever
// the first attempt did. Red needs the full attempt allowance to be spent,
// which is what keeps the supervisor from calling a flake a failure.
func classifyComponentBuild(runs []BuildRunInfo, prefix string) componentBuildState {
	failed := 0
	for _, r := range runs {
		if !strings.HasPrefix(strings.ToLower(r.Name), prefix) || !r.Terminal {
			continue
		}
		if r.Succeeded {
			return buildGreen
		}
		failed++
	}
	if failed >= redBuildAttempts {
		return buildRed
	}
	return buildPending
}

// CycleBuildState is how far a cycle's build fan-out has got: how many
// components the merge touched, how many have reached a verdict, and which ones
// are red.
//
// Expected == 0 is a real and common answer, not a degenerate one: a validation
// cycle's pull request carries only tests and a report, so it rebuilds nothing
// and is green the moment it merges.
type CycleBuildState struct {
	Expected int      `json:"expected"`
	Settled  int      `json:"settled"`
	Red      []string `json:"red,omitempty"`
	// Components is the set the merge touched, in the fan-out's own order.
	//
	// It is the poll's own bookkeeping — what Expected was counted over — and
	// NOTHING downstream reads it. It used to be the deploy stage's input, which
	// is what made the deploy set the cycle's path diff: a component green at an
	// earlier commit was in no later cycle's list, so nothing ever promoted it.
	// The deploy reads the version's own state instead (ADR-0026). Kept on the
	// wire because an activity result lives in Temporal history and a run in
	// flight across an upgrade decodes what its old worker recorded.
	Components []string `json:"components,omitempty"`
}

// Green reports whether every component the merge touched has built.
func (s CycleBuildState) Green() bool { return len(s.Red) == 0 && s.Settled >= s.Expected }

// ---- the version's deploy state (ADR-0026) ---------------------------------

// versionState classifies every component in the design against what has been
// built and what is deployed.
//
// DESIRED is the release the component's newest succeeded build would cut;
// ACTUAL is the release its binding pins, plus whether that is Ready. The
// difference between those two is the whole model: the deploy set used to be
// the cycle's path diff, so a component whose build went green in a cycle a
// sibling failed was promoted by nothing, ever — its files had stopped
// changing. Classifying the DESIGN instead makes "what is serving" a function
// of what has been built rather than of which files a fix happened to touch.
//
// A component the deployment read did not answer for reads as having no binding
// — which is `behind`, and correct: it has a green build and nothing pinning
// it.
func versionState(projectID string, components []string, builds map[string][]BuildRunInfo,
	deploys map[string]delivery.ComponentDeploy) delivery.VersionState {
	out := delivery.VersionState{Components: make([]delivery.ComponentState, 0, len(components))}
	for _, name := range components {
		out.Components = append(out.Components,
			classifyComponentState(projectID, name, builds[name], deploys[name]))
	}
	return out
}

// classifyComponentState is one component's state, as a pure function of its
// builds and its binding.
//
// It does NOT branch on the binding's Failed verdict, and that is deliberate. A
// binding pinned at the right release that will never be Ready is `converging`
// here, because a pin cannot tell a doomed rollout from a slow one — and it does
// not have to: converging components are waited on, and the readiness poll turns
// a terminal Ready reason into a deploy failure within one tick. Folding failure
// into a sixth state would put the same verdict in two places, which is how the
// two come to disagree.
func classifyComponentState(projectID, component string, runs []BuildRunInfo,
	deploy delivery.ComponentDeploy) delivery.ComponentState {
	st := delivery.ComponentState{
		Component: component,
		Pinned:    deploy.Release,
		Ready:     deploy.Ready,
		Reason:    deploy.Reason,
	}
	if deploy.Undeploy {
		// Withdrawn on purpose. Nothing is owed: promoting a release over a
		// deliberate undeploy would be the platform overruling the person who
		// asked for it, and refusing to deliver the version would fail a run
		// over the same decision.
		//
		// The marker rides along because it does NOT make this component a
		// satisfied hard provider — it has no active release, so it has no
		// address (ComponentState.ServesConsumers).
		st.State, st.Undeploy = delivery.ComponentStateServing, true
		return st
	}
	st.DesiredSHA = newestGreenCommit(runs)
	if st.DesiredSHA == "" {
		// Never built. Not a failure: a red build already minted its fix issue,
		// and a component nobody has written yet has its development issue open.
		st.State = delivery.ComponentStateUnbuilt
		return st
	}
	st.DesiredRelease = delivery.ReleaseNameFor(projectID, component, st.DesiredSHA)
	switch {
	case st.Pinned != st.DesiredRelease:
		st.State = delivery.ComponentStateBehind
	case st.Ready:
		st.State = delivery.ComponentStateServing
	default:
		st.State = delivery.ComponentStateConverging
	}
	return st
}

// newestGreenCommit is the commit of a component's newest SUCCEEDED build.
//
// Succeeded rather than newest: a component whose latest attempt failed is
// still built, at the commit that worked, and a version does not un-deploy
// because somebody pushed a broken commit afterwards — the red build's own fix
// issue is what moves it forward.
//
// Ordered by the run's creation time because the host returns the list
// unordered, with the NAME as the tie-break so two runs admitted in the same
// instant classify the same way on every poll. A green run with no commit
// (a build of whatever the branch tip was, triggered outside a cycle) is
// skipped: it names no release the platform could pin.
func newestGreenCommit(runs []BuildRunInfo) string {
	best := BuildRunInfo{}
	for _, r := range runs {
		if !r.Terminal || !r.Succeeded || r.CommitSHA == "" {
			continue
		}
		if best.CommitSHA == "" || r.StartedAt.After(best.StartedAt) ||
			(r.StartedAt.Equal(best.StartedAt) && r.Name > best.Name) {
			best = r
		}
	}
	return best.CommitSHA
}

// unorderedPlan is the plan a run with no deployer wired gets: everything
// behind in one wave, nothing held. It mirrors PromoteWave's own no-deployer
// behaviour — the stage is walked, there is simply nothing to write.
func unorderedPlan(v delivery.VersionState) delivery.DeployPlan {
	plan := delivery.DeployPlan{}
	var wave []delivery.DeployTarget
	for _, c := range v.Components {
		switch c.State {
		case delivery.ComponentStateBehind:
			wave = append(wave, delivery.DeployTarget{Component: c.Component, CommitSHA: c.DesiredSHA})
			plan.Waited = append(plan.Waited, c.Component)
		case delivery.ComponentStateConverging:
			plan.Waited = append(plan.Waited, c.Component)
		}
	}
	if len(wave) > 0 {
		plan.Waves = [][]delivery.DeployTarget{wave}
	}
	return plan
}

// planWaveNames / heldNames render a plan for the log — the names, wave by
// wave, and what each held component is waiting on.
func planWaveNames(plan delivery.DeployPlan) [][]string {
	out := make([][]string, 0, len(plan.Waves))
	for _, wave := range plan.Waves {
		out = append(out, delivery.TargetNames(wave))
	}
	return out
}

func heldNames(plan delivery.DeployPlan) []string {
	out := make([]string, 0, len(plan.Held))
	for _, c := range plan.Held {
		entry := c.Component
		if len(c.WaitingOn) > 0 {
			entry += " waiting on " + strings.Join(c.WaitingOn, ", ")
		}
		out = append(out, entry)
	}
	return out
}

// reconcileWork reports whether a version state gives the deploy stage anything
// to do at all: something to promote, or something to wait for.
//
// Asked in the WORKFLOW, off the state it already has, because it decides
// whether the stage runs — and a fully serving version must reconcile in one
// read and zero writes, never park on a deploy gate it will not use.
func reconcileWork(v delivery.VersionState) bool {
	for _, c := range v.Components {
		if c.State == delivery.ComponentStateBehind || c.State == delivery.ComponentStateConverging {
			return true
		}
	}
	return false
}

// convergeSet is what the stage's closing pass re-asserts the wiring of: every
// component this pass waited on, plus every component that was ALREADY serving.
//
// The already-serving half is load-bearing and was the narrow reading's bug. A
// soft edge runs from consumer to provider — a protected API's CORS allowlist is
// the project's SPA origins — so promoting a web app in a later pass has to
// finish the wiring of an api that was promoted in an earlier one. A converge
// scoped to what this pass promoted would leave that api permanently unaware of
// the SPA it serves.
//
// Held and unbuilt components are excluded: a converge writes a binding with no
// release pinned, which OpenChoreo cannot render, so a component that has never
// been promoted must not be in it.
func convergeSet(v delivery.VersionState, waited []string) []string {
	seen := make(map[string]struct{}, len(waited)+len(v.Components))
	out := make([]string, 0, len(waited)+len(v.Components))
	add := func(name string) {
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, name := range waited {
		add(name)
	}
	for _, c := range v.Components {
		if c.State == delivery.ComponentStateServing && c.Pinned != "" {
			add(c.Component)
		}
	}
	sort.Strings(out)
	return out
}

// targetsFor pairs component names with the commit each one's release was being
// cut from, for the fix issues a failed pass files. A name the state does not
// know answers an empty commit rather than being dropped: a failure the platform
// cannot attribute to a commit is still a failure that needs an issue.
func targetsFor(names []string, v delivery.VersionState) []delivery.DeployTarget {
	commits := make(map[string]string, len(v.Components))
	for _, c := range v.Components {
		commits[c.Component] = c.DesiredSHA
	}
	out := make([]delivery.DeployTarget, 0, len(names))
	for _, name := range names {
		out = append(out, delivery.DeployTarget{Component: name, CommitSHA: commits[name]})
	}
	return out
}

// CycleDeployState is how far a cycle's DEPLOY has got: how many components were
// promoted, how many are serving, and which ones the cluster has given up on.
//
// It mirrors CycleBuildState deliberately — the loop asks the same question of
// both stages ("is this settled, and did it settle badly?") and answering it in
// two different shapes would earn nothing.
//
// Expected == 0 is a real answer, not a degenerate one: a validation cycle's
// pull request carries only tests and a report, so it rebuilds and redeploys
// nothing and is Ready the moment it merges.
type CycleDeployState struct {
	Expected int      `json:"expected"`
	Ready    int      `json:"ready"`
	Failed   []string `json:"failed,omitempty"`
	// Pending NAMES the components that have reached no verdict yet, where Ready
	// is only counted.
	//
	// Named because the deadline reports them AS the failure: a cycle can expire
	// with some components serving and others still rolling out, and a fix issue
	// that named the whole cycle would file work against components that deployed
	// perfectly. Counting was enough while the only question was "are we done
	// yet"; the deadline made "which ones aren't" a question too.
	Pending []string `json:"pending,omitempty"`
	// Reasons carries OpenChoreo's own condition reason per failed component,
	// for the issue body a failed deploy mints. Never branched on.
	Reasons map[string]string `json:"reasons,omitempty"`
}

// Green reports whether every component the cycle deployed is serving.
func (s CycleDeployState) Green() bool { return len(s.Failed) == 0 && s.Ready >= s.Expected }

// classifyCycleDeploys folds the per-component reads into the cycle's verdict.
//
// A component that is neither Ready nor Failed is still rolling out, and counts
// as neither — which is what keeps the poll waiting rather than declaring a slow
// rollout finished or broken.
func classifyCycleDeploys(expected int, states []delivery.ComponentDeploy) CycleDeployState {
	out := CycleDeployState{Expected: expected}
	for _, st := range states {
		switch {
		case st.Failed:
			out.Failed = append(out.Failed, st.Component)
			if st.Reason != "" {
				if out.Reasons == nil {
					out.Reasons = map[string]string{}
				}
				out.Reasons[st.Component] = st.Reason
			}
		case st.Ready:
			out.Ready++
		default:
			out.Pending = append(out.Pending, st.Component)
		}
	}
	return out
}
