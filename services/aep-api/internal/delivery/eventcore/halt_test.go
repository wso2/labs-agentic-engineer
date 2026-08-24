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
	"fmt"
	"slices"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// The halt is what stops the reconcile sweep from restarting the work a failed
// run gave up on, so what these tests pin is its REACH: exactly the run's own
// working set, and nothing beside it.

// TestHaltUnfinishedWork_MarksTheRunsOwnWorkAndNothingElse walks a failed dev
// run's milestone as it really looks at the moment it gives up: planned work it
// never finished, a deploy-fix bug IT filed at the last boundary, the version's
// validation task, an open gate, and a human's ledger note.
//
// The bug the run filed itself is the one that matters most. It is the newest
// thing in the milestone and therefore the first thing a restarted run would pick
// up, so a halt that covered only "work the run was given" would leave the loop
// exactly as restartable as before.
func TestHaltUnfinishedWork_MarksTheRunsOwnWorkAndNothingElse(t *testing.T) {
	h := newHarness(t)
	h.issues.
		withIssue(7, 21, delivery.LabelAgentWork, delivery.KindDevelopment).
		withIssue(7, 22, delivery.LabelAgentWork, delivery.KindBug, delivery.SrcDeploy).
		withIssue(7, 23, delivery.LabelAgentWork, delivery.KindConflict).
		withIssue(7, 55, delivery.LabelAgentWork, delivery.KindValidation).
		withIssue(7, 60, delivery.KindProvision, "aep:dep/orders-db").
		withIssue(7, 61) // a human's note: unarmed, nobody's work

	halted, err := h.events.HaltUnfinishedWork(t.Context(), testOrg, testProject, 7,
		delivery.RunKindDev, delivery.RunReasonDeployBudget)
	if err != nil {
		t.Fatalf("HaltUnfinishedWork: %v", err)
	}
	wantHalted := []int{21, 22, 23}
	if !slices.Equal(halted, wantHalted) {
		t.Fatalf("halted = %v, want %v — the dev working set, including the bug the run filed itself",
			halted, wantHalted)
	}
	for _, n := range wantHalted {
		if !slices.Contains(h.issues.labelled, fmt.Sprintf("%d+%s", n, delivery.LabelHalted)) {
			t.Errorf("issue #%d was not stamped %s", n, delivery.LabelHalted)
		}
		if !slices.Contains(h.issues.commented, n) {
			t.Errorf("issue #%d was stamped without a comment naming the reason", n)
		}
	}
	// The populations that are NOT this run's work. The validation task belongs to
	// the validation loop, a gate is a hold nobody works, and a ledger note is a
	// human's — halting any of them would mark work no run ever gave up on.
	for _, n := range []int{55, 60, 61} {
		if slices.Contains(h.issues.commented, n) {
			t.Errorf("issue #%d is not the run's work and must not be halted", n)
		}
	}
	if len(h.issues.closed) != 0 {
		t.Errorf("halting closes nothing — a failed increment's work stays open, got %v", h.issues.closed)
	}
}

// A TASK run's halt must not reach the planned work it was never allowed to
// touch. That work belongs to a build, and a build is what will resume it — so
// marking it here would abandon a version's plan on the strength of an unrelated
// bug-fix run running out of re-dispatches.
func TestHaltUnfinishedWork_ATaskRunNeverHaltsPlannedWork(t *testing.T) {
	h := newHarness(t)
	h.issues.
		withIssue(7, 21, delivery.LabelAgentWork, delivery.KindDevelopment).
		withIssue(7, 22, delivery.LabelAgentWork, delivery.KindBug, delivery.SrcUser)

	halted, err := h.events.HaltUnfinishedWork(t.Context(), testOrg, testProject, 7,
		delivery.RunKindTask, delivery.RunReasonRedispatchBudget)
	if err != nil {
		t.Fatalf("HaltUnfinishedWork: %v", err)
	}
	if !slices.Equal(halted, []int{22}) {
		t.Fatalf("halted = %v, want [22] — a task run's working set excludes planned work", halted)
	}
}

// A VALIDATION run halts nothing, and that is a decision rather than a gap. Its
// own work is the version's validation task, which it closes on every ending; the
// repair issues a failed verdict files and the conflict issue a stuck validation
// pull request produces are deliberately an ordinary task run's work. Halting
// them would break the repair chain instead of protecting a budget.
func TestHaltUnfinishedWork_AValidationRunHaltsNothing(t *testing.T) {
	h := newHarness(t)
	h.issues.
		withIssue(7, 22, delivery.LabelAgentWork, delivery.KindBug, delivery.SrcValidation).
		withIssue(7, 23, delivery.LabelAgentWork, delivery.KindConflict)

	halted, err := h.events.HaltUnfinishedWork(t.Context(), testOrg, testProject, 7,
		delivery.RunKindValidation, delivery.RunReasonRedispatchBudget)
	if err != nil {
		t.Fatalf("HaltUnfinishedWork: %v", err)
	}
	if len(halted) != 0 {
		t.Fatalf("halted = %v, want none — a validation run has no working set of its own", halted)
	}
}

// An issue an earlier run already halted is left alone. The comment names a
// terminal reason, and two of them on one issue read as two runs abandoning it,
// which is a story about the loop that never happened.
func TestHaltUnfinishedWork_DoesNotRepeatItselfOnAnAlreadyHaltedIssue(t *testing.T) {
	h := newHarness(t)
	h.issues.withIssue(7, 21, delivery.LabelAgentWork, delivery.KindBug, delivery.LabelHalted)

	halted, err := h.events.HaltUnfinishedWork(t.Context(), testOrg, testProject, 7,
		delivery.RunKindTask, delivery.RunReasonNoProgress)
	if err != nil {
		t.Fatalf("HaltUnfinishedWork: %v", err)
	}
	if len(halted) != 0 || len(h.issues.commented) != 0 {
		t.Fatalf("an already-halted issue is not re-halted: halted=%v commented=%v",
			halted, h.issues.commented)
	}
}
