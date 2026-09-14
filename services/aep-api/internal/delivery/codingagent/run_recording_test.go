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
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

// TestRecordingStore_StateMachine walks the states the console branches on. The
// two that matter most are the ones that look identical on screen: `none` (the
// platform never had a record) and `lost` (it had one and cannot serve it).
func TestRecordingStore_StateMachine(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := NewRecordingStore(root, 0)

	if got := store.State("acme", "c1"); got != gen.RunCycleViewRecordingNone {
		t.Fatalf("a cycle nobody recorded = %q, want none", got)
	}
	if _, err := store.Begin("acme", "c1", 1); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if got := store.State("acme", "c1"); got != gen.RunCycleViewRecordingRecording {
		t.Fatalf("after Begin = %q, want recording", got)
	}
	if _, _, err := store.Append("acme", "c1", 1, []gen.RunEvent{recordedEvent(2, "one")}, recordCursor{ProducerSeq: 1}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.Close("acme", "c1", gen.RunCycleViewRecordingComplete); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := store.State("acme", "c1"); got != gen.RunCycleViewRecordingComplete {
		t.Fatalf("after Close = %q, want complete", got)
	}
	if !store.Finished("acme", "c1", 1) {
		t.Error("a closed recording did not report finished — a restart would re-record it")
	}

	// Losing the events under a state.json that says they are there is `lost`.
	if err := os.Remove(filepath.Join(gitfs.RunsDir(root), "acme", "c1", "events.1.ndjson")); err != nil {
		t.Fatalf("remove events: %v", err)
	}
	if got := store.State("acme", "c1"); got != gen.RunCycleViewRecordingLost {
		t.Fatalf("events gone = %q, want lost", got)
	}
}

// TestRecordingStore_GapsIsStickyAndDoesNotClose pins two rules that together
// keep a partial feed from ever being presented as the whole of it: a
// `complete` written over a known-gapped recording keeps the gaps, and marking
// gaps MID-RUN does not close the recording — the rest of the run is still
// worth recording.
func TestRecordingStore_GapsIsStickyAndDoesNotClose(t *testing.T) {
	t.Parallel()

	store := NewRecordingStore(t.TempDir(), 0)
	if _, err := store.Begin("acme", "c1", 1); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, _, err := store.Append("acme", "c1", 1, []gen.RunEvent{recordedEvent(2, "one")}, recordCursor{}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.Mark("acme", "c1", gen.RunCycleViewRecordingGaps); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if store.Finished("acme", "c1", 1) {
		t.Error("a gap mid-run closed the recording — the rest of the feed would never be written")
	}
	if err := store.Close("acme", "c1", gen.RunCycleViewRecordingComplete); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := store.State("acme", "c1"); got != gen.RunCycleViewRecordingGaps {
		t.Fatalf("state = %q, want gaps to survive a complete close", got)
	}
	// Re-Begin (a restart, or a later attempt) must not clear it either.
	if _, err := store.Begin("acme", "c1", 2); err != nil {
		t.Fatalf("Begin (attempt 2): %v", err)
	}
	if got := store.State("acme", "c1"); got != gen.RunCycleViewRecordingGaps {
		t.Fatalf("state after a second attempt = %q, want gaps", got)
	}
}

// TestRecordingStore_BeginResetsTheCursorOnANewAttemptOnly pins the re-dispatch
// rule: a new attempt is a new pod whose seqs restart at 1, so carrying the
// previous attempt's cursor would make the retry's whole feed look like
// duplicates. Re-Begin of the SAME attempt (a process restart) must resume.
func TestRecordingStore_BeginResetsTheCursorOnANewAttemptOnly(t *testing.T) {
	t.Parallel()

	store := NewRecordingStore(t.TempDir(), 0)
	if _, err := store.Begin("acme", "c1", 1); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, _, err := store.Append("acme", "c1", 1, []gen.RunEvent{recordedEvent(2, "one")}, recordCursor{ProducerSeq: 42, LastSeq: 84}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	resumed, err := store.Begin("acme", "c1", 1)
	if err != nil {
		t.Fatalf("Begin (resume): %v", err)
	}
	if resumed.ProducerSeq != 42 || resumed.LastSeq != 84 {
		t.Fatalf("resume cursor = %+v, want the persisted one", resumed)
	}
	fresh, err := store.Begin("acme", "c1", 2)
	if err != nil {
		t.Fatalf("Begin (attempt 2): %v", err)
	}
	if fresh.ProducerSeq != 0 || fresh.LastSeq != 0 {
		t.Fatalf("attempt-2 cursor = %+v, want a fresh one", fresh)
	}
}

// TestRecordingStore_ReadFromStopsAtTheLastCompleteLine is what lets a viewer
// follow a file the recorder is still writing: an offset never lands mid-event,
// so a reader that catches a partial append re-reads that line whole rather than
// parsing half of one.
func TestRecordingStore_ReadFromStopsAtTheLastCompleteLine(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := NewRecordingStore(root, 0)
	if _, err := store.Begin("acme", "c1", 1); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, _, err := store.Append("acme", "c1", 1, []gen.RunEvent{recordedEvent(2, "one")}, recordCursor{}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// Simulate a write the reader catches half-way through.
	path := filepath.Join(gitfs.RunsDir(root), "acme", "c1", "events.1.ndjson")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString(`{"v":2,"seq":4,"kind":"tool_`); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	_ = f.Close()

	events, next, err := store.ReadFrom("acme", "c1", 1, 0)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if len(events) != 1 || events[0].Seq != 2 {
		t.Fatalf("events = %+v, want only the complete line", events)
	}
	// The offset stopped BEFORE the fragment, so completing the line later
	// delivers it whole.
	f, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := f.WriteString("use\",\"agentId\":\"lead\"}\n"); err != nil {
		t.Fatalf("finish line: %v", err)
	}
	_ = f.Close()

	rest, _, err := store.ReadFrom("acme", "c1", 1, next)
	if err != nil {
		t.Fatalf("ReadFrom (tail): %v", err)
	}
	if len(rest) != 1 || rest[0].Seq != 4 {
		t.Fatalf("tail = %+v, want the completed line", rest)
	}
}

// TestRecordingStore_MissingAttemptIsNoRecording pins the sentinel a reader
// renders as `lost`, rather than as an empty feed.
func TestRecordingStore_MissingAttemptIsNoRecording(t *testing.T) {
	t.Parallel()

	store := NewRecordingStore(t.TempDir(), 0)
	if _, err := store.Begin("acme", "c1", 1); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, _, err := store.ReadFrom("acme", "c1", 7, 0); !errors.Is(err, ErrNoRecording) {
		t.Fatalf("ReadFrom(missing attempt) err = %v, want ErrNoRecording", err)
	}
}

// TestRecordingStore_RejectsPoisonedPathSegments is defense in depth: the org
// and cycle come from database rows, never from client input, and one poisoned
// row must not be able to name a directory outside the tree.
func TestRecordingStore_RejectsPoisonedPathSegments(t *testing.T) {
	t.Parallel()

	store := NewRecordingStore(t.TempDir(), 0)
	for _, bad := range []string{"..", "../etc", "a/b", ""} {
		if _, err := store.Begin(bad, "c1", 1); err == nil {
			t.Errorf("Begin(org=%q) was allowed", bad)
		}
		if _, err := store.Begin("acme", bad, 1); err == nil {
			t.Errorf("Begin(cycle=%q) was allowed", bad)
		}
	}
}

// TestRecordingStore_NilStoreIsSilent pins the degraded boot: no workspace
// volume means no recording, and every call is a no-op reporting `none` rather
// than an error nobody can act on.
func TestRecordingStore_NilStoreIsSilent(t *testing.T) {
	t.Parallel()

	var store *RecordingStore
	if NewRecordingStore("", 0) != nil {
		t.Fatal("an empty workspace root produced a store")
	}
	if _, err := store.Begin("acme", "c1", 1); err != nil {
		t.Errorf("Begin on a nil store: %v", err)
	}
	if store.State("acme", "c1") != gen.RunCycleViewRecordingNone {
		t.Error("a nil store did not report none")
	}
	if store.HasRecording("acme", "c1") || store.Finished("acme", "c1", 1) || store.OverCap("acme", "c1") {
		t.Error("a nil store claimed to hold something")
	}
}
