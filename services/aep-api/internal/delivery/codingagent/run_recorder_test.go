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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

// fakeClock is ONE clock shared by the recorder and its log source, which is
// what lets a test state a relationship between two instants — a read that took
// longer than the window it was about to ask for. That relationship is the whole
// of the failure these tests exist to pin, and it cannot be expressed against
// the wall clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(at time.Time) *fakeClock { return &fakeClock{t: at.UTC()} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fakeLogLine is one line of a synthetic container log: the instant the
// container wrote it, and what it wrote.
type fakeLogLine struct {
	at   time.Time
	text string
}

// fakeRecordSource is a synthetic pod log served against a fake clock, and it
// HONOURS THE WINDOW IT IS ASKED FOR.
//
// Its predecessor did not, and that is why the loss below shipped: it recorded
// each call's `sinceSeconds` and then served scripted pages by call index, so a
// window that missed a line was indistinguishable from one that did not, and no
// test could express the failure the code actually had. A fake that ignores the
// parameter under test cannot fail for the reason the code is wrong.
//
// Two properties carry every test here:
//
//   - a line outside the requested window is NOT served, ever. The window is
//     absolute and applied exactly. (The real API rounds to whole seconds, which
//     only widens it; a fake that rounded too would hand a buggy window slack it
//     must never be allowed to rely on.)
//   - a call's answer describes the log AS OF THE INSTANT THE CALL WAS ISSUED,
//     and the call then takes `latency` on the clock. That is the conservative
//     reading of a real read and the only honest one: a measured OpenChoreo log
//     call took 13.22 s, and nothing in the response says which moment inside it
//     the log was read at. It is also exactly the case that a read window
//     anchored on when the PREVIOUS read returned gets wrong.
type fakeRecordSource struct {
	mu    sync.Mutex
	clock *fakeClock
	lines []fakeLogLine
	pod   openchoreo.RuntimePod
	err   error
	// latency is how long each read takes on the clock, by call index; the last
	// value repeats, and no values at all means instant.
	latency []time.Duration
	// block makes every read wait on the caller's context instead of answering,
	// which is how a test drives a poll into its own deadline.
	block bool

	windows    []time.Time // the absolute `since` of every read, in order
	binding    string
	bindingErr error
	bindings   int
	seen       chan struct{}
}

// Binding resolves the attempt's release binding, counting the calls: it is
// fixed for the attempt, so how OFTEN it is asked for is part of the contract.
func (f *fakeRecordSource) Binding(_ context.Context, _, _, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bindings++
	if f.bindingErr != nil {
		return "", f.bindingErr
	}
	if f.binding == "" {
		return "rb-dev", nil
	}
	return f.binding, nil
}

func (f *fakeRecordSource) ReadSince(ctx context.Context, _, _ string, since time.Time) (LiveTail, error) {
	f.mu.Lock()
	issuedAt := f.clockNow()
	n := len(f.windows)
	f.windows = append(f.windows, since)
	lat := time.Duration(0)
	if len(f.latency) > 0 {
		if n >= len(f.latency) {
			n = len(f.latency) - 1
		}
		lat = f.latency[n]
	}
	lines, pod, err, block := f.lines, f.pod, f.err, f.block
	seen := f.seen
	f.mu.Unlock()

	if seen != nil {
		select {
		case seen <- struct{}{}:
		default:
		}
	}
	if block {
		<-ctx.Done()
		return LiveTail{}, ctx.Err()
	}
	if f.clock != nil && lat > 0 {
		f.clock.advance(lat)
	}
	if err != nil {
		return LiveTail{}, err
	}
	if !pod.Found {
		return LiveTail{Pod: pod}, nil
	}
	var b strings.Builder
	for _, ln := range lines {
		if ln.at.After(issuedAt) {
			continue // the container had not written it when this read was issued
		}
		if !since.IsZero() && ln.at.Before(since) {
			continue // outside the window: this read did not ask for it
		}
		b.WriteString(ln.at.UTC().Format(time.RFC3339Nano))
		b.WriteByte(' ')
		b.WriteString(ln.text)
		b.WriteByte('\n')
	}
	return LiveTail{Text: b.String(), Pod: pod}, nil
}

// clockNow must be called with the mutex held.
func (f *fakeRecordSource) clockNow() time.Time {
	if f.clock == nil {
		return time.Now().UTC()
	}
	return f.clock.now()
}

// windowCalls is the absolute `since` of every read, in order. A test reads it
// to pin how wide the recorder asked, and how many times it had to ask.
func (f *fakeRecordSource) windowCalls() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Time{}, f.windows...)
}

func (f *fakeRecordSource) setPod(pod openchoreo.RuntimePod) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pod = pod
}

func (f *fakeRecordSource) setLines(lines ...fakeLogLine) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = lines
	f.windows = nil
}

func (f *fakeRecordSource) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeRecordSource) bindingCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bindings
}

// spyArchive counts its calls, so a test can prove the LIVE pod was asked
// first: while the Component exists the lines are still there, and the archive
// is the fallback for what the pod can no longer give back.
type spyArchive struct {
	mu    sync.Mutex
	text  string
	err   error
	calls int
}

func (a *spyArchive) CycleArchive(context.Context, ArchiveScope) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	return a.text, a.err
}

func (a *spyArchive) reads() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

// v1LogLine is one runner NDJSON event as a synthetic log line: the kubelet
// wrote it at `at`, and the envelope says the same.
func v1LogLine(seq int, at time.Time, body string) fakeLogLine {
	return fakeLogLine{at: at.UTC(), text: fmt.Sprintf(`{"schemaVersion":1,"ts":%q,"seq":%d,%s}`,
		at.UTC().Format(time.RFC3339Nano), seq, body)}
}

// pageOf renders lines into the raw shape a read returns, for the few tests that
// hand a page straight to ingest rather than going through a read.
func pageOf(lines ...fakeLogLine) string {
	var b strings.Builder
	for _, ln := range lines {
		b.WriteString(ln.at.UTC().Format(time.RFC3339Nano))
		b.WriteByte(' ')
		b.WriteString(ln.text)
		b.WriteByte('\n')
	}
	return b.String()
}

func runningPod() openchoreo.RuntimePod {
	return openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"}
}

func succeededPod() openchoreo.RuntimePod {
	return openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Succeeded"}
}

// recorderFixture wires a recorder over a temp workspace root and returns the
// pieces a test drives. Recorder and source share the fixture's clock.
type recorderFixture struct {
	rec   *CycleRecorder
	store *RecordingStore
	src   *fakeRecordSource
	clock *fakeClock
	root  string
	cycle *delivery.RunCycle
}

func newRecorderFixture(t *testing.T, now time.Time, lines ...fakeLogLine) *recorderFixture {
	t.Helper()
	return newCappedRecorderFixture(t, 0, now, lines...)
}

func newCappedRecorderFixture(t *testing.T, maxBytes int64, now time.Time, lines ...fakeLogLine) *recorderFixture {
	t.Helper()
	root := t.TempDir()
	store := NewRecordingStore(root, maxBytes)
	clock := newFakeClock(now)
	src := &fakeRecordSource{clock: clock, lines: lines, pod: runningPod(), seen: make(chan struct{}, 64)}
	cyc := liveCycle("c1")
	cyc.Attempts = 1
	rec := NewCycleRecorder(src, store)
	rec.now = clock.now
	return &recorderFixture{rec: rec, store: store, src: src, clock: clock, root: root, cycle: cyc}
}

// session builds a session WITHOUT its goroutine, so a test drives poll() one
// call at a time instead of racing a ticker.
func (f *recorderFixture) session(t *testing.T, attempt int) *recordingSession {
	t.Helper()
	cyc := *f.cycle
	cyc.Attempts = attempt
	s := newRecordingSession(f.rec, &cyc)
	cur, err := f.store.Begin(cyc.OrgID, cyc.ID, attempt)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	s.cur = cur
	return s
}

// meta reads the cycle's state.json — the one file a reader still has after the
// pod is gone, so what it says has to agree with the events beside it.
func (f *recorderFixture) meta(t *testing.T) recordingMeta {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(gitfs.RunsDir(f.root), "acme", "c1", recordingStateFile))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	var m recordingMeta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse state.json: %v", err)
	}
	return m
}

// recorded reads back everything the recorder wrote for one attempt.
func (f *recorderFixture) recorded(t *testing.T, attempt int) []gen.RunEvent {
	t.Helper()
	events, _, err := f.store.ReadFrom("acme", "c1", attempt, 0)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	return events
}

// gapNotices are the rows the platform wrote to say the feed is short.
func gapNotices(events []gen.RunEvent) []gen.RunEvent {
	var out []gen.RunEvent
	for i := range events {
		if events[i].Code == gen.RunEventCodeGap {
			out = append(out, events[i])
		}
	}
	return out
}

// TestRecorder_AStalledReadWidensTheNextWindowInsteadOfLeavingAHole is the
// measured incident, replayed.
//
// One run lost three bursts of events — two, then two, then seven — and told the
// user each time that they "could not be recovered". Nothing had gone wrong with
// the pod, the log API or the network: every poll returned 200, and probing the
// log API directly afterwards returned a complete, in-order `1..478`. What went
// wrong is that the recorder measured its read window from ITS OWN CLOCK,
// stamped when the previous read returned, which asserts that the answer
// described that instant. One `logs` handler took 13.22 s. The next window then
// began after lines nobody had ever read, and because the cursor only moves
// forward, nothing asked for them again.
//
// Here: two lines are read, the next read stalls for 13 s — far past the 3 s
// overlap — and three more lines land while it does. The recording must hold
// every one of them.
func TestRecorder_AStalledReadWidensTheNextWindowInsteadOfLeavingAHole(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 9, 8, 9, 29, 40, 0, time.UTC)
	f := newRecorderFixture(t, t0,
		v1LogLine(1, t0.Add(-2*time.Second), `"kind":"log","summary":"s1"`),
		v1LogLine(2, t0.Add(-1*time.Second), `"kind":"log","summary":"s2"`),
		// Written while the stalled read is in flight.
		v1LogLine(3, t0.Add(3*time.Second), `"kind":"log","summary":"s3"`),
		v1LogLine(4, t0.Add(5*time.Second), `"kind":"log","summary":"s4"`),
		v1LogLine(5, t0.Add(12*time.Second), `"kind":"log","summary":"s5"`),
	)
	f.src.latency = []time.Duration{500 * time.Millisecond, 13 * time.Second, 500 * time.Millisecond}

	s := f.session(t, 1)
	for i := 0; i < 3; i++ {
		if done, _ := s.poll(context.Background()); done {
			t.Fatalf("poll %d ended the session; the pod is still Running", i+1)
		}
	}

	events := f.recorded(t, 1)
	var got []string
	for _, ev := range events {
		got = append(got, ev.Detail)
	}
	want := []string{"s1", "s2", "s3", "s4", "s5"}
	if len(got) != len(want) {
		t.Fatalf("recorded %v, want %v — the stall hid s3 and s4, and the next window has to reach back past them", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %q, want %q", i, got[i], want[i])
		}
	}
	if notices := gapNotices(events); len(notices) != 0 {
		t.Errorf("the feed reported a gap it did not have: %+v", notices)
	}
	if got := f.store.State("acme", "c1"); got != gen.RunCycleViewRecordingRecording {
		t.Errorf("state = %q, want recording — nothing was lost", got)
	}

	// The ORDINARY read has to be what keeps the feed whole. Three polls, three
	// reads: a fourth would mean the recorder had to fall back on the gap repair
	// to recover what its own window should never have skipped.
	windows := f.src.windowCalls()
	if len(windows) != 3 {
		t.Fatalf("%d reads for 3 polls, want 3 — the window, not the repair, is what must keep the feed whole", len(windows))
	}
	if !windows[2].Before(t0.Add(3 * time.Second)) {
		t.Errorf("the window after the stall began at %s, at or after the first line the stall hid (%s)",
			windows[2], t0.Add(3*time.Second))
	}
}

// TestRecorder_RecordsTheFeedAndClosesCompleteOnATerminalPod is the happy path,
// plus the ONE FINAL FULL READ that closes the "pod exited between two polls"
// loss: the runner's last words land after the last incremental read, so the
// recorder asks for the whole log once more before it closes.
func TestRecorder_RecordsTheFeedAndClosesCompleteOnATerminalPod(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC)
	f := newRecorderFixture(t, at.Add(time.Second),
		v1LogLine(1, at, `"kind":"tool_use","tool":"Read","summary":"read api.go"`),
		v1LogLine(2, at.Add(time.Second), `"kind":"tool_result","tool":"Read","ok":true`),
		// The run's ending — the line a poll-interval-short reader used to miss
		// every time.
		v1LogLine(3, at.Add(2*time.Second), `"kind":"result","status":"success"`),
	)

	s := f.session(t, 1)
	if done, phase := s.poll(context.Background()); done || phase != "Running" {
		t.Fatalf("first poll = (done=%v, phase=%q), want (false, Running)", done, phase)
	}
	if got := f.store.State("acme", "c1"); got != gen.RunCycleViewRecordingRecording {
		t.Fatalf("state mid-run = %q, want recording", got)
	}

	f.clock.advance(2 * time.Second)
	f.src.setPod(succeededPod())
	if done, _ := s.poll(context.Background()); !done {
		t.Fatal("a terminal pod did not end the session")
	}
	if got := f.store.State("acme", "c1"); got != gen.RunCycleViewRecordingComplete {
		t.Fatalf("state = %q, want complete", got)
	}

	events := f.recorded(t, 1)
	if len(events) != 3 {
		t.Fatalf("recorded %d events, want 3 (the overlap must be deduped)\n%+v", len(events), events)
	}
	if events[2].Kind != gen.RunEventKindRunSettled || events[2].Outcome != gen.RunEventOutcomeSuccess {
		t.Errorf("last recorded event = %+v, want the run's own settle", events[2])
	}
	// Read 1 asks for the whole log, read 2 uses the time cursor, and read 3 is
	// the final FULL read.
	windows := f.src.windowCalls()
	if len(windows) != 3 || !windows[0].IsZero() || windows[1].IsZero() || !windows[2].IsZero() {
		t.Errorf("read windows = %v, want [whole, cursor, whole] — the last one is the final full read", windows)
	}
	// The binding is fixed for the attempt: three reads, one resolve.
	if got := f.src.bindingCalls(); got != 1 {
		t.Errorf("resolved the release binding %d times for one session, want 1", got)
	}
}

// TestRecorder_SeqGapBecomesANoticeAndFlipsTheState covers the loss the gap
// detector exists for. It runs in the PRODUCER's numbering: a lifted v1 feed
// occupies only the even v2 seqs, so a detector reading those would call every
// single step a missing event.
func TestRecorder_SeqGapBecomesANoticeAndFlipsTheState(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC)
	f := newRecorderFixture(t, at.Add(time.Second),
		v1LogLine(1, at, `"kind":"log","summary":"one"`),
		v1LogLine(5, at.Add(time.Second), `"kind":"log","summary":"five"`),
	)

	s := f.session(t, 1)
	if done, _ := s.poll(context.Background()); done {
		t.Fatal("a Running pod ended the session")
	}
	if got := f.store.State("acme", "c1"); got != gen.RunCycleViewRecordingGaps {
		t.Fatalf("state = %q, want gaps", got)
	}

	events := f.recorded(t, 1)
	notices := gapNotices(events)
	if len(notices) != 1 {
		t.Fatalf("%d gap notices in %+v, want 1", len(notices), events)
	}
	notice := notices[0]
	if notice.Kind != gen.RunEventKindNotice || notice.Level != gen.RunEventLevelWarn || notice.AgentID != leadAgentID {
		t.Errorf("gap notice = %+v, want a warn notice on the lead", notice)
	}
	if !strings.Contains(notice.Detail, "3 event(s)") {
		t.Errorf("detail = %q, want it to name how many went missing (seqs 2,3,4)", notice.Detail)
	}
	// The pod is still Running and its log is still readable, so the honest word
	// is YET. The sentence used to say "could not be recovered" at the very
	// moment the recorder was about to ask for that stretch of log again.
	if !strings.Contains(notice.Detail, "yet") || strings.Contains(notice.Detail, "could not be recovered") {
		t.Errorf("detail = %q — a hole in a live pod's feed is not yet a loss", notice.Detail)
	}
	// It sits WHERE the hole is: after the last recorded event and before the
	// one that revealed it.
	if events[0].Seq >= notice.Seq || notice.Seq >= events[len(events)-1].Seq {
		t.Errorf("gap notice seq %d is not between %d and %d", notice.Seq, events[0].Seq, events[len(events)-1].Seq)
	}
}

// TestRecorder_AGapIsRepairedFromTheLivePodBeforeTheArchive pins the order of
// the two repair sources. While the pod's Component exists its whole log is
// still served — the incident proved that by fetching a complete `1..478` after
// the fact — so a hole in the recording is usually a hole in what the platform
// ASKED FOR, and asking again is the direct repair. The archive is for what the
// pod can no longer give back.
func TestRecorder_AGapIsRepairedFromTheLivePodBeforeTheArchive(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC)
	all := []fakeLogLine{
		v1LogLine(1, at, `"kind":"log","summary":"one"`),
		v1LogLine(2, at.Add(time.Second), `"kind":"log","summary":"two"`),
		v1LogLine(3, at.Add(2*time.Second), `"kind":"log","summary":"three"`),
		v1LogLine(4, at.Add(3*time.Second), `"kind":"log","summary":"four"`),
	}
	f := newRecorderFixture(t, at.Add(3*time.Second), all...)
	archive := &spyArchive{text: pageOf(all...)}
	f.rec.WithArchive(archive)

	// A session that has recorded line 1 and is then handed a page with 2 and 3
	// missing — whatever the cause, the pod still holds them.
	s := f.session(t, 1)
	s.cur.ProducerSeq = 1
	s.cur.LastLineTS = at
	s.ingest(context.Background(), LiveTail{Pod: runningPod(), Text: pageOf(all[0], all[3])})

	events := f.recorded(t, 1)
	var got []string
	for _, ev := range events {
		got = append(got, ev.Detail)
	}
	if want := []string{"two", "three", "four"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("recorded %v, want %v — the pod still had the missing lines", got, want)
	}
	if notices := gapNotices(events); len(notices) != 0 {
		t.Errorf("a repaired gap still reported a notice: %+v", notices)
	}
	if got := f.store.State("acme", "c1"); got != gen.RunCycleViewRecordingRecording {
		t.Errorf("state = %q — a fully repaired gap is not a gap", got)
	}
	if archive.reads() != 0 {
		t.Errorf("the archive was read %d times; the live pod answers first", archive.reads())
	}
}

// TestRecorder_ArchiveBackfillRepairsAGapTheLivePodCannot pins the archive's ONE
// remaining job. It indexed the same pod's output all along, so a burst the pod
// itself will no longer serve may still be there — and a gap that is fully
// recovered is not a gap.
func TestRecorder_ArchiveBackfillRepairsAGapTheLivePodCannot(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC)
	// The pod's own log no longer holds 2 and 3 (kubelet rotation); the archive
	// indexed them when they were written.
	f := newRecorderFixture(t, at.Add(3*time.Second),
		v1LogLine(1, at, `"kind":"log","summary":"one"`),
		v1LogLine(4, at.Add(3*time.Second), `"kind":"log","summary":"four"`),
	)
	archive := &spyArchive{text: pageOf(
		v1LogLine(1, at, `"kind":"log","summary":"one"`),
		v1LogLine(2, at.Add(time.Second), `"kind":"log","summary":"two"`),
		v1LogLine(3, at.Add(2*time.Second), `"kind":"log","summary":"three"`),
		v1LogLine(4, at.Add(3*time.Second), `"kind":"log","summary":"four"`),
	)}
	f.rec.WithArchive(archive)

	s := f.session(t, 1)
	if done, _ := s.poll(context.Background()); done {
		t.Fatal("a Running pod ended the session")
	}
	if got := f.store.State("acme", "c1"); got != gen.RunCycleViewRecordingRecording {
		t.Fatalf("state = %q — a fully backfilled gap is not a gap", got)
	}
	events := f.recorded(t, 1)
	if len(events) != 4 {
		t.Fatalf("recorded %d events, want all four\n%+v", len(events), events)
	}
	if notices := gapNotices(events); len(notices) != 0 {
		t.Errorf("a repaired gap still reported a notice: %+v", notices)
	}
	if archive.reads() != 1 {
		t.Errorf("the archive was read %d times, want 1 — at most one repair per page", archive.reads())
	}
}

// TestRecorder_ArchiveFillsTheBottomOfAGapTheLivePodOnlyTopsUp pins the MERGE of
// the repair's two sources, which is the case where they disagree about how much
// of the hole they can answer.
//
// The pod is serving the top of the hole (4, 5) and has rotated the bottom away
// (2, 3); the archive indexed all of it. Filtering the archive by how far the
// live answer reached asks it for the stretch above 5 — nothing — and reports 2
// and 3 permanently missing while they sit in the archive. Both sources are
// therefore asked for the whole hole, and the merge is ordered on the PRODUCER's
// seq, not on which source answered first: `number` stamps position in the file,
// so appending the archive's 2 and 3 after the live 4 and 5 would write a
// descending seq run into the recording.
//
// Driven through repairGap directly rather than through a poll. The repair's read
// window is anchored on the last line ingested, so it can never reach FURTHER
// BACK than the page that revealed the gap — no arrangement of this fixture's
// clock makes a poll produce a part-answered hole. What the two sources can each
// return is a property of the repair, and this is the level it lives at.
func TestRecorder_ArchiveFillsTheBottomOfAGapTheLivePodOnlyTopsUp(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC)
	line := func(seq int, summary string) fakeLogLine {
		return v1LogLine(seq, at.Add(time.Duration(seq)*time.Second),
			`"kind":"log","summary":"`+summary+`"`)
	}
	// The pod no longer holds 2 and 3.
	f := newRecorderFixture(t, at.Add(10*time.Second),
		line(1, "one"), line(4, "four"), line(5, "five"), line(6, "six"),
	)
	f.rec.WithArchive(&spyArchive{text: pageOf(
		line(1, "one"), line(2, "two"), line(3, "three"),
		line(4, "four"), line(5, "five"), line(6, "six"),
	)})

	s := f.session(t, 1)
	s.cur.ProducerSeq = 1

	repaired := false
	out, missing := s.repairGap(context.Background(), 1, 6, &repaired)

	if missing != 0 {
		t.Errorf("still missing %d — the archive holds every event in the hole", missing)
	}
	var got []string
	for _, ev := range out {
		got = append(got, ev.Detail)
	}
	// Ascending, because that is the order the producer wrote them in and the
	// order `number` is about to stamp as their position in the file.
	if want := []string{"two", "three", "four", "five"}; !slices.Equal(got, want) {
		t.Errorf("recovered %v, want %v", got, want)
	}
}

// TestRecorder_CancelClosesWithRunSettledCancelledAndGaps covers the fourth
// loss: cancel deletes the Component immediately, so the pod's log is
// unreadable from that instant and the last poll interval is genuinely gone.
// The recording therefore says so — `gaps` — and carries a runner-less settle
// so a reader can see the run ENDED rather than merely stopped talking.
func TestRecorder_CancelClosesWithRunSettledCancelledAndGaps(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC)
	f := newRecorderFixture(t, at, v1LogLine(1, at, `"kind":"log","summary":"working"`))

	s := f.session(t, 1)
	if done, _ := s.poll(context.Background()); done {
		t.Fatal("a Running pod ended the session")
	}

	f.rec.CloseCancelled(context.Background(), f.cycle)

	if got := f.store.State("acme", "c1"); got != gen.RunCycleViewRecordingGaps {
		t.Fatalf("state = %q, want gaps — the tail between the last poll and the delete is lost", got)
	}
	if !f.store.Finished("acme", "c1", 1) {
		t.Error("a cancelled recording was left open")
	}
	events := f.recorded(t, 1)
	last := events[len(events)-1]
	if last.Kind != gen.RunEventKindRunSettled {
		t.Fatalf("last recorded event = %+v, want run_settled", last)
	}
	// `cancelled` is deliberately not `failure`: the work was taken away, and
	// nothing went wrong.
	if last.Outcome != gen.RunEventOutcomeCancelled {
		t.Errorf("outcome = %q, want cancelled", last.Outcome)
	}
	if last.Error != "" {
		t.Errorf("a cancelled settle invented an error: %q", last.Error)
	}
	if last.Seq <= events[0].Seq {
		t.Errorf("the settle sits at seq %d, at or before the feed it closes (%d)", last.Seq, events[0].Seq)
	}
}

// TestRecorder_CancelOnAnUnrecordedCycleWritesNothing pins that cancel does not
// mint a recording for a cycle the platform never recorded: that would turn
// `none` into a one-event feed and hide the fact that nothing was captured.
func TestRecorder_CancelOnAnUnrecordedCycleWritesNothing(t *testing.T) {
	t.Parallel()

	f := newRecorderFixture(t, time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC))
	f.rec.CloseCancelled(context.Background(), f.cycle)
	if got := f.store.State("acme", "c1"); got != gen.RunCycleViewRecordingNone {
		t.Fatalf("state = %q, want none", got)
	}
}

// TestRecorder_ReDispatchWritesASecondAttemptFileAndLeavesTheFirst covers the
// fifth loss. A re-dispatch is a new pod whose seqs restart at 1: one file per
// attempt is what keeps two events numbered `1` from colliding in a consumer
// that dedups on (cycle, attempt, seq), and the first attempt's history is not
// rewritten by the retry.
func TestRecorder_ReDispatchWritesASecondAttemptFileAndLeavesTheFirst(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC)
	f := newRecorderFixture(t, at, v1LogLine(1, at, `"kind":"log","summary":"first attempt"`))

	first := f.session(t, 1)
	if done, _ := first.poll(context.Background()); done {
		t.Fatal("a Running pod ended the session")
	}

	// The retry's own pod: a new log whose seq restarts at 1.
	f.clock.advance(time.Minute)
	f.src.setLines(v1LogLine(1, at.Add(time.Minute), `"kind":"log","summary":"second attempt"`))

	second := f.session(t, 2)
	if done, _ := second.poll(context.Background()); done {
		t.Fatal("a Running pod ended the retry's session")
	}

	dir := filepath.Join(gitfs.RunsDir(f.root), "acme", "c1")
	for _, name := range []string{"events.1.ndjson", "events.2.ndjson"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
	}
	one := f.recorded(t, 1)
	two := f.recorded(t, 2)
	if len(one) != 1 || !strings.Contains(one[0].Detail, "first attempt") {
		t.Fatalf("attempt 1 = %+v, want it untouched by the retry", one)
	}
	if len(two) != 1 || !strings.Contains(two[0].Detail, "second attempt") {
		t.Fatalf("attempt 2 = %+v, want the retry's own feed", two)
	}
	if got := f.store.Attempts("acme", "c1"); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("attempts = %v, want [1 2] ascending", got)
	}
}

// TestRecorder_DarkZoneIsRecordedOncePerState pins the narration of the stretch
// before the runner speaks. It is RECORDED rather than re-derived per viewer,
// because a viewer that reads only the file would otherwise see nothing at all
// for the slowest part of the flow — and it collapses to one row per state,
// because the same state re-derived every second would otherwise write hundreds.
func TestRecorder_DarkZoneIsRecordedOncePerState(t *testing.T) {
	t.Parallel()

	f := newRecorderFixture(t, time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC))
	f.src.setPod(openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Pending", WaitingReason: "ContainerCreating"})
	s := f.session(t, 1)

	for i := 0; i < 3; i++ {
		if done, _ := s.poll(context.Background()); done {
			t.Fatalf("poll %d ended the session", i)
		}
	}
	events := f.recorded(t, 1)
	if len(events) != 1 {
		t.Fatalf("recorded %d dark-zone rows over three identical polls, want 1\n%+v", len(events), events)
	}
	if events[0].Code != gen.RunEventCodeRunnerPullingImage || events[0].Seq != seqBootPulling {
		t.Errorf("dark-zone row = %+v, want the stable pulling marker", events[0])
	}

	// A TRANSITION writes exactly one more row.
	f.src.setPod(runningPod())
	if done, _ := s.poll(context.Background()); done {
		t.Fatal("a Running pod with no output ended the session")
	}
	events = f.recorded(t, 1)
	if len(events) != 2 || events[1].Code != gen.RunEventCodeRunnerStarting {
		t.Fatalf("after the transition = %+v, want exactly one more row (starting)", events)
	}
}

// TestRecorder_ComponentGoneClosesWithGaps pins the backstop for a Component
// deleted out from under a running recording (retention, or a cancel that
// raced the session): nothing more can ever be read, and what is on disk is by
// definition short of the ending — which is the ONE place the feed is entitled
// to call a loss unrecoverable.
func TestRecorder_ComponentGoneClosesWithGaps(t *testing.T) {
	t.Parallel()

	f := newRecorderFixture(t, time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC))
	f.src.setErr(fmt.Errorf("%w: ca-c1", ErrComponentGone))

	s := f.session(t, 1)
	if done, _ := s.poll(context.Background()); !done {
		t.Fatal("a gone Component did not end the session")
	}
	if got := f.store.State("acme", "c1"); got != gen.RunCycleViewRecordingGaps {
		t.Fatalf("state = %q, want gaps", got)
	}
	notices := gapNotices(f.recorded(t, 1))
	if len(notices) != 1 {
		t.Fatalf("%d gap notices, want the closing one", len(notices))
	}
	if !strings.Contains(notices[0].Detail, "nothing can recover it now") {
		t.Errorf("closing notice = %q, want it to say the loss is permanent", notices[0].Detail)
	}
}

// TestRecorder_BindingIsResolvedOncePerSessionAndReResolvedWhenItGoes pins the
// round trip taken out of every poll. The binding name is fixed for the attempt,
// so re-resolving it every second spent a call re-deriving a constant — and
// spent it BETWEEN choosing a read window and having one applied. Caching it is
// only safe because a read that 404s clears it: a cached name a re-render
// replaced would otherwise 404 for the rest of the run.
func TestRecorder_BindingIsResolvedOncePerSessionAndReResolvedWhenItGoes(t *testing.T) {
	t.Parallel()

	f := newRecorderFixture(t, time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC))
	s := f.session(t, 1)
	for i := 0; i < 3; i++ {
		if done, _ := s.poll(context.Background()); done {
			t.Fatalf("poll %d ended the session", i)
		}
	}
	if got := f.src.bindingCalls(); got != 1 {
		t.Fatalf("resolved the binding %d times over three polls, want 1", got)
	}

	f.src.setErr(fmt.Errorf("%w: ca-c1", ErrComponentGone))
	if done, _ := s.poll(context.Background()); !done {
		t.Fatal("a gone Component did not end the session")
	}
	if got := f.src.bindingCalls(); got != 2 {
		t.Errorf("resolved the binding %d times, want 2 — a 404 on a cached name is worth one re-resolve", got)
	}
}

// TestRecorder_TransientReadFailureKeepsTheSessionAndTheCursor pins that a
// transport failure is not an answer about the cycle: the session survives and
// the next read asks for the same window, so nothing is skipped.
func TestRecorder_TransientReadFailureKeepsTheSessionAndTheCursor(t *testing.T) {
	t.Parallel()

	f := newRecorderFixture(t, time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC))
	f.src.setErr(fmt.Errorf("connection refused"))

	s := f.session(t, 1)
	if done, _ := s.poll(context.Background()); done {
		t.Fatal("a transport failure ended the session")
	}
	if got := f.store.State("acme", "c1"); got != gen.RunCycleViewRecordingRecording {
		t.Fatalf("state = %q, want the recording left open", got)
	}
	if windows := f.src.windowCalls(); len(windows) != 1 || !windows[0].IsZero() {
		t.Errorf("read windows = %v, want the cursor to have stayed at the whole log", windows)
	}
}

// TestRecorder_AStalledPollFailsInsteadOfBlockingTheSession pins the per-poll
// deadline. The OpenChoreo client carries no timeout, so a hung call used to
// block the session loop for as long as it liked: a recording that simply
// stopped growing, with no warning anywhere to say why. Failing is safe — the
// cursor does not move, so the next poll asks for the same window and picks up
// everything the stalled one would have.
func TestRecorder_AStalledPollFailsInsteadOfBlockingTheSession(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC)
	f := newRecorderFixture(t, at, v1LogLine(1, at, `"kind":"log","summary":"one"`))
	f.rec.pollTimeout = 50 * time.Millisecond
	f.src.mu.Lock()
	f.src.block = true
	f.src.mu.Unlock()

	s := f.session(t, 1)
	if done, _ := s.poll(context.Background()); done {
		t.Fatal("a stalled poll ended the session")
	}
	if s.cur.ProducerSeq != 0 || !s.cur.LastLineTS.IsZero() {
		t.Errorf("a failed poll moved the cursor: %+v", s.cur)
	}

	// The next poll picks up what the stalled one never returned.
	f.src.mu.Lock()
	f.src.block = false
	f.src.mu.Unlock()
	if done, _ := s.poll(context.Background()); done {
		t.Fatal("a Running pod ended the session")
	}
	if events := f.recorded(t, 1); len(events) != 1 || events[0].Detail != "one" {
		t.Errorf("recorded %+v, want the line the stalled poll would have read", events)
	}
}

// TestRecorder_EnsureSkipsAClosedRecording pins the restart rule: aep-api coming
// back must not re-record a cycle that is already over, which would append a
// second copy of the whole feed to a file a viewer is reading from an offset.
func TestRecorder_EnsureSkipsAClosedRecording(t *testing.T) {
	t.Parallel()

	f := newRecorderFixture(t, time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC))
	f.src.setPod(succeededPod())
	if _, err := f.store.Begin("acme", "c1", 1); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := f.store.Close("acme", "c1", gen.RunCycleViewRecordingComplete); err != nil {
		t.Fatalf("Close: %v", err)
	}
	f.rec.Ensure(context.Background(), f.cycle)
	// Give a session that should not exist every chance to read.
	select {
	case <-f.src.seen:
		t.Fatal("Ensure started a session for a closed recording")
	case <-time.After(150 * time.Millisecond):
	}
}

// TestRecorder_EnsureIsIdempotentPerAttempt pins that the watcher may hand the
// same cycle over on every tick: a session already running for this attempt is
// left alone rather than replaced, which would restart its cursor.
func TestRecorder_EnsureIsIdempotentPerAttempt(t *testing.T) {
	t.Parallel()

	// A Running pod with no output: the session polls, records the dark zone and
	// then sleeps for an hour, so it is still registered when the second Ensure
	// arrives.
	f := newRecorderFixture(t, time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC))
	f.rec.WithIntervals(time.Hour, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.rec.Ensure(ctx, f.cycle)
	select {
	case <-f.src.seen:
	case <-time.After(2 * time.Second):
		t.Fatal("the session never read")
	}
	f.rec.Ensure(ctx, f.cycle)
	f.rec.Ensure(ctx, f.cycle)
	select {
	case <-f.src.seen:
		t.Fatal("a repeated Ensure started a second session for the same attempt")
	case <-time.After(150 * time.Millisecond):
	}
}

// TestRecorder_NilRecorderIsANoOp pins the degraded boot: no workspace volume
// means no recorder, and every entry point has to survive that rather than the
// watcher needing to know.
func TestRecorder_NilRecorderIsANoOp(t *testing.T) {
	t.Parallel()

	if NewCycleRecorder(&fakeRecordSource{}, nil) != nil {
		t.Fatal("a nil store produced a recorder")
	}
	var rec *CycleRecorder
	rec.Ensure(context.Background(), liveCycle("c1"))
	rec.CloseCancelled(context.Background(), liveCycle("c1"))
	rec.retain(map[string]bool{})
}

// TestRecorder_SizeCapNamesItselfAndKeepsRunning pins the cap's trade: the cap
// is off by default and exists for a pathological producer, and a run that
// trips it says so on the feed and CARRIES ON — stopping an agent to protect a
// log would be the wrong way round.
func TestRecorder_SizeCapNamesItselfAndKeepsRunning(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 4, 9, 25, 39, 0, time.UTC)
	// One byte: anything trips it.
	f := newCappedRecorderFixture(t, 1, at, v1LogLine(1, at, `"kind":"log","summary":"one"`))
	s := f.session(t, 1)

	if done, _ := s.poll(context.Background()); done {
		t.Fatal("the size cap ended the session — the run must keep going")
	}
	if got := f.store.State("acme", "c1"); got != gen.RunCycleViewRecordingGaps {
		t.Fatalf("state = %q, want gaps", got)
	}
	events := f.recorded(t, 1)
	last := events[len(events)-1]
	if last.Code != gen.RunEventCodeGap || !strings.Contains(last.Detail, "size limit") {
		t.Errorf("last event = %+v, want the size-cap notice", last)
	}
	// Past the cap it stops WRITING, not polling: the pod's terminal phase still
	// has to close the recording.
	before := len(events)
	if done, _ := s.poll(context.Background()); done {
		t.Fatal("the second poll ended the session")
	}
	if after := f.recorded(t, 1); len(after) != before {
		t.Errorf("recorded %d → %d events past the cap, want no more writes", before, len(after))
	}
}

// TestProducerSeq_ReadsBothEnvelopeVersionsAndRejectsProse pins the one input
// dedupe and gap detection depend on. A line with no producer numbering (a
// container's bootstrap output) must be recognised as such, not read as seq 0.
func TestProducerSeq_ReadsBothEnvelopeVersionsAndRejectsProse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw     string
		want    int64
		wantSeq bool
	}{
		{`{"schemaVersion":1,"seq":7,"kind":"log","summary":"x"}`, 7, true},
		{`{"v":2,"seq":9,"kind":"notice","agentId":"lead"}`, 9, true},
		{`[oneshot] materialised 3 skill(s)`, 0, false},
		{`{"seq":4,"kind":"log"}`, 0, false},           // neither envelope version
		{`{"schemaVersion":1,"kind":"log"}`, 0, false}, // enveloped but unnumbered
		{`{"broken`, 0, false},
		{``, 0, false},
	}
	for _, tc := range cases {
		seq, ok := producerSeq(tc.raw)
		if seq != tc.want || ok != tc.wantSeq {
			t.Errorf("producerSeq(%q) = (%d,%v), want (%d,%v)", tc.raw, seq, ok, tc.want, tc.wantSeq)
		}
	}
}

// TestSinceSecondsAt_ConvertsLateAndRoundsOutwards pins the conversion that may
// not be done early. `sinceSeconds` counts back from whenever the API reads it,
// so a value computed before three OpenChoreo round trips describes a window
// that has since slid forward — which is how a run lost eleven events. Rounding
// is outwards for the same reason: a truncating divide asks for a window that
// starts a fraction late, and the line in that fraction is gone for good.
func TestSinceSecondsAt_ConvertsLateAndRoundsOutwards(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 8, 9, 29, 48, 0, time.UTC)
	cases := []struct {
		name  string
		since time.Time
		now   time.Time
		want  int64
	}{
		{"the zero time is the whole log", time.Time{}, now, 0},
		{"a whole number of seconds back", now.Add(-5 * time.Second), now, 6},
		{"a fraction rounds outwards", now.Add(-1500 * time.Millisecond), now, 2},
		{"a window start in the future asks for the smallest window", now.Add(time.Second), now, 1},
		// The incident: the window was chosen at 09:29:41.8 and applied at
		// 09:29:48.2, and a value computed early would have asked for 5s of log
		// when the honest ask was 14s.
		{"latency in front of the call only widens it",
			time.Date(2026, 9, 8, 9, 29, 34, 294000000, time.UTC),
			time.Date(2026, 9, 8, 9, 29, 48, 159000000, time.UTC), 14},
	}
	for _, tc := range cases {
		if got := sinceSecondsAt(tc.since, tc.now); got != tc.want {
			t.Errorf("%s: sinceSecondsAt = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestRecorder_EveryLineGetsItsOwnSeq is the measured loss, replayed.
//
// The tail of a real 55-minute run (testdata/run-2026-09-08-npm-tail.ndjson):
// six runner events, then five lines of raw container stdout — npm's own update
// notice, written to fd 1 by the `npx` shim AFTER the runner's node process had
// already settled, so no emitter in the runner could ever have numbered them.
// The lift gave all five `seq: 0`, and a console deduping on the key the
// contract names — (cycleId, attempt, seq) — rendered exactly one of them. The
// recording held 1189 events; the screen showed 1185.
//
// It is not really about npm. Everything unstructured takes this path: a stack
// trace, a compiler's error list, a crash tail, the container's bootstrap
// output. A multi-line diagnostic reached the console as its first line, and the
// day that costs somebody a day is the day a run dies and the reason is on lines
// two through eight.
//
// The test drives the whole read path — poll, the terminal FULL RE-READ, then a
// restart re-ingesting the same page off the persisted cursor — because the
// numbering has to be stable across all three or the fix trades a dropped line
// for a duplicated one.
func TestRecorder_EveryLineGetsItsOwnSeq(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "run-2026-09-08-npm-tail.ndjson"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rawLines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(rawLines) != 11 {
		t.Fatalf("fixture holds %d lines, want 11 (6 runner events + 5 prose)", len(rawLines))
	}
	// The pod's own log, line by line, with the kubelet clock the fixture
	// carries — so the read window is applied against the run's real timing.
	log := make([]fakeLogLine, 0, len(rawLines))
	for _, raw := range rawLines {
		ts, msg := splitTimestampPrefix(raw)
		at := parseEventTime(ts)
		if at.IsZero() {
			t.Fatalf("fixture line has no kubelet timestamp: %q", raw)
		}
		log = append(log, fakeLogLine{at: at, text: msg})
	}
	wantProse := []string{
		"npm notice",
		"npm notice New major version of npm available! 10.9.8 -> 12.0.2",
		"npm notice Changelog: https://github.com/npm/cli/releases/tag/v12.0.2",
		"npm notice To update run: npm install -g npm@12.0.2",
		"npm notice",
	}

	// The runner's own events land first, while the pod is Running; the prose
	// arrives on the page that also carries the terminal phase, which is exactly
	// how it happened (the five lines are stamped 108ms after `run_settled`).
	f := newRecorderFixture(t, log[5].at, log...)
	s := f.session(t, 1)
	if done, _ := s.poll(context.Background()); done {
		t.Fatal("a Running pod ended the session")
	}
	f.clock.advance(time.Second)
	f.src.setPod(succeededPod())
	if done, _ := s.poll(context.Background()); !done {
		t.Fatal("a terminal pod did not end the session")
	}

	events := f.recorded(t, 1)
	if len(events) != 11 {
		t.Fatalf("recorded %d events, want 11 — the whole tail, deduped once\n%+v", len(events), events)
	}

	// EVERY line has its own identity. `seq` is the console's dedup key, so a
	// repeat here is a row the user never sees.
	seen := map[int64]int{}
	for i, ev := range events {
		if n, dup := seen[ev.Seq]; dup {
			t.Fatalf("event %d (%s %q) reuses seq %d, already taken by event %d — a consumer deduping on (attempt, seq) drops one of them",
				i, ev.Kind, ev.Detail, ev.Seq, n)
		}
		seen[ev.Seq] = i
		if i > 0 && ev.Seq <= events[i-1].Seq {
			t.Fatalf("seq went backwards at %d: %d after %d", i, ev.Seq, events[i-1].Seq)
		}
	}

	// All five prose lines survive, in order and whole.
	var prose []string
	for _, ev := range events {
		if strings.HasPrefix(ev.Detail, "npm notice") {
			if ev.Kind != gen.RunEventKindNotice {
				t.Errorf("raw stdout lifted to %q, want a notice", ev.Kind)
			}
			prose = append(prose, ev.Detail)
		}
	}
	if len(prose) != len(wantProse) {
		t.Fatalf("recorded %d prose lines, want %d — this is the loss\n%v", len(prose), len(wantProse), prose)
	}
	for i := range wantProse {
		if prose[i] != wantProse[i] {
			t.Errorf("prose line %d = %q, want %q", i, prose[i], wantProse[i])
		}
	}

	// The seq is the event's POSITION IN THE RECORDING, which is what the
	// contract says it is: dense from 1, one per line, prose included. (This
	// fixture is a TAIL, so position 1 is the producer's 1179. A recording that
	// watched the run from its first line — the ordinary case — numbers every
	// runner event exactly as the runner did, right up to the first seq-less
	// line, which keeps a recording diffable against the pod's own log.)
	for i, ev := range events {
		if ev.Seq != int64(i+1) {
			t.Errorf("event %d is at seq %d, want %d — seq is the position in the attempt", i, ev.Seq, i+1)
		}
	}

	// F13: state.json must not disagree with itself. It used to report 1189
	// events under a cursor that had only ever seen 1184 of them, because the
	// seq-less lines advanced nothing.
	meta := f.meta(t)
	if meta.Events != int64(len(events)) {
		t.Errorf("state.json events = %d, want %d", meta.Events, len(events))
	}
	if meta.Cursor.LastSeq != events[len(events)-1].Seq {
		t.Errorf("state.json cursor.lastSeq = %d, want %d — the cursor must account for the seq-less lines too",
			meta.Cursor.LastSeq, events[len(events)-1].Seq)
	}
	if meta.Cursor.ProseTS.IsZero() {
		t.Error("state.json cursor.proseTs is unset; a restart would re-record the prose")
	}
	// The read window's anchor is persisted too, or a restart would fall back on
	// the platform's clock and re-open the hole this cursor exists to close.
	if !meta.Cursor.LastLineTS.Equal(log[len(log)-1].at) {
		t.Errorf("state.json cursor.lastLineTs = %s, want the newest line ingested (%s)",
			meta.Cursor.LastLineTS, log[len(log)-1].at)
	}

	// A RESTART re-reads the whole log off the persisted cursor. Nothing may be
	// written twice and nothing renumbered — the recorder's numbering is stable
	// only because ProducerSeq and ProseTS both survive the restart.
	restarted := f.session(t, 1)
	restarted.ingest(context.Background(), LiveTail{Pod: succeededPod(), Text: string(raw)})
	after := f.recorded(t, 1)
	if len(after) != len(events) {
		t.Fatalf("a restart re-recorded the page: %d events, want %d", len(after), len(events))
	}
	for i := range after {
		if after[i].Seq != events[i].Seq || after[i].Detail != events[i].Detail {
			t.Errorf("event %d changed across the restart: %+v, want %+v", i, after[i], events[i])
		}
	}
}
