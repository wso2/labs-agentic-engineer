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

// run_recording.go — the platform's RECORD of one cycle's feed, on disk.
//
// Until this file existed the platform stored no agent feed: every viewer
// re-read the pod, kept the last 64KiB, and threw the bytes away. That lost
// output five measured ways (a burst bigger than the page between two polls, a
// post-mortem capped at 200 events, a pod that exited between polls, a cancel
// that deleted the Component mid-run, and a re-dispatch whose seqs restart at
// 1). A recording closes all five, and every choice below is one of those
// failures answered:
//
//	<root>/runs/<orgId>/<cycleId>/events.<attempt>.ndjson   one v2 RunEvent per line
//	<root>/runs/<orgId>/<cycleId>/state.json                what can be served
//
// ONE FILE PER ATTEMPT, because a re-dispatch is a new pod whose seq restarts
// at 1: appending it to the previous attempt's file would put two events with
// the same seq in one stream and a consumer deduping on (cycle, attempt, seq)
// would drop one of them. Separate files also make the console's "attempts as
// sequential crews under one cycle" a read of two files rather than a partition
// of one.
//
// APPEND-ONLY NDJSON, because that is what makes a live viewer cheap: a reader
// holds a byte offset, reads forward from it, and the same read from offset
// zero is the whole replay. No index, no rewrite, and a reader never has to
// re-parse what it has already seen.
//
// The store is deliberately NOT a ledger. What happened to a version is the
// `run_cycles` row in Postgres and stays there; this is observability, and the
// volume it sits on is otherwise a rebuildable cache (ADR-0027). Losing it
// costs a feed, never a fact — which is why `state.json` can honestly say
// `lost` and the console can say so too.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

// recordingStateFile is the per-cycle state document. It is a file rather than
// a column because the events are a file: a state in Postgres and events on a
// volume could disagree after a restore, and the console branches on the state
// to decide whether to present the events as the whole story.
const recordingStateFile = "state.json"

// recordingLineCap bounds ONE recorded event's JSON. It exists for the reader,
// not the writer: bufio.Scanner refuses a token over its buffer and would then
// silently drop the REST of the file, so the cap is set well above the
// contract's own field caps (capLongText ×  a few dozen fields) and a line that
// somehow exceeds it is reported rather than swallowed.
const recordingLineCap = 1 << 20 // 1 MiB

// recordingSegment is the allowed shape of an org / cycle path segment. The
// values come from database rows, never from client input, and are validated
// anyway for the same defense-in-depth reason gitfs validates its own path
// keys: one poisoned row must not be able to name a directory outside the tree.
var recordingSegment = regexp.MustCompile(`^[A-Za-z0-9._-]{1,200}$`)

// ErrNoRecording means the platform holds no record of this cycle's feed —
// `none` on the wire. It is not a failure: a cycle dispatched before the
// recorder shipped, or one dispatched on a boot with no workspace volume, has
// nothing recorded and never will.
var ErrNoRecording = errors.New("codingagent: no recording for this cycle")

// RecordingStore reads and writes cycle feed recordings under the workspace
// volume's `runs/` tree.
//
// One process writes (the CycleRecorder's session for a cycle) and any number
// read. The mutex covers state.json's read-modify-write only — the events files
// are append-only and a reader that races an append simply sees a shorter file,
// which is indistinguishable from polling a moment earlier.
type RecordingStore struct {
	root string // <workspaceRoot>/runs

	// maxCycleBytes caps one CYCLE's recording across its attempts. Zero — the
	// default — means no cap: a 55-minute run wrote about 300KB, so a cap is a
	// safety valve for a pathological producer and not an operating limit. A run
	// that hits it records a notice and continues WITHOUT recording, because
	// stopping the agent to protect a log would be the wrong trade.
	maxCycleBytes int64

	mu sync.Mutex
}

// NewRecordingStore wires the store over the workspace root. An empty root
// returns nil — a boot with no workspace volume records nothing, and every
// reader then reports `none` rather than erroring.
func NewRecordingStore(workspaceRoot string, maxCycleBytes int64) *RecordingStore {
	if strings.TrimSpace(workspaceRoot) == "" {
		return nil
	}
	if maxCycleBytes < 0 {
		maxCycleBytes = 0
	}
	return &RecordingStore{root: gitfs.RunsDir(workspaceRoot), maxCycleBytes: maxCycleBytes}
}

// recordingMeta is state.json.
//
// Attempt is the newest attempt recorded, so a reader knows how far to walk
// without listing the directory on every poll. Bytes is the cycle's total
// across attempts, which is what the size cap is measured against.
type recordingMeta struct {
	State   gen.RunCycleViewRecording `json:"state"`
	Attempt int                       `json:"attempt"`
	// Events is how many lines the CYCLE's files hold, across attempts. It is a
	// count of rows and not a position, so it is not Cursor.LastSeq and the two
	// are not required to agree: LastSeq is one attempt's highest position, and
	// the dark-zone markers (stable negative seqs) are counted here and occupy no
	// position at all. What the two must never do is disagree about whether an
	// event EXISTS — which they did while seq-less prose was recorded and left
	// out of the cursor entirely, so state.json reported 1189 events under a
	// cursor that had only ever seen 1184 of them.
	Events int64        `json:"events"`
	Bytes  int64        `json:"bytes"`
	Cursor recordCursor `json:"cursor"`
	// Closed marks a recording nobody will write to again. It is distinct from
	// State because `gaps` is set the MOMENT a gap is detected, while the run is
	// still going: without this flag a restart would read `gaps`, conclude the
	// cycle was over and never resume recording it.
	Closed    bool      `json:"closed"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// recordCursor is the RECORDER's position in the producer's stream for the
// attempt named by recordingMeta.Attempt. It is persisted beside the events
// because a restart has to resume, and every field answers "what would we
// otherwise write twice, or misread as missing?":
//
//   - ProducerSeq is the highest ENVELOPE seq recorded — the producer's own
//     numbering, which is a different sequence from the one written to the file.
//     Dedupe and gap detection both run in THIS space, because it is the only
//     one that can answer "did the producer write something we never saw": the
//     recorded seq counts what the platform wrote down, so a hole in it is
//     invisible by construction, and a lift that turns one line into two events
//     (an inferred `agent_started` and the line that revealed it) would make a
//     detector reading recorded seqs report a missing event between every pair.
//   - ProseTS is the pod-clock timestamp of the last seq-LESS line recorded
//     (container bootstrap output, a stray library write, a subprocess writing
//     straight to fd 1, a crash tail). Those carry no producer numbering at all,
//     so the only cursor they have is the kubelet's own monotonic stamp; a
//     re-read skips anything at or before it. Counting them instead would not
//     survive an incremental read, which starts partway down the log and has no
//     idea how many lines it skipped. It is this cursor — persisted, so it
//     outlives a restart and the final full re-read — that makes the recorder's
//     own numbering of those lines stable.
//   - LastSeq is the last position handed out in the v2 feed. It is the
//     recorder's own numbering, not the producer's: EVERY event written takes
//     the next one (recordingSession.number), which is why the cursor now
//     accounts for the seq-less lines it used to ignore. Dark-zone markers are
//     the one exception — they keep their stable negative seqs and consume no
//     position.
//   - LastLineTS is the pod-clock timestamp of the newest line INGESTED, seq
//     -carrying ones included, and it is what the next read's window is measured
//     from. It has to be the DATA's clock and not the platform's: the window used
//     to start at the instant the previous read RETURNED, which assumes the
//     answer described that instant. It does not — an OpenChoreo log call took a
//     measured 13.22 s, and nothing says which moment inside it the log was read
//     at — so the assumption moved the window's start past lines that had never
//     been read, and a cursor that only moves forward never asked for them
//     again. Anchored on the data, a stalled read makes the next window WIDER by
//     exactly the length of the stall, which costs a re-read that dedupe throws
//     away. (It is a superset of ProseTS, which is kept because it is the cursor
//     that dedupes the seq-LESS lines; this one dedupes nothing.)
//   - BootSeq is the last dark-zone marker written. Those markers carry stable
//     NEGATIVE seqs and are re-derived on every poll, so recording one per poll
//     would write the same row hundreds of times; only a state TRANSITION is
//     worth a line.
type recordCursor struct {
	ProducerSeq int64     `json:"producerSeq"`
	ProseTS     time.Time `json:"proseTs,omitempty"`
	LastLineTS  time.Time `json:"lastLineTs,omitempty"`
	LastSeq     int64     `json:"lastSeq"`
	BootSeq     int64     `json:"bootSeq"`
}

// cycleDir is <root>/runs/<orgId>/<cycleId>.
func (s *RecordingStore) cycleDir(orgID, cycleID string) (string, error) {
	if !recordingSegment.MatchString(orgID) || orgID == "." || orgID == ".." {
		return "", fmt.Errorf("codingagent: invalid org segment %q", orgID)
	}
	if !recordingSegment.MatchString(cycleID) || cycleID == "." || cycleID == ".." {
		return "", fmt.Errorf("codingagent: invalid cycle segment %q", cycleID)
	}
	return filepath.Join(s.root, orgID, cycleID), nil
}

// eventsFile is <cycleDir>/events.<attempt>.ndjson. Attempt is 1-based; a cycle
// row that has never been dispatched carries 0, which the recorder never
// records for.
func eventsFile(dir string, attempt int) string {
	return filepath.Join(dir, fmt.Sprintf("events.%d.ndjson", attempt))
}

// Begin opens (or re-opens) a cycle's recording for one attempt: it creates the
// directory, creates the attempt's events file if it is not there, and moves
// the state to `recording`.
//
// It is idempotent, which is what makes a restart safe: aep-api coming back
// mid-run re-Begins the attempt it finds and appends from wherever the file
// already reaches. A `gaps` state is NOT cleared by a re-Begin — a cycle whose
// feed is known incomplete stays known incomplete however many attempts follow.
//
// It returns the cursor the caller should resume from: the attempt already on
// disk resumes where it left off, and a NEW attempt starts from zero — a
// re-dispatch is a new pod whose seqs restart at 1, so carrying the previous
// attempt's cursor would silently swallow the retry's whole feed.
func (s *RecordingStore) Begin(orgID, cycleID string, attempt int) (recordCursor, error) {
	if s == nil {
		return recordCursor{}, nil
	}
	dir, err := s.cycleDir(orgID, cycleID)
	if err != nil {
		return recordCursor{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return recordCursor{}, fmt.Errorf("codingagent: create recording dir: %w", err)
	}
	f, err := os.OpenFile(eventsFile(dir, attempt), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return recordCursor{}, fmt.Errorf("codingagent: create recording events file: %w", err)
	}
	_ = f.Close()

	s.mu.Lock()
	defer s.mu.Unlock()
	meta := s.readMeta(dir)
	if meta.State != gen.RunCycleViewRecordingGaps {
		meta.State = gen.RunCycleViewRecordingRecording
	}
	if attempt != meta.Attempt {
		meta.Attempt = attempt
		meta.Cursor = recordCursor{}
	}
	if err := s.writeMeta(dir, meta); err != nil {
		return recordCursor{}, err
	}
	return meta.Cursor, nil
}

// Append writes events to the attempt's file and reports the cycle's total
// recorded bytes.
//
// The whole batch is marshalled into ONE buffer and written with a single
// syscall so a reader can never observe half of it: appends are the only writes
// this tree takes, and an interleaved partial line is the one way an
// append-only log can hand a reader a corrupt event.
//
// An event that will not marshal is skipped rather than failing the batch —
// losing one event is worth less than losing the rest of the run — and the skip
// is reported so the caller can mark the recording `gaps`.
func (s *RecordingStore) Append(orgID, cycleID string, attempt int, events []gen.RunEvent, cur recordCursor) (total int64, dropped int, err error) {
	if s == nil || len(events) == 0 {
		return 0, 0, nil
	}
	dir, err := s.cycleDir(orgID, cycleID)
	if err != nil {
		return 0, 0, err
	}
	var buf strings.Builder
	var written int64
	for i := range events {
		b, merr := json.Marshal(events[i])
		if merr != nil {
			dropped++
			continue
		}
		buf.Write(b)
		buf.WriteByte('\n')
		written++
	}
	if buf.Len() == 0 {
		return 0, dropped, nil
	}
	f, err := os.OpenFile(eventsFile(dir, attempt), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, dropped, fmt.Errorf("codingagent: open recording for append: %w", err)
	}
	if _, err := f.WriteString(buf.String()); err != nil {
		_ = f.Close()
		return 0, dropped, fmt.Errorf("codingagent: append to recording: %w", err)
	}
	if err := f.Close(); err != nil {
		return 0, dropped, fmt.Errorf("codingagent: close recording: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	meta := s.readMeta(dir)
	meta.Events += written
	meta.Bytes += int64(buf.Len())
	meta.Attempt = attempt
	meta.Cursor = cur
	if err := s.writeMeta(dir, meta); err != nil {
		return meta.Bytes, dropped, err
	}
	return meta.Bytes, dropped, nil
}

// OverCap reports whether the cycle's recording has passed the configured
// per-cycle size cap. Always false when no cap is configured, which is the
// default.
func (s *RecordingStore) OverCap(orgID, cycleID string) bool {
	if s == nil || s.maxCycleBytes <= 0 {
		return false
	}
	dir, err := s.cycleDir(orgID, cycleID)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readMeta(dir).Bytes > s.maxCycleBytes
}

// Mark moves the recording's state WITHOUT closing it — what a gap detected
// mid-run does.
//
// `gaps` is STICKY: once the platform knows it lost events from a cycle's feed,
// no later attempt can make the cycle's record whole again, and a `complete`
// written over it would present a partial feed as the whole story — which is
// the one thing the state exists to prevent.
func (s *RecordingStore) Mark(orgID, cycleID string, state gen.RunCycleViewRecording) error {
	return s.mark(orgID, cycleID, state, false)
}

// Close marks the recording final: nothing will be appended to it again. A
// `complete` close over a recording already known to have `gaps` keeps the
// gaps (see Mark).
func (s *RecordingStore) Close(orgID, cycleID string, state gen.RunCycleViewRecording) error {
	return s.mark(orgID, cycleID, state, true)
}

// HasRecording reports whether the platform holds any record of this cycle's
// feed at all — the difference between `none` and every other state.
func (s *RecordingStore) HasRecording(orgID, cycleID string) bool {
	if s == nil {
		return false
	}
	dir, err := s.cycleDir(orgID, cycleID)
	if err != nil {
		return false
	}
	_, serr := os.Stat(dir)
	return serr == nil
}

// Cursor returns the recorder's persisted position for the cycle. Callers use
// it to place a row AFTER everything already recorded — the cancelled
// `run_settled`, say — without re-reading the whole file to find the last seq.
func (s *RecordingStore) Cursor(orgID, cycleID string) recordCursor {
	if s == nil {
		return recordCursor{}
	}
	dir, err := s.cycleDir(orgID, cycleID)
	if err != nil {
		return recordCursor{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readMeta(dir).Cursor
}

// Finished reports whether the cycle's recording is closed at or past attempt —
// the question a recorder asks before starting a session, so a restart does not
// re-record a cycle that is already over.
func (s *RecordingStore) Finished(orgID, cycleID string, attempt int) bool {
	if s == nil {
		return false
	}
	dir, err := s.cycleDir(orgID, cycleID)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	meta := s.readMeta(dir)
	return meta.Closed && meta.Attempt >= attempt
}

func (s *RecordingStore) mark(orgID, cycleID string, state gen.RunCycleViewRecording, closed bool) error {
	if s == nil {
		return nil
	}
	dir, err := s.cycleDir(orgID, cycleID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	meta := s.readMeta(dir)
	if meta.State == gen.RunCycleViewRecordingGaps && state == gen.RunCycleViewRecordingComplete {
		state = gen.RunCycleViewRecordingGaps
	}
	meta.State = state
	if closed {
		meta.Closed = true
	}
	return s.writeMeta(dir, meta)
}

// State is what the platform can serve of this cycle's feed, in the contract's
// own vocabulary.
//
// The two empty answers are kept apart on purpose. `none` means no record was
// ever made — a cycle from before the recorder, or a boot with no volume — and
// `lost` means one was made and cannot be served. They paint the same empty
// screen and are very different bugs, so the reader never collapses them.
func (s *RecordingStore) State(orgID, cycleID string) gen.RunCycleViewRecording {
	if s == nil {
		return gen.RunCycleViewRecordingNone
	}
	dir, err := s.cycleDir(orgID, cycleID)
	if err != nil {
		return gen.RunCycleViewRecordingNone
	}
	if _, serr := os.Stat(dir); serr != nil {
		return gen.RunCycleViewRecordingNone
	}
	s.mu.Lock()
	meta := s.readMeta(dir)
	s.mu.Unlock()
	if meta.State == "" {
		// The directory is there and state.json is not (or will not parse). We
		// recorded SOMETHING here — the directory is only ever created by Begin —
		// and cannot say what, which is exactly `lost`.
		return gen.RunCycleViewRecordingLost
	}
	if attempts := s.Attempts(orgID, cycleID); len(attempts) == 0 {
		return gen.RunCycleViewRecordingLost
	}
	return meta.State
}

// Attempts lists the attempt numbers this cycle has events files for, ascending.
func (s *RecordingStore) Attempts(orgID, cycleID string) []int {
	if s == nil {
		return nil
	}
	dir, err := s.cycleDir(orgID, cycleID)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n, ok := attemptOfFile(e.Name())
		if !ok {
			continue
		}
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// attemptOfFile parses `events.<n>.ndjson`.
func attemptOfFile(name string) (int, bool) {
	if !strings.HasPrefix(name, "events.") || !strings.HasSuffix(name, ".ndjson") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "events."), ".ndjson"))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// ReadFrom returns the events in one attempt's file starting at byte offset,
// and the offset a caller should carry into its next read.
//
// It stops at the last COMPLETE line: an offset is only ever advanced past a
// newline, so a reader that catches a partial append re-reads that line whole
// next time instead of parsing half an event. That single rule is what lets a
// live viewer follow a file the recorder is still writing.
//
// A missing file for an attempt the store knows about is ErrNoRecording, which
// the caller renders as `lost` rather than as an empty feed.
func (s *RecordingStore) ReadFrom(orgID, cycleID string, attempt int, offset int64) ([]gen.RunEvent, int64, error) {
	if s == nil {
		return nil, offset, ErrNoRecording
	}
	dir, err := s.cycleDir(orgID, cycleID)
	if err != nil {
		return nil, offset, err
	}
	f, err := os.Open(eventsFile(dir, attempt))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, offset, ErrNoRecording
		}
		return nil, offset, fmt.Errorf("codingagent: open recording: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, offset, fmt.Errorf("codingagent: stat recording: %w", err)
	}
	if offset < 0 || offset > info.Size() {
		// A cursor past the end is a cursor from a different file — a caller that
		// carried one across a restore, or across a mode it should not have. Start
		// this attempt again rather than seeking into nothing: a duplicate event is
		// deduped by (cycle, attempt, seq); a silently empty feed is not.
		offset = 0
	}
	if _, err := f.Seek(offset, 0); err != nil {
		return nil, offset, fmt.Errorf("codingagent: seek recording: %w", err)
	}

	out := make([]gen.RunEvent, 0, 64)
	next := offset
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), recordingLineCap)
	for scanner.Scan() {
		line := scanner.Bytes()
		// +1 for the newline the scanner stripped. Only complete lines are
		// counted, so `next` never lands mid-event.
		consumed := int64(len(line)) + 1
		if next+consumed > info.Size() {
			break // an unterminated tail: leave it for the next read
		}
		next += consumed
		var ev gen.RunEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			// A line that will not parse is skipped, not fatal: the rest of the
			// recording is worth more than this one row. The offset has already
			// advanced past it so it is never retried.
			continue
		}
		out = append(out, ev)
	}
	if err := scanner.Err(); err != nil {
		// The reader stopped early; everything before the failure is returned and
		// `next` names where to resume. Reporting the error would throw away
		// events already parsed.
		return out, next, nil
	}
	return out, next, nil
}

// readMeta reads state.json, answering the zero value when it is absent or
// unreadable. Callers hold s.mu.
func (s *RecordingStore) readMeta(dir string) recordingMeta {
	b, err := os.ReadFile(filepath.Join(dir, recordingStateFile))
	if err != nil {
		return recordingMeta{}
	}
	var m recordingMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return recordingMeta{}
	}
	return m
}

// writeMeta replaces state.json atomically — write a sibling temp file, then
// rename over it. A torn state.json reads as `lost` and would report a perfectly
// good recording as unservable, so the one write that could tear is the one
// write that is made atomic. Callers hold s.mu.
func (s *RecordingStore) writeMeta(dir string, m recordingMeta) error {
	m.UpdatedAt = time.Now().UTC()
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("codingagent: marshal recording state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, recordingStateFile+".*")
	if err != nil {
		return fmt.Errorf("codingagent: stage recording state: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("codingagent: write recording state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("codingagent: close recording state: %w", err)
	}
	if err := os.Rename(name, filepath.Join(dir, recordingStateFile)); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("codingagent: publish recording state: %w", err)
	}
	return nil
}
