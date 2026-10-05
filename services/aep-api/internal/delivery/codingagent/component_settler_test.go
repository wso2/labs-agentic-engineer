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

package codingagent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// ---- fakes -----------------------------------------------------------------

// settleCycles is the settler's store. ListSettling follows the repository's
// contract: rows not deleted, settle_checked_at NULLS FIRST then ended_at,
// at most limit, with the settle stamps taken from the maps.
type settleCycles struct {
	rows        []delivery.RunCycle
	goneAt      map[string]time.Time
	suspendedAt map[string]time.Time
	checkedAt   map[string]time.Time
	deleted     map[string]bool
	marked      map[string]bool
	cleared     map[string]int
}

func newSettleCycles(rows ...delivery.RunCycle) *settleCycles {
	c := &settleCycles{
		rows:        rows,
		goneAt:      map[string]time.Time{},
		suspendedAt: map[string]time.Time{},
		checkedAt:   map[string]time.Time{},
		deleted:     map[string]bool{},
		marked:      map[string]bool{},
		cleared:     map[string]int{},
	}
	for _, r := range rows {
		if r.JobSuspendedAt != nil {
			c.suspendedAt[r.ID] = *r.JobSuspendedAt
		}
	}
	return c
}

func (c *settleCycles) ListSettling(_ context.Context, limit int) ([]delivery.RunCycle, error) {
	var out []delivery.RunCycle
	for _, r := range c.rows {
		if c.deleted[r.ID] {
			continue
		}
		r.PodGoneAt, r.JobSuspendedAt, r.SettleCheckedAt = nil, nil, nil
		if at, ok := c.goneAt[r.ID]; ok {
			r.PodGoneAt = &at
		}
		if at, ok := c.suspendedAt[r.ID]; ok {
			r.JobSuspendedAt = &at
		}
		if at, ok := c.checkedAt[r.ID]; ok {
			r.SettleCheckedAt = &at
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].SettleCheckedAt, out[j].SettleCheckedAt
		switch {
		case a == nil && b != nil:
			return true
		case a != nil && b == nil:
			return false
		case a != nil && b != nil && !a.Equal(*b):
			return a.Before(*b)
		}
		return out[i].EndedAt.Before(*out[j].EndedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (c *settleCycles) NoteSettleChecked(_ context.Context, id string, at time.Time) error {
	c.checkedAt[id] = at
	return nil
}

func (c *settleCycles) MarkJobSuspended(_ context.Context, id string) error {
	c.marked[id] = true
	if _, ok := c.suspendedAt[id]; !ok {
		c.suspendedAt[id] = time.Now()
	}
	return nil
}

func (c *settleCycles) NotePodGone(_ context.Context, id string, at time.Time) error {
	if _, ok := c.goneAt[id]; !ok {
		c.goneAt[id] = at
	}
	return nil
}

func (c *settleCycles) ClearPodGone(_ context.Context, id string) error {
	delete(c.goneAt, id)
	c.cleared[id]++
	return nil
}

func (c *settleCycles) MarkComponentDeleted(_ context.Context, id string) error {
	c.deleted[id] = true
	return nil
}

type fakeDeleter struct {
	names []string
	err   error
}

func (f *fakeDeleter) DeleteComponent(_ context.Context, _, _, name string) error {
	f.names = append(f.names, name)
	return f.err
}

// settled is a cycle that closed an hour ago, optionally with its Job already
// suspended.
func settled(id string, suspended bool) delivery.RunCycle {
	now := time.Now().Add(-time.Hour)
	c := delivery.RunCycle{ID: id, OrgID: "acme", ProjectID: "shop", JobRef: "ca-" + id, Environment: "development", EndedAt: &now, ComponentUID: "uid-" + id}
	if suspended {
		c.JobSuspendedAt = &now
	}
	return c
}

// settlerAt is a settler on a movable clock with a 5-minute grace.
func settlerAt(rt openchoreo.RuntimeClient, jobs jobSuspender, del componentDeleter, cycles settleStore, clock *time.Time) *ComponentSettler {
	return NewComponentSettler(rt, jobs, del, cycles, testWriteTargets(), nil).
		WithGrace(5 * time.Minute).WithClock(func() time.Time { return *clock })
}

// ---- grace (Review Focus 1) ------------------------------------------------

func TestSettler_DeletesOnlyAfterTwoNoPodReadsAGraceApart(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	del := &fakeDeleter{}
	cycles := newSettleCycles(settled("c1", true))
	s := settlerAt(rt, &fakeJobs{}, del, cycles, &clock)
	s.Tick(ctx)
	if len(del.names) != 0 {
		t.Fatal("first no-pod read only notes the time")
	}
	clock = clock.Add(4 * time.Minute)
	s.Tick(ctx)
	if len(del.names) != 0 {
		t.Fatal("inside the grace")
	}
	clock = clock.Add(2 * time.Minute)
	s.Tick(ctx)
	if !reflect.DeepEqual(del.names, []string{"ca-c1"}) || !cycles.deleted["c1"] {
		t.Fatalf("deleted %v", del.names)
	}
}

func TestSettler_EmptySnapshotOnceThenPodAgainKeepsTheComponent(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	del := &fakeDeleter{}
	cycles := newSettleCycles(settled("c1", true))
	s := settlerAt(rt, &fakeJobs{}, del, cycles, &clock)
	s.Tick(ctx) // transient empty tree
	rt.pod = openchoreo.RuntimePod{Found: true, Name: "p", Phase: "Succeeded"}
	clock = clock.Add(6 * time.Minute)
	s.Tick(ctx) // the pod is back: clears pod_gone_at
	if cycles.cleared["c1"] != 1 {
		t.Fatalf("a seen pod must clear pod_gone_at (cleared %d)", cycles.cleared["c1"])
	}
	rt.pod = openchoreo.RuntimePod{Found: false}
	clock = clock.Add(time.Minute)
	s.Tick(ctx) // first no-pod read again
	if len(del.names) != 0 {
		t.Fatalf("deleted %v while a pod was seen inside the window", del.names)
	}
}

// "No pod" is no pod of ANY attempt: the watcher's leftover rule (a previous
// attempt's finished pod) does not apply here, the leftover still blocks.
func TestSettler_APreviousAttemptsPodStillBlocksTheDelete(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	c := settled("c1", true)
	c.Attempts = 2
	dispatched := clock.Add(-30 * time.Minute)
	c.DispatchedAt = &dispatched
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{
		Found: true, Name: "p-attempt-1", Phase: "Succeeded",
		CreatedAt: dispatched.Add(-2 * time.Hour), FinishedAt: dispatched.Add(-time.Hour),
	}}
	del := &fakeDeleter{}
	cycles := newSettleCycles(c)
	s := settlerAt(rt, &fakeJobs{}, del, cycles, &clock)
	for i := 0; i < 3; i++ {
		s.Tick(ctx)
		clock = clock.Add(6 * time.Minute)
	}
	if len(del.names) != 0 || len(cycles.goneAt) != 0 {
		t.Fatalf("deleted %v / gone %v with a leftover pod in the tree", del.names, cycles.goneAt)
	}
}

// A Job that is not suspended (and whose suspend is not known to be
// unsupported) is never deleted, however long its pod has been gone.
func TestSettler_NeverDeletesAnUnsuspendedJob(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	jobs := &fakeJobs{err: errors.New("oc unavailable")}
	del := &fakeDeleter{}
	cycles := newSettleCycles(settled("c1", false))
	s := settlerAt(rt, jobs, del, cycles, &clock)
	for i := 0; i < 3; i++ {
		s.Tick(ctx)
		clock = clock.Add(6 * time.Minute)
	}
	if len(del.names) != 0 || cycles.marked["c1"] {
		t.Fatalf("deleted %v, marked %v", del.names, cycles.marked)
	}
	if len(jobs.suspends) != 3 {
		t.Fatalf("the backstop must retry every pass, got %d suspends", len(jobs.suspends))
	}
}

func TestSettler_DeleteFailureLeavesTheRowSettling(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	del := &fakeDeleter{err: errors.New("oc 500")}
	cycles := newSettleCycles(settled("c1", true))
	s := settlerAt(rt, &fakeJobs{}, del, cycles, &clock)
	s.Tick(ctx)
	clock = clock.Add(6 * time.Minute)
	s.Tick(ctx)
	if len(del.names) != 1 || cycles.deleted["c1"] {
		t.Fatalf("a failed delete must not be recorded (calls %v, deleted %v)", del.names, cycles.deleted)
	}
}

func TestSettler_LogsTheDeleteValueFree(t *testing.T) {
	records := captureLogs(t)
	ctx := context.Background()
	clock := time.Now()
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	cycles := newSettleCycles(settled("c1", true))
	s := settlerAt(rt, &fakeJobs{}, &fakeDeleter{}, cycles, &clock)
	s.Tick(ctx)
	clock = clock.Add(6 * time.Minute)
	s.Tick(ctx)
	got := logsNamed(*records, "codingagent.component_deleted")
	if len(got) != 1 {
		t.Fatalf("component_deleted logged %d times", len(got))
	}
	want := map[string]string{"cycle": "c1", "component": "ca-c1", "componentUid": "uid-c1"}
	if !reflect.DeepEqual(got[0].attrs, want) {
		t.Fatalf("attrs = %v, want %v", got[0].attrs, want)
	}
}

// ---- backstop (Review Focus 2) ---------------------------------------------

func TestSettler_BackstopSuspendsATerminalPod(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p", Phase: "Succeeded"}}
	jobs := &fakeJobs{}
	cycles := newSettleCycles(settled("c1", false))
	NewComponentSettler(rt, jobs, &fakeDeleter{}, cycles, testWriteTargets(), nil).Tick(context.Background())
	if len(jobs.suspends) != 1 || !cycles.marked["c1"] {
		t.Fatalf("suspends %v", jobs.suspends)
	}
}

func TestSettler_BackstopSuspendsWhenNoPodIsLeft(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	jobs := &fakeJobs{}
	cycles := newSettleCycles(settled("c1", false))
	NewComponentSettler(rt, jobs, &fakeDeleter{}, cycles, testWriteTargets(), nil).Tick(context.Background())
	if !reflect.DeepEqual(jobs.suspends, []string{"ca-c1@development"}) || !cycles.marked["c1"] {
		t.Fatalf("suspends %v marked %v", jobs.suspends, cycles.marked)
	}
	if _, noted := cycles.goneAt["c1"]; !noted {
		t.Fatal("once suspended in this pass, the no-pod read counts as the first one")
	}
}

func TestSettler_BackstopLeavesARunningPodToTheWatcher(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p", Phase: "Running"}}
	jobs := &fakeJobs{}
	c := settled("c1", false)
	recent := time.Now().Add(-time.Minute)
	c.EndedAt = &recent // merged a minute ago, pod still writing its result line
	NewComponentSettler(rt, jobs, &fakeDeleter{}, newSettleCycles(c), testWriteTargets(), nil).Tick(context.Background())
	if len(jobs.suspends) != 0 {
		t.Fatal("must not kill a running pod inside the deadline ceiling")
	}
}

func TestSettler_BackstopLeavesAPendingPodInsideTheCeiling(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p", Phase: "Pending"}}
	jobs := &fakeJobs{}
	c := settled("c1", false)
	ended := time.Now().Add(-3 * time.Hour)
	c.EndedAt = &ended
	NewComponentSettler(rt, jobs, &fakeDeleter{}, newSettleCycles(c), testWriteTargets(), nil).Tick(context.Background())
	if len(jobs.suspends) != 0 {
		t.Fatal("3h after close is still inside the 3h10m ceiling")
	}
}

func TestSettler_BackstopSuspendsARunningPodPastTheCeiling(t *testing.T) {
	records := captureLogs(t)
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p", Phase: "Running"}}
	jobs := &fakeJobs{}
	c := settled("c1", false)
	ended := time.Now().Add(-(3*time.Hour + 11*time.Minute))
	c.EndedAt = &ended
	cycles := newSettleCycles(c)
	NewComponentSettler(rt, jobs, &fakeDeleter{}, cycles, testWriteTargets(), nil).Tick(context.Background())
	if len(jobs.suspends) != 1 || !cycles.marked["c1"] {
		t.Fatalf("suspends %v", jobs.suspends)
	}
	got := logsNamed(*records, "codingagent.job_suspended")
	if len(got) != 1 || got[0].attrs["cause"] != "backstop" || got[0].attrs["cycle"] != "c1" {
		t.Fatalf("job_suspended logs = %+v", got)
	}
}

// A cancel whose own suspend failed is suspended at once, whatever the pod is
// doing: the ceiling protects a merge-closed cycle's last line, and a
// cancelled run has none to protect.
func TestSettler_BackstopSuspendsACancelledCycleAtOnce(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p", Phase: "Running"}}
	jobs := &fakeJobs{}
	c := settled("c1", false)
	recent := time.Now().Add(-time.Minute)
	c.EndedAt = &recent
	c.AgentReason = delivery.CycleReasonCancelled
	cycles := newSettleCycles(c)
	NewComponentSettler(rt, jobs, &fakeDeleter{}, cycles, testWriteTargets(), nil).Tick(context.Background())
	if len(jobs.suspends) != 1 || !cycles.marked["c1"] {
		t.Fatalf("suspends %v", jobs.suspends)
	}
}

// The backstop's suspend finding the binding gone marks the cycle suspended
// (nothing left to suspend), as the watcher does.
func TestSettler_BackstopOnAGoneBindingMarksSuspended(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	jobs := &fakeJobs{err: fmt.Errorf("x: %w", openchoreo.ErrNotFound)}
	cycles := newSettleCycles(settled("c1", false))
	NewComponentSettler(rt, jobs, &fakeDeleter{}, cycles, testWriteTargets(), nil).Tick(context.Background())
	if !cycles.marked["c1"] {
		t.Fatal("a gone binding has nothing to suspend")
	}
}

// ---- legacy releases (Q-5 a) -----------------------------------------------

func TestSettler_LegacyWithoutSuspendIsNeverDeletedWithAPod(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p", Phase: "Succeeded"}}
	jobs := &fakeJobs{err: fmt.Errorf("x: %w", openchoreo.ErrSuspendUnsupported)}
	del := &fakeDeleter{}
	cycles := newSettleCycles(settled("c1", false))
	s := settlerAt(rt, jobs, del, cycles, &clock)
	for i := 0; i < 3; i++ {
		s.Tick(ctx)
		clock = clock.Add(6 * time.Minute)
	}
	if len(del.names) != 0 || cycles.marked["c1"] {
		t.Fatalf("legacy component with a pod: deleted %v marked %v", del.names, cycles.marked)
	}
}

func TestSettler_LegacyIsDeletedAfterTheGraceWithNoPod(t *testing.T) {
	records := captureLogs(t)
	ctx := context.Background()
	clock := time.Now()
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	jobs := &fakeJobs{err: fmt.Errorf("x: %w", openchoreo.ErrSuspendUnsupported)}
	del := &fakeDeleter{}
	// A pre-5.6 row: no UID. It is settled by NAME.
	c := settled("c1", false)
	c.ComponentUID = ""
	cycles := newSettleCycles(c)
	s := settlerAt(rt, jobs, del, cycles, &clock)
	s.Tick(ctx)
	if len(del.names) != 0 {
		t.Fatal("first no-pod read only notes the time")
	}
	clock = clock.Add(6 * time.Minute)
	s.Tick(ctx)
	if !reflect.DeepEqual(del.names, []string{"ca-c1"}) || !cycles.deleted["c1"] || cycles.marked["c1"] {
		t.Fatalf("deleted %v recorded %v marked %v", del.names, cycles.deleted, cycles.marked)
	}
	// The release cannot change for a closed cycle: one suspend call learns it.
	if len(jobs.suspends) != 1 {
		t.Fatalf("suspend asked %d times for a legacy release", len(jobs.suspends))
	}
	if n := len(logsNamed(*records, "codingagent.job_suspend_unsupported")); n != 1 {
		t.Fatalf("legacy warning logged %d times, want once", n)
	}
}

// A legacy pod that OC re-creates after a no-pod read resets the grace.
func TestSettler_LegacyPodReappearingResetsTheGrace(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	jobs := &fakeJobs{err: fmt.Errorf("x: %w", openchoreo.ErrSuspendUnsupported)}
	del := &fakeDeleter{}
	cycles := newSettleCycles(settled("c1", false))
	s := settlerAt(rt, jobs, del, cycles, &clock)
	s.Tick(ctx)
	clock = clock.Add(3 * time.Minute)
	rt.pod = openchoreo.RuntimePod{Found: true, Name: "p2", Phase: "Running"}
	s.Tick(ctx)
	clock = clock.Add(3 * time.Minute)
	rt.pod = openchoreo.RuntimePod{Found: false}
	s.Tick(ctx)
	if len(del.names) != 0 {
		t.Fatalf("deleted %v: the pod seen in between resets the grace", del.names)
	}
}

// ---- identity, idempotence, fairness ---------------------------------------

func TestSettler_BindingGoneMarksDeleted(t *testing.T) {
	rt := &fakeRuntime{bindingErr: fmt.Errorf("x: %w", openchoreo.ErrNotFound)}
	del := &fakeDeleter{}
	cycles := newSettleCycles(settled("c1", true))
	NewComponentSettler(rt, &fakeJobs{}, del, cycles, testWriteTargets(), nil).Tick(context.Background())
	if !cycles.deleted["c1"] {
		t.Fatal("a component someone else deleted is recorded as deleted")
	}
	if len(del.names) != 0 {
		t.Fatal("nothing is left to delete")
	}
}

func TestSettler_ReadFailuresDecideNothing(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	rt := &fakeRuntime{podErr: errors.New("oc 503")}
	del := &fakeDeleter{}
	cycles := newSettleCycles(settled("c1", true))
	cycles.goneAt["c1"] = clock.Add(-time.Hour)
	s := settlerAt(rt, &fakeJobs{}, del, cycles, &clock)
	s.Tick(ctx)
	rt.podErr, rt.bindingErr = nil, errors.New("oc 503")
	s.Tick(ctx)
	if len(del.names) != 0 || cycles.deleted["c1"] || cycles.cleared["c1"] != 0 {
		t.Fatalf("a failed read is no evidence: deleted %v cleared %d", del.names, cycles.cleared["c1"])
	}
}

func TestSettler_FallsBackToTheWriteTarget(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	c := settled("c1", true)
	c.Environment = ""
	targets := &fakeWriteTargets{env: "staging"}
	NewComponentSettler(rt, &fakeJobs{}, &fakeDeleter{}, newSettleCycles(c), targets, nil).Tick(context.Background())
	if !reflect.DeepEqual(rt.bindingEnvs, []string{"staging"}) {
		t.Fatalf("binding read in %v", rt.bindingEnvs)
	}
}

func TestSettler_StampsEveryRowItVisits(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p", Phase: "Succeeded"}}
	jobs := &fakeJobs{err: errors.New("bound release is missing")}
	cycles := newSettleCycles(settled("c1", false), settled("c2", false))
	NewComponentSettler(rt, jobs, &fakeDeleter{}, cycles, testWriteTargets(), nil).Tick(context.Background())
	if len(cycles.checkedAt) != 2 {
		t.Fatalf("checked %v: a failing row must still be stamped so it rotates back", cycles.checkedAt)
	}
}

// More stuck rows than one pass lists (legacy Components with a pod, or a
// binding naming a missing release) must not starve a cycle that closes later.
func TestSettler_StuckRowsBeyondTheLimitDoNotStarveANewCycle(t *testing.T) {
	ctx := context.Background()
	clock := time.Now()
	rt := &perComponentRuntime{pods: map[string]openchoreo.RuntimePod{}}
	jobs := &fakeJobs{err: errors.New("bound release is missing")}
	var rows []delivery.RunCycle
	for i := 0; i < settleBatch+50; i++ {
		c := settled(fmt.Sprintf("stuck-%03d", i), false)
		ended := clock.Add(-48*time.Hour + time.Duration(i)*time.Second)
		c.EndedAt = &ended
		rows = append(rows, c)
		rt.pods[c.JobRef] = openchoreo.RuntimePod{Found: true, Name: "p", Phase: "Succeeded"}
	}
	fresh := settled("fresh", true)
	rows = append(rows, fresh)
	cycles := newSettleCycles(rows...)
	del := &fakeDeleter{}
	s := settlerAt(rt, jobs, del, cycles, &clock)
	for i := 0; i < 12; i++ {
		s.Tick(ctx)
		clock = clock.Add(time.Minute)
	}
	if !reflect.DeepEqual(del.names, []string{"ca-fresh"}) {
		t.Fatalf("deleted %v: the fresh cycle was starved", del.names)
	}
}

// perComponentRuntime answers each Component's binding with its own pod.
type perComponentRuntime struct {
	fakeRuntime
	pods map[string]openchoreo.RuntimePod
}

func (f *perComponentRuntime) ReleaseBindingName(_ context.Context, _, _, component, _ string) (string, error) {
	return component, nil
}

func (f *perComponentRuntime) PodSnapshot(_ context.Context, _, binding string) (openchoreo.RuntimePod, error) {
	return f.pods[binding], nil
}

func TestSettler_RunsUnderTheServiceIdentity(t *testing.T) {
	type key struct{}
	rt := &identityRuntime{key: key{}}
	asService := func(ctx context.Context) context.Context { return context.WithValue(ctx, key{}, "svc") }
	NewComponentSettler(rt, &fakeJobs{}, &fakeDeleter{}, newSettleCycles(settled("c1", true)), testWriteTargets(), asService).
		Tick(context.Background())
	if !rt.sawService {
		t.Fatal("OC reads must run under the service identity")
	}
}

type identityRuntime struct {
	fakeRuntime
	key        any
	sawService bool
}

func (f *identityRuntime) ReleaseBindingName(ctx context.Context, _, _, _, _ string) (string, error) {
	f.sawService = ctx.Value(f.key) == "svc"
	return "rb", nil
}
