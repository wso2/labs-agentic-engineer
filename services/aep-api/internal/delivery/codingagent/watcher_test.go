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
	"log/slog"
	"reflect"
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

	// delay runs before every call that precedes a log read. A test sets it to
	// advance a fake clock, which is the only way to pin that the log window is
	// computed AFTER the round trips in front of it rather than before them.
	delay func()
	// logSince is the sinceSeconds of the last PodLogs call — the coarse window
	// the API actually received.
	logSince int64

	bindingCalls int
	logCalls     int
	// bindingEnvs is the environment each ReleaseBindingName call asked for.
	bindingEnvs []string
}

func (f *fakeRuntime) ReleaseBindingName(_ context.Context, _, _, _, environment string) (string, error) {
	f.bindingCalls++
	f.bindingEnvs = append(f.bindingEnvs, environment)
	if f.delay != nil {
		f.delay()
	}
	if f.bindingErr != nil {
		return "", f.bindingErr
	}
	return "rb-dev", nil
}

func (f *fakeRuntime) PodSnapshot(context.Context, string, string) (openchoreo.RuntimePod, error) {
	if f.delay != nil {
		f.delay()
	}
	return f.pod, f.podErr
}

func (f *fakeRuntime) PodLogs(_ context.Context, _, _, _ string, sinceSeconds int64) ([]openchoreo.PodLogLine, error) {
	f.logCalls++
	f.logSince = sinceSeconds
	return f.logs, nil
}

func (f *fakeRuntime) PodEvents(context.Context, string, string, string) ([]openchoreo.RuntimeEvent, error) {
	return f.events, nil
}

type watchedCycles struct {
	rows      []delivery.RunCycle
	finished  map[string]string
	usage     map[string]contracts.CapturedUsage
	suspended map[string]bool
}

func newWatchedCycles(rows ...delivery.RunCycle) *watchedCycles {
	return &watchedCycles{
		rows:      rows,
		finished:  map[string]string{},
		usage:     map[string]contracts.CapturedUsage{},
		suspended: map[string]bool{},
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
	// The repository re-reads the whole row after the update, so the double
	// returns the listed row rather than a stub — a lossy double here would let
	// a caller depend on columns the real write does populate, or miss one it
	// does not.
	for _, row := range c.rows {
		if row.ID == id {
			row.AgentReason, row.EndedAt = reason, &now
			return &row, nil
		}
	}
	return &delivery.RunCycle{ID: id, AgentReason: reason, EndedAt: &now}, nil
}

func (c *watchedCycles) RecordUsage(_ context.Context, id string, u contracts.CapturedUsage) error {
	c.usage[id] = u
	return nil
}

func (c *watchedCycles) MarkJobSuspended(_ context.Context, id string) error {
	c.suspended[id] = true
	return nil
}

// fakeJobs records each SuspendJobBinding as component@environment. before,
// when set, runs inside the call so a test can pin what had (or had not)
// happened by the time the suspend went out.
type fakeJobs struct {
	suspends []string
	err      error
	before   func()
}

func (f *fakeJobs) SuspendJobBinding(_ context.Context, _, _, component, env string) error {
	if f.before != nil {
		f.before()
	}
	f.suspends = append(f.suspends, component+"@"+env)
	return f.err
}

// deathNotice is one AgentDied call, recorded whole so a test can assert the
// run it would wake and not merely that something fired.
type deathNotice struct {
	orgID, runID, reason string
}

type recordingDeaths struct {
	notices []deathNotice
	err     error
}

func (d *recordingDeaths) AgentDied(_ context.Context, orgID, runID, reason string) error {
	d.notices = append(d.notices, deathNotice{orgID, runID, reason})
	return d.err
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
	return newTestWatcherWith(rt, cycles, testWriteTargets())
}

func newTestWatcherWith(rt openchoreo.RuntimeClient, cycles cycleWatchStore, targets writeTargetResolver) *JobWatcher {
	return NewJobWatcher(rt, cycles, targets, &fakeJobs{}, nil).WithIntervals(time.Millisecond, 10*time.Minute)
}

// resultLine is the runner's terminal settle line, carrying usage.
func resultLine(t *testing.T) []openchoreo.PodLogLine {
	t.Helper()
	return []openchoreo.PodLogLine{{
		Timestamp: time.Now().UTC(),
		Log:       `{"schemaVersion":1,"kind":"result","usage":{"inputTokens":11,"outputTokens":22,"model":"claude-sonnet-5"}}`,
	}}
}

// logRecord is one captured slog record: its message and its attributes.
type logRecord struct {
	msg   string
	attrs map[string]string
}

type recordingHandler struct{ records *[]logRecord }

func (h recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h recordingHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]string{}
	r.Attrs(func(a slog.Attr) bool { attrs[a.Key] = a.Value.String(); return true })
	*h.records = append(*h.records, logRecord{msg: r.Message, attrs: attrs})
	return nil
}
func (h recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordingHandler) WithGroup(string) slog.Handler      { return h }

// captureLogs routes the default logger into a slice for the test's duration.
func captureLogs(t *testing.T) *[]logRecord {
	t.Helper()
	var records []logRecord
	prev := slog.Default()
	slog.SetDefault(slog.New(recordingHandler{records: &records}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &records
}

func logsNamed(records []logRecord, msg string) []logRecord {
	var out []logRecord
	for _, r := range records {
		if r.msg == msg {
			out = append(out, r)
		}
	}
	return out
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

// A dead agent is TOLD to its run, not waited out. Without this the run holds
// its 2h landing deadline, re-dispatches, and holds another — four hours for a
// pod that OOMed in twenty minutes.
func TestTick_FailedPodTellsTheRunItsAgentDied(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{
		Found: true, Name: "p1", Phase: "Failed", TerminatedReason: "OOMKilled",
	}}
	cycles := newWatchedCycles(dispatchedCycle("c2", time.Minute))
	deaths := &recordingDeaths{}

	newTestWatcher(rt, cycles).WithAgentDeathNotifier(deaths).Tick(context.Background())

	want := []deathNotice{{"acme", "run-1", "agent_failed:OOMKilled"}}
	if !reflect.DeepEqual(deaths.notices, want) {
		t.Fatalf("notices = %+v, want %+v", deaths.notices, want)
	}
}

// The notice rides the repository's once-only fence, so the second replica to
// reach the same dead pod closes nothing and wakes nobody. A run woken twice
// would spend a re-dispatch it was not owed.
func TestTick_AgentDeathIsToldOnceAcrossReplicas(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{
		Found: true, Name: "p1", Phase: "Failed", TerminatedReason: "OOMKilled",
	}}
	cycles := newWatchedCycles(dispatchedCycle("c2", time.Minute))
	deaths := &recordingDeaths{}
	w := newTestWatcher(rt, cycles).WithAgentDeathNotifier(deaths)

	w.Tick(context.Background())
	w.Tick(context.Background())

	if len(deaths.notices) != 1 {
		t.Fatalf("notices = %+v, want exactly one", deaths.notices)
	}
}

// Waking the run is best-effort by the port's contract: the cycle's terminal
// reason is already durable, and the run still settles on its landing deadline.
// A notifier that fails must not unwind the write or stop the tick.
func TestTick_AgentDeathNotifyFailureStillClosesTheCycle(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{
		Found: true, Name: "p1", Phase: "Failed", TerminatedReason: "OOMKilled",
	}}
	cycles := newWatchedCycles(dispatchedCycle("c2", time.Minute))
	deaths := &recordingDeaths{err: errors.New("temporal is down")}

	newTestWatcher(rt, cycles).WithAgentDeathNotifier(deaths).Tick(context.Background())

	if cycles.finished["c2"] != "agent_failed:OOMKilled" {
		t.Fatalf("finished = %+v, want the cycle closed regardless", cycles.finished)
	}
}

// Every other watcher test constructs without a notifier, and so does a
// degraded boot. nil must stay a valid state.
func TestTick_NoNotifierIsStillAValidWatcher(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{
		Found: true, Name: "p1", Phase: "Failed", TerminatedReason: "OOMKilled",
	}}
	cycles := newWatchedCycles(dispatchedCycle("c2", time.Minute))

	newTestWatcher(rt, cycles).Tick(context.Background())

	if cycles.finished["c2"] != "agent_failed:OOMKilled" {
		t.Fatalf("finished = %+v, want the cycle closed with no notifier wired", cycles.finished)
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

// A cycle reads its binding in the environment its Job was bound into, even
// when the project's write target has moved since: the Job is still there.
func TestTick_ReadsTheBindingInTheCyclesRecordedEnvironment(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}}
	cycle := dispatchedCycle("c1", time.Minute)
	cycle.Environment = "dev-b"
	targets := &fakeWriteTargets{env: "moved-on"}

	newTestWatcherWith(rt, newWatchedCycles(cycle), targets).Tick(context.Background())

	if len(rt.bindingEnvs) != 1 || rt.bindingEnvs[0] != "dev-b" {
		t.Fatalf("binding read in %v, want [dev-b]", rt.bindingEnvs)
	}
	if n := targets.resolves(); n != 0 {
		t.Fatalf("resolved the write target %d times, want never for a cycle that recorded one", n)
	}
}

// A cycle dispatched before its environment was recorded (or whose launch write
// failed) has none. It must not be read in "": it falls back to the project's
// write target now.
func TestTick_ACycleWithNoRecordedEnvironmentUsesTheProjectsWriteTarget(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}}
	targets := &fakeWriteTargets{env: "dev-b"}

	newTestWatcherWith(rt, newWatchedCycles(dispatchedCycle("c1", time.Minute)), targets).Tick(context.Background())

	if len(rt.bindingEnvs) != 1 || rt.bindingEnvs[0] != "dev-b" {
		t.Fatalf("binding read in %v, want [dev-b]", rt.bindingEnvs)
	}
}

// A fallback that cannot be resolved is no evidence about the cycle: nothing is
// read, and the cycle is not failed, however many ticks it lasts. Even when the
// cause wraps a not-found (a pipeline that 404s), which must not be counted as
// the cycle's Component going missing.
func TestTick_AnUnresolvableFallbackNeverFailsACycle(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}}
	cycles := newWatchedCycles(dispatchedCycle("c1", time.Minute))
	targets := &fakeWriteTargets{err: &openchoreo.ErrNoWriteTarget{Org: "acme", Project: "shop", Cause: openchoreo.ErrNotFound}}
	w := newTestWatcherWith(rt, cycles, targets)

	for i := 0; i < missingTicksToFail+1; i++ {
		w.Tick(context.Background())
	}

	if len(rt.bindingEnvs) != 0 {
		t.Fatalf("binding read in %v, want no read without an environment", rt.bindingEnvs)
	}
	if len(cycles.finished) != 0 {
		t.Fatalf("an unresolvable write target must not fail a cycle: %+v", cycles.finished)
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

// An empty snapshot is not "no pod was ever scheduled". A pod the watcher has
// seen running cannot become a startup failure when a later read comes back
// empty — that is the platform under load, not the cycle.
func TestTick_EmptySnapshotAfterThePodWasSeenIsNoVerdict(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}}
	cycles := newWatchedCycles(dispatchedCycle("c10", 20*time.Minute))
	w := newTestWatcher(rt, cycles)

	w.Tick(context.Background())
	rt.pod = openchoreo.RuntimePod{}
	for i := 0; i < 5; i++ {
		w.Tick(context.Background())
	}

	if len(cycles.finished) != 0 {
		t.Fatalf("a seen pod must never yield a startup verdict on empty snapshots: %+v", cycles.finished)
	}
}

// A pod that was never seen still gets the no-pod verdict past the grace, but
// only once the empty snapshot has held for as many ticks as a sustained 404.
func TestTick_NoPodVerdictNeedsSustainedEmptySnapshots(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{}}
	cycles := newWatchedCycles(dispatchedCycle("c11", 20*time.Minute))
	w := newTestWatcher(rt, cycles)

	for i := 1; i < missingTicksToFail; i++ {
		w.Tick(context.Background())
		if len(cycles.finished) != 0 {
			t.Fatalf("tick %d: an empty snapshot must not be a verdict yet: %+v", i, cycles.finished)
		}
	}
	w.Tick(context.Background())
	if got := cycles.finished["c11"]; got != "startup_failed:no_pod_scheduled" {
		t.Fatalf("finished = %q, want startup_failed:no_pod_scheduled after %d empty ticks", got, missingTicksToFail)
	}
}

// One pod sighting in the middle of the empty streak resets it: the streak is
// consecutive, like the 404 streak, and the sighting also marks the pod seen —
// so the empties that follow never add up to a verdict either.
func TestTick_EmptySnapshotStreakResetsWhenThePodAppears(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{}}
	cycles := newWatchedCycles(dispatchedCycle("c12", 20*time.Minute))
	w := newTestWatcher(rt, cycles)

	w.Tick(context.Background())
	w.Tick(context.Background())
	rt.pod = openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}
	w.Tick(context.Background())
	rt.pod = openchoreo.RuntimePod{}
	for i := 0; i < 4; i++ {
		w.Tick(context.Background())
	}
	if len(cycles.finished) != 0 {
		t.Fatalf("a sighted pod resets the streak and marks the pod seen: %+v", cycles.finished)
	}
}

// Positive evidence past the grace is still a verdict: a pod that exists but
// never reached Running is what the startup grace is for.
func TestTick_FoundPendingPodPastGraceFailsAtOnce(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Pending"}}
	cycles := newWatchedCycles(dispatchedCycle("c13", 20*time.Minute))

	newTestWatcher(rt, cycles).Tick(context.Background())

	if got := cycles.finished["c13"]; got != "startup_failed:pod_not_running" {
		t.Fatalf("finished = %q, want startup_failed:pod_not_running on the first tick", got)
	}
}

// ---- provider limits ---------------------------------------------------------

// recordingFailures is the run repository's RecordFailure, recorded whole.
type recordingFailures struct {
	runID    string
	failures []delivery.RunFailure
	err      error
	// order is shared with the cycle double so a test can see which write came
	// first — the record has to land while the run is still non-terminal.
	order *[]string
}

func (r *recordingFailures) RecordFailure(_ context.Context, id string, f delivery.RunFailure) (*delivery.MilestoneRun, error) {
	r.runID = id
	r.failures = append(r.failures, f)
	if r.order != nil {
		*r.order = append(*r.order, "record")
	}
	return &delivery.MilestoneRun{}, r.err
}

// orderedCycles notes when the cycle closes, against the same order slice.
type orderedCycles struct {
	*watchedCycles
	order *[]string
}

func (c orderedCycles) FinishAgentFailed(ctx context.Context, id, reason string) (*delivery.RunCycle, error) {
	*c.order = append(*c.order, "close")
	return c.watchedCycles.FinishAgentFailed(ctx, id, reason)
}

// providerLimitedPod is a runner that stopped on its provider's limit: exit 1,
// and a terminal `run_settled` carrying the code.
func providerLimitedPod(settle string) *fakeRuntime {
	return &fakeRuntime{
		pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Failed", TerminatedReason: "Error"},
		logs: []openchoreo.PodLogLine{
			{Timestamp: time.Now().UTC(), Log: `{"v":2,"seq":7,"ts":"2026-09-26T10:05:00Z","agentId":"lead","kind":"notice","level":"error","code":"terminated","detail":"[provider] terminated"}`},
			{Timestamp: time.Now().UTC(), Log: settle},
		},
	}
}

// The runner's settle is what separates "the provider refused" from "the agent
// died": the cycle closes under the reason the supervisor settles BLOCKED on,
// and the run's failure record carries what the console's sentence names. The
// record lands BEFORE the close, because closing wakes the supervisor into
// settling the run, and a settled run takes no record.
func TestTick_ProviderLimitClosesTheCycleUnderItsOwnReasonAndRecordsTheLimit(t *testing.T) {
	rt := providerLimitedPod(`{"v":2,"seq":8,"ts":"2026-09-26T10:05:00Z","agentId":"lead","kind":"run_settled","outcome":"failure",` +
		`"error":"model provider limit reached on ollama.com until 2026-09-26T14:00:00Z","code":"provider_limit",` +
		`"host":"ollama.com","resetAt":"2026-09-26T14:00:00Z","providerDetail":"Too Many Requests: weekly usage limit reached",` +
		`"usage":{"inputTokens":5,"outputTokens":1,"model":"gpt-oss:20b"}}`)
	var order []string
	cycles := orderedCycles{newWatchedCycles(dispatchedCycle("c9", time.Minute)), &order}
	failures := &recordingFailures{order: &order}
	deaths := &recordingDeaths{}

	newTestWatcher(rt, cycles).WithAgentDeathNotifier(deaths).WithRunFailures(failures).Tick(context.Background())

	if got := cycles.finished["c9"]; got != delivery.CycleReasonModelProviderLimit {
		t.Fatalf("finished = %q, want %q", got, delivery.CycleReasonModelProviderLimit)
	}
	if failures.runID != "run-1" || len(failures.failures) != 1 {
		t.Fatalf("record = %q %+v, want one record on run-1", failures.runID, failures.failures)
	}
	f := failures.failures[0]
	wantReset := time.Date(2026, 9, 26, 14, 0, 0, 0, time.UTC)
	if f.Code != delivery.RunFailureCodeModelProviderLimit || f.Phase != delivery.RunPhaseCoding ||
		f.Host != "ollama.com" || f.ResetAt == nil || !f.ResetAt.Equal(wantReset) || f.Permanent {
		t.Fatalf("record = %+v", f)
	}
	if f.Detail != "" {
		t.Fatalf("the provider's text is logged, never stored: detail = %q", f.Detail)
	}
	if !reflect.DeepEqual(order, []string{"record", "close"}) {
		t.Fatalf("write order = %v, want the record before the close", order)
	}
	// The run is still woken — on the same once-only fence as a death.
	if len(deaths.notices) != 1 || deaths.notices[0].reason != delivery.CycleReasonModelProviderLimit {
		t.Fatalf("notices = %+v", deaths.notices)
	}
	// And the usage on the same line is banked as on any terminal pod.
	if got := cycles.usage["c9"]; got.InputTokens != 5 {
		t.Fatalf("usage = %+v", cycles.usage)
	}
}

// No reset stated (the five-minute fallback) records no reset time, and a
// validation cycle records the validating phase.
func TestTick_ProviderLimitWithNoResetOnAValidationCycle(t *testing.T) {
	rt := providerLimitedPod(`{"v":2,"seq":8,"ts":"2026-09-26T10:05:00Z","agentId":"lead","kind":"run_settled","outcome":"failure",` +
		`"error":"model provider limit reached on ollama.com","code":"provider_limit","host":"ollama.com","providerDetail":"429 rate_limit"}`)
	row := dispatchedCycle("c10", time.Minute)
	row.Kind = delivery.CycleKindValidation
	cycles := newWatchedCycles(row)
	failures := &recordingFailures{}

	newTestWatcher(rt, cycles).WithRunFailures(failures).Tick(context.Background())

	if cycles.finished["c10"] != delivery.CycleReasonModelProviderLimit {
		t.Fatalf("finished = %+v", cycles.finished)
	}
	if len(failures.failures) != 1 || failures.failures[0].ResetAt != nil || failures.failures[0].Phase != delivery.RunPhaseValidating {
		t.Fatalf("record = %+v", failures.failures)
	}
}

// The provider is named by the host the cycle was dispatched on, not by the
// runner's report: the platform's own record of which connection the Job
// mounted wins, and the report is only the fallback for an unstamped cycle.
func TestTick_ProviderLimitNamesTheDispatchedHost(t *testing.T) {
	rt := providerLimitedPod(`{"v":2,"seq":8,"ts":"2026-09-26T10:05:00Z","agentId":"lead","kind":"run_settled","outcome":"failure",` +
		`"code":"provider_limit","host":"proxy.example.com"}`)
	row := dispatchedCycle("c13", time.Minute)
	row.ModelHost = "ollama.com"
	cycles := newWatchedCycles(row)
	failures := &recordingFailures{}

	newTestWatcher(rt, cycles).WithRunFailures(failures).Tick(context.Background())

	if len(failures.failures) != 1 || failures.failures[0].Host != "ollama.com" {
		t.Fatalf("record = %+v, want the cycle's dispatched host", failures.failures)
	}
}

// A failed settle with no code is agent death: no record,
// and the pod's own reason on the cycle.
func TestTick_AFailedSettleWithoutTheCodeIsStillAgentDeath(t *testing.T) {
	rt := providerLimitedPod(`{"v":2,"seq":8,"ts":"2026-09-26T10:05:00Z","agentId":"lead","kind":"run_settled","outcome":"failure","error":"agent stream ended without result"}`)
	cycles := newWatchedCycles(dispatchedCycle("c11", time.Minute))
	failures := &recordingFailures{}

	newTestWatcher(rt, cycles).WithRunFailures(failures).Tick(context.Background())

	if cycles.finished["c11"] != "agent_failed:Error" {
		t.Fatalf("finished = %+v, want the pod's own reason", cycles.finished)
	}
	if len(failures.failures) != 0 {
		t.Fatalf("no provider limit, no record: %+v", failures.failures)
	}
}

// The record is best-effort: a repository that refuses it must not stop the
// cycle closing under the reason the run settles on.
func TestTick_ProviderLimitRecordFailureStillClosesTheCycle(t *testing.T) {
	rt := providerLimitedPod(`{"v":2,"seq":8,"ts":"2026-09-26T10:05:00Z","agentId":"lead","kind":"run_settled","outcome":"failure","code":"provider_limit","host":"ollama.com"}`)
	cycles := newWatchedCycles(dispatchedCycle("c12", time.Minute))

	newTestWatcher(rt, cycles).WithRunFailures(&recordingFailures{err: errors.New("db down")}).Tick(context.Background())

	if cycles.finished["c12"] != delivery.CycleReasonModelProviderLimit {
		t.Fatalf("finished = %+v", cycles.finished)
	}
}

// ---- suspend at the first terminal pod --------------------------------------

func TestTick_SucceededPodIsSuspendedAfterUsageIsCaptured(t *testing.T) {
	logs := captureLogs(t)
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Succeeded"}, logs: resultLine(t)}
	c := dispatchedCycle("c1", time.Minute)
	c.Environment = "development"
	cycles := newWatchedCycles(c)
	usageFirst := false
	jobs := &fakeJobs{before: func() { _, usageFirst = cycles.usage["c1"] }}
	NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil).WithIntervals(time.Millisecond, 10*time.Minute).Tick(context.Background())
	if !usageFirst {
		t.Fatal("usage must be captured before the Job is suspended")
	}
	if !reflect.DeepEqual(jobs.suspends, []string{c.JobRef + "@development"}) || !cycles.suspended["c1"] {
		t.Fatalf("suspends %v, marked %v", jobs.suspends, cycles.suspended)
	}
	got := logsNamed(*logs, "codingagent.job_suspended")
	want := map[string]string{"cycle": "c1", "component": c.JobRef, "cause": "terminal"}
	if len(got) != 1 || !reflect.DeepEqual(got[0].attrs, want) {
		t.Fatalf("job_suspended events = %+v, want one with %v", got, want)
	}
}

func TestTick_FailedPodIsSuspendedAfterUsageIsCaptured(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Failed"}, logs: resultLine(t)}
	cycles := newWatchedCycles(dispatchedCycle("c1", time.Minute))
	usageFirst := false
	jobs := &fakeJobs{before: func() { _, usageFirst = cycles.usage["c1"] }}
	NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil).Tick(context.Background())
	if !usageFirst || !cycles.suspended["c1"] || cycles.finished["c1"] == "" {
		t.Fatalf("usageFirst %v, marked %v, finished %v", usageFirst, cycles.suspended, cycles.finished)
	}
}

func TestTick_ProviderLimitPodIsSuspendedToo(t *testing.T) {
	rt := providerLimitedPod(`{"v":2,"seq":8,"ts":"2026-09-26T10:05:00Z","agentId":"lead","kind":"run_settled","outcome":"failure",` +
		`"error":"limit","code":"provider_limit","host":"ollama.com","resetAt":"2026-09-26T14:00:00Z",` +
		`"usage":{"inputTokens":5,"outputTokens":1,"model":"gpt-oss:20b"}}`)
	cycles := newWatchedCycles(dispatchedCycle("c1", time.Minute))
	jobs := &fakeJobs{}
	NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil).Tick(context.Background())
	if cycles.finished["c1"] != delivery.CycleReasonModelProviderLimit {
		t.Fatalf("finished = %v", cycles.finished)
	}
	if len(jobs.suspends) != 1 || !cycles.suspended["c1"] {
		t.Fatalf("suspends %v, marked %v", jobs.suspends, cycles.suspended)
	}
}

func TestTick_RunningPodIsNotSuspended(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}}
	jobs := &fakeJobs{}
	NewJobWatcher(rt, newWatchedCycles(dispatchedCycle("c1", time.Minute)), testWriteTargets(), jobs, nil).Tick(context.Background())
	if len(jobs.suspends) != 0 {
		t.Fatalf("suspends = %v", jobs.suspends)
	}
}

func TestTick_AlreadySuspendedCycleIsNotSuspendedAgain(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Succeeded"}}
	c := dispatchedCycle("c1", time.Minute)
	now := time.Now()
	c.JobSuspendedAt, c.ModelID = &now, "m"
	jobs := &fakeJobs{}
	NewJobWatcher(rt, newWatchedCycles(c), testWriteTargets(), jobs, nil).Tick(context.Background())
	if len(jobs.suspends) != 0 {
		t.Fatalf("suspends = %v", jobs.suspends)
	}
}

func TestTick_SuspendedCycleWithNoPodIsNotAStartupFailure(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	c := dispatchedCycle("c1", time.Hour) // far past the startup grace
	now := time.Now()
	c.JobSuspendedAt = &now
	cycles := newWatchedCycles(c)
	w := NewJobWatcher(rt, cycles, testWriteTargets(), &fakeJobs{}, nil).WithIntervals(time.Millisecond, time.Minute)
	for i := 0; i < missingTicksToFail+1; i++ {
		w.Tick(context.Background())
	}
	if len(cycles.finished) != 0 {
		t.Fatalf("a suspended Job's missing pod is expected, got %v", cycles.finished)
	}
}

func TestTick_SuspendFailureIsRetriedNextTick(t *testing.T) {
	logs := captureLogs(t)
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Failed"}}
	cycles := newWatchedCycles(dispatchedCycle("c1", time.Minute))
	jobs := &fakeJobs{err: errors.New("oc 500")}
	w := NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil)
	w.Tick(context.Background())
	if cycles.suspended["c1"] || len(logsNamed(*logs, "codingagent.job_suspended")) != 0 {
		t.Fatalf("a failed suspend must not be marked or announced: marked %v", cycles.suspended)
	}
	jobs.err = nil
	w.Tick(context.Background())
	if len(jobs.suspends) != 2 || !cycles.suspended["c1"] {
		t.Fatalf("suspends %v marked %v", jobs.suspends, cycles.suspended)
	}
}

// A legacy release has no suspend schema: the Job is left to its TTL, nothing
// is marked (so the settler treats it as "suspend not applicable"), and the
// warning carries no value beyond the cycle's identity.
func TestTick_SuspendUnsupportedIsWarnedAndNotMarked(t *testing.T) {
	logs := captureLogs(t)
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Succeeded"}}
	c := dispatchedCycle("c1", time.Minute)
	cycles := newWatchedCycles(c)
	jobs := &fakeJobs{err: fmt.Errorf("suspend b: %w", openchoreo.ErrSuspendUnsupported)}
	NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil).Tick(context.Background())
	if cycles.suspended["c1"] {
		t.Fatal("an unsupported suspend must not be marked")
	}
	if len(logsNamed(*logs, "codingagent.job_suspended")) != 0 {
		t.Fatal("job_suspended must be emitted only when the suspend took effect")
	}
	warn := logsNamed(*logs, "codingagent.job_suspend_unsupported")
	want := map[string]string{"cycle": "c1", "component": c.JobRef}
	if len(warn) != 1 || !reflect.DeepEqual(warn[0].attrs, want) {
		t.Fatalf("job_suspend_unsupported = %+v, want one with %v", warn, want)
	}
}

// A binding that is already gone has nothing left to suspend: marked, so the
// watcher stops asking, but not announced as a suspend.
func TestTick_SuspendOfAGoneBindingIsMarkedNotAnnounced(t *testing.T) {
	logs := captureLogs(t)
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Succeeded"}}
	cycles := newWatchedCycles(dispatchedCycle("c1", time.Minute))
	jobs := &fakeJobs{err: fmt.Errorf("binding: %w", openchoreo.ErrNotFound)}
	NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil).Tick(context.Background())
	if !cycles.suspended["c1"] {
		t.Fatal("a gone binding must be marked so it is not re-asked")
	}
	if len(logsNamed(*logs, "codingagent.job_suspended")) != 0 {
		t.Fatal("job_suspended must be emitted only when the suspend took effect")
	}
}

// redispatched returns c as the repository leaves it after a second
// NoteDispatch: attempt 2, dispatched `ago`, settle stamps cleared.
func redispatched(c delivery.RunCycle, ago time.Duration) delivery.RunCycle {
	at := time.Now().UTC().Add(-ago)
	c.Attempts, c.DispatchedAt, c.UpdatedAt = 2, &at, at
	c.JobSuspendedAt, c.PodGoneAt = nil, nil
	return c
}

// The suspend stamp belongs to the ATTEMPT, not the cycle. A landing-timeout
// re-dispatch reuses the cycle's Component, so attempt 1's finished pod can
// still be in the tree when attempt 2 is dispatched. That pod predates the
// attempt: it must not re-suspend the binding (attempt 2 would be born
// suspended), must not count as attempt 2's pod for the startup grace, and
// attempt 2's own pod is then watched as usual.
func TestTick_RedispatchedCycleIsWatchedAsAFreshAttempt(t *testing.T) {
	old := time.Now().UTC().Add(-3 * time.Hour)
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Succeeded", CreatedAt: old}}
	c := dispatchedCycle("c1", 3*time.Hour)
	c.Attempts = 1
	cycles := newWatchedCycles(c)
	jobs := &fakeJobs{}
	w := NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil).WithIntervals(time.Millisecond, 10*time.Minute)

	// Attempt 1 ends: suspended and stamped (and its pod has been seen).
	w.Tick(context.Background())
	if len(jobs.suspends) != 1 || !cycles.suspended["c1"] {
		t.Fatalf("attempt 1: suspends %v, marked %v", jobs.suspends, cycles.suspended)
	}

	// Attempt 2 is dispatched past the grace; attempt 1's pod is still there.
	cycles.rows[0] = redispatched(cycles.rows[0], time.Hour)
	delete(cycles.suspended, "c1")
	for i := 0; i < missingTicksToFail; i++ {
		w.Tick(context.Background())
	}
	if len(jobs.suspends) != 1 || cycles.suspended["c1"] {
		t.Fatalf("a pod from before the attempt must not suspend it: suspends %v, marked %v", jobs.suspends, cycles.suspended)
	}
	if cycles.finished["c1"] != StartupFailureReason(openchoreo.RuntimePod{}, nil) {
		t.Fatalf("finished = %v: attempt 2 never started, and attempt 1's pod must not hide it", cycles.finished)
	}

	// Attempt 2's own pod appears and ends: the normal path, suspended again.
	delete(cycles.finished, "c1")
	rt.pod = openchoreo.RuntimePod{Found: true, Name: "p2", Phase: "Succeeded", CreatedAt: time.Now().UTC()}
	w.Tick(context.Background())
	if len(jobs.suspends) != 2 || !cycles.suspended["c1"] {
		t.Fatalf("attempt 2: suspends %v, marked %v", jobs.suspends, cycles.suspended)
	}
}

// A leftover terminal pod is not this attempt's: no suspend, no stamp, no
// usage banked as this attempt's, no verdict inside the grace.
func TestTick_LeftoverPodFromThePreviousAttemptIsIgnored(t *testing.T) {
	rt := &fakeRuntime{
		pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Failed",
			CreatedAt: time.Now().UTC().Add(-3 * time.Hour), FinishedAt: time.Now().UTC().Add(-2 * time.Hour)},
		logs: resultLine(t),
	}
	cycles := newWatchedCycles(redispatched(dispatchedCycle("c1", time.Minute), time.Minute))
	jobs := &fakeJobs{}
	NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil).WithIntervals(time.Millisecond, 10*time.Minute).Tick(context.Background())
	if len(jobs.suspends) != 0 || cycles.suspended["c1"] {
		t.Fatalf("suspends %v, marked %v", jobs.suspends, cycles.suspended)
	}
	if _, ok := cycles.usage["c1"]; ok || rt.logCalls != 0 {
		t.Fatalf("a leftover pod's log is not this attempt's usage (reads %d)", rt.logCalls)
	}
	if len(cycles.finished) != 0 {
		t.Fatalf("finished = %v", cycles.finished)
	}
}

// A first dispatch writes its dispatch time AFTER the launch, so its own pod
// can predate it by seconds: the leftover rule only applies from attempt 2,
// when the binding is reused.
func TestTick_FirstAttemptsPodIsNeverALeftover(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Succeeded", CreatedAt: time.Now().UTC().Add(-time.Hour)}}
	c := dispatchedCycle("c1", time.Minute)
	at := time.Now().UTC()
	c.Attempts, c.DispatchedAt = 1, &at
	cycles := newWatchedCycles(c)
	jobs := &fakeJobs{}
	NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil).Tick(context.Background())
	if len(jobs.suspends) != 1 {
		t.Fatalf("suspends = %v", jobs.suspends)
	}
}

// Clock skew between the cluster and aep-api must not turn attempt 2's own pod
// into a leftover.
func TestTick_PodWithinTheClockSkewIsTheCurrentAttempts(t *testing.T) {
	c := redispatched(dispatchedCycle("c1", time.Minute), 0)
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p2", Phase: "Succeeded",
		CreatedAt: c.DispatchedAt.Add(-podClockSkew / 2)}}
	jobs := &fakeJobs{}
	NewJobWatcher(rt, newWatchedCycles(c), testWriteTargets(), jobs, nil).Tick(context.Background())
	if len(jobs.suspends) != 1 {
		t.Fatalf("suspends = %v", jobs.suspends)
	}
}

// Re-dispatched at the landing timeout while attempt 1's agent is still
// running (its deadline is longer): the same Job cannot start a second pod, so
// that Running pod IS the in-flight attempt. It counts as present (no startup
// verdict), and when it ends it is captured and suspended like any other.
func TestTick_RunningPodAtRedispatchIsTheCurrentAttempt(t *testing.T) {
	c := redispatched(dispatchedCycle("c1", 3*time.Hour), time.Hour) // past the grace
	created := time.Now().UTC().Add(-2 * time.Hour)
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running", CreatedAt: created}}
	cycles := newWatchedCycles(c)
	jobs := &fakeJobs{}
	w := NewJobWatcher(rt, cycles, testWriteTargets(), jobs, nil).WithIntervals(time.Millisecond, 10*time.Minute)
	for i := 0; i < missingTicksToFail+1; i++ {
		w.Tick(context.Background())
	}
	if len(cycles.finished) != 0 || len(jobs.suspends) != 0 {
		t.Fatalf("a running pod is present: finished %v, suspends %v", cycles.finished, jobs.suspends)
	}

	// It finishes after the dispatch: this attempt's terminal pod.
	rt.pod = openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Succeeded", CreatedAt: created, FinishedAt: time.Now().UTC()}
	rt.logs = resultLine(t)
	w.Tick(context.Background())
	if _, ok := cycles.usage["c1"]; !ok {
		t.Fatal("the in-flight pod's usage must be captured")
	}
	if len(jobs.suspends) != 1 || !cycles.suspended["c1"] {
		t.Fatalf("suspends %v, marked %v", jobs.suspends, cycles.suspended)
	}
}

// The same holds for a watcher that restarted (or never saw the pod running):
// a pre-dispatch pod that FINISHED after the dispatch was in flight at it.
func TestTick_PodFinishedAfterTheRedispatchIsTheCurrentAttempts(t *testing.T) {
	c := redispatched(dispatchedCycle("c1", 3*time.Hour), time.Hour)
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Failed",
		CreatedAt: time.Now().UTC().Add(-2 * time.Hour), FinishedAt: time.Now().UTC().Add(-time.Minute)}}
	jobs := &fakeJobs{}
	NewJobWatcher(rt, newWatchedCycles(c), testWriteTargets(), jobs, nil).Tick(context.Background())
	if len(jobs.suspends) != 1 {
		t.Fatalf("suspends = %v", jobs.suspends)
	}
}

// A cycle whose Component the settler deleted has nothing left to watch: the
// watcher stops reading its (deleted) binding.
func TestTick_SkipsACycleWhoseComponentIsDeleted(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	c := dispatchedCycle("c1", time.Hour)
	ended := time.Now().UTC().Add(-time.Hour)
	c.EndedAt, c.ComponentDeletedAt = &ended, &ended
	cycles := newWatchedCycles(c)

	newTestWatcher(rt, cycles).Tick(context.Background())

	if rt.bindingCalls != 0 {
		t.Fatalf("read the binding of a deleted Component %d times", rt.bindingCalls)
	}
}
