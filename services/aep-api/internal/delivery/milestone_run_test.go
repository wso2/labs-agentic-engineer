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

package delivery_test

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// TestIsTerminalRunState pins the settled/unsettled split every guarded run
// transition and the build mutex are written against. The two must agree: a
// state the helper calls terminal is a state the mutex index must NOT count and
// no mutator may write through.
func TestIsTerminalRunState(t *testing.T) {
	t.Parallel()

	terminal := []string{
		delivery.RunStateSucceeded,
		delivery.RunStateFailed,
		delivery.RunStateCancelled,
		delivery.RunStateBlocked,
	}
	for _, s := range terminal {
		if !delivery.IsTerminalRunState(s) {
			t.Errorf("IsTerminalRunState(%q) = false, want true", s)
		}
	}

	nonTerminal := []string{
		delivery.RunStateWaiting,
		delivery.RunStateRunning,
		"", // an unset state is not settled
		"bogus",
	}
	for _, s := range nonTerminal {
		if delivery.IsTerminalRunState(s) {
			t.Errorf("IsTerminalRunState(%q) = true, want false", s)
		}
	}
}

// TestRunBudgetLimits pins the §7 budget numbers as constants so a change to
// any of them is a deliberate, reviewed edit — each one names exactly one
// failure class, which is what keeps the terminal reasons honest.
func TestRunBudgetLimits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  int
		want int
	}{
		{"re-dispatch per cycle", delivery.RunMaxRedispatchPerCycle, 2},
		{"build re-trigger per component per SHA", delivery.RunMaxBuildRetriggersPerComponentSHA, 1},
		{"fix cycles per run", delivery.RunMaxFixCycles, 2},
		{"conflict cycles per run", delivery.RunMaxConflictCycles, 2},
		{"default total-cycle ceiling", delivery.RunDefaultCycleCeiling, 8},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s budget = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// TestSettleClosesTheMilestone pins the LIFE OF A VERSION in one table: which
// ending finishes the version, and which one merely finishes a run.
//
// The rule is here rather than at each settle site because one function settles
// all three workflows, and a plain "succeeded closes it" closed the milestone at
// the dev run's HAND-OFF — over the validation task it had just minted. That is
// not cosmetic: the validation agent discovers its work with `gh issue list
// --milestone`, which resolves by title and sees only OPEN milestones, so the one
// agent meant to work that task could not find it.
func TestSettleClosesTheMilestone(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		kind, state     string
		awaitingVerdict bool
		want            bool
	}{
		// The HAND-OFF. Deployed and unjudged is not finished.
		{"a dev run that filed the validation task leaves the milestone open",
			delivery.RunKindDev, delivery.RunStateSucceeded, true, false},
		// The one dev ending that finishes a version: nothing will ever judge it,
		// so nothing is being waited for.
		{"a dev run with no oracle to ask closes it",
			delivery.RunKindDev, delivery.RunStateSucceeded, false, true},
		// The GREEN ENDING. A succeeded validation run is one by construction —
		// every fatal verdict settles the run failed.
		{"a validation run's green ending closes it",
			delivery.RunKindValidation, delivery.RunStateSucceeded, true, true},
		// A defect fixed inside a version somebody else delivered says nothing
		// about that version.
		{"a task run never closes it",
			delivery.RunKindTask, delivery.RunStateSucceeded, false, false},
		{"a task run that reopened the validation task still never closes it",
			delivery.RunKindTask, delivery.RunStateSucceeded, true, false},
		// FAILED stays open, of every kind: the way forward is more work in the
		// same version.
		{"a failed build leaves it open", delivery.RunKindDev, delivery.RunStateFailed, false, false},
		{"a failing verdict leaves it open", delivery.RunKindValidation, delivery.RunStateFailed, true, false},
		{"a failed bug-fix run leaves it open", delivery.RunKindTask, delivery.RunStateFailed, false, false},
		// CANCEL is the abandonment rule, and only a build's abandons the increment.
		{"a cancelled build abandons the increment", delivery.RunKindDev, delivery.RunStateCancelled, false, true},
		{"a cancelled validation run withdraws no release",
			delivery.RunKindValidation, delivery.RunStateCancelled, true, false},
		{"a cancelled bug-fix run withdraws no release",
			delivery.RunKindTask, delivery.RunStateCancelled, false, false},
		// BLOCKED is a wait somebody else clears, not an ending.
		{"a quota block is not an ending", delivery.RunKindDev, delivery.RunStateBlocked, false, false},
		// A non-terminal state never reaches this, and answers the safe way if it
		// somehow does.
		{"a running run finishes nothing", delivery.RunKindDev, delivery.RunStateRunning, false, false},
	}
	for _, c := range cases {
		if got := delivery.SettleClosesTheMilestone(c.kind, c.state, c.awaitingVerdict); got != c.want {
			t.Errorf("%s: SettleClosesTheMilestone(%q, %q, %v) = %v, want %v",
				c.name, c.kind, c.state, c.awaitingVerdict, got, c.want)
		}
	}
}

func TestSettleHandsWorkOnward(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		state string
		want  bool
	}{
		// The three hand-offs the platform makes to itself, and the states they
		// arrive in. A dev run files the version's validation task and succeeds; a
		// task run reopens that task and succeeds; a failed verdict files repair
		// issues for a task run to work.
		{"a delivered version hands over its validation task", delivery.RunStateSucceeded, true},
		{"a failing verdict hands over its repair issues", delivery.RunStateFailed, true},

		// A QUOTA BLOCK changes nothing about the milestone, so reconciling it is a
		// spin rather than a hand-off: the halt is failed-only, so the working set
		// is untouched, and the replacement run meets the same refusal and blocks
		// again. Only the sweep's timer has ever bounded that, and it must stay the
		// only thing that does.
		{"a quota block hands over nothing", delivery.RunStateBlocked, false},

		// A CANCEL is a person saying stop. A cancelled build's increment is
		// abandoned and the plane skips its milestone anyway; a validation run
		// cancelled before its first read leaves the version's task open on purpose,
		// and restarting the judging a second after the click would overrule the
		// person who stopped it. Asking again is the revalidate button's job.
		{"a cancel hands over nothing", delivery.RunStateCancelled, false},

		// Non-terminal states never reach this, and answer the quiet way if they do.
		{"a running run has handed over nothing yet", delivery.RunStateRunning, false},
		{"a waiting run has handed over nothing yet", delivery.RunStateWaiting, false},
		{"a planning run has handed over nothing yet", delivery.RunStatePlanning, false},
	}
	for _, c := range cases {
		if got := delivery.SettleHandsWorkOnward(c.state); got != c.want {
			t.Errorf("%s: SettleHandsWorkOnward(%q) = %v, want %v", c.name, c.state, got, c.want)
		}
	}
}
