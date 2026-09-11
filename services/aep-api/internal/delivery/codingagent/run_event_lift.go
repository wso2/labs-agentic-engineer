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

// run_event_lift.go — one runner stdout line → v2 RunEvents.
//
// Two producers write that stream and both have to be readable at once, because
// a run dispatched before the cutover is still in flight while the new image
// rolls out:
//
//   - a v2 runner emits `{"v":2,…}`, which IS a RunEvent and needs no
//     translation;
//   - a v1 runner emits `{"schemaVersion":1,…}`, which is LIFTED here;
//   - anything else (bootstrap stdout, a stray library write, a half-line) is
//     wrapped rather than dropped, so the feed stays continuous.
//
// The lift is not a field rename. v1 attributes a line with `emitter` +
// `emitterId`, which is a fact about the LINE; v2 attributes it to an agent that
// STARTED and later SETTLED, which is a fact about the run. So the lift infers
// the agents: the first line carrying a new `emitterId` proves an agent exists,
// and the fan-out tool_result that answers that same id proves it is over. Both
// inferences are marked as such — a synthesised `agent_started` carries
// `role: "inferred"`, which is how a reader tells an agent the runtime declared
// from one this reader deduced.
//
// THE LIFT DOES NOT NUMBER. Every event it returns carries `seq: 0`, and the
// recorder stamps the real one as it appends (recordingSession.number). That
// split is not tidiness — it is the fix for a measured data loss. Numbering here
// could only ever be a function of the PRODUCER's seq, and a third of this
// stream has none: raw stdout (container bootstrap, a stray library write, a
// subprocess writing straight to fd 1, a crash tail) reaches the default arm
// below with no envelope at all. Those lines were all numbered 0, so a consumer
// deduping on (cycle, attempt, seq) — which the contract tells it to — kept the
// first and threw the rest away. A five-line npm notice arrived as one line; a
// five-line stack trace would have arrived as its first line. Only the party
// that writes the events in order can number them, and that is the recorder.

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// leadAgentID is the run's top-level agent, per the RunEvent contract. Every
// event needs an agentId, and a v1 line with no `emitterId` is the main agent's
// by construction — the runner stamps the field only on lines it forwards from
// inside a Task tool call, so absence is a positive fact and not an unknown.
const leadAgentID = "lead"

// roleInferred marks an agent this reader DEDUCED from the shape of a v1 feed
// rather than one a runtime declared. A v1 runner never announced its
// subagents; it only stamped their lines. Naming the inference on the event is
// what stops a consumer from later reporting a deduced tree as an observed one.
const roleInferred = "inferred"

// Field caps from the RunEvent schema (maxLength). They are enforced HERE, at
// the only point where unbounded producer text enters a v2 event: a lift that
// emitted an over-long `detail` would put the platform in breach of its own
// published contract, and the offender is always someone else's prose (a
// kubelet message, an agent's summary).
const (
	capShortText = 200 // label, phrase, summary, title
	capLongText  = 500 // detail, error, report
)

// v1 kinds the lift recognises. They are spelled out rather than reused from
// the TS schema because this is the compatibility half of a cutover: the list
// is frozen at what v1 could emit, and a v1 producer will never grow another.
const (
	v1KindPhase        = "phase"
	v1KindToolUse      = "tool_use"
	v1KindActivity     = "activity"
	v1KindToolResult   = "tool_result"
	v1KindGitCommit    = "git_commit"
	v1KindGitPush      = "git_push"
	v1KindGhAction     = "gh_action"
	v1KindLog          = "log"
	v1KindProgressItem = "progress_item"
	v1KindResult       = "result"
)

// v1PhaseAgentStarted is the one v1 phase that is not a phase of the platform's
// work but the START of the agent session — the runner emitted it off the SDK's
// init message. v2 has a kind for exactly that, so it lifts to `run_started`
// rather than to a phrase. (Its NAME collides with v2's `agent_started` kind and
// means something different: v2's is one spawned agent appearing, this is the
// whole run's lead session beginning.)
const v1PhaseAgentStarted = "agent_started"

// fanOutTools are the tool names a v1 runner used for a fan-out call. A
// tool_result for one of these whose toolUseId IS the emitterId is the SDK
// reporting on the subagent as a whole — its duration, tool count and line
// counts — which is v2's `agent_settled` and not a tool result at all.
var fanOutTools = map[string]bool{"Agent": true, "Task": true}

// lifter carries the only state a lift needs: which agents this page has
// already announced. It is per PAGE, not per run, and that is the honest scope
// — a live tail is a sliding 64KiB window, so a reader that attaches mid-run
// re-announces the agents it can see rather than pretending it watched them
// start. The console upserts agents by id, so a re-announcement repaints one
// row instead of adding one.
type lifter struct {
	announced map[string]bool
}

func newLifter() *lifter {
	return &lifter{announced: map[string]bool{}}
}

// line turns ONE raw stdout line into the events it stands for. podTs is the
// Kubernetes `timestamps=true` prefix already peeled off the front, used when
// the envelope carries no clock of its own.
func (l *lifter) line(raw, podTs string) []gen.RunEvent {
	if ev, ok := parseRunEvent(raw); ok {
		if ev.TS.IsZero() {
			ev.TS = parseEventTime(podTs)
		}
		if ev.AgentID == "" {
			ev.AgentID = leadAgentID
		}
		// The producer's own seq is DROPPED here and re-stamped by the recorder.
		// It is not lost: the recorder reads it off the raw line for dedupe and
		// gap detection (producerSeq), which is the only job it can do — one
		// producer's numbering cannot also number the seq-less lines interleaved
		// with it. In the ordinary case the recorder hands the line back the same
		// number, because both sequences are dense and start at 1.
		ev.Seq = 0
		return []gen.RunEvent{ev}
	}
	ln := parseProgressLine(raw)
	if ln.Ts == "" {
		ln.Ts = podTs
	}
	return l.lift(ln)
}

// parseRunEvent decodes a NATIVE v2 line. It reports false for anything else —
// a v1 envelope, prose, or a v2 envelope naming a kind this build does not know
// — and the caller then takes the v1 path, which wraps whatever it was as a
// readable event. An unknown kind is deliberately NOT passed through: `kind` is
// what every consumer switches on, so forwarding one nobody can render would
// put an unreadable row on a user's feed instead of a legible one.
func parseRunEvent(raw string) (gen.RunEvent, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed[0] != '{' {
		return gen.RunEvent{}, false
	}
	var probe struct {
		V    int    `json:"v"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		return gen.RunEvent{}, false
	}
	if gen.RunEventV(probe.V) != gen.RunEventV2 || !gen.RunEventKind(probe.Kind).Valid() {
		return gen.RunEvent{}, false
	}
	var ev gen.RunEvent
	if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
		return gen.RunEvent{}, false
	}
	return ev, true
}

// lift translates one decoded v1 line into v2. It returns a SLICE because a
// line can stand for two events: the first sighting of a subagent both proves
// the agent exists and reports whatever it was doing.
func (l *lifter) lift(ln runnerLine) []gen.RunEvent {
	var out []gen.RunEvent
	if ln.EmitterID != "" && !l.announced[ln.EmitterID] {
		l.announced[ln.EmitterID] = true
		out = append(out, l.announce(ln))
	}
	return append(out, l.event(ln))
}

// announce is the synthesised `agent_started` for a subagent this page has just
// met. It is returned BEFORE the line that revealed the agent, and the recorder
// numbers the slice in order, so the row that opens an agent always precedes the
// row that reports on it. depth is 1 because that is all a v1 feed can support: it carried one
// `emitterId` per line and no parent, so every agent it can describe is one the
// lead spawned. Saying 1 rather than leaving it absent is the honest reading —
// absence on this kind would mean "the lead's own event".
func (l *lifter) announce(ln runnerLine) gen.RunEvent {
	ev := l.base(ln, gen.RunEventKindAgentStarted)
	ev.Label = capText(ln.EmitterLabel, capShortText)
	ev.Depth = 1
	ev.Role = roleInferred
	return ev
}

// base fills the envelope every lifted event shares.
func (l *lifter) base(ln runnerLine, kind gen.RunEventKind) gen.RunEvent {
	agent := ln.EmitterID
	if agent == "" {
		agent = leadAgentID
	}
	return gen.RunEvent{
		V:       gen.RunEventV2,
		TS:      parseEventTime(ln.Ts),
		Kind:    kind,
		AgentID: agent,
	}
}

// event is the per-kind half of the lift.
func (l *lifter) event(ln runnerLine) gen.RunEvent {
	switch ln.Kind {
	case v1KindPhase:
		// The session's own start is a run event; every other phase is a coarse
		// state of the run that REPLACES the previous one, which is exactly what
		// `agent_progress`'s phrase is defined to be. Carrying the phase NAME as
		// the phrase keeps the stable id a console maps to a friendly label —
		// `notice` has nowhere to put one (its `code` is a closed set that has no
		// member for a workspace being provisioned).
		if ln.Phase == v1PhaseAgentStarted {
			return l.base(ln, gen.RunEventKindRunStarted)
		}
		ev := l.base(ln, gen.RunEventKindAgentProgress)
		ev.Phrase = capText(ln.Phase, capShortText)
		return ev

	case v1KindActivity:
		// v1's `activity` and v2's `agent_progress` are the same idea in both
		// contracts: the sentence an agent says about what it is doing right now,
		// which replaces its row's status rather than printing a line. v1's
		// running `toolCount` is dropped on purpose — the contract scopes
		// toolCount to `agent_settled`, where the runtime's authoritative total
		// arrives, and two different tool counts on one agent is the kind of
		// disagreement a feed never recovers from.
		ev := l.base(ln, gen.RunEventKindAgentProgress)
		ev.Phrase = capText(ln.Summary, capShortText)
		l.attributeAgent(&ev, ln)
		return ev

	case v1KindToolUse:
		ev := l.base(ln, gen.RunEventKindToolUse)
		ev.Tool = ln.Tool
		ev.Summary = capText(ln.Summary, capShortText)
		ev.ToolUseID = ln.ToolUseID
		return ev

	case v1KindToolResult:
		if l.isFanOutSettle(ln) {
			return l.settle(ln)
		}
		ev := l.base(ln, gen.RunEventKindToolResult)
		ev.Tool = ln.Tool
		ev.Summary = capText(ln.Summary, capShortText)
		ev.ToolUseID = ln.ToolUseID
		ev.DurationMs = ln.DurationMs
		ev.Ok = ln.OK
		ev.ExitCode = ln.ExitCode
		return ev

	case v1KindGitCommit:
		ev := l.base(ln, gen.RunEventKindGitCommit)
		ev.Sha, ev.Files = ln.SHA, ln.Files
		ev.Summary = capText(ln.Summary, capShortText)
		ev.ToolUseID = ln.ToolUseID
		return ev

	case v1KindGitPush:
		ev := l.base(ln, gen.RunEventKindGitPush)
		ev.Sha, ev.Branch = ln.SHA, ln.Branch
		ev.Summary = capText(ln.Summary, capShortText)
		ev.ToolUseID = ln.ToolUseID
		return ev

	case v1KindGhAction:
		ev := l.base(ln, gen.RunEventKindGhAction)
		ev.Command = ln.Command
		ev.Summary = capText(ln.Summary, capShortText)
		ev.ToolUseID = ln.ToolUseID
		return ev

	case v1KindProgressItem:
		// v1 had exactly one kind of item — a validation acceptance criterion —
		// so the source is not a guess. v2 gained plan items alongside them, and
		// `source` is what tells the two vocabularies apart.
		ev := l.base(ln, gen.RunEventKindWorkItem)
		ev.Source = gen.RunEventSourceCriterion
		ev.ItemID = ln.ItemID
		if st := gen.RunEventItemStatus(ln.Status); st.Valid() {
			ev.ItemStatus = st
		}
		return ev

	case v1KindResult:
		// v1's `summary` on a result has no v2 home — the contract scopes
		// `summary` to the tool and git kinds and gives `run_settled` only an
		// outcome, an error and a usage. It is dropped rather than smuggled into
		// `error`, which would report a successful run as one that failed.
		ev := l.base(ln, gen.RunEventKindRunSettled)
		ev.Outcome = runOutcome(ln.Status)
		ev.Error = capText(ln.Error, capLongText)
		ev.Usage = liftUsage(ln.Usage)
		return ev

	case v1KindLog:
		fallthrough
	default:
		// A `log` line, and anything this build does not recognise, is prose
		// somebody wrote for a reader. `notice` is where prose belongs, and it
		// carries NO code: the code enum is a closed set of conditions a consumer
		// branches on, and claiming one the producer never asserted would be a
		// worse lie than saying nothing.
		ev := l.base(ln, gen.RunEventKindNotice)
		ev.Level = noticeLevel(ln.Level)
		ev.Detail = capText(ln.Summary, capLongText)
		return ev
	}
}

// isFanOutSettle recognises the v1 shape that reports on a whole subagent: the
// tool_result answering the fan-out call whose id IS the subagent's id. The
// three-way test matters — a subagent's own ordinary tool results carry its
// emitterId too, and differ only in that their toolUseId is some other call's.
func (l *lifter) isFanOutSettle(ln runnerLine) bool {
	return ln.EmitterID != "" && ln.ToolUseID == ln.EmitterID && fanOutTools[ln.Tool]
}

// settle turns that result into `agent_settled`, carrying the SDK's own totals.
// They are the only measure of a spawned agent's work that exists: a v1 feed
// never carried a subagent's per-edit line counts, so nothing downstream can
// reconstruct them.
func (l *lifter) settle(ln runnerLine) gen.RunEvent {
	ev := l.base(ln, gen.RunEventKindAgentSettled)
	ev.Status = agentStatus(ln.Status, ln.OK)
	ev.DurationMs = ln.DurationMs
	ev.ToolCount = ln.ToolCount
	ev.LinesAdded, ev.LinesRemoved = ln.LinesAdded, ln.LinesRemoved
	l.attributeAgent(&ev, ln)
	return ev
}

// attributeAgent adds the label/depth that only the three agent-lifecycle kinds
// may carry, so a console can title and indent a row it never saw start.
func (l *lifter) attributeAgent(ev *gen.RunEvent, ln runnerLine) {
	if ln.EmitterID == "" {
		return
	}
	ev.Label = capText(ln.EmitterLabel, capShortText)
	ev.Depth = 1
}

// runOutcome maps v1's result status onto the v2 outcome. v1 had no word for a
// cancelled run — it only ever wrote success or failure — so an unrecognised
// status reads as a failure rather than being invented into `cancelled`, which
// would report a broken run as one somebody stopped on purpose.
func runOutcome(status string) gen.RunEventOutcome {
	if status == "success" {
		return gen.RunEventOutcomeSuccess
	}
	return gen.RunEventOutcomeFailure
}

// agentStatus maps the SDK's own verdict word onto AgentStatus, falling back to
// the tool result's ok flag when the runner reported no word. A settle with
// neither is `completed`: the fan-out call returned, and reporting an agent that
// finished as `failed` on the strength of a missing field is the worse error.
func agentStatus(word string, ok *bool) gen.AgentStatus {
	if st := gen.AgentStatus(word); st.Valid() {
		return st
	}
	if ok != nil && !*ok {
		return gen.AgentStatusFailed
	}
	return gen.AgentStatusCompleted
}

// noticeLevel maps v1's log level, defaulting to info — v1 left the field off
// ordinary lines.
func noticeLevel(level string) gen.RunEventLevel {
	if lv := gen.RunEventLevel(level); lv.Valid() {
		return lv
	}
	return gen.RunEventLevelInfo
}

// liftUsage carries the runner's captured token usage onto `run_settled`.
//
// The per-model SPLIT travels with the aggregate. A real coding run regularly
// touches a second model (the SDK's small-model helpers), which blanks the
// aggregate's model id under TokenUsage.Add's agreement rule and used to leave
// the whole run unpriceable; `models` is the breakdown that fixes that, and
// dropping it here would put the v1 compatibility path back in the hole the
// split was added to climb out of. It is a breakdown, not extra spend — the
// entries sum to the aggregate.
//
// CostUsd stays nil on purpose: USD is stamped at CAPTURE time from the rates
// then in force (ADR-0011), and this read is not that path — a price invented
// here would be a second, unstamped answer to what the run cost.
func liftUsage(u *contracts.CapturedUsage) *gen.TurnUsage {
	if u == nil {
		return nil
	}
	out := &gen.TurnUsage{
		InputTokens:         u.InputTokens,
		OutputTokens:        u.OutputTokens,
		CacheReadTokens:     u.CacheReadTokens,
		CacheCreationTokens: u.CacheCreationTokens,
		Model:               u.Model,
	}
	for _, m := range u.Models {
		out.Models = append(out.Models, gen.Usage{
			InputTokens:         m.InputTokens,
			OutputTokens:        m.OutputTokens,
			CacheReadTokens:     m.CacheReadTokens,
			CacheCreationTokens: m.CacheCreationTokens,
			Model:               m.Model,
		})
	}
	return out
}

// parseEventTime reads the runner's RFC3339Nano stamp. The ZERO time is the
// honest answer for a line that carried none: it is what the reader's cursor
// skips (lastRunEventMillis ignores it), so an untimestamped event can never
// advance a cursor past output nobody has seen.
func parseEventTime(ts string) time.Time {
	if ts == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// capText trims producer prose to the contract's maxLength, counting RUNES
// because that is what JSON Schema counts — a byte cut would also risk slicing a
// multi-byte character in half. The ellipsis says the text was cut rather than
// ending mid-word for no visible reason.
func capText(s string, max int) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
