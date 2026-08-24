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
	// Components is the set the merge touched, in the fan-out's own order. The
	// DEPLOY stage promotes exactly this list rather than re-deriving it from
	// GitHub: the two stages must agree on which components a cycle owns, and a
	// second path to that answer is a second chance to disagree.
	Components []string `json:"components,omitempty"`
}

// Green reports whether every component the merge touched has built.
func (s CycleBuildState) Green() bool { return len(s.Red) == 0 && s.Settled >= s.Expected }

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
