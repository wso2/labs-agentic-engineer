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
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// configWithNoTemporal is a runtime that will never connect — the degraded boot
// every nil-safety assertion below is about.
func configWithNoTemporal() config.TemporalConfig { return config.TemporalConfig{} }

// fakeDispatcher stands in for the coding agent. Its only job in these tests is
// to be non-nil.
type fakeDispatcher struct{}

func (fakeDispatcher) Dispatch(context.Context, delivery.MilestoneDispatch) (string, error) {
	return "job-1", nil
}

// countingRuns records whether the start path reached the run store at all.
type countingRuns struct {
	RunStore
	admits int
	lives  int
}

func (r *countingRuns) TryAdmit(context.Context, *delivery.MilestoneRun) (bool, *delivery.MilestoneRun, error) {
	r.admits++
	return false, nil, errors.New("countingRuns: unexpected admit")
}

func (r *countingRuns) LiveRunForMilestone(context.Context, string, string, int) (*delivery.MilestoneRun, error) {
	r.lives++
	return nil, nil
}

// TestSupervisorIsNilSafe pins the property the event plane and the build click
// both depend on: they hold the supervisor unconditionally, so every verb on a
// nil one is a no-op rather than a panic. It is the same guarantee the
// task-keyed Signaler makes, and it is what keeps a degraded boot from needing
// a nil check at each call site.
func TestSupervisorIsNilSafe(t *testing.T) {
	var s *Supervisor
	if err := s.StartRun(context.Background(), delivery.StartRunRequest{}); err != nil {
		t.Fatalf("StartRun on a nil supervisor: %v", err)
	}
	if err := s.SignalRun(context.Background(), &delivery.MilestoneRun{}, delivery.SigRunWorkable, delivery.RunSignal{}); err != nil {
		t.Fatalf("SignalRun on a nil supervisor: %v", err)
	}
	if err := s.CancelRun(context.Background(), &delivery.MilestoneRun{}); err != nil {
		t.Fatalf("CancelRun on a nil supervisor: %v", err)
	}
	if err := s.AbandonRun(context.Background(), testOrg, testProject, testMilepost); err != nil {
		t.Fatalf("AbandonRun on a nil supervisor: %v", err)
	}
}

// TestStartRunRefusesWithoutADispatcher: a run that could dispatch nothing must
// not be started, because starting it would burn the version's run row on a
// loop with no way forward.
//
// It REPORTS the refusal rather than swallowing it. Callers that re-offer on a
// timer (adoption, the reconcile sweep) treat the sentinel as nothing to do; the
// build click, which has no timer, settles the row it armed — without which a
// non-terminal row with no workflow behind it would refuse every later build on
// that project forever.
func TestStartRunRefusesWithoutADispatcher(t *testing.T) {
	runs := &countingRuns{}
	s := NewSupervisor(delivery.NewRuntime(configWithNoTemporal()), runs, nil)
	err := s.StartRun(context.Background(), delivery.StartRunRequest{
		OrgID: testOrg, ProjectID: testProject, MilestoneNumber: testMilepost,
		Origin: delivery.RunOriginIncidentAdoption,
	})
	if !errors.Is(err, delivery.ErrRunNotStarted) {
		t.Fatalf("StartRun = %v, want ErrRunNotStarted", err)
	}
	if runs.admits != 0 || runs.lives != 0 {
		t.Fatalf("an unwired dispatcher must not touch the run store (admits=%d lives=%d)", runs.admits, runs.lives)
	}
}

// TestStartRunWaitsForTemporal: with a dispatcher but no connected Temporal
// client, aep-api still boots and serves everything else — but the start is
// REPORTED, not silently dropped. Same reasoning as the dispatcher case above.
func TestStartRunWaitsForTemporal(t *testing.T) {
	runs := &countingRuns{}
	s := NewSupervisor(delivery.NewRuntime(configWithNoTemporal()), runs, fakeDispatcher{})
	err := s.StartRun(context.Background(), delivery.StartRunRequest{
		OrgID: testOrg, ProjectID: testProject, MilestoneNumber: testMilepost,
		Origin: delivery.RunOriginIncidentAdoption,
	})
	if !errors.Is(err, delivery.ErrRunNotStarted) {
		t.Fatalf("StartRun = %v, want ErrRunNotStarted", err)
	}
	if runs.admits != 0 {
		t.Fatalf("no run row may be admitted while the engine that would drive it is down")
	}
}

// TestSignalAndCancelAreInertWhileTemporalIsDown pins the best-effort contract:
// a webhook handler must never fail because the workflow engine is unreachable.
// Cancel is the exception — it is a human's instruction, so it reports honestly
// that it could not be delivered.
func TestSignalAndCancelAreInertWhileTemporalIsDown(t *testing.T) {
	s := NewSupervisor(delivery.NewRuntime(configWithNoTemporal()), &countingRuns{}, fakeDispatcher{})
	row := &delivery.MilestoneRun{ID: testRunID, OrgID: testOrg, ProjectID: testProject, MilestoneNumber: testMilepost}
	if err := s.SignalRun(context.Background(), row, delivery.SigRunWorkable, delivery.RunSignal{}); err != nil {
		t.Fatalf("SignalRun must never fail a webhook: %v", err)
	}
	if err := s.CancelRun(context.Background(), row); !errors.Is(err, delivery.ErrTemporalUnavailable) {
		t.Fatalf("CancelRun error = %v, want ErrTemporalUnavailable", err)
	}
	// Abandon reports for the same reason cancel does: a supervisor this could
	// not reach is still there when the engine comes back, and the project-delete
	// teardown has to be able to say the workflow outlived its project.
	if err := s.AbandonRun(context.Background(), testOrg, testProject, testMilepost); !errors.Is(err, delivery.ErrTemporalUnavailable) {
		t.Fatalf("AbandonRun error = %v, want ErrTemporalUnavailable", err)
	}
}

// inheritingRuns answers the two reads admit makes on the incident path: is
// anybody already on this milestone, and which version does it belong to.
type inheritingRuns struct {
	RunStore
	tag      string
	tagErr   error
	admitted *delivery.MilestoneRun
}

func (r *inheritingRuns) LiveRunForMilestone(context.Context, string, string, int) (*delivery.MilestoneRun, error) {
	return nil, nil
}

func (r *inheritingRuns) MilestoneSpecTag(context.Context, string, string, int) (string, error) {
	return r.tag, r.tagErr
}

func (r *inheritingRuns) TryAdmit(_ context.Context, row *delivery.MilestoneRun) (bool, *delivery.MilestoneRun, error) {
	r.admitted = row
	return true, row, nil
}

// TestAdmitInheritsTheMilestonesVersion: an incident run holds no tag of its
// own — it adopts a milestone somebody else's build claimed. Without inheriting
// that milestone's version it would surface in the version ledger under the
// milestone's GitHub title instead of a `v<N>`.
func TestAdmitInheritsTheMilestonesVersion(t *testing.T) {
	runs := &inheritingRuns{tag: "v4"}
	s := NewSupervisor(delivery.NewRuntime(configWithNoTemporal()), runs, fakeDispatcher{})
	row, err := s.admit(context.Background(), delivery.StartRunRequest{
		OrgID: testOrg, ProjectID: testProject, MilestoneNumber: testMilepost,
		MilestoneTitle: "Phase 1", Origin: delivery.RunOriginIncidentAdoption,
	})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if row.Tag != "v4" || row.SpecTag() != "v4" {
		t.Fatalf("admitted row = %+v, want the milestone's version v4", row)
	}
}

// A version read that fails costs the ledger this run's label, never the run:
// the incident still has to be worked.
func TestAdmitSurvivesAVersionReadFailure(t *testing.T) {
	runs := &inheritingRuns{tagErr: errors.New("db down")}
	s := NewSupervisor(delivery.NewRuntime(configWithNoTemporal()), runs, fakeDispatcher{})
	row, err := s.admit(context.Background(), delivery.StartRunRequest{
		OrgID: testOrg, ProjectID: testProject, MilestoneNumber: testMilepost,
		MilestoneTitle: "Phase 1", Origin: delivery.RunOriginIncidentAdoption,
	})
	if err != nil || row == nil {
		t.Fatalf("admit = (%+v, %v), want the run admitted anyway", row, err)
	}
	if row.Tag != "" {
		t.Fatalf("admitted row tag = %q, want empty — nothing was read", row.Tag)
	}
}

// TestMilestoneRunWorkflowID pins the identity §7 names, which is also the id
// the event plane's signals and the console's cancel both address.
func TestMilestoneRunWorkflowID(t *testing.T) {
	if got := delivery.MilestoneRunWorkflowID("acme", "shop", 7); got != "run-acme-shop-7" {
		t.Fatalf("MilestoneRunWorkflowID = %q, want run-acme-shop-7", got)
	}
}
