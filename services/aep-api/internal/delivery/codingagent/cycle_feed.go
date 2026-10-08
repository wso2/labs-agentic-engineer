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

// cycle_feed.go — one run cycle's v2 RunEvent feed, read from wherever its log
// still is.
//
// SOURCE. While the cycle's pod exists its log is read from the pod, whole
// (OpenChoreo's pod log API, no window). Once the pod is gone the observability
// plane holds the same lines: by COMPONENT scope while the Component's release
// binding still resolves, by PROJECT scope once the settler has deleted it (the
// component scope resolves names through the control plane and a deleted name
// no longer resolves). Every observer read filters on the Component UID the
// cycle stored at dispatch, so a later Component reusing the name, or another
// Component of the project, never lends the cycle its lines. The switch is on
// whether a pod EXISTS, not on the Job's state: a suspended Job's finished pod
// still serves its own log, which is complete, while the index may lag.
//
// ONE PIPELINE FOR BOTH SOURCES. The console dedups on (cycle, attempt, seq),
// so a viewer connected across the pod → observer switch sees no duplicate and
// no hole only if both sources yield the same seqs for the same lines. They do
// because the feed keeps the PRODUCER's number: a v2 line keeps its own `seq`;
// a v1 line's seq s becomes 2s, with its synthesised `agent_started` at 2s-1.
// Lines with no producer seq (container bootstrap output, a stray library
// write) are not in the v2 feed at all — the observer's project scope cannot
// return them (its phrase admits only lines that carry `agentId`, which every
// v2 runner line does), so serving them from the pod would make the two
// sources disagree. They stay in the v1 CycleProgress surface.
// Lines are sorted by seq (the observer orders only to the second) and deduped
// on it, so a line the index returned twice is served once.
//
// ATTEMPTS. A re-dispatch reuses the cycle's Component, so one log can hold
// several pods. Producer-seq lines group by pod name (seq-less lines number
// nothing: the project scope never returns them), ordered by each pod's first
// line; the newest pod is the cycle's current attempt and earlier pods count down. A pod
// whose last line is older than the current attempt's dispatch (less the
// watcher's clock skew) is the previous attempt's leftover — the watcher's rule
// (isLeftoverPod), stated on lines so both sources apply it identically.
//
// PLATFORM NOTICES take negative seqs that are stable for what they describe:
// the dark-zone markers per pod state, a gap per hole start, "logs
// unavailable" for a read that failed. The one positive seq the platform mints
// is a cancelled run's closing `run_settled`, at the attempt's last seq + 1 —
// and only once the index has had time to take the tail, or it would take a
// real line's seq.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/wso2/aep/aep-api/internal/clients/observability"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// cycleLogQuerier is the observability plane's cycle read. Satisfied by
// observability.Client.
type cycleLogQuerier interface {
	QueryCycleLogs(ctx context.Context, q observability.CycleLogQuery) ([]observability.LogLine, observability.CycleLogStats, error)
}

const (
	// seqGapBase anchors gap notices: a hole starting at producer seq n is
	// notice seq seqGapBase-n, so the same hole is the same row on every poll
	// and from either source.
	seqGapBase = -1_000_000

	// feedReadTTL is how long one read of a cycle's log serves every viewer.
	// N viewers polling a cycle cost one OpenChoreo (or observer) read per
	// tick, not N.
	feedReadTTL = 2 * time.Second

	// feedFailureHold is how long a failed read keeps the cycle's recording
	// state `unavailable`.
	feedFailureHold = 60 * time.Second

	// feedIndexLag is how long after a cancelled cycle settled the feed waits
	// before minting its `run_settled`. The runner's last lines may still be
	// on their way into the index, and a settle numbered before they land
	// would take one of their seqs.
	feedIndexLag = 60 * time.Second

	// feedReadTimeout bounds one shared read. The read runs detached from the
	// viewer that started it (others are waiting on it), so it needs its own
	// bound.
	feedReadTimeout = 30 * time.Second

	// feedLineCap drops a single line larger than this. No runner event is
	// near it; a line that is would only be someone's blob.
	feedLineCap = 1 << 20

	// feedCursorPrefix marks a cursor as this feed's: `f:<attempt>:<lastSeq>`.
	// Anything else, the retired recording's `r:` cursors included, restarts
	// at the first attempt.
	feedCursorPrefix = "f:"
)

// CycleFeed serves run cycles' v2 feeds and their recording state.
type CycleFeed struct {
	runtime   openchoreo.RuntimeClient
	obs       cycleLogQuerier
	targets   writeTargetResolver
	retention time.Duration
	now       func() time.Time

	reads    singleflight.Group
	mu       sync.Mutex
	memo     map[string]feedRead
	failures map[string]time.Time
}

// NewCycleFeed wires the feed. obs may be nil (no observability plane): a
// cycle whose pod is gone then reads as unavailable. retention is the
// observability plane's log retention; zero never expires a cycle by age.
func NewCycleFeed(runtime openchoreo.RuntimeClient, obs cycleLogQuerier, targets writeTargetResolver, retention time.Duration) *CycleFeed {
	return &CycleFeed{
		runtime:   runtime,
		obs:       obs,
		targets:   targets,
		retention: retention,
		now:       time.Now,
		memo:      map[string]feedRead{},
		failures:  map[string]time.Time{},
	}
}

// feedRead is one read of a cycle's log, shared by every viewer for
// feedReadTTL.
type feedRead struct {
	at time.Time
	// pod is the cycle's newest pod; zero when there is none or the Component
	// is gone.
	pod openchoreo.RuntimePod
	// lines are the raw log lines, in source order.
	lines []feedLine
	// expired: the platform no longer keeps this cycle's log.
	expired bool
	err     error
}

// feedLine is one raw log line and the pod that wrote it.
type feedLine struct {
	pod string
	at  time.Time
	log string
}

// feedAttempt is one attempt's whole feed, in order.
type feedAttempt struct {
	number int
	events []gen.RunEvent
}

// Events returns the next events of one cycle's feed after cursor, the attempt
// they belong to, and the cursor to come back with. One call serves one
// attempt; the cursor rolls to the next attempt once the current one has
// nothing new and a later one exists.
//
// run is the cycle's run row, read for its cancel stamp; nil is allowed. It
// never returns an error: a failed read is a `logs unavailable` notice on the
// feed and the cycle's state turns `unavailable`.
func (f *CycleFeed) Events(ctx context.Context, run *delivery.MilestoneRun, cycle *delivery.RunCycle, cursor string) ([]gen.RunEvent, int, string, error) {
	if f == nil || cycle == nil || cycle.JobRef == "" {
		return nil, 0, cursor, nil
	}
	rd := f.read(ctx, cycle)
	current := currentAttempt(cycle)
	switch {
	case rd.err != nil:
		f.noteFailure(cycle.ID, rd.at)
		return []gen.RunEvent{logsUnavailableRunEvent(rd.at, "")}, current, cursor, nil
	case rd.expired:
		f.clearFailure(cycle.ID)
		return []gen.RunEvent{logsUnavailableRunEvent(rd.at, "it is older than the platform keeps agent logs")}, current, cursor, nil
	}
	f.clearFailure(cycle.ID)
	evs, attempt, next := walkFeed(f.assemble(cycle, run, rd), cursor, current)
	return evs, attempt, next, nil
}

// State is RunCycleView.recording for the cycle: the row's facts plus the
// outcome of the last observer read (a recent failure reads as unavailable).
// It never reads a log itself.
func (f *CycleFeed) State(cycle *delivery.RunCycle) gen.RunCycleViewRecording {
	if f == nil || cycle == nil {
		return gen.RunCycleViewRecordingUnavailable
	}
	now := f.clock()
	switch {
	case cycleLive(cycle):
		return gen.RunCycleViewRecordingLive
	case f.expired(cycle, now):
		return gen.RunCycleViewRecordingExpired
	case f.failedRecently(cycle.ID, now):
		return gen.RunCycleViewRecordingUnavailable
	default:
		return gen.RunCycleViewRecordingKept
	}
}

// read returns the cycle's current read, from the memo or from one shared
// load.
func (f *CycleFeed) read(ctx context.Context, cycle *delivery.RunCycle) feedRead {
	if rd, ok := f.cached(cycle.ID); ok {
		return rd
	}
	v, _, _ := f.reads.Do(cycle.ID, func() (any, error) {
		if rd, ok := f.cached(cycle.ID); ok {
			return rd, nil
		}
		// Detached from the caller's cancellation (other viewers wait on this
		// read) but not from its values: the observer authorizes the user's
		// bearer, and every viewer passed the same project fence.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), feedReadTimeout)
		defer cancel()
		rd := f.load(rctx, cycle)
		rd.at = f.clock().UTC()
		f.store(cycle.ID, rd)
		return rd, nil
	})
	return v.(feedRead)
}

// load reads the cycle's log from the pod while one exists, else from the
// observability plane.
func (f *CycleFeed) load(ctx context.Context, cycle *delivery.RunCycle) feedRead {
	env, err := cycleEnvironment(ctx, f.targets, cycle)
	if err != nil {
		return f.failed(ctx, cycle, "environment", fmt.Errorf("cycle environment: %w", err))
	}
	componentExists := true
	var pod openchoreo.RuntimePod
	binding, err := f.runtime.ReleaseBindingName(ctx, cycle.OrgID, cycle.ProjectID, cycle.JobRef, env)
	switch {
	case errors.Is(err, openchoreo.ErrNotFound):
		componentExists = false
	case err != nil:
		return f.failed(ctx, cycle, "binding", err)
	default:
		pod, err = f.runtime.PodSnapshot(ctx, cycle.OrgID, binding)
		switch {
		case errors.Is(err, openchoreo.ErrNotFound):
			componentExists, pod = false, openchoreo.RuntimePod{}
		case err != nil:
			return f.failed(ctx, cycle, "pod", err)
		case pod.Found:
			lines, err := f.runtime.PodLogs(ctx, cycle.OrgID, binding, pod.Name, 0)
			if err == nil {
				out := make([]feedLine, 0, len(lines))
				for _, l := range lines {
					out = append(out, feedLine{pod: pod.Name, at: l.Timestamp, log: l.Log})
				}
				return feedRead{pod: pod, lines: out}
			}
			if cycleLive(cycle) && podNeverRan(pod) {
				// The pod has written nothing yet, so the index has nothing of
				// it either, and OpenChoreo answers a container still being
				// created with an error. Its state is the whole report.
				if !errors.Is(err, openchoreo.ErrNotFound) {
					slog.DebugContext(ctx, "codingagent.CycleFeed: pending pod log not readable",
						"cycle", cycle.ID, "error", err)
				}
				return feedRead{pod: pod}
			}
			if !errors.Is(err, openchoreo.ErrNotFound) {
				return f.failed(ctx, cycle, "pod_log", err)
			}
			// The pod is listed and its log is not (a container not started,
			// or one already reaped): whatever it wrote is the index's.
		}
	}
	if cycle.ComponentUID == "" {
		// Nothing to filter the index on. A closed cycle's log is gone for
		// the platform; an open one is still in its dark zone.
		return feedRead{pod: pod, expired: !cycleLive(cycle)}
	}
	if f.expired(cycle, f.clock()) {
		return feedRead{pod: pod, expired: true}
	}
	from, to := cycleLogWindow(cycle, f.clock())
	lines, err := readCycleObserver(ctx, f.obs, ArchiveScope{
		CycleID:       cycle.ID,
		OrgName:       cycle.OrgID,
		ProjectName:   cycle.ProjectID,
		ComponentName: cycle.JobRef,
		ComponentUID:  cycle.ComponentUID,
		Environment:   env,
		From:          from,
		To:            to,
	}, componentExists)
	if err != nil {
		if cycleLive(cycle) && componentExists && !terminalPod(pod) {
			// An open cycle whose Component has no readable pod log is still
			// in its dark zone (or between its pod's end and the close, where
			// the next read catches up): narrate the pod state rather than
			// call the log lost, as the v1 resolver does.
			slog.WarnContext(ctx, "codingagent.CycleFeed: cycle log read failed, serving the dark zone",
				"cycle", cycle.ID, "source", "observer", "error", err)
			return feedRead{pod: pod}
		}
		return f.failed(ctx, cycle, "observer", err)
	}
	out := make([]feedLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, feedLine{pod: l.PodName, at: l.Timestamp, log: l.Log})
	}
	return feedRead{pod: pod, lines: out}
}

// failed logs one failed read (at most once per cycle per feedReadTTL, since
// reads are shared) and returns it.
func (f *CycleFeed) failed(ctx context.Context, cycle *delivery.RunCycle, source string, err error) feedRead {
	slog.WarnContext(ctx, "codingagent.CycleFeed: cycle log read failed",
		"cycle", cycle.ID, "source", source, "error", err)
	return feedRead{err: err}
}

// assemble builds every attempt's feed from one read.
func (f *CycleFeed) assemble(cycle *delivery.RunCycle, run *delivery.MilestoneRun, rd feedRead) []feedAttempt {
	groups := groupByPod(producerLines(rd.lines))
	numbers := attemptNumbers(cycle, groups)
	byAttempt := map[int][]feedLine{}
	for i, g := range groups {
		byAttempt[numbers[i]] = append(byAttempt[numbers[i]], g.lines...)
	}
	current := currentAttempt(cycle)
	attempts := make([]feedAttempt, 0, len(byAttempt)+1)
	for n, lines := range byAttempt {
		if evs := attemptEvents(lines); len(evs) > 0 {
			attempts = append(attempts, feedAttempt{number: n, events: evs})
		}
	}
	sort.Slice(attempts, func(i, j int) bool { return attempts[i].number < attempts[j].number })

	idx := -1
	for i := range attempts {
		if attempts[i].number == current {
			idx = i
		}
	}
	if idx < 0 && cycleLive(cycle) && !terminalPod(rd.pod) {
		// The dark zone: the current attempt has said nothing and the pod is
		// still coming up. Its own state is the only report there is.
		boot := bootstrapRunEvent(rd.at, rd.pod.Found, rd.pod.Phase, rd.pod.WaitingReason, rd.pod.Message)
		attempts = append(attempts, feedAttempt{number: current, events: []gen.RunEvent{boot}})
		idx = len(attempts) - 1
	}
	if !rd.pod.Found && f.cancelledAndIndexed(cycle, run) {
		if idx < 0 {
			attempts = append(attempts, feedAttempt{number: current})
			idx = len(attempts) - 1
		}
		if a := &attempts[idx]; !hasRunSettled(a.events) {
			a.events = append(a.events, cancelledSettle(cycle, lastPositiveSeq(a.events)+1))
		}
	}
	sort.Slice(attempts, func(i, j int) bool { return attempts[i].number < attempts[j].number })
	return attempts
}

// cancelledAndIndexed reports whether the cycle was ended by a cancel and the
// index has had time to take the runner's last lines.
//
// A cancel is recognised two ways. The cycle's own reason is the ordinary
// one. The run's cancel stamp covers the two closes that do not carry it: a
// cancel that landed before the dispatch was noted, and one whose close lost
// the race to the loop's own Finish. A cycle that closed BEFORE the stamp was
// not the one cancelled.
func (f *CycleFeed) cancelledAndIndexed(cycle *delivery.RunCycle, run *delivery.MilestoneRun) bool {
	if cycle.EndedAt == nil {
		return false
	}
	byReason := cycle.AgentReason == delivery.CycleReasonCancelled
	byRun := run != nil && run.CancelRequestedAt != nil && !cycle.EndedAt.Before(*run.CancelRequestedAt)
	if !byReason && !byRun {
		return false
	}
	end := cycleEnd(cycle)
	return f.clock().Sub(*end) >= feedIndexLag
}

// producerLines keeps the lines that carry a producer seq, redacted. Only these
// are numbered into attempts: a pod that wrote only seq-less output (a crash
// before the runner started) is absent from the observer's project scope, so
// counting it would number the same attempt differently on each source.
func producerLines(lines []feedLine) []feedLine {
	out := make([]feedLine, 0, len(lines))
	for _, l := range lines {
		if len(l.log) > feedLineCap {
			continue
		}
		l.log = redactSecrets(l.log)
		if _, _, ok := producerSeq(l.log); ok {
			out = append(out, l)
		}
	}
	return out
}

// attemptEvents turns one attempt's producer lines (see producerLines) into
// its feed: sorted and deduped on seq, lifted to v2, with a gap notice before
// the line after each hole.
func attemptEvents(lines []feedLine) []gen.RunEvent {
	type produced struct {
		seq  int64
		v1   bool
		msg  string
		line feedLine
	}
	parsed := make([]produced, 0, len(lines))
	for _, l := range lines {
		seq, v1, ok := producerSeq(l.log)
		if !ok {
			continue
		}
		parsed = append(parsed, produced{seq: seq, v1: v1, msg: l.log, line: l})
	}
	sort.SliceStable(parsed, func(i, j int) bool { return parsed[i].seq < parsed[j].seq })

	lift := newLifter()
	out := make([]gen.RunEvent, 0, len(parsed))
	var prev int64
	for _, p := range parsed {
		if p.seq == prev {
			continue // the same producer event, returned twice
		}
		podTS := ""
		if !p.line.at.IsZero() {
			podTS = p.line.at.UTC().Format(time.RFC3339Nano)
		}
		evs := lift.line(p.msg, podTS)
		if len(evs) == 0 {
			continue
		}
		base := p.seq
		if p.v1 {
			base = 2 * p.seq
		}
		// The line's own event takes base; a v1 lift's synthesised
		// agent_started precedes it at base-1. A v2 line lifts to one event.
		for i := range evs {
			evs[i].Seq = base - int64(len(evs)-1-i)
		}
		if p.seq > prev+1 {
			at := evs[0].TS
			if at.IsZero() {
				at = p.line.at.UTC()
			}
			out = append(out, feedGapNotice(at, prev+1, p.seq-prev-1))
		}
		out = append(out, evs...)
		prev = p.seq
	}
	return out
}

// feedGapNotice is the row for a hole in the producer's seqs, placed before the
// line after it. Its seq is fixed by where the hole starts.
func feedGapNotice(at time.Time, holeStart, missing int64) gen.RunEvent {
	ev := platformNotice(at, seqGapBase-holeStart, gen.RunEventLevelWarn,
		fmt.Sprintf("… %d event(s) of this run are missing from its log", missing))
	ev.Code = gen.RunEventCodeGap
	return ev
}

// cancelledSettle closes a cancelled attempt whose runner never settled it.
func cancelledSettle(cycle *delivery.RunCycle, seq int64) gen.RunEvent {
	return gen.RunEvent{
		V:       gen.RunEventVTwo,
		Seq:     seq,
		TS:      cycle.EndedAt.UTC(),
		Kind:    gen.RunEventKindRunSettled,
		AgentID: leadAgentID,
		Outcome: gen.RunEventOutcomeCancelled,
	}
}

// podGroup is one pod's lines.
type podGroup struct {
	name        string
	first, last time.Time
	lines       []feedLine
}

// groupByPod splits lines by the pod that wrote them, ordered by each pod's
// first line (ties by name, so the order is the same on every read).
func groupByPod(lines []feedLine) []podGroup {
	index := map[string]int{}
	var groups []podGroup
	for _, l := range lines {
		i, ok := index[l.pod]
		if !ok {
			i = len(groups)
			index[l.pod] = i
			groups = append(groups, podGroup{name: l.pod, first: l.at, last: l.at})
		}
		g := &groups[i]
		g.lines = append(g.lines, l)
		if !l.at.IsZero() && (g.first.IsZero() || l.at.Before(g.first)) {
			g.first = l.at
		}
		if l.at.After(g.last) {
			g.last = l.at
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if !groups[i].first.Equal(groups[j].first) {
			return groups[i].first.Before(groups[j].first)
		}
		return groups[i].name < groups[j].name
	})
	return groups
}

// attemptNumbers numbers pod groups: the newest is the current attempt, unless
// it is the previous attempt's leftover, and earlier pods count down. A cycle
// with more pods than attempts folds the oldest into attempt 1.
func attemptNumbers(cycle *delivery.RunCycle, groups []podGroup) []int {
	out := make([]int, len(groups))
	if len(groups) == 0 {
		return out
	}
	top := currentAttempt(cycle)
	if newest := groups[len(groups)-1]; top > 1 && cycle.DispatchedAt != nil && !newest.last.IsZero() &&
		newest.last.Before(cycle.DispatchedAt.Add(-dispatchClockSkew)) {
		top--
	}
	for i := range groups {
		n := top - (len(groups) - 1 - i)
		if n < 1 {
			n = 1
		}
		out[i] = n
	}
	return out
}

// walkFeed serves the events after the cursor, one attempt per call.
func walkFeed(attempts []feedAttempt, cursor string, current int) ([]gen.RunEvent, int, string) {
	if len(attempts) == 0 {
		return nil, current, cursor
	}
	attempt, after, ok := parseFeedCursor(cursor)
	idx := -1
	if ok {
		for i := range attempts {
			if attempts[i].number == attempt {
				idx = i
			}
		}
	}
	if idx < 0 {
		idx, after = 0, 0
	}
	for {
		a := attempts[idx]
		evs := eventsAfter(a.events, after)
		if len(evs) > 0 {
			return evs, a.number, formatFeedCursor(a.number, max(after, lastPositiveSeq(evs)))
		}
		if idx+1 < len(attempts) {
			idx, after = idx+1, 0
			continue
		}
		return nil, a.number, formatFeedCursor(a.number, after)
	}
}

// eventsAfter returns the events that follow the last producer event at or
// before seq: a notice travels with the line after it, and the dark-zone
// markers of an attempt that has said nothing are served on every poll (their
// stable seqs dedup them).
func eventsAfter(events []gen.RunEvent, seq int64) []gen.RunEvent {
	cut := 0
	for i, e := range events {
		if e.Seq > 0 && e.Seq <= seq {
			cut = i + 1
		}
	}
	return events[cut:]
}

func lastPositiveSeq(events []gen.RunEvent) int64 {
	var last int64
	for _, e := range events {
		if e.Seq > last {
			last = e.Seq
		}
	}
	return last
}

func hasRunSettled(events []gen.RunEvent) bool {
	for _, e := range events {
		if e.Kind == gen.RunEventKindRunSettled {
			return true
		}
	}
	return false
}

// formatFeedCursor renders `f:<attempt>:<lastSeq>`.
func formatFeedCursor(attempt int, lastSeq int64) string {
	return feedCursorPrefix + strconv.Itoa(attempt) + ":" + strconv.FormatInt(lastSeq, 10)
}

// parseFeedCursor reads one back; ok is false for anything else.
func parseFeedCursor(cursor string) (attempt int, lastSeq int64, ok bool) {
	rest, found := strings.CutPrefix(cursor, feedCursorPrefix)
	if !found {
		return 0, 0, false
	}
	a, s, found := strings.Cut(rest, ":")
	if !found {
		return 0, 0, false
	}
	attempt, err := strconv.Atoi(a)
	if err != nil || attempt <= 0 {
		return 0, 0, false
	}
	lastSeq, err = strconv.ParseInt(s, 10, 64)
	if err != nil || lastSeq < 0 {
		return 0, 0, false
	}
	return attempt, lastSeq, true
}

// producerSeq reads the producer's SEQ off a runner line, and whether it is a
// v1 line, without committing to an envelope: `{"v":2,…}` and
// `{"schemaVersion":1,…}` both number their events one up per line.
func producerSeq(raw string) (seq int64, v1 bool, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed[0] != '{' {
		return 0, false, false
	}
	var probe struct {
		V             int   `json:"v"`
		SchemaVersion int   `json:"schemaVersion"`
		Seq           int64 `json:"seq"`
	}
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		return 0, false, false
	}
	if probe.Seq <= 0 {
		return 0, false, false
	}
	switch {
	case probe.V == int(gen.RunEventVTwo):
		return probe.Seq, false, true
	case probe.SchemaVersion == progressSchemaVersion:
		return probe.Seq, true, true
	default:
		return 0, false, false
	}
}

// cycleLive is an open cycle whose Job has not been suspended.
func cycleLive(cycle *delivery.RunCycle) bool {
	return cycle.EndedAt == nil && cycle.JobSuspendedAt == nil
}

// podNeverRan is a listed pod still Pending with no container that ever
// finished: nothing it would write has been written yet.
func podNeverRan(pod openchoreo.RuntimePod) bool {
	return pod.Found && pod.Phase == "Pending" && pod.FinishedAt.IsZero()
}

// cycleEnd is the later of the cycle's close and its Job's suspend, nil while
// it is live. The pod writes until the suspend, which can land long after the
// merge closed the cycle.
func cycleEnd(cycle *delivery.RunCycle) *time.Time {
	end := cycle.EndedAt
	if s := cycle.JobSuspendedAt; s != nil && (end == nil || s.After(*end)) {
		end = s
	}
	return end
}

// currentAttempt is the attempt the cycle's row names (at least 1).
func currentAttempt(cycle *delivery.RunCycle) int {
	return max(cycle.Attempts, 1)
}

// expired reports a cycle whose log the platform no longer keeps: it predates
// Component UID capture, or it ended longer ago than the log retention.
func (f *CycleFeed) expired(cycle *delivery.RunCycle, now time.Time) bool {
	end := cycleEnd(cycle)
	if end == nil {
		return false
	}
	if cycle.ComponentUID == "" {
		return true
	}
	return f.retention > 0 && now.Sub(*end) > f.retention
}

// cycleLogWindow is the observer window for the cycle: its lifetime, padded
// either side. The dispatch write and the first pod line are seconds apart,
// and the last lines land after the close, the suspend, or the pod's going —
// whichever came last. A cycle with none of those reads up to now.
func cycleLogWindow(cycle *delivery.RunCycle, now time.Time) (from, to time.Time) {
	from = cycle.CreatedAt.UTC().Add(-5 * time.Minute)
	end := cycleEnd(cycle)
	if g := cycle.PodGoneAt; g != nil && (end == nil || g.After(*end)) {
		end = g
	}
	if end == nil {
		return from, now.UTC()
	}
	return from, end.UTC().Add(10 * time.Minute)
}

func (f *CycleFeed) clock() time.Time {
	if f.now == nil {
		return time.Now()
	}
	return f.now()
}

func (f *CycleFeed) cached(cycleID string) (feedRead, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rd, ok := f.memo[cycleID]
	if !ok || f.clock().Sub(rd.at) >= feedReadTTL {
		return feedRead{}, false
	}
	return rd, true
}

func (f *CycleFeed) store(cycleID string, rd feedRead) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.clock()
	for id, old := range f.memo {
		if now.Sub(old.at) >= feedReadTTL {
			delete(f.memo, id)
		}
	}
	f.memo[cycleID] = rd
}

func (f *CycleFeed) noteFailure(cycleID string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.clock()
	for id, t := range f.failures {
		if now.Sub(t) >= feedFailureHold {
			delete(f.failures, id)
		}
	}
	f.failures[cycleID] = at
}

func (f *CycleFeed) clearFailure(cycleID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.failures, cycleID)
}

func (f *CycleFeed) failedRecently(cycleID string, now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.failures[cycleID]
	if !ok {
		return false
	}
	if now.Sub(t) >= feedFailureHold {
		delete(f.failures, cycleID)
		return false
	}
	return true
}
