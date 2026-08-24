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

import (
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// This file holds the event plane's three DECISIONS as pure functions over
// facts: may this pull request merge, which components does this diff touch,
// and may this run dispatch. They are separated from the handlers that fetch
// the facts so each can be read, argued with and tested on its own — the merge
// policy in particular, because it is the seam review logic arrives behind.

// mergeDecision is the auto-merge policy seam's verdict. Reason is written for
// a log line and for whoever later replaces this policy; Matched lists the
// milestone issues the pull request claims to have FINISHED, which is the
// evidence the verdict rests on and the record of what the cycle did.
type mergeDecision struct {
	Merge  bool
	Reason string
	// Matched is the closing-keyword matches only — the issues this pull request
	// resolves, which the merge closes. It is recorded on the cycle
	// (RunCycle.Resolves) as the durable answer to "what did this cycle work",
	// so a non-closing reference must never be folded into it.
	Matched []int
	// Validated is the validation task this pull request judges, matched from a
	// deliberately NON-closing reference. Kept apart from Matched for the reason
	// above; it admits the merge without claiming the issue was finished by it.
	Validated []int
}

// decideAutoMerge IS the merge policy: a pull request that REFERENCES at least
// one issue this run is working in its milestone squash-merges.
//
// There are two ways to reference one, and the difference is which side owns the
// issue's close:
//
//   - A CLOSING keyword (`Resolves #N`, and GitHub's other eight spellings) on
//     any ARMED issue in the milestone. This is the coding cycles' path: the
//     merge closes the work, GitHub does it, and the matched list is also the
//     cycle's record of what it finished.
//   - `Validates #N` on the milestone's `validation`-kind task. This is the
//     validation cycle's path, and it exists because the platform owns that
//     task's lifecycle: it reopens it for the next attempt and must close it even
//     on an ending where no pull request merged at all. A closing keyword would
//     put two owners on one issue.
//
// The second admission is SCOPED to a validation-kind issue on purpose. Without
// the scope, `Validates #N` would become a general-purpose way for any agent to
// get a pull request merged while closing nothing — and the working set would
// never empty, because the issues it claimed would still be open. A coding pull
// request carrying only `Validates #N` is therefore declined, exactly as one
// carrying nothing is.
//
// Dropping the validation reference altogether was the other option and is a
// trap: this predicate is the only thing the platform checks before merging, so a
// body that referenced nothing would be read as somebody else's work, sit
// unmerged until the cycle's landing deadline, and settle every validation
// `unreported` — a verdict about the software derived from the platform's own
// filter.
//
// A dispatch GATE is not admitted either way, because it carries no arming
// label: nothing opens a pull request against a gate, and a pull request claiming
// one would be claiming work the platform owns.
//
// There is deliberately no verification BEFORE the merge. The verification is
// the POST-MERGE build: the merge is what triggers it, so gating the merge on a
// build would gate it on the thing it exists to cause. A red build is not a
// dead end either — it mints a bug issue into the same milestone, the run works
// it in the next cycle, and the loop converges. The agent's own compile-level
// checks (go build / tsc, lockfile resolution) run before it opens the pull
// request and catch the cheap failures; the cluster catches the rest.
//
// What the predicate DOES buy is scope: a pull request that claims nothing in
// this milestone is not this run's work and is left alone for a human. Review
// logic (approvals, checks, a human gate) arrives later BEHIND this function —
// it is one named decision over facts precisely so that swapping it is a local
// change with a local test, not a rewrite of the handler.
func decideAutoMerge(resolves, validates []int, milestoneIssues []sourcecontrol.IssueInfo) mergeDecision {
	if len(resolves) == 0 && len(validates) == 0 {
		return mergeDecision{Reason: "pull request references no issue"}
	}
	work := make(map[int]bool, len(milestoneIssues))
	validation := make(map[int]bool, 1)
	for _, iss := range milestoneIssues {
		if !delivery.HasLabel(iss.Labels, delivery.LabelAgentWork) {
			continue
		}
		work[iss.Number] = true
		if delivery.IsValidationWork(iss.Labels) {
			validation[iss.Number] = true
		}
	}
	var matched, validated []int
	for _, n := range resolves {
		if work[n] {
			matched = append(matched, n)
		}
	}
	for _, n := range validates {
		if validation[n] {
			validated = append(validated, n)
		}
	}
	if len(matched) == 0 && len(validated) == 0 {
		return mergeDecision{Reason: "no referenced issue is this run's work in this milestone"}
	}
	if len(matched) == 0 {
		return mergeDecision{
			Merge:     true,
			Reason:    "validates this version's validation task",
			Validated: validated,
		}
	}
	return mergeDecision{
		Merge:     true,
		Reason:    "resolves this run's work in its milestone",
		Matched:   matched,
		Validated: validated,
	}
}

// The path diff itself is delivery.DiffComponents in the domain root: the run
// supervisor has to compute the SAME expected component set to know when a
// cycle's builds have all reported, and two copies of a prefix-matching rule
// would eventually disagree about what a merge rebuilds.

// dispatchable is the dispatch predicate: no gate is open in the milestone and
// its working set is non-empty.
//
// It reads the ONE GraphQL call behind MilestoneIssueCounts rather than a
// milestone's open_issues field, which counts pull requests and would keep a
// finished run "workable" for as long as one of its PRs stayed open.
//
// The second clause is the WORKING SET, not "some issue is open": a milestone
// holding only ledger issues (human-filed, unarmed) has nothing to work, and
// declaring it workable would wake a run whose first act is to find an empty
// working set. The arithmetic lives on the counts type so this predicate and any
// settle check agree on what work is.
//
// It is the DEV working set, which is the wider of the two — it includes the
// milestone's planned work. This is the predicate that wakes a waiting run, and
// waking a run that then finds nothing to do costs a cycle boundary, where
// failing to wake one leaves a version stalled with nobody looking at it.
//
// It must agree, issue for issue, with delivery.InDevWorkingSet — the same rule
// read off an issue's own labels rather than counted host-side. Where the two
// disagree the loop learns two different things about the same milestone from
// two different reads, which is how a run settles a version nobody built.
//
// The decision ITSELF is delivery.MilestoneWork.Dispatchable, shared with the
// supervisor's own boundary check (run.Dispatchable) because a run woken by a
// predicate its boundary then rejects is a wasted cycle, and the reverse is a
// version nobody wakes. This function is only the adapter from host counts to
// that rule — nil-tolerant, because an unknown milestone is not dispatchable.
func dispatchable(counts *sourcecontrol.MilestoneIssueCounts) bool {
	if counts == nil {
		return false
	}
	return delivery.MilestoneWork{
		Gates: counts.OpenProvision,
		Work:  counts.OpenDevWork(),
	}.Dispatchable()
}

// attemptsFor counts how many of a component's WorkflowRuns belong to one
// (component, commit) pair — the automatic re-trigger budget, derived from
// OpenChoreo instead of stored.
//
// Deriving it is what keeps the budget honest under every restart, replay and
// redelivery: the runs ARE the attempts. A counter column would have to be
// incremented by exactly the code paths that create runs, and a redelivered
// webhook or a crashed handler would desynchronise it from the cluster.
func attemptsFor(runs []BuildRun, prefix string) int {
	n := 0
	for _, r := range runs {
		if strings.HasPrefix(strings.ToLower(r.Name), prefix) {
			n++
		}
	}
	return n
}
