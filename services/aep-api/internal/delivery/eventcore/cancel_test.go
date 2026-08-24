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
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// Closing the issues is what makes a cancel STICK — the sweep starts a run for
// a milestone's open WORK when no run is live on it — so what these tests
// pin is the REACH per run species, and the one property the rebuild depends on:
// only what was open at cancel time is marked.

// TestCloseCancelledWork_ADevCancelClosesTheIncrement walks a cancelled build's
// milestone as it really looks: planned work, a bug a cycle threw up, a conflict,
// the version's validation task, an open dependency gate and a human's ledger
// note.
//
// Everything the INCREMENT was carrying goes. A cancelled build abandons it, so
// none of that is anybody's work any more — and the gates going is the deliberate
// asymmetry with the halt, where the run may be retried in the same version and
// its gates still name dependencies somebody must resolve.
//
// TWO populations survive, for two different reasons.
//
// The version's VALIDATION TASK, because cancel reverts nothing: commits a cycle
// merged stay on `main` and components it promoted keep serving, so that task is a
// handle on software still running and closing it would discard a pending
// judgement of it. Leaving it open is free — the sweep skips a cancelled
// increment, and a later rebuild's dev run adopts it.
//
// The human's LEDGER NOTE, because the ledger is the one population the platform
// never touches. It is not armed, so it is not the platform's to close, and
// closing it would put a machine comment on somebody's own record. Nor is it
// needed for suppression: the sweep skips a cancelled increment whole, and an
// unarmed note is not work to it in any case.
func TestCloseCancelledWork_ADevCancelClosesTheIncrement(t *testing.T) {
	h := newHarness(t)
	h.issues.
		withIssue(7, 21, delivery.LabelAgentWork, delivery.KindDevelopment).
		withIssue(7, 22, delivery.LabelAgentWork, delivery.KindBug, delivery.SrcDeploy).
		withIssue(7, 23, delivery.LabelAgentWork, delivery.KindConflict).
		withIssue(7, 55, delivery.LabelAgentWork, delivery.KindValidation).
		withIssue(7, 60, delivery.KindProvision, "aep:dep/orders-db").
		withIssue(7, 61) // a human's note: unarmed, nobody's work

	closed, err := h.events.CloseCancelledWork(t.Context(), testOrg, testProject, 7, delivery.RunKindDev)
	if err != nil {
		t.Fatalf("CloseCancelledWork: %v", err)
	}
	want := []int{21, 22, 23, 60}
	if !slices.Equal(closed, want) {
		t.Fatalf("closed = %v, want %v — the working set and the gates, and nothing else", closed, want)
	}
	if slices.Contains(h.issues.closed, 55) {
		t.Error("a cancelled build closed the version's validation task — it judges what is still deployed")
	}
	if slices.Contains(h.issues.closed, 61) {
		t.Error("a cancelled build closed a human's ledger note — the ledger is never the platform's to touch")
	}
	if slices.Contains(h.issues.labelled, fmt.Sprintf("61+%s", delivery.LabelCancelled)) {
		t.Error("a human's ledger note must not even be MARKED — it was never in flight")
	}
	for _, n := range want {
		if !slices.Contains(h.issues.labelled, fmt.Sprintf("%d+%s", n, delivery.LabelCancelled)) {
			t.Errorf("issue #%d was closed without the %s mark a rebuild reopens it by",
				n, delivery.LabelCancelled)
		}
		if c := h.issues.closeComments[n]; !strings.Contains(c, "cancelled") {
			t.Errorf("issue #%d closed with no comment saying why: %q", n, c)
		}
	}
	// The way back is the non-obvious half, so the comment says it.
	if c := h.issues.closeComments[21]; !strings.Contains(c, "REOPENS") {
		t.Errorf("the comment must say a rebuild of the same spec reopens this, got %q", c)
	}
}

// The property the whole marker exists for: work a cycle GENUINELY FINISHED is
// already closed, so it is neither touched nor stamped — and a rebuild of the
// same spec therefore cannot resurrect it. Reopening the milestone's issues
// wholesale would dispatch an agent at work that is merged and serving.
func TestCloseCancelledWork_OnlyWhatWasOpenIsMarked(t *testing.T) {
	h := newHarness(t)
	h.issues.
		withIssue(7, 21, delivery.LabelAgentWork, delivery.KindDevelopment).
		withClosedIssue(7, 20, delivery.LabelAgentWork, delivery.KindDevelopment)

	closed, err := h.events.CloseCancelledWork(t.Context(), testOrg, testProject, 7, delivery.RunKindDev)
	if err != nil {
		t.Fatalf("CloseCancelledWork: %v", err)
	}
	if !slices.Equal(closed, []int{21}) {
		t.Fatalf("closed = %v, want [21] — the finished Task #20 is already closed", closed)
	}
	if slices.Contains(h.issues.closed, 20) {
		t.Errorf("issue #20 was finished before the cancel and must not be touched")
	}
	if slices.Contains(h.issues.labelled, fmt.Sprintf("20+%s", delivery.LabelCancelled)) {
		t.Errorf("issue #20 must stay UNMARKED, or the next rebuild reopens work that already shipped")
	}
}

// A TASK run's cancel abandons only the defects it was working. The version it
// works is the DEPLOYED one and is not being abandoned, so its plan, its
// validation task and its gates are untouched — closing them would say a release
// nobody cancelled had been withdrawn.
func TestCloseCancelledWork_ATaskCancelClosesOnlyItsBugsAndConflicts(t *testing.T) {
	h := newHarness(t)
	h.issues.
		withIssue(7, 21, delivery.LabelAgentWork, delivery.KindDevelopment).
		withIssue(7, 22, delivery.LabelAgentWork, delivery.KindBug, delivery.SrcUser).
		withIssue(7, 23, delivery.LabelAgentWork, delivery.KindConflict).
		withIssue(7, 55, delivery.LabelAgentWork, delivery.KindValidation).
		withIssue(7, 60, delivery.KindProvision)

	closed, err := h.events.CloseCancelledWork(t.Context(), testOrg, testProject, 7, delivery.RunKindTask)
	if err != nil {
		t.Fatalf("CloseCancelledWork: %v", err)
	}
	if !slices.Equal(closed, []int{22, 23}) {
		t.Fatalf("closed = %v, want [22 23] — a task run's cancel reaches its own defects only", closed)
	}
	for _, n := range []int{21, 55, 60} {
		if slices.Contains(h.issues.closed, n) {
			t.Errorf("issue #%d belongs to the version, not to the cancelled bug-fix run", n)
		}
	}
}

// A VALIDATION run closes nothing HERE, and that is a decision rather than a gap.
// Its consequence is the version's validation task, and the workflow closes that
// on every ending — scoped to the task it ADOPTED. Reaching the milestone from
// here would close a task a run cancelled before its first read never adopted,
// which is exactly the case that must stay open for the next trigger.
func TestCloseCancelledWork_AValidationCancelClosesNothingHere(t *testing.T) {
	h := newHarness(t)
	h.issues.
		withIssue(7, 55, delivery.LabelAgentWork, delivery.KindValidation).
		withIssue(7, 22, delivery.LabelAgentWork, delivery.KindBug, delivery.SrcValidation)

	closed, err := h.events.CloseCancelledWork(t.Context(), testOrg, testProject, 7, delivery.RunKindValidation)
	if err != nil {
		t.Fatalf("CloseCancelledWork: %v", err)
	}
	if len(closed) != 0 || len(h.issues.closed) != 0 {
		t.Fatalf("a validation run's cancel closes nothing here: closed=%v writes=%v", closed, h.issues.closed)
	}
}

// The LABEL goes on before the CLOSE. An issue closed with no marker is invisible
// to every future rebuild and its work is silently lost, where an issue marked but
// left open costs nothing — the sweep skips its milestone, and the rebuild reopens
// it (a no-op) and clears the mark either way.
//
// So a failing label must leave the issue OPEN, and the error must surface for the
// activity to retry.
func TestCloseCancelledWork_AFailedMarkLeavesTheIssueOpen(t *testing.T) {
	h := newHarness(t)
	h.issues.withIssue(7, 21, delivery.LabelAgentWork, delivery.KindDevelopment)
	h.issues.labelErr = errors.New("github said no")

	closed, err := h.events.CloseCancelledWork(t.Context(), testOrg, testProject, 7, delivery.RunKindDev)
	if err == nil {
		t.Fatal("a failed mark must surface, so the supervisor's activity retries")
	}
	if len(closed) != 0 || len(h.issues.closed) != 0 {
		t.Fatalf("nothing may be closed unmarked: closed=%v writes=%v", closed, h.issues.closed)
	}
}
