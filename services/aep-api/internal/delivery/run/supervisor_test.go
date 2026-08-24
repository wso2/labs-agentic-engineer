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

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

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
		Kind: delivery.RunKindTask, Origin: delivery.RunOriginIncidentAdoption,
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
		Kind: delivery.RunKindTask, Origin: delivery.RunOriginIncidentAdoption,
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
		MilestoneTitle: "Phase 1", Kind: delivery.RunKindTask, Origin: delivery.RunOriginIncidentAdoption,
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
		MilestoneTitle: "Phase 1", Kind: delivery.RunKindTask, Origin: delivery.RunOriginIncidentAdoption,
	})
	if err != nil || row == nil {
		t.Fatalf("admit = (%+v, %v), want the run admitted anyway", row, err)
	}
	if row.Tag != "" {
		t.Fatalf("admitted row tag = %q, want empty — nothing was read", row.Tag)
	}
}

// TestMilestoneRunWorkflowID pins the id the event plane's signals and the
// console's cancel both address — and, above all, that a run's KIND is part of
// it.
//
// The prefix is not decoration. Ids are reused
// (WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE), so one grammar for all three kinds
// would let a milestone's dev run, its validation run and its task run claim the
// same id in turn: a stale merge signal aimed at a settled dev run would land on
// whichever run claimed the id afterwards, and that run would act on a merge that
// was never its own.
func TestMilestoneRunWorkflowID(t *testing.T) {
	cases := []struct{ kind, want string }{
		{delivery.RunKindDev, "dev-acme-shop-7"},
		{delivery.RunKindValidation, "validation-acme-shop-7"},
		{delivery.RunKindTask, "task-acme-shop-7"},
		// A row written before the kind column existed can only have been a dev
		// run, and dev is the id a pre-split execution already answers to.
		{"", "dev-acme-shop-7"},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		got := delivery.MilestoneRunWorkflowID(c.kind, "acme", "shop", 7)
		if got != c.want {
			t.Fatalf("MilestoneRunWorkflowID(%q) = %q, want %q", c.kind, got, c.want)
		}
		seen[got] = true
	}
	// Three DISTINCT ids for one milestone. If two of them ever collapsed, a
	// signal for one species would be delivered to another.
	if len(seen) != 3 {
		t.Fatalf("the three kinds produced %d distinct ids, want 3: %v", len(seen), seen)
	}
}

// TestRunWorkflowName pins the workflow TYPE a run of each kind executes, and
// that an unroutable kind yields nothing rather than a guess: silently starting a
// dev workflow over an unclassified row would take the project's build mutex on
// behalf of a run nobody asked for.
func TestRunWorkflowName(t *testing.T) {
	cases := []struct{ kind, want string }{
		{delivery.RunKindDev, delivery.DevRunWorkflowName},
		{delivery.RunKindValidation, delivery.ValidationRunWorkflowName},
		{delivery.RunKindTask, delivery.TaskRunWorkflowName},
		{"", ""},
		{"nonsense", ""},
	}
	for _, c := range cases {
		if got := delivery.RunWorkflowName(c.kind); got != c.want {
			t.Fatalf("RunWorkflowName(%q) = %q, want %q", c.kind, got, c.want)
		}
	}
	// And the three names are distinct, for the reason the ids are: one task
	// queue serves all three, and two kinds sharing a type would execute the
	// wrong loop.
	if delivery.DevRunWorkflowName == delivery.ValidationRunWorkflowName ||
		delivery.DevRunWorkflowName == delivery.TaskRunWorkflowName ||
		delivery.ValidationRunWorkflowName == delivery.TaskRunWorkflowName {
		t.Fatal("two run kinds share a workflow type")
	}
}

// TestRoutableRunKind pins the resolution the two SILENT call sites depend on —
// the id a signal is aimed at, and the type a start executes — including the
// rows it must refuse.
//
// The refusal is the half that matters. Every unroutable row has exactly one
// reading available to it, `dev`, and that is the kind which takes the project's
// build mutex and plans a version: guessing it starts a build nobody asked for
// and blocks every later one behind it. A corrupt row must fail visibly instead.
func TestRoutableRunKind(t *testing.T) {
	cases := []struct {
		kind, origin string
		want         string
		routable     bool
	}{
		// The ordinary case: the kind is what the row says it is, whatever the
		// origin beside it.
		{delivery.RunKindValidation, delivery.RunOriginSpecBuild, delivery.RunKindValidation, true},
		{delivery.RunKindDev, delivery.RunOriginSpecBuild, delivery.RunKindDev, true},
		{delivery.RunKindTask, delivery.RunOriginIncidentAdoption, delivery.RunKindTask, true},
		// The pre-column case, and the only reason a fallback exists at all: a row
		// backfilled from a deployment older than the column, or a Temporal input
		// replaying out of history without the field.
		{"", delivery.RunOriginRevalidate, delivery.RunKindValidation, true},
		{"", delivery.RunOriginIncidentAdoption, delivery.RunKindTask, true},
		{"", delivery.RunOriginSpecBuild, delivery.RunKindDev, true},
		// Nothing to route on: neither column says anything this package knows.
		{"", "", "", false},
		{"", "made-up-origin", "", false},
		// A non-empty kind nobody recognises is CORRUPTION, not history —
		// admission validates against IsRunKind, so no writer can produce one —
		// and re-reading it through the origin would be the guess this refuses.
		{"typo", "", "", false},
		{"typo", delivery.RunOriginSpecBuild, "", false},
	}
	for _, c := range cases {
		got, routable := delivery.RoutableRunKind(c.kind, c.origin)
		if got != c.want || routable != c.routable {
			t.Fatalf("RoutableRunKind(%q, %q) = (%q, %v), want (%q, %v)",
				c.kind, c.origin, got, routable, c.want, c.routable)
		}
	}
}

// TestAbandonRunTerminatesEveryKindsWorkflow: a project delete has to end the
// supervisor over every id a milestone could be running under, not the one that
// happens to be live.
//
// There is no row left to ask. The run rows are purged in the same teardown, so
// by the time abandon runs the platform cannot tell whether that milestone ever
// had a validation run or a task run — only that it could have. A kind missed
// here leaves a workflow retrying its milestone poll forever (Temporal's default
// policy is unbounded) against a repository that is gone, and squatting on an id
// that any later same-named project's first run is refused as AlreadyStarted on.
func TestAbandonRunTerminatesEveryKindsWorkflow(t *testing.T) {
	got := abandonWorkflowIDs(testOrg, testProject, testMilepost)
	want := []string{
		"dev-" + testOrg + "-" + testProject + "-7",
		"task-" + testOrg + "-" + testProject + "-7",
		"validation-" + testOrg + "-" + testProject + "-7",
	}
	if len(got) != len(delivery.RunKinds) {
		t.Fatalf("abandonWorkflowIDs = %v (%d ids), want one per run kind (%d)",
			got, len(got), len(delivery.RunKinds))
	}
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for _, id := range want {
		if !seen[id] {
			t.Fatalf("abandonWorkflowIDs = %v, missing %q — that supervisor outlives its project", got, id)
		}
	}
}

// TestRegisterPutsEveryWorkflowOnOneQueueWithoutColliding is the worker-boot
// guard.
//
// Temporal registers an activity by its reflected METHOD NAME, so two activity
// structs sharing any method name panic the worker — and three structs carved out
// of one loop would share a great many. The panic is raised at registration,
// which the watcher performs immediately before worker.Start, so it surfaces as a
// boot-time crash whose stack names neither workflow. ONE Activities struct with
// three workflows taking method expressions off it is the only shape that cannot
// break that way.
//
// The second half is what stops this passing vacuously: a deliberately colliding
// second struct MUST panic, which is the failure the first half proves is absent.
func TestRegisterPutsEveryWorkflowOnOneQueueWithoutColliding(t *testing.T) {
	// A lazy client never dials, so no Temporal server is involved. The worker is
	// real, which is the point — the registry doing the rejecting is the real one.
	c, err := client.NewLazyClient(client.Options{HostPort: "127.0.0.1:1", Namespace: "default"})
	if err != nil {
		t.Fatalf("NewLazyClient: %v", err)
	}
	defer c.Close()
	wk := worker.New(c, "aep-delivery", worker.Options{})

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Register panicked — the three workflows and the one Activities "+
					"struct must coexist on one queue: %v", r)
			}
		}()
		Register(wk, NewActivities(Deps{}))
	}()

	// Now prove the guard is real: a second struct carrying ANY of the same method
	// names is rejected. This is the crash the one-struct shape exists to avoid.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("registering a second activity struct with a colliding method name " +
					"was accepted; this test can no longer detect the duplicate-name panic")
			}
		}()
		wk.RegisterActivity(&collidingActivities{})
	}()
}

// collidingActivities exists only to be rejected: PollMilestone is already
// registered by the real Activities struct, and Temporal keys activities by
// method name.
type collidingActivities struct{}

func (*collidingActivities) PollMilestone(context.Context, MilestoneRef) (MilestoneSnapshot, error) {
	return MilestoneSnapshot{}, nil
}
