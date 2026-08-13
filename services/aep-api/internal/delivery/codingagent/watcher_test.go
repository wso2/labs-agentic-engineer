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
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// ---- fakes -----------------------------------------------------------------

type fakeRuntime struct {
	bindingErr error
	pod        openchoreo.RuntimePod
	podErr     error
	logs       []openchoreo.PodLogLine
	events     []openchoreo.RuntimeEvent

	bindingCalls int
	logCalls     int
}

func (f *fakeRuntime) ReleaseBindingName(context.Context, string, string, string, string) (string, error) {
	f.bindingCalls++
	if f.bindingErr != nil {
		return "", f.bindingErr
	}
	return "rb-dev", nil
}

func (f *fakeRuntime) PodSnapshot(context.Context, string, string) (openchoreo.RuntimePod, error) {
	return f.pod, f.podErr
}

func (f *fakeRuntime) PodLogs(context.Context, string, string, string, int64) ([]openchoreo.PodLogLine, error) {
	f.logCalls++
	return f.logs, nil
}

func (f *fakeRuntime) PodEvents(context.Context, string, string, string) ([]openchoreo.RuntimeEvent, error) {
	return f.events, nil
}

type watchedCycles struct {
	rows     []delivery.RunCycle
	finished map[string]string
	usage    map[string]contracts.CapturedUsage
}

func newWatchedCycles(rows ...delivery.RunCycle) *watchedCycles {
	return &watchedCycles{
		rows:     rows,
		finished: map[string]string{},
		usage:    map[string]contracts.CapturedUsage{},
	}
}

func (c *watchedCycles) ListRecentDispatched(context.Context, time.Time) ([]delivery.RunCycle, error) {
	return c.rows, nil
}

func (c *watchedCycles) FinishAgentFailed(_ context.Context, id, reason string) (*delivery.RunCycle, error) {
	if _, done := c.finished[id]; done {
		return nil, nil
	}
	c.finished[id] = reason
	now := time.Now().UTC()
	return &delivery.RunCycle{ID: id, AgentReason: reason, EndedAt: &now}, nil
}

func (c *watchedCycles) RecordUsage(_ context.Context, id string, u contracts.CapturedUsage) error {
	c.usage[id] = u
	return nil
}

// ---- harness ---------------------------------------------------------------

func dispatchedCycle(id string, dispatchedAgo time.Duration) delivery.RunCycle {
	return delivery.RunCycle{
		ID: id, OrgID: "acme", ProjectID: "shop", RunID: "run-1",
		Kind: delivery.CycleKindCoding, JobRef: "ca-" + id + "-2608061000",
		UpdatedAt: time.Now().UTC().Add(-dispatchedAgo),
	}
}

func newTestWatcher(rt openchoreo.RuntimeClient, cycles cycleWatchStore) *JobWatcher {
	return NewJobWatcher(rt, cycles, nil).WithIntervals(time.Millisecond, 10*time.Minute)
}

// ---- tests -----------------------------------------------------------------

// A zero exit means the process ended, not that the work landed. Completion is
// the pull request's, and it reaches the run as a webhook.
func TestTick_SucceededPodNeitherClosesTheCycleNorDeletesAnything(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Succeeded"}}
	cycles := newWatchedCycles(dispatchedCycle("c1", time.Minute))

	newTestWatcher(rt, cycles).Tick(context.Background())

	if len(cycles.finished) != 0 {
		t.Fatalf("a succeeded pod must not close the cycle, got %+v", cycles.finished)
	}
}

func TestTick_FailedPodClosesTheCycleWithItsReason(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{
		Found: true, Name: "p1", Phase: "Failed", TerminatedReason: "DeadlineExceeded",
	}}
	cycles := newWatchedCycles(dispatchedCycle("c2", time.Minute))

	newTestWatcher(rt, cycles).Tick(context.Background())

	if cycles.finished["c2"] != ReasonTimedOut {
		t.Fatalf("finished = %+v, want c2 -> %s", cycles.finished, ReasonTimedOut)
	}
}

// The pod's log is read for its terminal usage line only — nothing is persisted
// as a log. Postgres is not the agent-log system of record any more.
func TestTick_TerminalPodCapturesUsageAndWritesNoLog(t *testing.T) {
	rt := &fakeRuntime{
		pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Succeeded"},
		logs: []openchoreo.PodLogLine{
			{Timestamp: time.Now().UTC(), Log: `{"schemaVersion":1,"kind":"result","usage":{"inputTokens":11,"outputTokens":22,"model":"claude-sonnet-5"}}`},
		},
	}
	cycles := newWatchedCycles(dispatchedCycle("c3", time.Minute))

	newTestWatcher(rt, cycles).Tick(context.Background())

	got, ok := cycles.usage["c3"]
	if !ok {
		t.Fatalf("usage was not captured: %+v", cycles.usage)
	}
	if got.InputTokens != 11 || got.OutputTokens != 22 {
		t.Fatalf("unexpected usage: %+v", got)
	}
}

// Usage capture is DB-driven and idempotent: a cycle that already carries a
// model id has been captured, so a re-tick must not re-read a 256KiB log.
func TestTick_UsageIsCapturedOnce(t *testing.T) {
	row := dispatchedCycle("c4", time.Minute)
	row.ModelID = "claude-sonnet-5"
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Succeeded"}}
	cycles := newWatchedCycles(row)

	newTestWatcher(rt, cycles).Tick(context.Background())

	if rt.logCalls != 0 {
		t.Fatalf("an already-captured cycle must not re-read its log, got %d reads", rt.logCalls)
	}
}

func TestTick_StartupGraceFailsWithTheEventsReason(t *testing.T) {
	rt := &fakeRuntime{
		pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Pending", WaitingReason: "ImagePullBackOff"},
	}
	cycles := newWatchedCycles(dispatchedCycle("c5", 20*time.Minute))

	newTestWatcher(rt, cycles).Tick(context.Background())

	if got := cycles.finished["c5"]; got != "startup_failed:ImagePullBackOff" {
		t.Fatalf("finished = %q, want startup_failed:ImagePullBackOff", got)
	}
}

func TestTick_PendingInsideTheGraceIsLeftAlone(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Pending"}}
	cycles := newWatchedCycles(dispatchedCycle("c6", time.Minute))

	newTestWatcher(rt, cycles).Tick(context.Background())

	if len(cycles.finished) != 0 {
		t.Fatalf("a pod inside the startup grace must not fail: %+v", cycles.finished)
	}
}

// A 5xx is the platform having a bad second. It must never be evidence about a
// cycle, no matter how many ticks it lasts.
func TestTick_TransientErrorsNeverFailACycle(t *testing.T) {
	rt := &fakeRuntime{bindingErr: fmt.Errorf("%w: status 503", openchoreo.ErrInternalServerError)}
	cycles := newWatchedCycles(dispatchedCycle("c7", time.Minute))
	w := newTestWatcher(rt, cycles)

	for i := 0; i < 5; i++ {
		w.Tick(context.Background())
	}

	if len(cycles.finished) != 0 {
		t.Fatalf("transient errors must not fail a cycle: %+v", cycles.finished)
	}
}

func TestTick_SustainedNotFoundFailsTheCycleOnTheThirdTick(t *testing.T) {
	rt := &fakeRuntime{bindingErr: fmt.Errorf("%w: gone", openchoreo.ErrNotFound)}
	cycles := newWatchedCycles(dispatchedCycle("c8", time.Minute))
	w := newTestWatcher(rt, cycles)

	w.Tick(context.Background())
	w.Tick(context.Background())
	if len(cycles.finished) != 0 {
		t.Fatalf("two missing ticks must not be enough: %+v", cycles.finished)
	}

	w.Tick(context.Background())
	if got := cycles.finished["c8"]; got != ReasonJobNotFound {
		t.Fatalf("finished = %q, want %s", got, ReasonJobNotFound)
	}
}

// A transient 5xx between missing reads breaks consecutiveness, so 404/503
// interleaving cannot accumulate to three without three NotFound ticks in a row.
func TestTick_TransientErrorResetsNotFoundStreak(t *testing.T) {
	rt := &fakeRuntime{bindingErr: fmt.Errorf("%w: gone", openchoreo.ErrNotFound)}
	cycles := newWatchedCycles(dispatchedCycle("c8b", time.Minute))
	w := newTestWatcher(rt, cycles)

	w.Tick(context.Background())
	w.Tick(context.Background())
	if len(cycles.finished) != 0 {
		t.Fatalf("two missing ticks must not be enough: %+v", cycles.finished)
	}

	rt.bindingErr = fmt.Errorf("%w: status 503", openchoreo.ErrInternalServerError)
	w.Tick(context.Background())
	if len(cycles.finished) != 0 {
		t.Fatalf("transient error must not fail the cycle: %+v", cycles.finished)
	}

	rt.bindingErr = fmt.Errorf("%w: gone", openchoreo.ErrNotFound)
	w.Tick(context.Background())
	w.Tick(context.Background())
	if len(cycles.finished) != 0 {
		t.Fatalf("two missing ticks after reset must not be enough: %+v", cycles.finished)
	}

	w.Tick(context.Background())
	if got := cycles.finished["c8b"]; got != ReasonJobNotFound {
		t.Fatalf("finished = %q, want %s", got, ReasonJobNotFound)
	}
}

// A Component that comes back (a slow render, a re-list) resets the streak, so
// three NON-consecutive misses never add up to a verdict.
func TestTick_NotFoundStreakResetsWhenTheBindingReturns(t *testing.T) {
	rt := &fakeRuntime{bindingErr: fmt.Errorf("%w: gone", openchoreo.ErrNotFound)}
	cycles := newWatchedCycles(dispatchedCycle("c9", time.Minute))
	w := newTestWatcher(rt, cycles)

	w.Tick(context.Background())
	w.Tick(context.Background())
	rt.bindingErr = nil
	rt.pod = openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}
	w.Tick(context.Background())
	rt.bindingErr = fmt.Errorf("%w: gone", openchoreo.ErrNotFound)
	w.Tick(context.Background())
	w.Tick(context.Background())

	if len(cycles.finished) != 0 {
		t.Fatalf("a reset streak must not reach the verdict: %+v", cycles.finished)
	}
}

// A closed cycle is retention's business, not the watcher's: it may still be
// polled for usage, but nothing about it is re-decided.
func TestTick_ClosedCycleIsNeverReclassified(t *testing.T) {
	row := dispatchedCycle("c10", time.Minute)
	ended := time.Now().UTC()
	row.EndedAt = &ended
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Failed"}}
	cycles := newWatchedCycles(row)

	newTestWatcher(rt, cycles).Tick(context.Background())

	if len(cycles.finished) != 0 {
		t.Fatalf("a closed cycle must not be re-closed: %+v", cycles.finished)
	}
}

func TestTick_SkipsCyclesThatNeverDispatchedAJob(t *testing.T) {
	row := dispatchedCycle("c11", time.Minute)
	row.JobRef = ""
	rt := &fakeRuntime{}
	cycles := newWatchedCycles(row)

	newTestWatcher(rt, cycles).Tick(context.Background())

	if rt.bindingCalls != 0 {
		t.Fatalf("a cycle with no Job must not be looked up, got %d lookups", rt.bindingCalls)
	}
}
