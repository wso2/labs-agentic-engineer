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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/observability"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// feedT0 is the instant the test lines were written at.
var feedT0 = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

// v2Line is one runner v2 line with its own seq and clock.
func v2Line(seq int64, at time.Time) string {
	return fmt.Sprintf(`{"v":2,"seq":%d,"ts":%q,"kind":"agent_progress","agentId":"lead","phrase":"step %d"}`,
		seq, at.Format(time.RFC3339Nano), seq)
}

// v2Lines renders seqs as v2 lines a quarter-second apart, so four of them
// share one observer second.
func v2Lines(t *testing.T, seqs ...int64) []string {
	t.Helper()
	out := make([]string, 0, len(seqs))
	for _, s := range seqs {
		out = append(out, v2Line(s, feedT0.Add(time.Duration(s)*250*time.Millisecond)))
	}
	return out
}

func toPodLines(lines []string) []openchoreo.PodLogLine {
	out := make([]openchoreo.PodLogLine, 0, len(lines))
	for i, l := range lines {
		out = append(out, openchoreo.PodLogLine{Timestamp: feedT0.Add(time.Duration(i) * 250 * time.Millisecond), Log: l})
	}
	return out
}

// toObsLines renders lines as the observer returns them: to the second.
func toObsLines(lines []string, uid, pod string) []observability.LogLine {
	return toObsLinesAt(lines, uid, pod, feedT0)
}

func toObsLinesAt(lines []string, uid, pod string, at time.Time) []observability.LogLine {
	out := make([]observability.LogLine, 0, len(lines))
	for i, l := range lines {
		ts := at.Add(time.Duration(i) * 250 * time.Millisecond).Truncate(time.Second)
		out = append(out, observability.LogLine{Timestamp: ts, Log: l, ComponentUID: uid, PodName: pod})
	}
	return out
}

func seqsOf(evs []gen.RunEvent) []int64 {
	out := make([]int64, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Seq)
	}
	return out
}

// settledCycle is a closed, suspended cycle within retention, settled long
// enough ago for the index to have its tail.
func settledCycle(id, uid string) delivery.RunCycle {
	ended := time.Now().UTC().Add(-2 * time.Minute)
	return delivery.RunCycle{
		ID: id, OrgID: "acme", ProjectID: "shop", RunID: "run-1", Kind: delivery.CycleKindCoding,
		JobRef: "ca-" + id, Attempts: 1, ComponentUID: uid, Environment: "development",
		CreatedAt: ended.Add(-30 * time.Minute), EndedAt: &ended, JobSuspendedAt: &ended,
	}
}

func openCycle(id, uid string) delivery.RunCycle {
	c := settledCycle(id, uid)
	c.EndedAt, c.JobSuspendedAt = nil, nil
	return c
}

// fakeObserverSeq serves a sequence of answers, one per call (the last repeats).
type fakeObserverSeq struct {
	mu      sync.Mutex
	answers [][]observability.LogLine
	stats   observability.CycleLogStats
	calls   int
	last    observability.CycleLogQuery
}

func (f *fakeObserverSeq) QueryCycleLogs(_ context.Context, q observability.CycleLogQuery) ([]observability.LogLine, observability.CycleLogStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = q
	i := f.calls
	if i >= len(f.answers) {
		i = len(f.answers) - 1
	}
	f.calls++
	return f.answers[i], f.stats, nil
}

func newTestFeed(rt openchoreo.RuntimeClient, obs cycleLogQuerier) *CycleFeed {
	return NewCycleFeed(rt, obs, testWriteTargets(), 72*time.Hour)
}

// Review Focus 4: the console dedups on (cycle, attempt, seq), so the seqs a
// viewer saw from the pod must be the seqs the observer serves later.
func TestCycleFeed_PodLogAndObserverYieldTheSameSeqs(t *testing.T) {
	ctx := context.Background()
	lines := v2Lines(t, 1, 2, 3, 4)
	rawInterleaved := append([]openchoreo.PodLogLine{{Timestamp: feedT0, Log: "npm WARN raw line"}}, toPodLines(lines)...)
	live := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}, logs: rawInterleaved}
	c := settledCycle("c1", "uid-1")
	fromPod, podAttempt, _, err := newTestFeed(live, nil).Events(ctx, nil, &c, "")
	if err != nil {
		t.Fatalf("pod Events: %v", err)
	}

	// The observer returns the same second's lines in ANY order (it sorts on a
	// whole-second timestamp alone), with the raw line and a duplicate mixed in.
	obsLines := toObsLines(lines, "uid-1", "p1")
	shuffled := []observability.LogLine{obsLines[2], obsLines[0], {Timestamp: feedT0, Log: "npm WARN raw line", ComponentUID: "uid-1", PodName: "p1"}, obsLines[3], obsLines[1], obsLines[2]}
	gone := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	fromObs, obsAttempt, _, err := newTestFeed(gone, &fakeObserver{lines: shuffled}).Events(ctx, nil, &c, "")
	if err != nil {
		t.Fatalf("observer Events: %v", err)
	}

	if !reflect.DeepEqual(seqsOf(fromPod), []int64{1, 2, 3, 4}) || !reflect.DeepEqual(seqsOf(fromPod), seqsOf(fromObs)) {
		t.Fatalf("pod %v obs %v", seqsOf(fromPod), seqsOf(fromObs))
	}
	if podAttempt != 1 || obsAttempt != 1 {
		t.Fatalf("attempts pod %d obs %d, want 1", podAttempt, obsAttempt)
	}
	for i := range fromPod {
		if fromPod[i].Phrase != fromObs[i].Phrase || !fromPod[i].TS.Equal(fromObs[i].TS) {
			t.Fatalf("event %d differs: pod %+v obs %+v", i, fromPod[i], fromObs[i])
		}
	}
}

// A viewer connected across live → kept sees each event once and none missing.
func TestCycleFeed_LiveToKeptHasNoDuplicatesAndNoHoles(t *testing.T) {
	ctx := context.Background()
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}, logs: toPodLines(v2Lines(t, 1, 2, 3))}
	obs := &fakeObserver{lines: toObsLines(v2Lines(t, 1, 2, 3, 4, 5), "uid-1", "p1")}
	f := newTestFeed(rt, obs)
	clock := time.Now()
	f.now = func() time.Time { return clock }
	c := openCycle("c1", "uid-1")

	first, _, cur, _ := f.Events(ctx, nil, &c, "")
	rt.pod = openchoreo.RuntimePod{Found: false}
	clock = clock.Add(3 * time.Second) // past the memo
	second, _, _, _ := f.Events(ctx, nil, &c, cur)

	all := append(seqsOf(first), seqsOf(second)...)
	if !reflect.DeepEqual(all, []int64{1, 2, 3, 4, 5}) {
		t.Fatalf("viewer saw %v, want 1..5 once each", all)
	}
}

func TestCycleFeed_SwitchesOnPodPresenceNotJobState(t *testing.T) {
	ctx := context.Background()
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Succeeded"}, logs: toPodLines(v2Lines(t, 1))}
	obs := &fakeObserver{}
	c := settledCycle("c1", "uid-1") // suspended + ended: still read from the pod while it exists
	if _, _, _, err := newTestFeed(rt, obs).Events(ctx, nil, &c, ""); err != nil {
		t.Fatalf("Events: %v", err)
	}
	if obs.calls != 0 || rt.logCalls != 1 {
		t.Fatalf("observer %d pod log %d", obs.calls, rt.logCalls)
	}
	if rt.logSince != 0 {
		t.Fatalf("pod log read with sinceSeconds %d, want the whole log (0)", rt.logSince)
	}
}

func TestCycleFeed_DeletedComponentReadsProjectScopeByUID(t *testing.T) {
	ctx := context.Background()
	rt := &fakeRuntime{bindingErr: fmt.Errorf("x: %w", openchoreo.ErrNotFound)}
	obs := &fakeObserver{lines: toObsLines(v2Lines(t, 1, 2), "uid-1", "p1")}
	c := settledCycle("c1", "uid-1")
	evs, _, _, _ := newTestFeed(rt, obs).Events(ctx, nil, &c, "")
	if obs.got.Component != "" || obs.got.ComponentUID != "uid-1" || obs.got.Project != "shop" || obs.got.Environment != "development" {
		t.Fatalf("query %+v", obs.got)
	}
	if obs.got.From.After(c.CreatedAt) || obs.got.To.Before(c.EndedAt.Add(60*time.Second)) {
		t.Fatalf("window %v..%v", obs.got.From, obs.got.To)
	}
	if !reflect.DeepEqual(seqsOf(evs), []int64{1, 2}) {
		t.Fatalf("events %v", seqsOf(evs))
	}
}

func TestCycleFeed_LiveComponentWithoutAPodReadsComponentScope(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	obs := &fakeObserver{lines: toObsLines(v2Lines(t, 1), "uid-1", "p1")}
	c := settledCycle("c1", "uid-1")
	newTestFeed(rt, obs).Events(context.Background(), nil, &c, "")
	if obs.got.Component != openchoreo.ScopedComponentName("shop", "ca-c1") || obs.got.ComponentUID != "uid-1" {
		t.Fatalf("query %+v, want the component scope filtered on the UID", obs.got)
	}
}

// The window reaches past the suspend, which can land long after the cycle
// closed (the merge closes a cycle whose pod is still running).
func TestCycleFeed_WindowCoversALateSuspend(t *testing.T) {
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	obs := &fakeObserver{}
	c := settledCycle("c1", "uid-1")
	suspended := c.EndedAt.Add(2 * time.Hour)
	c.JobSuspendedAt = &suspended
	newTestFeed(rt, obs).Events(context.Background(), nil, &c, "")
	if obs.got.To.Before(suspended) {
		t.Fatalf("window ends %v, before the suspend at %v", obs.got.To, suspended)
	}
}

func TestCycleFeed_HoleBecomesAStableGapNotice(t *testing.T) {
	ctx := context.Background()
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	obs := &fakeObserver{lines: toObsLines(v2Lines(t, 1, 2, 5), "uid-1", "p1")}
	f := newTestFeed(rt, obs)
	clock := time.Now()
	f.now = func() time.Time { return clock }
	c := settledCycle("c1", "uid-1")

	for poll := 0; poll < 2; poll++ {
		evs, _, _, _ := f.Events(ctx, nil, &c, "")
		want := []int64{1, 2, seqGapBase - 3, 5}
		if !reflect.DeepEqual(seqsOf(evs), want) {
			t.Fatalf("poll %d: seqs %v, want %v", poll, seqsOf(evs), want)
		}
		gap := evs[2]
		if gap.Kind != gen.RunEventKindNotice || gap.Code != gen.RunEventCodeGap || gap.AgentID != leadAgentID {
			t.Fatalf("poll %d: gap %+v", poll, gap)
		}
		if gap.TS.IsZero() {
			t.Fatalf("poll %d: the gap notice carries no instant", poll)
		}
		clock = clock.Add(3 * time.Second)
	}
}

// A cursor past a gap's line does not repeat the notice.
func TestCycleFeed_GapNoticeTravelsWithTheLineAfterTheHole(t *testing.T) {
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	obs := &fakeObserver{lines: toObsLines(v2Lines(t, 1, 2, 5, 6), "uid-1", "p1")}
	c := settledCycle("c1", "uid-1")
	evs, _, _, _ := newTestFeed(rt, obs).Events(context.Background(), nil, &c, "f:1:5")
	if !reflect.DeepEqual(seqsOf(evs), []int64{6}) {
		t.Fatalf("seqs %v, want [6]", seqsOf(evs))
	}
}

func TestCycleFeed_CancelledCycleEndsWithACancelledSettle(t *testing.T) {
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	obs := &fakeObserver{lines: toObsLines(v2Lines(t, 1, 2, 3), "uid-1", "p1")}
	c := settledCycle("c1", "uid-1")
	c.AgentReason = delivery.CycleReasonCancelled
	evs, _, _, _ := newTestFeed(rt, obs).Events(context.Background(), nil, &c, "")
	last := evs[len(evs)-1]
	if last.Kind != gen.RunEventKindRunSettled || last.Outcome != gen.RunEventOutcomeCancelled || last.Seq != 4 {
		t.Fatalf("last event %+v, want run_settled{cancelled} at seq 4", last)
	}
}

// From 5.8: a cancel can close the cycle without agent_reason=cancelled (the
// cancel landed before NoteDispatch, or the loop's own Finish won the race).
// The run's cancel stamp, at or before the close, says it was a cancel.
func TestCycleFeed_ARunCancelThatClosedTheCycleEndsCancelled(t *testing.T) {
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	c := settledCycle("c1", "uid-1")
	c.AgentReason = "no_pr"
	stamp := c.EndedAt.Add(-time.Second)
	run := &delivery.MilestoneRun{ID: "run-1", CancelRequestedAt: &stamp}

	obs := &fakeObserver{lines: toObsLines(v2Lines(t, 1, 2), "uid-1", "p1")}
	evs, _, _, _ := newTestFeed(rt, obs).Events(context.Background(), run, &c, "")
	last := evs[len(evs)-1]
	if last.Kind != gen.RunEventKindRunSettled || last.Outcome != gen.RunEventOutcomeCancelled || last.Seq != 3 {
		t.Fatalf("last event %+v, want run_settled{cancelled} at seq 3", last)
	}

	// Cancelled before NoteDispatch: nothing ran, the feed is the settle alone.
	empty := &fakeObserver{}
	evs, attempt, _, _ := newTestFeed(rt, empty).Events(context.Background(), run, &c, "")
	if len(evs) != 1 || evs[0].Kind != gen.RunEventKindRunSettled || evs[0].Seq != 1 || attempt != 1 {
		t.Fatalf("events %+v attempt %d, want one run_settled at seq 1", evs, attempt)
	}
}

// A cycle that closed BEFORE the run was cancelled was not the one cancelled.
func TestCycleFeed_ACycleClosedBeforeTheCancelIsNotCancelled(t *testing.T) {
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	c := settledCycle("c1", "uid-1")
	stamp := c.EndedAt.Add(time.Minute)
	run := &delivery.MilestoneRun{ID: "run-1", CancelRequestedAt: &stamp}
	obs := &fakeObserver{lines: toObsLines(v2Lines(t, 1, 2), "uid-1", "p1")}
	evs, _, _, _ := newTestFeed(rt, obs).Events(context.Background(), run, &c, "")
	if !reflect.DeepEqual(seqsOf(evs), []int64{1, 2}) {
		t.Fatalf("seqs %v, want no synthesised settle", seqsOf(evs))
	}
}

// The runner's own settle wins; the platform never adds a second one.
func TestCycleFeed_ACancelledAttemptThatSettledItselfGetsNoSecondSettle(t *testing.T) {
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	lines := append(v2Lines(t, 1), `{"v":2,"seq":2,"ts":"2026-10-01T10:00:01Z","kind":"run_settled","agentId":"lead","outcome":"cancelled"}`)
	obs := &fakeObserver{lines: toObsLines(lines, "uid-1", "p1")}
	c := settledCycle("c1", "uid-1")
	c.AgentReason = delivery.CycleReasonCancelled
	evs, _, _, _ := newTestFeed(rt, obs).Events(context.Background(), nil, &c, "")
	if !reflect.DeepEqual(seqsOf(evs), []int64{1, 2}) {
		t.Fatalf("seqs %v, want the runner's own settle only", seqsOf(evs))
	}
}

// Lines still on their way into the index must not be pre-empted: a settle at
// lastSeq+1 while the tail is unindexed would take a real line's seq.
func TestCycleFeed_NoCancelledSettleWhileTheTailMayStillBeIndexing(t *testing.T) {
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	obs := &fakeObserver{lines: toObsLines(v2Lines(t, 1, 2), "uid-1", "p1")}
	c := settledCycle("c1", "uid-1")
	now := time.Now().UTC()
	c.EndedAt, c.JobSuspendedAt = &now, &now
	c.AgentReason = delivery.CycleReasonCancelled
	evs, _, _, _ := newTestFeed(rt, obs).Events(context.Background(), nil, &c, "")
	if !reflect.DeepEqual(seqsOf(evs), []int64{1, 2}) {
		t.Fatalf("seqs %v, want no settle inside the indexing allowance", seqsOf(evs))
	}
}

func TestCycleFeed_CursorReturnsOnlyNewEvents(t *testing.T) {
	ctx := context.Background()
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}, logs: toPodLines(v2Lines(t, 1, 2))}
	f := newTestFeed(rt, nil)
	clock := time.Now()
	f.now = func() time.Time { return clock }
	c := openCycle("c1", "uid-1")

	evs, _, cur, _ := f.Events(ctx, nil, &c, "")
	if !reflect.DeepEqual(seqsOf(evs), []int64{1, 2}) || cur != "f:1:2" {
		t.Fatalf("first poll %v cursor %q", seqsOf(evs), cur)
	}
	rt.logs = toPodLines(v2Lines(t, 1, 2, 3))
	clock = clock.Add(3 * time.Second)
	evs, _, cur, _ = f.Events(ctx, nil, &c, cur)
	if !reflect.DeepEqual(seqsOf(evs), []int64{3}) || cur != "f:1:3" {
		t.Fatalf("second poll %v cursor %q", seqsOf(evs), cur)
	}
	// Nothing new: no events, cursor held.
	evs, _, cur2, _ := f.Events(ctx, nil, &c, cur)
	if len(evs) != 0 || cur2 != cur {
		t.Fatalf("idle poll %v cursor %q", seqsOf(evs), cur2)
	}
	// A foreign cursor (the retired recording's) restarts at the first event.
	evs, _, _, _ = f.Events(ctx, nil, &c, "r:1:4096")
	if !reflect.DeepEqual(seqsOf(evs), []int64{1, 2, 3}) {
		t.Fatalf("foreign cursor %v", seqsOf(evs))
	}
}

// Lines group into attempts by pod, ordered by first timestamp; the newest pod
// is the cycle's current attempt. A cursor walks attempt 1 whole, then 2.
func TestCycleFeed_AttemptsGroupByPodAndRollForward(t *testing.T) {
	ctx := context.Background()
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	a1 := toObsLinesAt(v2Lines(t, 1, 2), "uid-1", "pod-a", feedT0)
	a2 := toObsLinesAt(v2Lines(t, 1), "uid-1", "pod-b", feedT0.Add(10*time.Minute))
	obs := &fakeObserver{lines: append(append([]observability.LogLine{}, a2...), a1...)}
	c := settledCycle("c1", "uid-1")
	c.Attempts = 2
	f := newTestFeed(rt, obs)

	evs, attempt, cur, _ := f.Events(ctx, nil, &c, "")
	if attempt != 1 || !reflect.DeepEqual(seqsOf(evs), []int64{1, 2}) {
		t.Fatalf("first read attempt %d seqs %v", attempt, seqsOf(evs))
	}
	evs, attempt, cur, _ = f.Events(ctx, nil, &c, cur)
	if attempt != 2 || !reflect.DeepEqual(seqsOf(evs), []int64{1}) || cur != "f:2:1" {
		t.Fatalf("second read attempt %d seqs %v cursor %q", attempt, seqsOf(evs), cur)
	}
}

// The previous attempt's pod, finished before this attempt was dispatched, is
// not the current attempt's (5.7's leftover rule), on either source.
func TestCycleFeed_ALeftoverPodIsThePreviousAttempt(t *testing.T) {
	ctx := context.Background()
	c := openCycle("c1", "uid-1")
	c.Attempts = 2
	dispatched := feedT0.Add(time.Hour)
	c.DispatchedAt = &dispatched

	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "pod-a", Phase: "Succeeded"}, logs: toPodLines(v2Lines(t, 1, 2))}
	evs, attempt, _, _ := newTestFeed(rt, nil).Events(ctx, nil, &c, "")
	if attempt != 1 || !reflect.DeepEqual(seqsOf(evs), []int64{1, 2}) {
		t.Fatalf("pod: attempt %d seqs %v, want attempt 1", attempt, seqsOf(evs))
	}

	gone := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	obs := &fakeObserver{lines: toObsLines(v2Lines(t, 1, 2), "uid-1", "pod-a")}
	evs, attempt, _, _ = newTestFeed(gone, obs).Events(ctx, nil, &c, "")
	if attempt != 1 || !reflect.DeepEqual(seqsOf(evs), []int64{1, 2}) {
		t.Fatalf("observer: attempt %d seqs %v, want attempt 1", attempt, seqsOf(evs))
	}
}

// A v1 producer's seq s lifts to 2s, and a synthesised agent_started to 2s-1.
func TestCycleFeed_V1LinesLiftToDoubledSeqs(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}, logs: []openchoreo.PodLogLine{
		{Timestamp: feedT0, Log: `{"schemaVersion":1,"seq":1,"kind":"phase","phase":"planning","ts":"2026-10-01T10:00:00Z"}`},
		{Timestamp: feedT0, Log: `{"schemaVersion":1,"seq":2,"kind":"activity","summary":"reading","emitter":"subagent","emitterId":"t1","emitterLabel":"Explore","ts":"2026-10-01T10:00:01Z"}`},
	}}
	c := openCycle("c1", "uid-1")
	evs, _, cur, _ := newTestFeed(rt, nil).Events(context.Background(), nil, &c, "")
	if !reflect.DeepEqual(seqsOf(evs), []int64{2, 3, 4}) || cur != "f:1:4" {
		t.Fatalf("seqs %v cursor %q, want [2 3 4] f:1:4", seqsOf(evs), cur)
	}
	if evs[1].Kind != gen.RunEventKindAgentStarted || evs[1].AgentID != "t1" {
		t.Fatalf("event at 3 = %+v, want the synthesised agent_started", evs[1])
	}
}

// The dark zone: the Job is bound, no pod yet, the cycle open.
func TestCycleFeed_NoPodYetOnAnOpenCycleNarratesTheDarkZone(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	c := openCycle("c1", "uid-1")
	evs, attempt, _, _ := newTestFeed(rt, &fakeObserver{}).Events(context.Background(), nil, &c, "")
	if len(evs) != 1 || evs[0].Seq != seqBootScheduling || evs[0].Code != gen.RunEventCodeRunnerScheduling || attempt != 1 {
		t.Fatalf("events %+v attempt %d", evs, attempt)
	}

	pending := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Pending", WaitingReason: "ImagePullBackOff"}}
	evs, _, _, _ = newTestFeed(pending, nil).Events(context.Background(), nil, &c, "")
	if len(evs) != 1 || evs[0].Seq != seqBootBackoff {
		t.Fatalf("pending pod events %+v", evs)
	}
}

func TestCycleFeed_NoUIDAndNoPodIsExpiredWithoutAnObserverRead(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	obs := &fakeObserver{}
	c := settledCycle("c1", "")
	f := newTestFeed(rt, obs)
	evs, _, _, _ := f.Events(context.Background(), nil, &c, "")
	if obs.calls != 0 || len(evs) != 1 || evs[0].Seq != seqLogsUnavailable || evs[0].Code != gen.RunEventCodeGap {
		t.Fatalf("observer calls %d events %+v", obs.calls, evs)
	}
	if st := f.State(&c); st != gen.RunCycleViewRecordingExpired {
		t.Fatalf("state %s", st)
	}
}

func TestCycleFeed_PastRetentionIsExpiredWithoutAnObserverRead(t *testing.T) {
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	obs := &fakeObserver{}
	c := settledCycle("c1", "uid-1")
	old := time.Now().Add(-73 * time.Hour)
	c.EndedAt, c.JobSuspendedAt, c.CreatedAt = &old, &old, old.Add(-time.Hour)
	evs, _, _, _ := newTestFeed(rt, obs).Events(context.Background(), nil, &c, "")
	if obs.calls != 0 || len(evs) != 1 || evs[0].Seq != seqLogsUnavailable {
		t.Fatalf("observer calls %d events %+v", obs.calls, evs)
	}
}

func TestCycleFeed_State(t *testing.T) {
	now := time.Now()
	old := now.Add(-73 * time.Hour)
	cases := []struct {
		name string
		c    delivery.RunCycle
		want gen.RunCycleViewRecording
	}{
		{"open", delivery.RunCycle{ComponentUID: "u"}, gen.RunCycleViewRecordingLive},
		{"suspended, within retention", delivery.RunCycle{ComponentUID: "u", JobSuspendedAt: &now}, gen.RunCycleViewRecordingKept},
		{"ended, within retention", delivery.RunCycle{ComponentUID: "u", EndedAt: &now}, gen.RunCycleViewRecordingKept},
		{"past retention", delivery.RunCycle{ComponentUID: "u", EndedAt: &old, JobSuspendedAt: &old}, gen.RunCycleViewRecordingExpired},
		{"ended long ago, suspended late", delivery.RunCycle{ComponentUID: "u", EndedAt: &old, JobSuspendedAt: &now}, gen.RunCycleViewRecordingKept},
		{"no uid", delivery.RunCycle{EndedAt: &now}, gen.RunCycleViewRecordingExpired},
	}
	f := NewCycleFeed(&fakeRuntime{}, nil, testWriteTargets(), 72*time.Hour)
	for _, tc := range cases {
		if got := f.State(&tc.c); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestCycleFeed_ObserverErrorIsUnavailableNotEmpty(t *testing.T) {
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	obs := &fakeObserver{err: errors.New("observer: 503")}
	f := newTestFeed(rt, obs)
	clock := time.Now()
	f.now = func() time.Time { return clock }
	c := settledCycle("c1", "uid-1")

	evs, _, cur, err := f.Events(context.Background(), nil, &c, "f:1:7")
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(evs) != 1 || evs[0].Code != gen.RunEventCodeGap || evs[0].Seq != seqLogsUnavailable {
		t.Fatalf("events %+v", evs)
	}
	if cur != "f:1:7" {
		t.Fatalf("cursor %q, want the caller's cursor held", cur)
	}
	if st := f.State(&c); st != gen.RunCycleViewRecordingUnavailable {
		t.Fatalf("state %s, want unavailable", st)
	}
	clock = clock.Add(61 * time.Second)
	if st := f.State(&c); st != gen.RunCycleViewRecordingKept {
		t.Fatalf("state %s after the hold, want kept", st)
	}
}

// The failure memo answers later State-only callers (the run list) for the
// hold, but only until a read works: the next successful read is `kept` at
// once, inside the hold, so a recovered log never reads as lost.
func TestCycleFeed_SuccessfulReadAfterAFailureIsKept(t *testing.T) {
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	obs := &fakeObserver{err: errors.New("observer: 500")}
	f := newTestFeed(rt, obs)
	clock := time.Now()
	f.now = func() time.Time { return clock }
	c := settledCycle("c1", "uid-1")

	if evs, _, _, _ := f.Events(context.Background(), nil, &c, ""); len(evs) != 1 || evs[0].Seq != seqLogsUnavailable {
		t.Fatalf("failed read events %+v, want the unavailable notice", evs)
	}
	if st := f.State(&c); st != gen.RunCycleViewRecordingUnavailable {
		t.Fatalf("state %s after a failed read, want unavailable", st)
	}

	obs.err, obs.lines = nil, toObsLines(v2Lines(t, 1, 2), "uid-1", "ca-c1-p1")
	clock = clock.Add(feedReadTTL + time.Second) // past the shared read, inside the hold
	evs, _, _, err := f.Events(context.Background(), nil, &c, "")
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(evs) != 2 || evs[0].Seq != 1 {
		t.Fatalf("recovered read events %+v, want seqs 1..2", evs)
	}
	if st := f.State(&c); st != gen.RunCycleViewRecordingKept {
		t.Fatalf("state %s after a successful read, want kept", st)
	}
}

func TestCycleFeed_NoObserverConfiguredIsUnavailable(t *testing.T) {
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	f := newTestFeed(rt, nil)
	c := settledCycle("c1", "uid-1")
	evs, _, _, _ := f.Events(context.Background(), nil, &c, "")
	if len(evs) != 1 || evs[0].Seq != seqLogsUnavailable {
		t.Fatalf("events %+v", evs)
	}
	if st := f.State(&c); st != gen.RunCycleViewRecordingUnavailable {
		t.Fatalf("state %s", st)
	}
}

// N viewers polling the same cycle cost one OpenChoreo log read per 2 s.
func TestCycleFeed_ViewersShareOneReadPerTick(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}, logs: toPodLines(v2Lines(t, 1))}
	f := newTestFeed(rt, nil)
	clock := time.Now()
	f.now = func() time.Time { return clock }
	c := openCycle("c1", "uid-1")
	for i := 0; i < 5; i++ {
		f.Events(context.Background(), nil, &c, "")
	}
	if rt.logCalls != 1 {
		t.Fatalf("pod log reads %d, want 1 for five viewers in one tick", rt.logCalls)
	}
	clock = clock.Add(2100 * time.Millisecond)
	f.Events(context.Background(), nil, &c, "")
	if rt.logCalls != 2 {
		t.Fatalf("pod log reads %d, want a fresh read after the memo", rt.logCalls)
	}
}

func TestCycleFeed_RedactsSecretsOnBothSources(t *testing.T) {
	secret := "ghp_" + strings.Repeat("a", 36)
	line := fmt.Sprintf(`{"v":2,"seq":1,"ts":"2026-10-01T10:00:00Z","kind":"agent_progress","agentId":"lead","phrase":"push %s"}`, secret)
	pod := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}, logs: toPodLines([]string{line})}
	c := openCycle("c1", "uid-1")
	evs, _, _, _ := newTestFeed(pod, nil).Events(context.Background(), nil, &c, "")
	if len(evs) != 1 || strings.Contains(evs[0].Phrase, secret) {
		t.Fatalf("pod events %+v leak the token", evs)
	}
	gone := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	obs := &fakeObserver{lines: toObsLines([]string{line}, "uid-1", "p1")}
	evs, _, _, _ = newTestFeed(gone, obs).Events(context.Background(), nil, &c, "")
	if len(evs) != 1 || strings.Contains(evs[0].Phrase, secret) {
		t.Fatalf("observer events %+v leak the token", evs)
	}
}

// The observer's index lags the pod: a tail that is not there yet arrives on a
// later read and is delivered then, never twice.
func TestCycleFeed_AnIndexThatCatchesUpDeliversTheTailOnce(t *testing.T) {
	ctx := context.Background()
	rt := &fakeRuntime{bindingErr: openchoreo.ErrNotFound}
	obs := &fakeObserverSeq{answers: [][]observability.LogLine{
		toObsLines(v2Lines(t, 1, 2), "uid-1", "p1"),
		toObsLines(v2Lines(t, 1, 2, 3), "uid-1", "p1"),
	}}
	f := newTestFeed(rt, obs)
	clock := time.Now()
	f.now = func() time.Time { return clock }
	c := settledCycle("c1", "uid-1")
	first, _, cur, _ := f.Events(ctx, nil, &c, "")
	clock = clock.Add(3 * time.Second)
	second, _, _, _ := f.Events(ctx, nil, &c, cur)
	if all := append(seqsOf(first), seqsOf(second)...); !reflect.DeepEqual(all, []int64{1, 2, 3}) {
		t.Fatalf("seqs %v", all)
	}
}

// R3-I1: an open cycle whose pod is Pending and has never run has written
// nothing, so the observer is not asked, and a pod-log read that fails on a
// container not yet created is the dark zone, not "logs unavailable".
func TestCycleFeed_PendingPodNarratesTheDarkZoneWithoutAnObserverRead(t *testing.T) {
	pending := openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Pending", WaitingReason: "ContainerCreating"}
	for name, logErr := range map[string]error{
		"log not found":  openchoreo.ErrNotFound,
		"log read fails": errors.New("openchoreo: internal server error"),
	} {
		t.Run(name, func(t *testing.T) {
			rt := &fakeRuntime{pod: pending, logErr: logErr}
			obs := &fakeObserver{err: errors.New("observer: 500")}
			f := newTestFeed(rt, obs)
			c := openCycle("c1", "uid-1")
			evs, attempt, _, _ := f.Events(context.Background(), nil, &c, "")
			if obs.calls != 0 {
				t.Fatalf("observer calls %d, want 0 for a pod that never ran", obs.calls)
			}
			if len(evs) != 1 || evs[0].Seq >= 0 || evs[0].Seq == seqLogsUnavailable || attempt != 1 {
				t.Fatalf("events %+v attempt %d, want one bootstrap marker", evs, attempt)
			}
			if st := f.State(&c); st != gen.RunCycleViewRecordingLive {
				t.Fatalf("state %s, want live", st)
			}
		})
	}
}

// R3-I1: an open cycle with no pod yet narrates its dark zone even when the
// observer read fails, as the v1 resolver does.
func TestCycleFeed_ObserverErrorOnAnOpenCycleWithoutAPodIsTheDarkZone(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}
	obs := &fakeObserver{err: errors.New("observer: 500")}
	c := openCycle("c1", "uid-1")
	evs, _, _, _ := newTestFeed(rt, obs).Events(context.Background(), nil, &c, "")
	if len(evs) != 1 || evs[0].Seq != seqBootScheduling {
		t.Fatalf("events %+v, want the scheduling marker", evs)
	}
}

// A running pod whose log read fails is still a failed read: it has spoken,
// and the dark-zone marker would hide that.
func TestCycleFeed_RunningPodLogFailureIsUnavailable(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}, logErr: errors.New("openchoreo: 500")}
	c := openCycle("c1", "uid-1")
	evs, _, _, _ := newTestFeed(rt, &fakeObserver{}).Events(context.Background(), nil, &c, "")
	if len(evs) != 1 || evs[0].Seq != seqLogsUnavailable {
		t.Fatalf("events %+v, want logs unavailable", evs)
	}
}

// 5.11 M-1: attempts are numbered on producer-seq lines only. A crashed
// attempt that wrote only bootstrap output is in the component scope but not
// the project scope (its phrase needs agentId), so counting it would shift
// earlier attempts by one across the Component's deletion.
func TestCycleFeed_SeqLessPodsDoNotTakeAnAttemptNumber(t *testing.T) {
	ctx := context.Background()
	podA := toObsLinesAt(v2Lines(t, 1, 2), "uid-1", "pod-a", feedT0)
	podB := []observability.LogLine{{Timestamp: feedT0.Add(5 * time.Minute), Log: "npm ERR! crashed before the runner", ComponentUID: "uid-1", PodName: "pod-b"}}
	podC := toObsLinesAt(v2Lines(t, 1, 2, 3), "uid-1", "pod-c", feedT0.Add(10*time.Minute))
	rawC := []observability.LogLine{{Timestamp: feedT0.Add(9 * time.Minute), Log: "npm WARN raw", ComponentUID: "uid-1", PodName: "pod-c"}}
	c := settledCycle("c1", "uid-1")
	c.Attempts = 3

	walk := func(f *CycleFeed) [][2]any {
		var out [][2]any
		cur := ""
		for i := 0; i < 4; i++ {
			evs, attempt, next, _ := f.Events(ctx, nil, &c, cur)
			if len(evs) > 0 {
				out = append(out, [2]any{attempt, seqsOf(evs)})
			}
			cur = next
		}
		return out
	}
	component := &fakeObserver{lines: append(append(append(append([]observability.LogLine{}, podA...), podB...), rawC...), podC...)}
	project := &fakeObserver{lines: append(append([]observability.LogLine{}, podA...), podC...)}
	fromComponent := walk(newTestFeed(&fakeRuntime{pod: openchoreo.RuntimePod{Found: false}}, component))
	fromProject := walk(newTestFeed(&fakeRuntime{bindingErr: openchoreo.ErrNotFound}, project))
	if !reflect.DeepEqual(fromComponent, fromProject) {
		t.Fatalf("component scope %v, project scope %v: want identical attempts and seqs", fromComponent, fromProject)
	}
	want := [][2]any{{2, []int64{1, 2}}, {3, []int64{1, 2, 3}}}
	if !reflect.DeepEqual(fromProject, want) {
		t.Fatalf("attempts %v, want %v", fromProject, want)
	}
}
