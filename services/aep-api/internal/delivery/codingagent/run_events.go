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

// run_events.go — one cycle's feed as v2 RunEvents, served from the RECORDING.
//
// This read used to derive the feed per viewer: every SSE connection tailed the
// pod on its own cursor, kept the newest 64KiB, capped the page at 200 events
// and stored nothing. That is retired. The platform now records each cycle's
// feed once, server-side (run_recorder.go), and this file is the read over that
// file: seek to the caller's offset, hand back what has been appended since,
// and return the offset to come back with.
//
// Three properties fall out of that, and each of them was a bug before:
//
//   - the WINDOW IS GONE. A reload mid-run replays from the first event, and a
//     reload after the pod has been reaped shows the whole cycle — not its
//     newest 200 events with nothing to say the run had started earlier.
//   - nothing is re-derived, so two viewers cost two file reads instead of two
//     pod reads, and a run nobody is watching is still recorded.
//   - "there is nothing to show" and "something was here and is gone" stay
//     apart: the first is RunCycleView.recording `none` and an empty feed, the
//     second is `lost` and a notice that says so.
//
// The observability archive is still wired, but only as the RECORDER's
// gap-backfill (see recordingSession.repairGap). It is no longer the ordinary
// post-mortem source, and the 200-event replay that used to be a finished run's
// whole history went with it.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// CycleEvents returns the next slice of one run CYCLE's recorded feed, the
// ATTEMPT those events belong to, and the cursor to carry into the next poll.
//
// One call serves ONE attempt. A cycle that was re-dispatched has a file per
// attempt (their seqs both start at 1, so they cannot share one), and the
// cursor rolls forward to the next attempt when the current one is exhausted —
// so a viewer replays attempt 1 whole, then attempt 2, which is exactly how the
// console renders them: sequential crews under one cycle.
//
// It reports no `final` flag and no truncation flag, unlike its v1 sibling: the
// run progress stream settles on the RUN's state, not on a cycle's log, and the
// two facts a caller would have taken from those flags are on the feed itself —
// a `notice` for a gap, and the run's own `run_settled`.
func (r *AgentProgressReader) CycleEvents(_ context.Context, cycle *delivery.RunCycle, cursor string) ([]gen.RunEvent, int, string, error) {
	if r == nil || cycle == nil || cycle.JobRef == "" {
		return nil, 0, cursor, nil
	}
	if r.recordings == nil {
		// A boot with no workspace volume records nothing. Reporting `none` and
		// an empty feed is the honest answer, and is what RecordingState says too.
		return nil, cycle.Attempts, cursor, nil
	}
	if !r.recordings.HasRecording(cycle.OrgID, cycle.ID) {
		// No record of this cycle at all: a cycle dispatched before the recorder
		// shipped, or one whose recorder has not reached its first tick yet.
		return nil, cycle.Attempts, cursor, nil
	}

	attempts := r.recordings.Attempts(cycle.OrgID, cycle.ID)
	if len(attempts) == 0 {
		// The directory is there and the events are not: the platform HAD a record
		// and cannot serve it. An empty feed would read as an agent that said
		// nothing, which is the opposite fact.
		return []gen.RunEvent{logsUnavailableRunEvent(time.Now(), "the recording could not be read")}, cycle.Attempts, cursor, nil
	}

	attempt, offset := parseFeedCursor(cursor)
	if attempt == 0 || !containsAttempt(attempts, attempt) {
		attempt, offset = attempts[0], 0
	}
	for {
		events, next, err := r.recordings.ReadFrom(cycle.OrgID, cycle.ID, attempt, offset)
		switch {
		case errors.Is(err, ErrNoRecording):
			return []gen.RunEvent{logsUnavailableRunEvent(time.Now(), "the recording could not be read")}, attempt, cursor, nil
		case err != nil:
			return nil, attempt, cursor, fmt.Errorf("read cycle recording: %w", err)
		}
		if len(events) > 0 {
			return events, attempt, formatFeedCursor(attempt, next), nil
		}
		// Nothing new in this attempt. Roll to the next one only when a LATER
		// attempt exists — a later attempt is proof this one is over, so an empty
		// read is the end of it rather than a pause in it. Rolling inside the loop
		// (rather than on the next poll) keeps a replay from costing one tick per
		// attempt.
		nxt, ok := nextAttempt(attempts, attempt)
		if !ok {
			return nil, attempt, formatFeedCursor(attempt, next), nil
		}
		attempt, offset = nxt, 0
	}
}

// RecordingState is what the platform can serve of this cycle's feed, for
// RunCycleView.recording. It is a different question from what the cycle DID,
// and one a client has to ask before presenting a feed as the story of a cycle.
func (r *AgentProgressReader) RecordingState(cycle *delivery.RunCycle) gen.RunCycleViewRecording {
	if r == nil || cycle == nil || r.recordings == nil {
		return gen.RunCycleViewRecordingNone
	}
	return r.recordings.State(cycle.OrgID, cycle.ID)
}

// feedCursorPrefix marks a cursor as an offset into a recording. It exists so a
// cursor that came from somewhere else — a client that kept one across a
// deployment, a future second source — is recognised as foreign and restarted
// from the beginning rather than seeked with.
const feedCursorPrefix = "r:"

// formatFeedCursor renders `r:<attempt>:<byteOffset>`.
func formatFeedCursor(attempt int, offset int64) string {
	return fmt.Sprintf("%s%d:%d", feedCursorPrefix, attempt, offset)
}

// parseFeedCursor reads one back. Anything it does not recognise — including
// the empty string a fresh connection sends — is (0, 0): start at the first
// attempt, from byte zero.
func parseFeedCursor(cursor string) (attempt int, offset int64) {
	rest, ok := strings.CutPrefix(cursor, feedCursorPrefix)
	if !ok {
		return 0, 0
	}
	a, o, ok := strings.Cut(rest, ":")
	if !ok {
		return 0, 0
	}
	attempt, err := strconv.Atoi(a)
	if err != nil || attempt <= 0 {
		return 0, 0
	}
	offset, err2 := strconv.ParseInt(o, 10, 64)
	if err2 != nil || offset < 0 {
		return attempt, 0
	}
	return attempt, offset
}

// containsAttempt reports whether the recording holds this attempt's file.
func containsAttempt(attempts []int, attempt int) bool {
	for _, a := range attempts {
		if a == attempt {
			return true
		}
	}
	return false
}

// nextAttempt returns the smallest recorded attempt greater than attempt.
func nextAttempt(attempts []int, attempt int) (int, bool) {
	for _, a := range attempts {
		if a > attempt {
			return a, true
		}
	}
	return 0, false
}

// platformNotice is a marker the PLATFORM put on the feed, not something a
// producer said. Three things mark it as such and each is deliberate:
//
//   - the negative, STABLE seq (the seqBoot* / seqLogs* space): a client dedups
//     on (cycle, attempt, seq) and a producer's seqs are positive, so the same
//     state re-derived every tick collapses to one row and a state TRANSITION
//     shows exactly one new row;
//   - `at`, the instant the platform DERIVED this marker, passed in by the
//     caller that derived it — see below;
//   - `agentId: lead`. The field is required and there is no honest third value:
//     any id other than `lead` names a SPAWNED agent under the contract, so a
//     made-up one would make a console open a row for an agent that does not
//     exist.
//
// `at` is a parameter and not a `time.Now()` in here because the caller always
// knows a truer instant than this function could read: the recorder stamps the
// read that observed the pod, and a gap takes the clock of the line that
// revealed it, which stays exact even when the page is a backfill read minutes
// later.
//
// IT MUST BE A REAL INSTANT. These markers used to carry no timestamp at all, on
// the reasoning that a recorded marker is ordered by its position in the file
// and needs no clock of its own — and ordering is indeed `seq`'s job, the
// contract says so. But `RunEvent.ts` is required and generates as a
// `time.Time`, so "no timestamp" was never on the wire: Go's zero value
// marshalled as `0001-01-01T00:00:00Z`, a well-formed date no consumer can tell
// from a real one. Because these notices belong to the lead, and the dark-zone
// one sits at the head of very nearly every recording, a console subtracting it
// from the clock showed the lead agent as 2026 years old (`1065409035m47s`)
// while its two children read `2m45s` and `2m25s`, and drew the lead's timeline
// lane across the whole axis. A field a producer cannot honestly fill has to be
// absent; this one cannot be absent, so it is filled honestly.
//
// Notices the RECORDER writes at a point in the run (a gap, the size cap) break
// the negative-seq rule on purpose: they are one-time facts about a position in
// the feed rather than a state re-derived every poll, so they take the next free
// positive seq and stay where they happened.
func platformNotice(at time.Time, seq int64, level gen.RunEventLevel, detail string) gen.RunEvent {
	return gen.RunEvent{
		V:       gen.RunEventV2,
		Seq:     seq,
		TS:      at.UTC(),
		Kind:    gen.RunEventKindNotice,
		AgentID: leadAgentID,
		Level:   level,
		Detail:  capText(detail, capLongText),
	}
}

// logsUnavailableRunEvent is the empty state for a cycle whose recording cannot
// be served — `lost`. `code: gap` is the contract's word for the feed itself
// losing events, which is exactly what has happened: an empty feed and a lost
// one look identical to a reader and mean opposite things about the agent, so
// the platform never lets "gone" render as "silent".
func logsUnavailableRunEvent(at time.Time, reason string) gen.RunEvent {
	detail := "The recording of this cycle is no longer available."
	if reason != "" {
		detail += " (" + reason + ")"
	}
	ev := platformNotice(at, seqLogsUnavailable, gen.RunEventLevelWarn, detail)
	ev.Code = gen.RunEventCodeGap
	return ev
}

// bootstrapRunEvent narrates the dark zone — the stretch (pod scheduling, image
// pull, container boot) before the runner writes its first line, which the
// console otherwise showed as a dead "waiting…" for the slowest part of the flow.
//
// It is a `notice` and NOT an agent event, because a pod that has not started is
// not an agent: there is no session, no lead turn and nothing to report a phrase
// about, and lifting a scheduling delay into `agent_progress` would put words in
// the mouth of an agent that does not exist yet.
//
// It carries a CODE and, ordinarily, no prose. `code` gained six pod-truth
// members (`runner_scheduling`, `runner_unschedulable`, `runner_pulling_image`,
// `runner_image_pull_backoff`, `runner_config_error`, `runner_starting`), and
// they are exactly the stable ids bootstrapState has always carried in its
// `name`. Sending the sentence instead would put the wording in a producer,
// which is the one place the design says it may not live: @aep/progress-view
// owns the copy, keyed off the code, so a consumer can relabel or translate the
// dark zone without a platform release. The stable negative seq stays the dedup
// key; `detail` is filled only where the code genuinely cannot say the whole
// thing (see bootState.detail).
//
// The RECORDER writes these into the recording rather than each viewer deriving
// them: a viewer that reads only the file would otherwise see nothing at all
// until the runner's first line, which is the very silence this narration
// exists to fill.
func bootstrapRunEvent(at time.Time, podFound bool, phase, waitingReason, message string) gen.RunEvent {
	st := bootstrapState(podFound, phase, waitingReason, message)
	level := gen.RunEventLevelInfo
	if st.alarming {
		level = gen.RunEventLevelWarn
	}
	ev := platformNotice(at, st.seq, level, st.detail)
	if code := gen.RunEventCode(st.name); code.Valid() {
		ev.Code = code
	}
	return ev
}
