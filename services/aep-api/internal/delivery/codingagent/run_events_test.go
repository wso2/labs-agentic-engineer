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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

// TestBootstrapRunEventNarratesTheDarkZone covers the stretch before the runner
// writes anything. The states are the same ones the v1 render reports — the two
// share bootstrapState — but the SHAPE is deliberately different: a pod that has
// not started is not an agent, so this is a platform notice and never an
// agent_progress phrase put in the mouth of an agent that does not exist yet.
func TestBootstrapRunEventNarratesTheDarkZone(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		podFound      bool
		phase, reason string
		message       string
		wantSeq       int64
		wantCode      gen.RunEventCode
		wantLevel     gen.RunEventLevel
	}{
		{"no pod yet", false, "", "", "", seqBootScheduling, gen.RunEventCodeRunnerScheduling, gen.RunEventLevelInfo},
		{"pulling", true, "Pending", "ContainerCreating", "", seqBootPulling, gen.RunEventCodeRunnerPullingImage, gen.RunEventLevelInfo},
		{"pull backoff", true, "Pending", "ImagePullBackOff", "", seqBootBackoff, gen.RunEventCodeRunnerImagePullBackOff, gen.RunEventLevelWarn},
		{"secrets", true, "Pending", "CreateContainerConfigError", "", seqBootConfig, gen.RunEventCodeRunnerConfigError, gen.RunEventLevelWarn},
		{"no capacity", true, "Pending", "Unschedulable", "0/3 nodes are available: Too many pods.", seqBootUnschedulable, gen.RunEventCodeRunnerUnschedulable, gen.RunEventLevelWarn},
		{"booting", true, "Running", "", "", seqBootStarting, gen.RunEventCodeRunnerStarting, gen.RunEventLevelInfo},
	}
	observedAt := time.Date(2026, 9, 9, 9, 15, 47, 0, time.UTC)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := bootstrapRunEvent(observedAt, tc.podFound, tc.phase, tc.reason, tc.message)
			if ev.Kind != gen.RunEventKindNotice || ev.AgentID != leadAgentID {
				t.Errorf("dark-zone event = %+v, want a notice on the lead", ev)
			}
			if ev.Seq != tc.wantSeq {
				t.Errorf("seq = %d, want the stable %d — the console dedups on it", ev.Seq, tc.wantSeq)
			}
			// The CODE is the whole message. Wording lives in @aep/progress-view,
			// keyed off it; a producer that shipped the sentence would be a second
			// place the copy could change.
			if ev.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", ev.Code, tc.wantCode)
			}
			if ev.Level != tc.wantLevel {
				t.Errorf("level = %q, want %q", ev.Level, tc.wantLevel)
			}
			if !ev.TS.Equal(observedAt) {
				t.Errorf("ts = %s, want the instant the platform read the pod (%s) — see TestPlatformNoticeCarriesARealInstant", ev.TS, observedAt)
			}
		})
	}
}

// TestPlatformNoticeCarriesARealInstant pins the ONE fact every platform-minted
// event on this feed has to get right, and it is pinned on the WIRE FORM because
// that is where it went wrong.
//
// `RunEvent.ts` is required by the contract and generates as a `time.Time`, so a
// notice built without one does not omit the field: it marshals Go's zero value
// into `0001-01-01T00:00:00Z`, a perfectly well-formed date that no consumer can
// tell from a real one. A console did exactly what it should with it and
// subtracted it from the clock, and because these notices belong to the lead,
// the lead's age column read `1065409035m47s` — the 2026 years from Go's zero
// time to the afternoon someone was watching a live run.
//
// So: a real instant, or the field would have to be absent, and the contract
// does not allow absent. The platform is the producer of these events and it
// knows when it derived each one, which makes the real instant the honest answer
// rather than merely the safe one.
func TestPlatformNoticeCarriesARealInstant(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 9, 9, 15, 47, 0, time.UTC)
	notices := map[string]gen.RunEvent{
		// The dark zone, which is at the head of very nearly every recording:
		// the recorder writes it before the runner has said anything at all.
		"dark zone":       bootstrapRunEvent(at, true, "Pending", "ContainerCreating", ""),
		"lost recording":  logsUnavailableRunEvent(at, "the recording could not be read"),
		"gap in the feed": gapNotice(at, 7, 3),
		"bare":            platformNotice(at, 9, gen.RunEventLevelInfo, "something the platform wants to say"),
	}
	for name, ev := range notices {
		t.Run(name, func(t *testing.T) {
			if !ev.TS.Equal(at) {
				t.Errorf("%s notice is stamped %s, want the instant its caller derived it (%s)", name, ev.TS, at)
			}
			// The wire form, because a struct field that reads fine in Go is what
			// marshalled into a date two millennia old.
			b, err := json.Marshal(ev)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if strings.Contains(string(b), "0001-01-01") {
				t.Errorf("%s notice on the wire carries Go's zero time: %s", name, b)
			}
		})
	}
}

// TestBootstrapRunEvent_DetailOnlyWhereTheCodeCannotSpeak pins the two
// exceptions to "no prose": which resource the scheduler ran out of, and a
// waiting reason this build has never seen. Everything else says it with a code.
func TestBootstrapRunEvent_DetailOnlyWhereTheCodeCannotSpeak(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 9, 9, 15, 47, 0, time.UTC)
	if ev := bootstrapRunEvent(at, true, "Running", "", ""); ev.Detail != "" {
		t.Errorf("booting notice carried prose %q — the code says it", ev.Detail)
	}
	full := bootstrapRunEvent(at, true, "Pending", "Unschedulable", "0/3 nodes are available: Too many pods.\nsecond line")
	if !strings.Contains(full.Detail, "Too many pods") || strings.Contains(full.Detail, "second line") {
		t.Errorf("unschedulable detail = %q, want the scheduler's FIRST line", full.Detail)
	}
	odd := bootstrapRunEvent(at, true, "Pending", "SomeBrandNewReason", "")
	if odd.Code != gen.RunEventCodeRunnerPullingImage || odd.Detail != "SomeBrandNewReason" {
		t.Errorf("unknown waiting reason = %+v, want it bucketed under pulling with the reason as detail", odd)
	}
}

// recordedReader builds a reader over a fresh recording store, plus the store
// itself so a test can lay a recording down by hand.
func recordedReader(t *testing.T) (*AgentProgressReader, *RecordingStore, string) {
	t.Helper()
	root := t.TempDir()
	store := NewRecordingStore(root, 0)
	return NewAgentProgressReader(nil, nil).WithRecordings(store), store, root
}

// recordEvents lays down one attempt's file through the store's own writer.
func recordEvents(t *testing.T, store *RecordingStore, cycleID string, attempt int, events ...gen.RunEvent) {
	t.Helper()
	if _, err := store.Begin("acme", cycleID, attempt); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, _, err := store.Append("acme", cycleID, attempt, events, recordCursor{}); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

func recordedEvent(seq int64, summary string) gen.RunEvent {
	return gen.RunEvent{
		V: gen.RunEventV2, Seq: seq, Kind: gen.RunEventKindToolUse,
		AgentID: leadAgentID, Tool: "Read", Summary: summary,
	}
}

// TestCycleEvents_ReplaysFromZeroAndFromMidFileIdentically is the property the
// whole phase rests on: a viewer that reloads mid-run replays from the FIRST
// event, and one that carries a cursor picks up exactly where it left off with
// nothing repeated and nothing skipped. The 64KiB window and the 200-event
// post-mortem are both gone with it.
func TestCycleEvents_ReplaysFromZeroAndFromMidFileIdentically(t *testing.T) {
	t.Parallel()

	r, store, _ := recordedReader(t)
	want := []gen.RunEvent{recordedEvent(2, "one"), recordedEvent(4, "two"), recordedEvent(6, "three")}
	recordEvents(t, store, "c1", 1, want...)

	cyc := liveCycle("c1")
	cyc.Attempts = 1

	whole, attempt, cursor, err := r.CycleEvents(context.Background(), cyc, "")
	if err != nil {
		t.Fatalf("CycleEvents: %v", err)
	}
	if attempt != 1 {
		t.Errorf("attempt = %d, want 1", attempt)
	}
	if len(whole) != 3 || whole[0].Seq != 2 || whole[2].Seq != 6 {
		t.Fatalf("replay from zero = %+v, want all three events", whole)
	}

	// A second poll from the returned cursor has nothing new, and does not move.
	rest, _, cursor2, err := r.CycleEvents(context.Background(), cyc, cursor)
	if err != nil {
		t.Fatalf("CycleEvents (second poll): %v", err)
	}
	if len(rest) != 0 {
		t.Errorf("second poll = %+v, want nothing new", rest)
	}
	if cursor2 != cursor {
		t.Errorf("cursor moved on an empty poll: %q → %q", cursor, cursor2)
	}

	// Appending more and polling from the mid-file cursor yields ONLY the new
	// events — and the same events a full replay would have shown there.
	if _, _, err := store.Append("acme", "c1", 1, []gen.RunEvent{recordedEvent(8, "four")}, recordCursor{}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	tail, _, _, err := r.CycleEvents(context.Background(), cyc, cursor)
	if err != nil {
		t.Fatalf("CycleEvents (tail): %v", err)
	}
	if len(tail) != 1 || tail[0].Summary != "four" {
		t.Fatalf("tail = %+v, want just the newly appended event", tail)
	}
	full, _, _, err := r.CycleEvents(context.Background(), cyc, "")
	if err != nil {
		t.Fatalf("CycleEvents (full): %v", err)
	}
	if len(full) != 4 || full[3].Summary != "four" {
		t.Fatalf("full replay = %+v, want all four in order", full)
	}
}

// TestCycleEvents_WalksAttemptsOldestFirst pins the re-dispatch read: a second
// attempt is a second FILE (its seqs restart at 1), and a viewer replays attempt
// 1 whole before attempt 2 — which is what lets the console show attempts as
// sequential crews under one cycle.
func TestCycleEvents_WalksAttemptsOldestFirst(t *testing.T) {
	t.Parallel()

	r, store, _ := recordedReader(t)
	recordEvents(t, store, "c1", 1, recordedEvent(2, "first attempt"))
	recordEvents(t, store, "c1", 2, recordedEvent(2, "second attempt"))

	cyc := liveCycle("c1")
	cyc.Attempts = 2

	first, attempt, cursor, err := r.CycleEvents(context.Background(), cyc, "")
	if err != nil {
		t.Fatalf("CycleEvents: %v", err)
	}
	if attempt != 1 || len(first) != 1 || first[0].Summary != "first attempt" {
		t.Fatalf("first read = attempt %d %+v, want attempt 1's event", attempt, first)
	}
	second, attempt, _, err := r.CycleEvents(context.Background(), cyc, cursor)
	if err != nil {
		t.Fatalf("CycleEvents (roll): %v", err)
	}
	// The ATTEMPT is the reader's, not the row's: both events carry seq 2, and a
	// client deduping on (cycle, attempt, seq) would drop one of them if the
	// platform filed both under the attempt in flight.
	if attempt != 2 || len(second) != 1 || second[0].Summary != "second attempt" {
		t.Fatalf("second read = attempt %d %+v, want attempt 2's event", attempt, second)
	}
}

// TestCycleEvents_NoRecordingIsSilentAndNone pins the honest empty state: a
// cycle the platform never recorded (a v1 cycle, or one whose recorder has not
// started) serves nothing and REPORTS nothing — never a synthetic "lost" notice,
// which would tell a user something had been taken away.
func TestCycleEvents_NoRecordingIsSilentAndNone(t *testing.T) {
	t.Parallel()

	r, _, _ := recordedReader(t)
	cyc := liveCycle("c1")
	cyc.Attempts = 1

	got, _, _, err := r.CycleEvents(context.Background(), cyc, "")
	if err != nil {
		t.Fatalf("CycleEvents: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("events = %+v, want nothing", got)
	}
	if st := r.RecordingState(cyc); st != gen.RunCycleViewRecordingNone {
		t.Errorf("recording = %q, want none", st)
	}
}

// TestCycleEvents_LostRecordingSaysSo covers the other empty screen: the
// platform HAD a record and cannot serve it. It has to be distinguishable from
// `none` — they look identical to a reader and are very different bugs.
func TestCycleEvents_LostRecordingSaysSo(t *testing.T) {
	t.Parallel()

	r, store, root := recordedReader(t)
	recordEvents(t, store, "c1", 1, recordedEvent(2, "one"))
	// The events file goes; the directory and its state.json stay.
	if err := os.Remove(filepath.Join(gitfs.RunsDir(root), "acme", "c1", "events.1.ndjson")); err != nil {
		t.Fatalf("remove events file: %v", err)
	}

	cyc := liveCycle("c1")
	cyc.Attempts = 1
	got, _, _, err := r.CycleEvents(context.Background(), cyc, "")
	if err != nil {
		t.Fatalf("CycleEvents: %v", err)
	}
	if len(got) != 1 || got[0].Code != gen.RunEventCodeGap || got[0].Seq != seqLogsUnavailable {
		t.Fatalf("events = %+v, want the single stable-seq gap notice", got)
	}
	if st := r.RecordingState(cyc); st != gen.RunCycleViewRecordingLost {
		t.Errorf("recording = %q, want lost", st)
	}
}

// TestCycleEvents_NoStoreReportsNoneRatherThanTailingThePod pins the degraded
// boot. Falling back to a per-viewer pod tail would hide the fact that nothing
// is being recorded, which is precisely what `none` is for.
func TestCycleEvents_NoStoreReportsNoneRatherThanTailingThePod(t *testing.T) {
	t.Parallel()

	r := NewAgentProgressReader(&stubLive{tail: LiveTail{Text: "should never be read\n"}}, nil)
	cyc := liveCycle("c1")
	got, _, _, err := r.CycleEvents(context.Background(), cyc, "")
	if err != nil {
		t.Fatalf("CycleEvents: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("events = %+v, want nothing from a boot that records nothing", got)
	}
	if st := r.RecordingState(cyc); st != gen.RunCycleViewRecordingNone {
		t.Errorf("recording = %q, want none", st)
	}
}

// TestParseFeedCursor_ForeignCursorRestartsFromTheBeginning pins the cursor's
// one safety rule: it is opaque, and anything that did not come out of this
// reader is treated as "start over" rather than seeked with. A duplicate event
// is deduped by (cycle, attempt, seq); a silently empty feed is not.
func TestParseFeedCursor_ForeignCursorRestartsFromTheBeginning(t *testing.T) {
	t.Parallel()

	for _, cursor := range []string{"", "1757000000000", "r:", "r:0:5", "r:abc:1", "nonsense"} {
		if attempt, offset := parseFeedCursor(cursor); attempt != 0 || offset != 0 {
			t.Errorf("parseFeedCursor(%q) = (%d,%d), want (0,0)", cursor, attempt, offset)
		}
	}
	if attempt, offset := parseFeedCursor(formatFeedCursor(3, 4096)); attempt != 3 || offset != 4096 {
		t.Errorf("round trip = (%d,%d), want (3,4096)", attempt, offset)
	}
}
