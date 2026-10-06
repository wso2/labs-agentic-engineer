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
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// watcher_startup_test.go — an agent whose pod cannot start: the durable
// waiting state while it waits, then the startup_failed close, and the Job
// suspend that keeps the pod from starting later on a closed cycle.

const unschedulableMsg = "0/1 nodes are available: 1 Insufficient memory."

func unschedulablePod() openchoreo.RuntimePod {
	return openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Pending", WaitingReason: "Unschedulable", Message: unschedulableMsg}
}

// closedStartupFailed is c as the repository leaves it after the watcher's
// startup close: ended, with the startup_failed reason, and not yet suspended.
func closedStartupFailed(c delivery.RunCycle) delivery.RunCycle {
	ended := time.Now().UTC().Add(-time.Minute)
	c.EndedAt = &ended
	c.AgentReason = "startup_failed:Unschedulable: " + unschedulableMsg
	return c
}

// The F1 path: a pod that never schedules is closed startup_failed at the end
// of the grace, and its Job is suspended in the same tick — after the close,
// which is the write that decides the verdict — so the pod Kubernetes would
// schedule once room frees never starts an agent on a closed cycle. The
// second tick (the row now closed and stamped) suspends nothing again.
func TestTick_StartupFailureSuspendsTheJob(t *testing.T) {
	logs := captureLogs(t)
	rt := &fakeRuntime{pod: unschedulablePod()}
	c := dispatchedCycle("c1", 20*time.Minute)
	c.Environment = "development"
	cycles := newWatchedCycles(c)
	closedFirst := false
	jobs := &fakeJobs{before: func() { _, closedFirst = cycles.finished["c1"] }}
	w := NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil).WithIntervals(time.Millisecond, 10*time.Minute)

	w.Tick(context.Background())

	if want := "startup_failed:Unschedulable: " + unschedulableMsg; cycles.finished["c1"] != want {
		t.Fatalf("finished = %q, want %q", cycles.finished["c1"], want)
	}
	if !closedFirst {
		t.Fatal("the cycle must be closed before its Job is suspended")
	}
	if !reflect.DeepEqual(jobs.suspends, []string{c.JobRef + "@development"}) || !cycles.suspended["c1"] {
		t.Fatalf("suspends %v, marked %v", jobs.suspends, cycles.suspended)
	}
	got := logsNamed(*logs, "codingagent.job_suspended")
	want := map[string]string{"cycle": "c1", "component": c.JobRef, "cause": "startup_failed"}
	if len(got) != 1 || !reflect.DeepEqual(got[0].attrs, want) {
		t.Fatalf("job_suspended events = %+v, want one with %v", got, want)
	}

	// What the store now holds: closed and stamped.
	row := closedStartupFailed(cycles.rows[0])
	now := time.Now().UTC()
	row.JobSuspendedAt = &now
	cycles.rows[0] = row
	w.Tick(context.Background())
	if len(jobs.suspends) != 1 || len(cycles.finished) != 1 {
		t.Fatalf("second tick: suspends %v, finished %v — nothing is left to do", jobs.suspends, cycles.finished)
	}
}

// A startup close of a pod that was never even created (no_pod_scheduled)
// suspends the Job too: the Job could still create the pod later.
func TestTick_StartupFailureWithNoPodSuspendsTheJob(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{}}
	cycles := newWatchedCycles(dispatchedCycle("c1", 20*time.Minute))
	jobs := &fakeJobs{}
	w := NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil).WithIntervals(time.Millisecond, 10*time.Minute)
	for i := 0; i < missingTicksToFail; i++ {
		w.Tick(context.Background())
	}
	if cycles.finished["c1"] != "startup_failed:no_pod_scheduled" {
		t.Fatalf("finished = %v", cycles.finished)
	}
	if len(jobs.suspends) != 1 || !cycles.suspended["c1"] {
		t.Fatalf("suspends %v, marked %v", jobs.suspends, cycles.suspended)
	}
}

// Another replica won the close (or a restart fell between the close and the
// suspend, or the row predates this rule): this tick's close loses and
// suspends nothing; the next tick reads a closed startup_failed cycle with a
// live pod and no suspend stamp, and suspends it — whether the pod is still
// Pending or has meanwhile started (the zombie this exists to stop).
func TestTick_ClosedStartupFailedCycleIsSuspendedOnALaterTick(t *testing.T) {
	for _, phase := range []string{"Pending", "Running"} {
		t.Run(phase, func(t *testing.T) {
			logs := captureLogs(t)
			pod := unschedulablePod()
			pod.Phase = phase
			rt := &fakeRuntime{pod: pod}
			cycles := newWatchedCycles(dispatchedCycle("c1", 20*time.Minute))
			cycles.finished["c1"] = "startup_failed:Unschedulable" // the other replica's write
			jobs := &fakeJobs{}
			w := NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil).WithIntervals(time.Millisecond, 10*time.Minute)

			if phase == "Pending" {
				w.Tick(context.Background())
				if len(jobs.suspends) != 0 {
					t.Fatalf("a lost close must not suspend on this tick: %v", jobs.suspends)
				}
			}

			cycles.rows[0] = closedStartupFailed(cycles.rows[0])
			cycles.waits = nil // the open tick above may have noted the wait
			w.Tick(context.Background())
			if len(jobs.suspends) != 1 || !cycles.suspended["c1"] {
				t.Fatalf("suspends %v, marked %v", jobs.suspends, cycles.suspended)
			}
			got := logsNamed(*logs, "codingagent.job_suspended")
			if len(got) != 1 || got[0].attrs["cause"] != "startup_failed" {
				t.Fatalf("job_suspended events = %+v", got)
			}
			if len(cycles.waits) != 0 {
				t.Fatalf("a closed cycle records no startup wait: %+v", cycles.waits)
			}
		})
	}
}

// The regression guard for the usage line: a merge-closed cycle's pod is
// still writing its terminal result, so the watcher leaves a Running (or
// Pending) pod alone until it is terminal.
func TestTick_MergeClosedCycleWithALivePodIsNotSuspended(t *testing.T) {
	for _, phase := range []string{"Running", "Pending"} {
		t.Run(phase, func(t *testing.T) {
			rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: phase}}
			c := dispatchedCycle("c1", 20*time.Minute)
			ended := time.Now().UTC()
			c.EndedAt, c.PRNumber, c.MergeSHA = &ended, 7, "abc123"
			jobs := &fakeJobs{}
			NewJobWatcher(rt, newWatchedCycles(c), testWriteTargets(), jobs, nil).Tick(context.Background())
			if len(jobs.suspends) != 0 {
				t.Fatalf("suspends = %v", jobs.suspends)
			}
		})
	}
}

// A legacy release with no suspend schema: the startup close still lands, the
// Job is not marked (the existing ErrSuspendUnsupported contract), and the
// warning is the one the terminal path gives.
func TestTick_StartupFailureSuspendUnsupportedIsNotMarked(t *testing.T) {
	logs := captureLogs(t)
	rt := &fakeRuntime{pod: unschedulablePod()}
	cycles := newWatchedCycles(dispatchedCycle("c1", 20*time.Minute))
	jobs := &fakeJobs{err: fmt.Errorf("suspend: %w", openchoreo.ErrSuspendUnsupported)}
	NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil).Tick(context.Background())
	if cycles.finished["c1"] == "" {
		t.Fatal("the startup close must land whatever the suspend answers")
	}
	if len(jobs.suspends) != 1 || cycles.suspended["c1"] {
		t.Fatalf("suspends %v, marked %v", jobs.suspends, cycles.suspended)
	}
	if len(logsNamed(*logs, "codingagent.job_suspend_unsupported")) != 1 {
		t.Fatal("job_suspend_unsupported must be warned")
	}
}

// The grace runs from the attempt's dispatch (dispatched_at), not from the
// row's last write: an unrelated write must not push the verdict back.
func TestTick_StartupGraceRunsFromTheAttemptsDispatch(t *testing.T) {
	rt := &fakeRuntime{pod: unschedulablePod()}
	c := dispatchedCycle("c1", 0) // updated_at: just now
	at := time.Now().UTC().Add(-20 * time.Minute)
	c.DispatchedAt = &at
	cycles := newWatchedCycles(c)
	newTestWatcher(rt, cycles).Tick(context.Background())
	if cycles.finished["c1"] == "" {
		t.Fatal("dispatched 20 minutes ago: past the grace whatever updated_at says")
	}
}

// ---- the durable startup wait (W1) -------------------------------------------

// A pod stuck before Running records why and since when, once: the console
// shows the wait from the row, and a tick that finds the same reason already
// recorded writes nothing. The cycle stays open inside the grace.
func TestTick_StuckPodRecordsTheStartupWaitOnce(t *testing.T) {
	logs := captureLogs(t)
	rt := &fakeRuntime{pod: unschedulablePod()}
	cycles := newWatchedCycles(dispatchedCycle("c1", time.Minute))
	w := newTestWatcher(rt, cycles)
	before := time.Now().UTC()

	w.Tick(context.Background())

	if len(cycles.waits) != 1 || cycles.waits[0].id != "c1" || cycles.waits[0].reason != "Unschedulable" {
		t.Fatalf("waits = %+v, want one Unschedulable note for c1", cycles.waits)
	}
	if at := cycles.waits[0].at; at.Before(before) || at.After(time.Now().UTC()) {
		t.Fatalf("since = %v, want the tick's time", at)
	}
	if len(cycles.finished) != 0 {
		t.Fatalf("inside the grace nothing closes: %v", cycles.finished)
	}
	got := logsNamed(*logs, "codingagent.startup_wait")
	want := map[string]string{"cycle": "c1", "component": cycles.rows[0].JobRef, "reason": "Unschedulable"}
	if len(got) != 1 || !reflect.DeepEqual(got[0].attrs, want) {
		t.Fatalf("startup_wait events = %+v, want one with %v", got, want)
	}

	since := cycles.waits[0].at
	cycles.rows[0].StartupWaitReason, cycles.rows[0].StartupWaitSince = "Unschedulable", &since
	w.Tick(context.Background())
	if len(cycles.waits) != 1 {
		t.Fatalf("the same reason must not be written again: %+v", cycles.waits)
	}

	// A different stuck reason is news: written again (the store keeps since).
	rt.pod = openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Pending", WaitingReason: "ImagePullBackOff"}
	w.Tick(context.Background())
	if len(cycles.waits) != 2 || cycles.waits[1].reason != "ImagePullBackOff" {
		t.Fatalf("waits = %+v", cycles.waits)
	}
}

// The pod runs: the wait is over and is cleared, once.
func TestTick_RunningPodClearsTheStartupWait(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}}
	c := dispatchedCycle("c1", 5*time.Minute)
	since := time.Now().UTC().Add(-4 * time.Minute)
	c.StartupWaitReason, c.StartupWaitSince = "Unschedulable", &since
	cycles := newWatchedCycles(c)
	w := newTestWatcher(rt, cycles)

	w.Tick(context.Background())
	if cycles.clears["c1"] != 1 {
		t.Fatalf("clears = %v, want one", cycles.clears)
	}
	cycles.rows[0].StartupWaitReason, cycles.rows[0].StartupWaitSince = "", nil
	w.Tick(context.Background())
	if cycles.clears["c1"] != 1 {
		t.Fatalf("a cleared row is not cleared again: %v", cycles.clears)
	}
}

// A pod that was stuck and is now merely starting (scheduled, creating its
// container) is no longer stuck: the recorded wait is cleared rather than
// left to say "no room" while the image pulls.
func TestTick_PodNoLongerStuckClearsTheStartupWait(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Pending", WaitingReason: "ContainerCreating"}}
	c := dispatchedCycle("c1", 5*time.Minute)
	since := time.Now().UTC().Add(-4 * time.Minute)
	c.StartupWaitReason, c.StartupWaitSince = "Unschedulable", &since
	cycles := newWatchedCycles(c)
	newTestWatcher(rt, cycles).Tick(context.Background())
	if cycles.clears["c1"] != 1 || len(cycles.waits) != 0 {
		t.Fatalf("clears %v, waits %+v", cycles.clears, cycles.waits)
	}
}

// Nothing stuck, nothing written: an ordinary start (no reason yet, or the
// normal ContainerCreating / PodInitializing), a node that stopped reporting
// (Unknown), and a pod not found.
func TestTick_NoStuckReasonWritesNoStartupWait(t *testing.T) {
	cases := map[string]openchoreo.RuntimePod{
		"pending, no reason": {Found: true, Name: "p1", Phase: "Pending"},
		"container creating": {Found: true, Name: "p1", Phase: "Pending", WaitingReason: "ContainerCreating"},
		"pod initializing":   {Found: true, Name: "p1", Phase: "Pending", WaitingReason: "PodInitializing"},
		"unknown phase":      {Found: true, Name: "p1", Phase: "Unknown"},
		"no pod":             {},
	}
	for name, pod := range cases {
		t.Run(name, func(t *testing.T) {
			cycles := newWatchedCycles(dispatchedCycle("c1", time.Minute))
			newTestWatcher(&fakeRuntime{pod: pod}, cycles).Tick(context.Background())
			if len(cycles.waits) != 0 || len(cycles.clears) != 0 {
				t.Fatalf("waits %+v, clears %v", cycles.waits, cycles.clears)
			}
		})
	}
}
