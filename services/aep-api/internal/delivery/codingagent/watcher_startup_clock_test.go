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
	"reflect"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// watcher_startup_clock_test.go — the start deadline counts from when the
// attempt's Job or pod exists, not from the dispatch. Cloud OpenChoreo applies
// a release 8-13 min after it is requested, so a grace from dispatched_at
// failed every Cloud coding run before its pod existed. Until the Job is
// applied, the attempt waits as NotYetApplied under a longer apply cap.

// attemptCycle is an open cycle on attempt n, dispatched ago.
func attemptCycle(id string, n int, ago time.Duration) delivery.RunCycle {
	c := dispatchedCycle(id, ago)
	at := time.Now().UTC().Add(-ago)
	c.Attempts, c.DispatchedAt, c.UpdatedAt = n, &at, at
	return c
}

// noteWaitOnRow copies the fake store's last startup-wait note onto the row,
// as the real store would, so the next tick lists it.
func noteWaitOnRow(cycles *watchedCycles) {
	if len(cycles.waits) == 0 {
		return
	}
	last := cycles.waits[len(cycles.waits)-1]
	cycles.rows[0].StartupWaitReason, cycles.rows[0].StartupWaitSince = last.reason, &last.at
}

// No Job twelve minutes after the dispatch (past the old 10-min grace): the
// cycle stays open, waiting NotYetApplied (noted once, logged with the reason
// only), with no start clock, so the run view fails it at dispatch + apply cap.
func TestTick_StartupWaitIsNotYetAppliedWhileNoJob(t *testing.T) {
	logs := captureLogs(t)
	rt := &fakeRuntime{}
	cycles := newWatchedCycles(attemptCycle("c1", 1, 12*time.Minute))
	jobs := &fakeJobs{}
	w := NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil)

	for i := 0; i < missingTicksToFail+1; i++ {
		w.Tick(context.Background())
		noteWaitOnRow(cycles)
	}

	if len(cycles.finished) != 0 || len(jobs.suspends) != 0 {
		t.Fatalf("inside the apply cap nothing closes: finished %v, suspends %v", cycles.finished, jobs.suspends)
	}
	if len(cycles.waits) != 1 || cycles.waits[0].reason != "NotYetApplied" {
		t.Fatalf("waits = %+v, want one NotYetApplied note", cycles.waits)
	}
	if len(cycles.clocks) != 0 || cycles.rows[0].StartupClockAt != nil {
		t.Fatalf("no Job, no start clock: %+v", cycles.clocks)
	}
	got := logsNamed(*logs, "codingagent.startup_wait")
	want := map[string]string{"cycle": "c1", "component": cycles.rows[0].JobRef, "reason": "NotYetApplied"}
	if len(got) != 1 || !reflect.DeepEqual(got[0].attrs, want) {
		t.Fatalf("startup_wait events = %+v, want one with %v", got, want)
	}
	if want := cycles.rows[0].DispatchedAt.Add(30 * time.Minute); !cycles.rows[0].StartupDeadline().Equal(want) {
		t.Fatalf("deadline = %v, want dispatch + 30m", cycles.rows[0].StartupDeadline())
	}
}

// The Cloud sequence: no Job, then the Job (applied 12 min after the
// dispatch), then its pod Pending Unschedulable. The grace counts from the
// Job's creation: one second inside it the cycle is open and waits
// Unschedulable; one second past it the F1 close lands and the Job is
// suspended.
func TestTick_StartupGraceCountsFromTheJobsCreation(t *testing.T) {
	for name, tc := range map[string]struct {
		sinceJob   time.Duration
		wantClosed bool
	}{
		"grace - 1s": {delivery.CycleStartupGrace - time.Second, false},
		"grace + 1s": {delivery.CycleStartupGrace + time.Second, true},
	} {
		t.Run(name, func(t *testing.T) {
			jobCreated := time.Now().UTC().Add(-tc.sinceJob)
			c := attemptCycle("c1", 1, 12*time.Minute+tc.sinceJob)
			c.Environment = "development"
			rt := &fakeRuntime{}
			cycles := newWatchedCycles(c)
			jobs := &fakeJobs{}
			w := NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil)

			w.Tick(context.Background()) // no Job
			noteWaitOnRow(cycles)
			rt.pod = openchoreo.RuntimePod{JobFound: true, JobCreatedAt: jobCreated}
			w.Tick(context.Background()) // the Job, no pod yet
			if len(cycles.clocks) != 1 || !cycles.clocks[0].at.Equal(jobCreated) || cycles.clocks[0].attempt != 1 {
				t.Fatalf("clocks = %+v, want one at the Job's creation %v", cycles.clocks, jobCreated)
			}
			if cycles.clears["c1"] != 1 {
				t.Fatalf("the applied Job ends the NotYetApplied wait: clears %v", cycles.clears)
			}
			cycles.rows[0].StartupWaitReason, cycles.rows[0].StartupWaitSince = "", nil

			pod := unschedulablePod()
			pod.CreatedAt, pod.JobFound, pod.JobCreatedAt = jobCreated.Add(2*time.Second), true, jobCreated
			rt.pod = pod
			w.Tick(context.Background())

			if len(cycles.clocks) != 1 {
				t.Fatalf("the start clock is written once per attempt: %+v", cycles.clocks)
			}
			if !tc.wantClosed {
				if len(cycles.finished) != 0 || len(jobs.suspends) != 0 {
					t.Fatalf("inside the grace from the Job: finished %v, suspends %v", cycles.finished, jobs.suspends)
				}
				if n := len(cycles.waits); n == 0 || cycles.waits[n-1].reason != "Unschedulable" {
					t.Fatalf("waits = %+v, want Unschedulable last", cycles.waits)
				}
				return
			}
			if want := "startup_failed:Unschedulable: " + unschedulableMsg; cycles.finished["c1"] != want {
				t.Fatalf("finished = %q, want %q", cycles.finished["c1"], want)
			}
			if !reflect.DeepEqual(jobs.suspends, []string{c.JobRef + "@development"}) || !cycles.suspended["c1"] {
				t.Fatalf("suspends %v, marked %v", jobs.suspends, cycles.suspended)
			}
		})
	}
}

// The Job is never applied: past the apply cap (sustained, like any empty
// snapshot) the cycle closes startup_failed:not_applied and the Job binding
// is suspended anyway, so a Job OpenChoreo applies late is created suspended
// and never starts an agent on the closed cycle.
func TestTick_StartupFailsNotAppliedPastTheApplyCap(t *testing.T) {
	rt := &fakeRuntime{}
	cycles := newWatchedCycles(attemptCycle("c1", 1, delivery.CycleApplyCap+time.Second))
	jobs := &fakeJobs{}
	w := NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil)
	for i := 0; i < missingTicksToFail; i++ {
		w.Tick(context.Background())
	}
	if cycles.finished["c1"] != "startup_failed:not_applied" {
		t.Fatalf("finished = %v, want startup_failed:not_applied", cycles.finished)
	}
	if len(jobs.suspends) != 1 || !cycles.suspended["c1"] {
		t.Fatalf("suspends %v, marked %v: not_applied always suspends", jobs.suspends, cycles.suspended)
	}
}

// A re-dispatch reuses the cycle's Component and release, so attempt 2's tree
// can still hold attempt 1's completed Job and its finished pod. That Job says
// nothing about attempt 2 (Kubernetes never starts a new pod for a complete
// Job; OpenChoreo re-creates it after its TTL): attempt 2 waits NotYetApplied
// under the apply cap. The Job OpenChoreo re-creates after the dispatch is
// attempt 2's and starts the clock; its pod stuck past the grace from it
// closes the F1 way.
func TestTick_StartupClockOnARedispatchIgnoresTheOldJob(t *testing.T) {
	for name, tc := range map[string]struct {
		sinceJob   time.Duration
		wantClosed bool
	}{
		"grace - 1s": {delivery.CycleStartupGrace - time.Second, false},
		"grace + 1s": {delivery.CycleStartupGrace + time.Second, true},
	} {
		t.Run(name, func(t *testing.T) {
			now := time.Now().UTC()
			newJob := now.Add(-tc.sinceJob)
			c := attemptCycle("c1", 2, 11*time.Minute+tc.sinceJob) // the new Job comes 11 min after the dispatch
			c.Environment = "development"
			old := c.DispatchedAt.Add(-45 * time.Minute)
			rt := &fakeRuntime{pod: openchoreo.RuntimePod{
				Found: true, Name: "p1", Phase: "Succeeded", CreatedAt: old.Add(time.Minute), FinishedAt: old.Add(15 * time.Minute),
				JobFound: true, JobCreatedAt: old,
			}}
			cycles := newWatchedCycles(c)
			jobs := &fakeJobs{}
			w := NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil)

			for i := 0; i < missingTicksToFail+1; i++ {
				w.Tick(context.Background())
				noteWaitOnRow(cycles)
			}
			if len(cycles.finished) != 0 || len(jobs.suspends) != 0 || len(cycles.clocks) != 0 {
				t.Fatalf("only the old Job: finished %v, suspends %v, clocks %+v", cycles.finished, jobs.suspends, cycles.clocks)
			}
			if len(cycles.waits) != 1 || cycles.waits[0].reason != WaitNotYetApplied {
				t.Fatalf("waits = %+v, want one NotYetApplied", cycles.waits)
			}
			if want := c.DispatchedAt.Add(delivery.CycleApplyCap); !cycles.rows[0].StartupDeadline().Equal(want) {
				t.Fatalf("deadline = %v, want dispatch + apply cap %v", cycles.rows[0].StartupDeadline(), want)
			}

			pod := unschedulablePod()
			pod.CreatedAt, pod.JobFound, pod.JobCreatedAt = newJob, true, newJob
			rt.pod = pod
			w.Tick(context.Background())
			if len(cycles.clocks) != 1 || !cycles.clocks[0].at.Equal(newJob) || cycles.clocks[0].attempt != 2 {
				t.Fatalf("clocks = %+v, want one at the new Job's (and pod's) creation %v", cycles.clocks, newJob)
			}
			if !tc.wantClosed {
				if len(cycles.finished) != 0 || len(jobs.suspends) != 0 {
					t.Fatalf("inside the grace from the new Job: finished %v, suspends %v", cycles.finished, jobs.suspends)
				}
				return
			}
			if want := "startup_failed:Unschedulable: " + unschedulableMsg; cycles.finished["c1"] != want {
				t.Fatalf("finished = %q, want %q", cycles.finished["c1"], want)
			}
			if !reflect.DeepEqual(jobs.suspends, []string{c.JobRef + "@development"}) {
				t.Fatalf("suspends %v", jobs.suspends)
			}
		})
	}
}

// The re-created Job is seen before its pod: the clock starts at that Job's
// creation, which is after the dispatch.
func TestTick_StartupClockStartsAtAJobCreatedAfterTheRedispatch(t *testing.T) {
	c := attemptCycle("c1", 2, 12*time.Minute)
	newJob := c.DispatchedAt.Add(11 * time.Minute)
	cycles := newWatchedCycles(c)
	NewJobWatcher(&fakeRuntime{pod: openchoreo.RuntimePod{JobFound: true, JobCreatedAt: newJob}}, cycles, testWriteTargets(), &fakeJobs{}, nil).
		Tick(context.Background())
	if len(cycles.clocks) != 1 || !cycles.clocks[0].at.Equal(newJob) {
		t.Fatalf("clocks = %+v, want one at the new Job's creation %v", cycles.clocks, newJob)
	}
	if len(cycles.waits) != 0 || len(cycles.finished) != 0 {
		t.Fatalf("waits %+v, finished %v", cycles.waits, cycles.finished)
	}
}

// Attempt 2 with only attempt 1's Job past the apply cap: OpenChoreo never
// re-created it for this attempt. Closed not_applied, and the Job binding is
// suspended (R-7).
func TestTick_StartupFailsNotAppliedOnARedispatchWithOnlyTheOldJob(t *testing.T) {
	c := attemptCycle("c1", 2, delivery.CycleApplyCap+time.Second)
	old := c.DispatchedAt.Add(-time.Hour)
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{JobFound: true, JobCreatedAt: old}}
	cycles := newWatchedCycles(c)
	jobs := &fakeJobs{}
	w := NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil)
	for i := 0; i < missingTicksToFail; i++ {
		w.Tick(context.Background())
	}
	if cycles.finished["c1"] != ReasonNotApplied {
		t.Fatalf("finished = %v, want %s", cycles.finished, ReasonNotApplied)
	}
	if len(jobs.suspends) != 1 || !cycles.suspended["c1"] {
		t.Fatalf("suspends %v, marked %v", jobs.suspends, cycles.suspended)
	}
}

// Attempt 1 keeps the rule as it was: its dispatch is stamped after the launch,
// so its own Job may be older than dispatched_at by more than the skew, and it
// still starts the clock (clamped to the dispatch).
func TestTick_StartupClockOnAttemptOneTakesAJobOlderThanTheDispatch(t *testing.T) {
	c := attemptCycle("c1", 1, 5*time.Minute)
	job := c.DispatchedAt.Add(-2 * time.Minute)
	cycles := newWatchedCycles(c)
	NewJobWatcher(&fakeRuntime{pod: openchoreo.RuntimePod{JobFound: true, JobCreatedAt: job}}, cycles, testWriteTargets(), &fakeJobs{}, nil).
		Tick(context.Background())
	if len(cycles.clocks) != 1 || !cycles.clocks[0].at.Equal(*c.DispatchedAt) {
		t.Fatalf("clocks = %+v, want one at the dispatch %v", cycles.clocks, *c.DispatchedAt)
	}
	if len(cycles.waits) != 0 {
		t.Fatalf("attempt 1's Job is applied: waits %+v", cycles.waits)
	}
}

// A clock already on the row is the attempt's: it is not rewritten, and the
// deadline is the grace from it — 25 min after the dispatch, a pod stuck since
// a Job applied 5 min ago is still inside its grace.
func TestTick_RecordedStartupClockDecidesTheDeadline(t *testing.T) {
	clock := time.Now().UTC().Add(-5 * time.Minute)
	c := attemptCycle("c1", 1, 25*time.Minute)
	c.StartupClockAt = &clock
	pod := unschedulablePod()
	pod.CreatedAt, pod.JobFound, pod.JobCreatedAt = clock, true, clock
	cycles := newWatchedCycles(c)
	jobs := &fakeJobs{}
	NewJobWatcher(&fakeRuntime{pod: pod}, cycles, testWriteTargets(), jobs, nil).Tick(context.Background())
	if len(cycles.finished) != 0 || len(jobs.suspends) != 0 || len(cycles.clocks) != 0 {
		t.Fatalf("finished %v, suspends %v, clocks %+v", cycles.finished, jobs.suspends, cycles.clocks)
	}
}

// A clock write that landed on no row (another replica wrote it first, or a
// re-dispatch moved the row to the next attempt) decides nothing on this tick:
// the deadline stays the apply cap until a tick reads the row's own clock.
func TestTick_StartupClockWriteThatDidNotLandDecidesNothing(t *testing.T) {
	jobCreated := time.Now().UTC().Add(-20 * time.Minute)
	pod := unschedulablePod()
	pod.CreatedAt, pod.JobFound, pod.JobCreatedAt = jobCreated, true, jobCreated
	cycles := newWatchedCycles(attemptCycle("c1", 1, 25*time.Minute))
	cycles.refuseClock = true
	jobs := &fakeJobs{}
	NewJobWatcher(&fakeRuntime{pod: pod}, cycles, testWriteTargets(), jobs, nil).Tick(context.Background())
	if len(cycles.finished) != 0 || len(jobs.suspends) != 0 {
		t.Fatalf("finished %v, suspends %v", cycles.finished, jobs.suspends)
	}
}
