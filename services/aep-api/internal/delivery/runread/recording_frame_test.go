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

// Component tier for the one coupling between a cycle frame and its feed: the
// frame's `recording` is what the platform could serve of the cycle's log, and
// the platform only learns that by READING the log. A stream that projects the
// cycle before it reads the feed reports the state of an earlier read — and a
// finished run's stream makes exactly one pass, so it has no later frame to
// correct itself with.
package runread_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
	deliveryhttpapi "github.com/wso2/aep/aep-api/internal/delivery/httpapi"
	"github.com/wso2/aep/aep-api/internal/delivery/runread"
	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
)

// unavailableSeq is the feed's "logs unavailable" notice seq.
const unavailableSeq = -20

// memoFeed is the cycle feed as codingagent.CycleFeed behaves: one object
// answers both the feed read and the recording state, and the state is a
// MEMO of the last read's outcome — a failed read turns the cycle
// `unavailable` and serves the "logs unavailable" notice in place of the feed;
// a successful one clears it. Before any read, a settled cycle is `kept`.
type memoFeed struct {
	mu     sync.Mutex
	fail   bool
	failed map[string]bool
}

func newMemoFeed(fail bool) *memoFeed {
	return &memoFeed{fail: fail, failed: map[string]bool{}}
}

func (f *memoFeed) recover() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = false
}

func (f *memoFeed) CycleEvents(_ context.Context, _ *delivery.MilestoneRun, c *delivery.RunCycle, cursor string) ([]gen.RunEvent, int, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		f.failed[c.ID] = true
		notice := event(unavailableSeq, gen.RunEventKindNotice, "lead")
		notice.Code = gen.RunEventCodeGap
		return []gen.RunEvent{notice}, c.Attempts, cursor, nil
	}
	delete(f.failed, c.ID)
	if cursor != "" {
		return nil, c.Attempts, cursor, nil
	}
	return []gen.RunEvent{event(1, gen.RunEventKindToolUse, "lead")}, c.Attempts, "drained", nil
}

func (f *memoFeed) CycleProgress(context.Context, *delivery.RunCycle, int64) (*contracts.ProgressResponse, error) {
	return &contracts.ProgressResponse{Final: true}, nil
}

func (f *memoFeed) RecordingState(c *delivery.RunCycle) gen.RunCycleViewRecording {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed[c.ID] {
		return gen.RunCycleViewRecordingUnavailable
	}
	return gen.RunCycleViewRecordingKept
}

// newMemoFeedHarness wires the run stream over one finished run with one
// settled cycle, its feed and recording state both answered by feed.
func newMemoFeedHarness(t *testing.T, feed *memoFeed) *componenttest.Harness {
	t.Helper()
	runs := fakeRuns{org: "acme", rows: []delivery.MilestoneRun{specRun("r1", delivery.RunStateSucceeded)}}
	cyc := fakeCycles{byRun: map[string][]delivery.RunCycle{"r1": {cycle("c1", delivery.CycleKindCoding, true)}}}
	handlers, err := deliveryhttpapi.New(deliveryhttpapi.Deps{
		RunReads:       runread.NewReads(runs, cyc).WithRecordings(feed),
		RunProgress:    runread.NewProgressService(runs, cyc, feed).WithRecordings(feed),
		RunCommands:    runread.NewCommands(runs, &fakeRecorder{}, nil, nil),
		RunCycleBuilds: runread.NewCycleBuilds(runs, cyc, nil),
	})
	if err != nil {
		t.Fatalf("assemble delivery aggregator: %v", err)
	}
	return componenttest.New(t, componenttest.Options{Deps: edge.Deps{Delivery: handlers}})
}

// streamCycleFrames opens the finished run's stream once and returns its cycle
// frames and its event frames, in order.
func streamCycleFrames(t *testing.T, h *componenttest.Harness) (cycles, events []map[string]any) {
	t.Helper()
	rec := h.AsOrg("acme").Get(progressPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("stream: code %d (%s)", rec.Code, rec.Body.String())
	}
	for _, f := range parseFrames(t, rec.Body.String()) {
		switch f["type"] {
		case "cycle":
			cycles = append(cycles, f["cycle"].(map[string]any))
		case "event":
			events = append(events, f["event"].(map[string]any))
		}
	}
	return cycles, events
}

// TestRunProgress_FinishedRunFirstReadFails_FrameSaysUnavailable: the contract
// says `unavailable` means the log could not be read right now. A finished run
// whose first log read fails gets that word in its first and only cycle frame,
// beside the notice the same read produced — never `kept` next to "logs
// unavailable".
func TestRunProgress_FinishedRunFirstReadFails_FrameSaysUnavailable(t *testing.T) {
	h := newMemoFeedHarness(t, newMemoFeed(true))

	cycles, events := streamCycleFrames(t, h)
	if len(cycles) != 1 {
		t.Fatalf("cycle frames = %d, want 1", len(cycles))
	}
	if got := cycles[0]["recording"]; got != string(gen.RunCycleViewRecordingUnavailable) {
		t.Errorf("recording = %v, want unavailable (the read that produced this frame's feed failed)", got)
	}
	if len(events) != 1 || events[0]["seq"] != float64(unavailableSeq) || events[0]["code"] != string(gen.RunEventCodeGap) {
		t.Errorf("events = %+v, want the one logs-unavailable notice", events)
	}
}

// TestRunProgress_NextSuccessfulReadSaysKept: the failure memo is not sticky
// past a read that worked. The next stream's read succeeds, so its frame says
// `kept` and carries the feed itself.
func TestRunProgress_NextSuccessfulReadSaysKept(t *testing.T) {
	feed := newMemoFeed(true)
	h := newMemoFeedHarness(t, feed)
	if cycles, _ := streamCycleFrames(t, h); len(cycles) != 1 || cycles[0]["recording"] != string(gen.RunCycleViewRecordingUnavailable) {
		t.Fatalf("first stream cycle frames = %+v, want one unavailable", cycles)
	}

	feed.recover()
	cycles, events := streamCycleFrames(t, h)
	if len(cycles) != 1 {
		t.Fatalf("cycle frames = %d, want 1", len(cycles))
	}
	if got := cycles[0]["recording"]; got != string(gen.RunCycleViewRecordingKept) {
		t.Errorf("recording = %v, want kept (this frame's read succeeded)", got)
	}
	if len(events) != 1 || events[0]["seq"] != float64(1) {
		t.Errorf("events = %+v, want the cycle's feed", events)
	}
}
