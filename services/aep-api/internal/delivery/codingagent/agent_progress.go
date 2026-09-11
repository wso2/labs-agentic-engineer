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

// Coding-agent activity feed for the console's V1 progress surfaces.
//
// The runner emits typed NDJSON events on stdout — v2 RunEvents, or the frozen
// v1 envelope from a pre-cutover image — and this reader turns that stream into
// a v1 feed from whichever source can still see it:
//
//   - while the cycle's Component exists, the pod's live log through the
//     OpenChoreo API (LiveLogSource);
//   - once the pod is reaped but the Component is retained, the observability
//     plane's archive (ArchiveLogSource);
//   - when neither can answer, a single synthetic "logs unavailable" line.
//
// That last case is deliberate. An empty stream and a lost log look identical
// to a reader, and they mean opposite things about the agent — so the reader
// never lets "gone" render as "silent".
//
// THIS DERIVE-PER-VIEWER SHAPE IS THE OLD ONE, and it survives only for the v1
// surfaces: the VERSION build-progress stream, which stitches many runs into
// one narrative, and the legacy execution path. The v2 RUN feed no longer
// derives anything — the platform records each cycle once and serves every
// viewer from that file (run_recorder.go, run_events.go). Which is why the
// page cap below is named `legacy`: a recording has no window at all.
//
// The legacy coding_agent_logs snapshot is still READ for execution rows that
// predate the milestone model, and nothing writes new ones.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

const (
	// progressSchemaVersion mirrors the runner NDJSON schema (schemaVersion=1).
	progressSchemaVersion = 1
	// legacyProgressLimit caps events surfaced per poll on the V1 surfaces.
	//
	// It is a property of deriving a feed from a sliding pod-log window: a page
	// has to end somewhere, and the newest events are the ones a live tail wants.
	// The cost was that a finished run's whole post-mortem WAS this window —
	// 200 events, however long the run — which is one of the five losses the
	// recording closes. The v2 run feed therefore has no cap: it reads a file
	// from offset zero. Nothing here applies to it.
	legacyProgressLimit = 200
)

// Bootstrap seqs identify the synthetic "dark zone" progress lines the reader
// emits BEFORE the runner writes its first NDJSON line — the stretch (pod
// scheduling, image pull, container boot) the live-tail is otherwise blind to,
// so the console showed a dead "waiting…" for the slowest part of the flow.
//
// Each pre-stdout state carries a STABLE, NEGATIVE seq. The console dedups the
// unified timeline by (executionId, seq) and the runner's real lines use
// positive seqs, so: negatives never collide with real output; the same state
// re-derived every ~2s collapses to one row; and a state TRANSITION (a new
// negative seq) shows exactly one new row. Ts is left empty on purpose — these
// are transient markers, not wall-clock log lines (filterEventsAfter keeps
// untimestamped events, and lastEventMillis ignores them, so the cursor never
// advances past a synthetic line).
const (
	seqBootScheduling    = -10 // Job applied, pod not scheduled / created yet
	seqBootPulling       = -11 // ContainerCreating / PodInitializing (image pull + setup)
	seqBootBackoff       = -12 // ImagePullBackOff / ErrImagePull (pull retrying)
	seqBootConfig        = -13 // CreateContainerConfigError / secrets not yet materialised
	seqBootStarting      = -14 // container Running, agent has not emitted its first line
	seqBootUnschedulable = -15 // PodScheduled=False (cluster capacity / Too many pods)

	// seqHeadDropped marks the "earlier output omitted" row; seqScanTruncated the
	// "line over the size cap" one. Same stable-negative contract as the bootstrap
	// seqs above, for the same dedup reason: both are re-derived on every poll and
	// must collapse to one row rather than accumulate.
	seqHeadDropped   = -20
	seqScanTruncated = -21
)

// seqLogsUnavailable is the stable seq of the "logs are gone" line. It sits in
// the same negative space as the dark-zone markers and for the same reason: the
// console dedups by (id, seq), so re-deriving this state every poll collapses
// to one row instead of repeating forever.
const seqLogsUnavailable = -20

// seqLogsTruncated is the stable seq of the "newest window only" banner when a
// finished cycle's archive exceeds legacyProgressLimit. Same negative-space
// reason as seqLogsUnavailable.
const seqLogsTruncated = -21

// logsUnavailableEvent is the console's empty state for a cycle whose log no
// longer exists anywhere — the component was reclaimed, or this deployment has
// no observability plane to have archived it.
func logsUnavailableEvent(reason string) contracts.ProgressEvent {
	summary := "Logs for this cycle are no longer available."
	if reason != "" {
		summary += " (" + reason + ")"
	}
	return contracts.ProgressEvent{
		SchemaVersion: progressSchemaVersion,
		Seq:           seqLogsUnavailable,
		Kind:          "phase",
		Phase:         "logs_unavailable",
		Summary:       summary,
	}
}

// logsTruncatedEvent tells the console the finished cycle had more output than
// one progress page can carry — we kept the newest window, not the whole run.
func logsTruncatedEvent() contracts.ProgressEvent {
	return contracts.ProgressEvent{
		SchemaVersion: progressSchemaVersion,
		Seq:           seqLogsTruncated,
		Kind:          "phase",
		Phase:         "logs_truncated",
		Summary: fmt.Sprintf(
			"Showing the newest %d lines of this cycle's output.",
			legacyProgressLimit,
		),
	}
}

// bootState is one pre-stdout runner state, resolved once and rendered by both
// envelope versions of the feed.
//
//   - seq is the stable negative id;
//   - name is the stable phase id — the v1 console maps it to a friendly label,
//     and it is ALSO the v2 `notice` code verbatim (RunEventCodeRunner*), which
//     is what lets the v2 render carry no prose at all;
//   - summary is the human sentence, and is the V1 render's alone. Wording lives
//     in @aep/progress-view, keyed off the code; a producer that shipped its own
//     copy would be a second place the words could change;
//   - detail is the only prose the v2 render carries, and only where the code
//     genuinely cannot say the whole thing: the scheduler's own message on an
//     unschedulable pod, and the raw waiting reason on one this build does not
//     recognise. Empty everywhere else;
//   - alarming marks the states that are a problem rather than a wait (the v2
//     feed renders it as a `warn` notice).
type bootState struct {
	seq      int64
	name     string
	summary  string
	detail   string
	alarming bool
}

// bootstrapState maps a pre-stdout runner state to its marker. podFound=false
// means the Job exists but no pod object does yet; otherwise phase/waitingReason/
// message come from the pod's status (waitingReason is the first waiting
// container's reason, or PodScheduled's Unschedulable when the pod never got a
// node; "" once the container is running).
func bootstrapState(podFound bool, phase, waitingReason, message string) bootState {
	scheduling := bootState{seq: seqBootScheduling, name: "runner_scheduling", summary: "Waiting for a runner to be scheduled…"}
	if !podFound {
		return scheduling
	}
	switch waitingReason {
	case "ImagePullBackOff", "ErrImagePull", "ImageInspectError", "RegistryUnavailable":
		return bootState{seq: seqBootBackoff, name: "runner_image_pull_backoff",
			summary: "Pulling the agent image is taking longer than usual (retrying)…", alarming: true}
	case "CreateContainerConfigError", "CreateContainerError", "InvalidImageName":
		return bootState{seq: seqBootConfig, name: "runner_config_error",
			summary: "Runner is waiting on its configuration and secrets…", alarming: true}
	case "ContainerCreating", "PodInitializing":
		return bootState{seq: seqBootPulling, name: "runner_pulling_image", summary: "Pulling the agent image and preparing the container…"}
	case "Unschedulable", "SchedulerError":
		// Cluster has no room (Too many pods / Insufficient cpu/memory). Do not
		// reuse runner_scheduling — that reads as "still queuing" when the truth
		// is capacity. The scheduler's own first line is the one thing the code
		// cannot carry (WHICH resource ran out), so it rides `detail`.
		st := bootState{seq: seqBootUnschedulable, name: "runner_unschedulable",
			summary: "No capacity to schedule the runner on the cluster…", alarming: true}
		if detail := firstLine(message); detail != "" {
			st.detail = detail
			st.summary = "No capacity to schedule the runner: " + detail
		}
		return st
	case "":
		// No waiting reason: a Running pod is booting the agent; anything else
		// (Pending with no container status yet) is still being scheduled.
		if strings.EqualFold(phase, "Running") {
			return bootState{seq: seqBootStarting, name: "runner_starting", summary: "Runner container started — booting the agent…"}
		}
		return scheduling
	default:
		// An unrecognised waiting reason — surface it verbatim so nothing hides,
		// bucketed under the pulling seq and code (its most common cause). The
		// reason itself is the detail: it is a word this build has never seen, so
		// no code can stand for it.
		return bootState{seq: seqBootPulling, name: "runner_pulling_image",
			summary: "Preparing the runner container (" + waitingReason + ")…", detail: waitingReason}
	}
}

// bootstrapEvent renders that state as the v1 synthetic progress line. Phase
// names are stable ids the console maps to friendly labels; Summary is the human
// fallback when it doesn't.
func bootstrapEvent(podFound bool, phase, waitingReason, message string) contracts.ProgressEvent {
	st := bootstrapState(podFound, phase, waitingReason, message)
	return contracts.ProgressEvent{
		SchemaVersion: progressSchemaVersion,
		Seq:           st.seq,
		Kind:          "phase",
		Phase:         st.name,
		Summary:       st.summary,
		// Ts intentionally empty — synthetic, wall-clock-less marker.
	}
}

// firstLine returns the first non-empty line of s, trimmed — kubelet / scheduler
// messages are often multi-line and the console only wants one sentence.
func firstLine(s string) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	return s
}

// AgentProgressReader serves a cycle's (or a legacy execution's) agent activity.
//
// It answers two different questions from two different places, which is the
// whole shape of the cutover: the v2 RUN feed comes out of the platform's own
// recording (recordings), and the v1 surfaces still derive theirs from the live
// pod log or the archive (live / archive).
type AgentProgressReader struct {
	live    LiveLogSource
	archive ArchiveLogSource

	// recordings is the v2 read: the file the CycleRecorder wrote for this
	// cycle. nil on a boot with no workspace volume, which every reader then
	// reports as `none` rather than falling back to a per-viewer pod tail —
	// silently re-deriving would hide the fact that nothing is being recorded.
	recordings *RecordingStore

	// logs is the LEGACY execution-keyed snapshot store. Read-only: the
	// milestone model mints no execution rows, so this serves history that
	// predates it and nothing writes to it any more.
	logs delivery.CodingAgentLogRepository
}

// NewAgentProgressReader wires the reader. live may be nil in a degraded boot
// (every read then reports unavailable rather than erroring).
func NewAgentProgressReader(live LiveLogSource, logs delivery.CodingAgentLogRepository) *AgentProgressReader {
	return &AgentProgressReader{live: live, logs: logs}
}

// WithArchive attaches the post-terminal source for the V1 surfaces. Optional —
// without it, a cycle whose pod is gone reports unavailable. Returns the
// receiver.
func (r *AgentProgressReader) WithArchive(a ArchiveLogSource) *AgentProgressReader {
	r.archive = a
	return r
}

// WithRecordings attaches the run-feed recording store — the v2 read's only
// source. Optional; without it every cycle reports `none`. Returns the receiver.
func (r *AgentProgressReader) WithRecordings(store *RecordingStore) *AgentProgressReader {
	r.recordings = store
	return r
}

// cycleLog is WHICH source could still answer for a cycle, plus how to read its
// silence. It serves the V1 surfaces only: the v2 run feed reads a recording
// and asks no such question, because the platform wrote the file and knows what
// it holds.
type cycleLog struct {
	// text is the raw pod stdout the winning source returned. Empty is a real
	// answer — "nothing said yet" — not a failure.
	text string
	// live says the dark zone MAY be narrated when text holds nothing: the
	// attempt is still running, so the pod's own state is the only report there
	// is.
	live bool
	// final marks a settled answer (a closed cycle) so consumers stop polling.
	final bool
	pod   openchoreo.RuntimePod
	// gone means no source can answer for this cycle any more; reason says why,
	// in words a user can act on rather than an error chain.
	gone   bool
	reason string
}

// resolveCycleLog picks the source that can still see a cycle's output: the live
// pod tail while its Component exists, then the observability archive while the
// Component is retained, then nothing at all.
func (r *AgentProgressReader) resolveCycleLog(ctx context.Context, cycle *delivery.RunCycle) (cycleLog, error) {
	closed := cycle.EndedAt != nil

	if r.live != nil {
		tail, err := r.live.Tail(ctx, cycle.OrgID, cycle.ProjectID, cycle.JobRef, logPageBytes)
		switch {
		case err == nil:
			// Real OCLogSource.Tail returns success + empty text when the
			// Component is retained but the pod has nothing to say — not
			// ErrComponentGone. Empty live falls through to the archive when
			// the cycle is closed, the pod is terminal, or the archive already
			// holds lines (pod reaped while the cycle is still open awaiting
			// its PR webhook). Otherwise empty live is still scheduling / boot.
			if strings.TrimSpace(tail.Text) != "" {
				return cycleLog{text: tail.Text, live: !closed, final: closed, pod: tail.Pod}, nil
			}
			if !closed && !terminalPod(tail.Pod) {
				if text, aerr := r.readArchive(ctx, cycle); aerr == nil && strings.TrimSpace(text) != "" {
					return cycleLog{text: text}, nil
				}
				return cycleLog{text: tail.Text, live: true, pod: tail.Pod}, nil
			}
		case !errors.Is(err, ErrComponentGone):
			// A transport failure is not an answer about the cycle: surface it so
			// the caller degrades this poll and tries again.
			return cycleLog{}, fmt.Errorf("tail cycle pod log: %w", err)
		}
	}

	// The Component is gone, live was empty on a closed cycle, or there is no
	// live source: the archive is the only remaining reader. It only answers
	// while the Component is retained — so this is also where "the component
	// was reclaimed" surfaces.
	text, err := r.readArchive(ctx, cycle)
	if err != nil {
		// A CLOSED cycle will never gain a new source, so its unavailability is
		// settled. An open one may still be mid-render or mid-observer-hiccup.
		return cycleLog{gone: true, reason: unavailableReason(err), final: closed}, nil
	}
	if strings.TrimSpace(text) == "" && closed {
		return cycleLog{gone: true, reason: "no archived output", final: true}, nil
	}
	return cycleLog{text: text, final: closed}, nil
}

// CycleProgress returns one run CYCLE's agent activity as V1 progress events,
// filtered to events strictly newer than sinceMillis.
//
// The v2 read of the same cycle is CycleEvents, and the two no longer share a
// source: this one derives per viewer from the pod (or the archive), while that
// one reads the platform's recording. This survives because the VERSION
// build-progress stream still speaks v1 — it stitches many runs into one
// narrative and is not part of the run feed's cutover.
func (r *AgentProgressReader) CycleProgress(ctx context.Context, cycle *delivery.RunCycle, sinceMillis int64) (*contracts.ProgressResponse, error) {
	resp := &contracts.ProgressResponse{
		SchemaVersion: progressSchemaVersion,
		Lines:         []contracts.ProgressEvent{},
		CursorMillis:  sinceMillis,
		Final:         false,
	}
	if r == nil || cycle == nil || cycle.JobRef == "" {
		return resp, nil
	}
	src, err := r.resolveCycleLog(ctx, cycle)
	if err != nil {
		return nil, err
	}
	if src.gone {
		resp.Lines = []contracts.ProgressEvent{logsUnavailableEvent(src.reason)}
		resp.Final = src.final
		return resp, nil
	}
	return r.fromText(resp, src.text, sinceMillis, src.live, src.final, src.pod), nil
}

// readArchive asks the observability plane for the cycle's window. The window
// is the cycle's own lifetime, padded either side: the dispatch write and the
// first pod line are seconds apart, and a closed cycle's last lines land after
// the merge webhook that closed it.
func (r *AgentProgressReader) readArchive(ctx context.Context, cycle *delivery.RunCycle) (string, error) {
	if r.archive == nil {
		return "", fmt.Errorf("%w: no archive configured", ErrArchiveUnavailable)
	}
	from := cycle.CreatedAt.UTC().Add(-5 * time.Minute)
	to := time.Now().UTC()
	if cycle.EndedAt != nil {
		to = cycle.EndedAt.UTC().Add(10 * time.Minute)
	}
	return r.archive.CycleArchive(ctx, ArchiveScope{
		OrgName:       cycle.OrgID,
		ProjectName:   cycle.ProjectID,
		ComponentName: cycle.JobRef,
		From:          from,
		To:            to,
	})
}

// fromText pages raw log text into the response, narrating the dark zone when
// the pod has said nothing yet and the attempt is still live. final marks a
// settled answer (closed cycle / terminal execution) so consumers stop polling.
func (r *AgentProgressReader) fromText(resp *contracts.ProgressResponse, text string, sinceMillis int64, live bool, final bool, pod openchoreo.RuntimePod) *contracts.ProgressResponse {
	lines, truncated, hadOutput := pageEvents(text, sinceMillis)
	if !hadOutput {
		// Only a source that has said NOTHING is still booting. A page that
		// holds output the caller has already seen means the agent is
		// mid-thought, and narrating there would pin a bootstrap line into the
		// middle of a running stream.
		if live {
			resp.Lines = []contracts.ProgressEvent{bootstrapEvent(pod.Found, pod.Phase, pod.WaitingReason, pod.Message)}
		}
		resp.Final = final
		return resp
	}
	resp.Lines, resp.Truncated = lines, truncated
	if truncated && final {
		// A settled cycle whose archive exceeded one page: lead with an honest
		// banner so the console never looks like the whole run was this short.
		resp.Lines = append([]contracts.ProgressEvent{logsTruncatedEvent()}, resp.Lines...)
	}
	if cur := lastEventMillis(resp.Lines); cur > resp.CursorMillis {
		resp.CursorMillis = cur
	}
	resp.Final = final
	return resp
}

// unavailableReason renders why nothing could be read, in words a user can act
// on rather than an error chain.
func unavailableReason(err error) string {
	switch {
	case errors.Is(err, ErrComponentGone):
		return "the runner's workload has been reclaimed"
	case errors.Is(err, ErrArchiveUnavailable):
		return "the observability plane is not available"
	default:
		return ""
	}
}

// terminalPod is true when the live source saw a Job pod that has already
// finished — empty live then means "try the archive", not "still scheduling".
func terminalPod(pod openchoreo.RuntimePod) bool {
	if !pod.Found {
		return false
	}
	switch pod.Phase {
	case "Succeeded", "Failed":
		return true
	default:
		return false
	}
}

// AgentProgress returns a legacy coding EXECUTION's activity. It prefers the
// persisted coding_agent_logs snapshot — rows written before the milestone
// model, which mints no execution rows and writes no new ones — and otherwise
// reads the same live pod source cycles use.
func (r *AgentProgressReader) AgentProgress(ctx context.Context, row *delivery.Execution, sinceMillis int64) (*contracts.ProgressResponse, error) {
	resp := &contracts.ProgressResponse{
		SchemaVersion: progressSchemaVersion,
		Lines:         []contracts.ProgressEvent{},
		CursorMillis:  sinceMillis,
		Final:         false,
	}
	if r == nil || row == nil || row.RunName == "" {
		return resp, nil
	}
	if r.logs != nil {
		execUUID, err := uuid.Parse(row.ID)
		if err != nil {
			return nil, fmt.Errorf("parse execution id: %w", err)
		}
		snap, err := r.logs.GetByRun(ctx, execUUID, row.RunName)
		if err != nil {
			return nil, fmt.Errorf("read coding_agent_logs: %w", err)
		}
		if snap != nil {
			resp.Lines, resp.Truncated, _ = pageEvents(snap.LogText, sinceMillis)
			if resp.Truncated {
				resp.Lines = append([]contracts.ProgressEvent{logsTruncatedEvent()}, resp.Lines...)
			}
			if cur := lastEventMillis(resp.Lines); cur > resp.CursorMillis {
				resp.CursorMillis = cur
			}
			if capturedMs := snap.CapturedAt.UnixMilli(); capturedMs > resp.CursorMillis {
				resp.CursorMillis = capturedMs
			}
			resp.Final = true
			return resp, nil
		}
	}
	if r.live == nil {
		return resp, nil
	}
	live := !taskmeta.ExecutionStatus(row.Status).IsTerminal()
	tail, err := r.live.Tail(ctx, row.OrgID, row.ProjectID, row.RunName, logPageBytes)
	if err != nil {
		if errors.Is(err, ErrComponentGone) {
			if !live {
				resp.Lines = []contracts.ProgressEvent{logsUnavailableEvent(unavailableReason(err))}
				resp.Final = true
			}
			return resp, nil
		}
		return nil, fmt.Errorf("tail pod log: %w", err)
	}
	return r.fromText(resp, tail.Text, sinceMillis, live, !live, tail.Pod), nil
}

// pageEvents parses a raw pod-log page into events newer than sinceMillis,
// capped at legacyProgressLimit. truncated is true when the raw page exceeded
// the cap (oldest lines dropped) or the post-filter set still exceeds it.
//
// hadOutput reports whether the page held ANY lines before the cursor filter —
// the distinction that decides whether a caller may narrate the dark zone. An
// empty result means "nothing new since your cursor" (the agent is thinking),
// which is emphatically not "the runner hasn't spoken yet"; conflating the two
// replays a bootstrap line into the middle of a live stream.
func pageEvents(text string, sinceMillis int64) (lines []contracts.ProgressEvent, truncated, hadOutput bool) {
	all, truncated := textToProgressEvents(text)
	newer := filterEventsAfter(all, sinceMillis)
	if len(newer) > legacyProgressLimit {
		newer = newer[:legacyProgressLimit]
		truncated = true
	}
	return newer, truncated, len(all) > 0
}

// textToProgressEvents splits raw pod stdout/stderr into progress events. Each
// line becomes one event; the K8s `timestamps=true` prefix
// (`YYYY-MM-DDTHH:MM:SS.NNNNNNNNNZ <line>`) is split off the front and used as
// the event Ts when the envelope carries none. When the page holds more than
// legacyProgressLimit lines the newest window is kept (live-tail freshness)
// and truncated is true.
func textToProgressEvents(text string) ([]contracts.ProgressEvent, bool) {
	if text == "" {
		return []contracts.ProgressEvent{}, false
	}
	// Single choke point for the console feed: both callers reach the UI through
	// here, so redacting the raw pod output once covers wrapped `log` lines and
	// structured envelope fields alike. See redact.go for why this runs even
	// though the runner already scrubs at the source.
	text = redactSecrets(text)
	text = dropTruncatedTail(text)
	out := make([]contracts.ProgressEvent, 0, 256)
	scanner := bufio.NewScanner(strings.NewReader(text))
	// Allow long lines — agent output occasionally dumps long JSON blobs that
	// exceed the default 64K scanner buffer.
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		ts, msg := splitTimestampPrefix(scanner.Text())
		ev := parseProgressLine(msg).ProgressEvent
		if ev.Ts == "" {
			ev.Ts = ts
		}
		if ev.SchemaVersion == 0 {
			ev.SchemaVersion = progressSchemaVersion
		}
		out = append(out, ev)
	}
	// A line past the buffer cap stops Scan() early and would drop the whole
	// REST of the page with no signal — the feed would just go quiet mid-run.
	// Name it in the stream instead. (Sibling: usageFromLog.)
	scanFailed := scanner.Err() != nil
	if scanFailed {
		slog.Warn("codingagent.textToProgressEvents: log scan stopped early — rest of page dropped", "error", scanner.Err())
		out = append(out, contracts.ProgressEvent{
			Kind:          "log",
			SchemaVersion: progressSchemaVersion,
			Seq:           seqScanTruncated,
			Summary:       "… an agent log line exceeded the reader's size cap; the rest of this page was skipped",
		})
	}
	if len(out) > legacyProgressLimit {
		// Keeping the NEWEST window is right, but dropping the rest in silence is
		// not: a fresh attach to a long finished run showed its last 200 events
		// with nothing to say the run had started earlier. Truncated carries that
		// fact on the response and no reader has ever consumed it (no wire field,
		// no console branch), so say it in the feed — the same way the scanner
		// overflow above does.
		kept := out[len(out)-(legacyProgressLimit-1):]
		return append([]contracts.ProgressEvent{headDroppedEvent(len(out) - len(kept))}, kept...), true
	}
	return out, scanFailed
}

// headDroppedEvent is the "earlier output omitted" row. Its seq is stable and
// negative for the same reason the bootstrap seqs are: the console dedups by
// (cycle, seq), positive seqs belong to the runner, and Ts is left empty so the
// marker never advances the cursor past a line the reader has not seen.
func headDroppedEvent(dropped int) contracts.ProgressEvent {
	return contracts.ProgressEvent{
		SchemaVersion: progressSchemaVersion,
		Seq:           seqHeadDropped,
		Kind:          "log",
		Summary:       fmt.Sprintf("… %d earlier line(s) omitted — showing the most recent %d", dropped, legacyProgressLimit-1),
	}
}

// dropTruncatedTail removes a trailing partial line — one the source cut
// mid-write rather than finished.
//
// Only an UNTERMINATED final line can be partial (a newline means the writer
// completed it), and among those only one that opens a JSON envelope it never
// closes is unambiguously a fragment. That pair of conditions is what keeps the
// rule safe: complete prose lines (bootstrap output, stray library writes) and
// complete envelopes both still reach the feed, including a final line the pod
// wrote without a trailing newline — the runner's terminal `result` among them,
// which is how the feed reports the run's outcome. (Token usage is NOT at stake
// here: usageFromLog parses the raw captured body, not this function's output.)
//
// Without this, parseProgressLine falls back to wrapping the raw bytes as a
// `log` event and the console renders `{"schemaVersion":1,"ts":"2026-` verbatim.
// Live tails re-read the line whole on the next poll; a captured snapshot loses
// only bytes that were never parseable.
func dropTruncatedTail(text string) string {
	if strings.HasSuffix(text, "\n") {
		return text
	}
	nl := strings.LastIndexByte(text, '\n')
	_, msg := splitTimestampPrefix(text[nl+1:]) // nl == -1 → the whole text
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" || trimmed[0] != '{' || json.Valid([]byte(trimmed)) {
		return text
	}
	return text[:nl+1]
}

// runnerLine is the runner's v1 NDJSON envelope AS THE READER DECODES IT: the
// console-facing contracts.ProgressEvent (embedded, so its fields decode in
// place) plus the terminal `result` line's token usage.
//
// The usage lives here and not on ProgressEvent because it is ACCOUNTING, not
// feed content: it is stamped onto the run cycle's row and must never reach a
// console. It used to sit on ProgressEvent, where it serialised as an
// undocumented `usage` field on a shape three surfaces put on the wire, kept
// harmless only by the fact that nothing but usage_capture.go ever filled it in.
// Moving it one level out makes that structural — a wire shape cannot carry
// what it does not have — while the reader still reads it from the same line.
type runnerLine struct {
	contracts.ProgressEvent
	Usage *contracts.CapturedUsage `json:"usage,omitempty"`
}

// parseProgressLine decodes one runner NDJSON envelope. Non-JSON lines, or lines
// without a recognised schema version / kind, are wrapped as a `log` event so
// the feed stays continuous (bootstrap output like "[oneshot] …" and stray
// library lines still render). Recovered from the retired observer.ParseProgressLine.
func parseProgressLine(raw string) runnerLine {
	wrapped := runnerLine{ProgressEvent: contracts.ProgressEvent{Kind: "log", Summary: raw}}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed[0] != '{' {
		return wrapped
	}
	var ln runnerLine
	if err := json.Unmarshal([]byte(trimmed), &ln); err != nil {
		return wrapped
	}
	if ln.SchemaVersion != progressSchemaVersion || ln.Kind == "" {
		return wrapped
	}
	return ln
}

// splitTimestampPrefix peels the K8s `?timestamps=true` prefix off a log line.
// Returns (ts, rest); ts="" when no RFC3339Nano prefix is present.
func splitTimestampPrefix(line string) (string, string) {
	i := strings.IndexByte(line, ' ')
	if i <= 0 {
		return "", line
	}
	candidate := line[:i]
	if _, err := time.Parse(time.RFC3339Nano, candidate); err != nil {
		return "", line
	}
	return candidate, line[i+1:]
}

// filterEventsAfter drops events whose ts is at or before sinceMillis (half-open
// (sinceMillis, +∞)). Events without a parseable ts are kept — the BFF never
// silently loses content it can't timestamp.
func filterEventsAfter(events []contracts.ProgressEvent, sinceMillis int64) []contracts.ProgressEvent {
	if sinceMillis <= 0 {
		return events
	}
	out := events[:0:len(events)]
	for _, e := range events {
		if e.Ts == "" {
			out = append(out, e)
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, e.Ts)
		if err != nil {
			out = append(out, e)
			continue
		}
		if t.UnixMilli() > sinceMillis {
			out = append(out, e)
		}
	}
	return out
}

// lastEventMillis returns the highest UnixMilli ts in events, or 0 when none
// carry a parseable ts. Advances the cursor only as far as emitted content
// reaches, so the next poll never skips late-arriving lines.
func lastEventMillis(events []contracts.ProgressEvent) int64 {
	var max int64
	for _, e := range events {
		if e.Ts == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, e.Ts)
		if err != nil {
			continue
		}
		if m := t.UnixMilli(); m > max {
			max = m
		}
	}
	return max
}
