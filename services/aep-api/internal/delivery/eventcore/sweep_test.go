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
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

func sweepOver(h *harness) *Sweep {
	return NewSweep(h.events, fakeRepoLister{repos: []RepoRef{
		{OrgID: testOrg, ProjectID: testProject, FullName: testRepo},
	}}, 0)
}

// TestSweep_StartsARunForUnworkedOpenIssues is the backstop's whole job: a
// milestone with open work and nobody on it. It heals a delivery GitHub never
// made and the adoption-versus-settle race, which leave the same footprint.
func TestSweep_StartsARunForUnworkedOpenIssues(t *testing.T) {
	h := newHarness(t, aRun("run-old", 7, delivery.RunStateSucceeded))
	h.issues.withCounts(7, 0, 2, 2)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 1 || h.sup.started[0].MilestoneNumber != 7 ||
		h.sup.started[0].Origin != delivery.RunOriginIncidentAdoption {
		t.Fatalf("the sweep must start a run over the unworked milestone, got %+v", h.sup.started)
	}
}

// TestSweep_ReOffersALiveRow is the wedge test.
//
// A live ROW is not a live WORKFLOW. Nothing else in the platform notices a row
// whose execution is gone, and because a non-terminal row makes
// LiveRunForMilestone answer forever, the sweep's open-work rule would skip it
// forever — while the partial unique indexes refuse every later run on that
// project. Re-offering is what heals it, and it is safe because StartRun is
// idempotent: a running execution answers AlreadyStarted and the row is reused
// rather than re-admitted.
//
// ZERO open issues is the case that matters. That is exactly what hasOpenWork
// skips, so a row stranded before its milestone was filled is the one the old
// rule could never reach.
func TestSweep_ReOffersALiveRow(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.issues.withCounts(7, 0, 0, 0)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 1 || h.sup.started[0].MilestoneNumber != 7 {
		t.Fatalf("a live row must be re-offered so a lost workflow is restarted, got %+v", h.sup.started)
	}
	// A re-offer resumes a run; it must never re-derive the version.
	if h.sup.started[0].Tag != "" || len(h.sup.started[0].ProvisionInputs) != 0 {
		t.Errorf("a re-offer must carry no planning inputs, got %+v", h.sup.started[0])
	}
}

// TestSweep_NeverReOffersARunStillPlanning — the one live row the sweep must
// leave alone.
//
// Re-offering it would start a fresh workflow with no Tag and no provision
// inputs (those ride the request, not the row), so the run would skip its
// planning phase entirely and settle an UNPLANNED version as delivered. A
// planning row belongs to the click, which starts the workflow synchronously
// and settles the row when it cannot.
func TestSweep_NeverReOffersARunStillPlanning(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStatePlanning))
	h.issues.withCounts(7, 0, 0, 0)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("a run still planning must not be re-offered, got %+v", h.sup.started)
	}
}

// TestSweep_NeverResurrectsASupersededMilestone is the property that makes the
// backstop safe to run forever: supersede closes the previous version's open
// issues before the next milestone is minted, so an abandoned milestone has no
// open work for the sweep to find.
func TestSweep_NeverResurrectsASupersededMilestone(t *testing.T) {
	h := newHarness(t, aRun("run-old", 6, delivery.RunStateCancelled))
	h.issues.withCounts(6, 0, 0, 0) // superseded: everything closed

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("an abandoned milestone holds no open work and must stay abandoned, got %+v", h.sup.started)
	}
}

// TestSweep_AnOpenGateDoesNotStopARunFromStarting — a gate holds DISPATCH, not
// the run. Healing into "started and waiting" is the correct repair.
func TestSweep_AnOpenGateDoesNotStopARunFromStarting(t *testing.T) {
	h := newHarness(t, aRun("run-old", 7, delivery.RunStateFailed))
	h.issues.withCounts(7, 1, 1, 2)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 1 {
		t.Fatalf("a gated milestone with work still needs a run to wait on it, got %+v", h.sup.started)
	}
}

// TestSweep_IsInertWithoutRunRows — the same gate as every handler: the sweep
// walks the milestones the PLATFORM has run, so a project it has never run for
// is invisible to it.
func TestSweep_IsInertWithoutRunRows(t *testing.T) {
	h := newHarness(t)
	h.issues.withCounts(7, 0, 5, 5) // GitHub is full of open issues

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("a milestone the platform never ran is somebody else's, got %+v", h.sup.started)
	}
}
