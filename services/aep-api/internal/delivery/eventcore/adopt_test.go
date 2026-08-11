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
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// The properties in this file are all one incident: an SRE/RCA handoff filed an
// issue against a project whose first build was still running, adoption refused
// it, the refusal became an opaque 500, and the issue was never worked. Each
// test below is one of the links in that chain.

// An adopted issue that is not agent work is invisible to the dispatch
// predicate — OpenNonGateWork counts the milestone's "aep" issues, so the run
// starts, finds an empty working set and parks forever. Milestone membership
// alone is NOT enough: ledger issues live in the milestone too.
func TestAdoption_StampsTheAgentWorkLabel(t *testing.T) {
	h := newHarness(t, succeededRun("run-1", 3))

	if err := h.events.AdoptIssue(context.Background(), testOrg, testProject, AdoptTarget{Number: 31}); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if !delivery.HasLabel(h.issues.labelsOn(31), delivery.LabelAgentWork) {
		t.Fatalf("an adopted issue must be marked agent work or no run can ever work it, got %v",
			h.issues.labelsOn(31))
	}
}

// The same holds for the GitHub-side route. This is the flow the platform's own
// red-main ledger issue tells a human to use ("Add the `aep:codingagent`
// label"), so if that one label is not sufficient the instruction is a dead end.
func TestAdoption_ByLabelAlsoStampsTheAgentWorkLabel(t *testing.T) {
	h := newHarness(t, succeededRun("run-1", 9))

	if err := h.deliver(t, "issues", issueBody("labeled", 31, 9, delivery.LabelAdopt, "human", false)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !delivery.HasLabel(h.issues.labelsOn(31), delivery.LabelAgentWork) {
		t.Fatalf("adopting by label must also make the issue agent work, got %v", h.issues.labelsOn(31))
	}
}

// THE REGRESSION. An incident raised by the very deployment a spec build is
// performing arrives while that build is still running, so there is no
// SUCCEEDED run to attach it to. Refusing there dropped the handoff for good —
// nothing retries it. The in-flight build's milestone is the right home: it IS
// the version the incident is against.
func TestAdoption_FallsBackToTheSpecBuildInFlight(t *testing.T) {
	h := newHarness(t, aRun("run-1", 4, delivery.RunStateRunning))

	if err := h.events.AdoptIssue(context.Background(), testOrg, testProject, AdoptTarget{Number: 31}); err != nil {
		t.Fatalf("an incident filed during the first build must still be adoptable, got %v", err)
	}
	if len(h.issues.assigned) != 1 || h.issues.assigned[0] != "31->4" {
		t.Fatalf("the issue must join the in-flight build's milestone, got %v", h.issues.assigned)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("a second run on a live milestone would put two agents on one branch, got %+v", h.sup.started)
	}
}

// A deployed version still wins over one in flight: an incident against what is
// actually running belongs to the version that is running.
func TestAdoption_PrefersTheDeployedVersionOverTheOneInFlight(t *testing.T) {
	h := newHarness(t, aRun("run-2", 5, delivery.RunStateRunning), succeededRun("run-1", 3))

	if err := h.events.AdoptIssue(context.Background(), testOrg, testProject, AdoptTarget{Number: 31}); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if len(h.issues.assigned) != 1 || h.issues.assigned[0] != "31->3" {
		t.Fatalf("the deployed version's milestone must win, got %v", h.issues.assigned)
	}
}

// With nothing built and nothing building there is genuinely no version to
// attach an incident to, and a guess would file it against a version that does
// not exist. The refusal is a shared sentinel so the HTTP edge can recognise it
// without importing this package.
func TestAdoption_NothingBuiltRefusesWithTheSharedSentinel(t *testing.T) {
	h := newHarness(t) // no runs at all

	err := h.events.AdoptIssue(context.Background(), testOrg, testProject, AdoptTarget{Number: 31})
	if !errors.Is(err, delivery.ErrNoAdoptableMilestone) {
		t.Fatalf("adoption with no version at all must refuse with the shared sentinel, got %v", err)
	}
	if len(h.issues.assigned) != 0 || len(h.sup.started) != 0 || len(h.issues.labelsOn(31)) != 0 {
		t.Fatal("a refused adoption must write nothing")
	}
}

// A run parked on an empty working set re-derives only when signalled. The
// label adoption just wrote comes back as a platform echo and is suppressed, so
// without an explicit wake the run stays parked with work sitting in front of
// it — the exact "dispatched but never progresses" symptom.
func TestAdoption_WakesARunParkedOnAnEmptyWorkingSet(t *testing.T) {
	h := newHarness(t, aRun("run-1", 4, delivery.RunStateWaiting))
	h.issues.withCounts(4, 0, 1, 1) // the adopted issue, now agent work

	if err := h.events.AdoptIssue(context.Background(), testOrg, testProject, AdoptTarget{Number: 31}); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if got := h.sup.named(delivery.SigRunWorkable); len(got) != 1 {
		t.Fatalf("a parked run must be told its working set is no longer empty, got %v", got)
	}
}

// A run mid-cycle re-reads its milestone at the next boundary on its own;
// waking it would only race the agent already working.
func TestAdoption_DoesNotWakeARunMidCycle(t *testing.T) {
	h := newHarness(t, aRun("run-1", 4, delivery.RunStateRunning))
	h.issues.withCounts(4, 0, 1, 1)

	if err := h.events.AdoptIssue(context.Background(), testOrg, testProject, AdoptTarget{Number: 31}); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if got := h.sup.named(delivery.SigRunWorkable); len(got) != 0 {
		t.Fatalf("a running run needs no signal, got %v", got)
	}
}

// succeededRun is a DEPLOYED version: the spec build that completed, whose
// milestone is where incidents belong.
func succeededRun(id string, milestone int) delivery.MilestoneRun {
	return aRun(id, milestone, delivery.RunStateSucceeded)
}
