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
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
)

// liftAll runs a whole NDJSON body through one lifter, the way a page of pod
// output reaches it.
func liftAll(t *testing.T, body string) []gen.RunEvent {
	t.Helper()
	l := newLifter()
	var out []gen.RunEvent
	for _, raw := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		out = append(out, l.line(raw, "")...)
	}
	return out
}

// one lifts a single line and insists it produced exactly one event.
func one(t *testing.T, raw string) gen.RunEvent {
	t.Helper()
	got := newLifter().line(raw, "")
	if len(got) != 1 {
		t.Fatalf("lift(%s) produced %d events, want 1", raw, len(got))
	}
	return got[0]
}

// TestLiftV1KindsOntoTheirV2Counterparts walks every kind a v1 runner could
// emit. The mapping is the compatibility contract for the whole cutover window,
// so a kind that quietly changed shape here would change what a console renders
// for every cycle dispatched before the new image rolled out.
func TestLiftV1KindsOntoTheirV2Counterparts(t *testing.T) {
	t.Parallel()

	// A phase is a coarse state that replaces the previous one — an agent_progress
	// phrase — and the phase NAME survives as that phrase, because it is the
	// stable id a console maps to a friendly label.
	ph := one(t, `{"schemaVersion":1,"seq":2,"kind":"phase","phase":"workspace_ready"}`)
	if ph.Kind != gen.RunEventKindAgentProgress || ph.Phrase != "workspace_ready" || ph.AgentID != leadAgentID {
		t.Errorf("phase = %+v, want agent_progress(phrase=workspace_ready) on the lead", ph)
	}
	// …except the SDK session's own start, which is the run starting.
	st := one(t, `{"schemaVersion":1,"seq":7,"kind":"phase","phase":"agent_started"}`)
	if st.Kind != gen.RunEventKindRunStarted {
		t.Errorf("phase agent_started = %q, want run_started", st.Kind)
	}

	act := one(t, `{"schemaVersion":1,"seq":9,"kind":"activity","summary":"Writing service.bal","toolCount":4}`)
	if act.Kind != gen.RunEventKindAgentProgress || act.Phrase != "Writing service.bal" {
		t.Errorf("activity = %+v, want agent_progress(phrase)", act)
	}
	// The running toolCount is deliberately dropped: the contract scopes it to
	// agent_settled, where the runtime's authoritative total arrives.
	if act.ToolCount != 0 {
		t.Errorf("activity carried a running toolCount (%d) onto agent_progress", act.ToolCount)
	}

	tu := one(t, `{"schemaVersion":1,"seq":11,"kind":"tool_use","tool":"Read","summary":"read api.go","toolUseId":"t1"}`)
	if tu.Kind != gen.RunEventKindToolUse || tu.Tool != "Read" || tu.Summary != "read api.go" || tu.ToolUseID != "t1" {
		t.Errorf("tool_use = %+v", tu)
	}

	tr := one(t, `{"schemaVersion":1,"seq":12,"kind":"tool_result","tool":"Bash","ok":false,"exitCode":2,"durationMs":900,"toolUseId":"t1","status":"failed"}`)
	if tr.Kind != gen.RunEventKindToolResult || tr.Ok == nil || *tr.Ok || tr.ExitCode == nil || *tr.ExitCode != 2 || tr.DurationMs != 900 {
		t.Errorf("tool_result = %+v, want an explicit false ok and the exit code", tr)
	}
	// The SDK's verdict word belongs to a SETTLED AGENT, not to one tool call —
	// the contract scopes `status` to agent_settled / task_settled.
	if tr.Status != "" {
		t.Errorf("ordinary tool_result carried status %q", tr.Status)
	}

	gc := one(t, `{"schemaVersion":1,"seq":13,"kind":"git_commit","sha":"abc123","files":4,"summary":"feat: api","toolUseId":"t2"}`)
	if gc.Kind != gen.RunEventKindGitCommit || gc.Sha != "abc123" || gc.Files != 4 || gc.ToolUseID != "t2" {
		t.Errorf("git_commit = %+v", gc)
	}
	gp := one(t, `{"schemaVersion":1,"seq":14,"kind":"git_push","sha":"abc123","branch":"aep/m4"}`)
	if gp.Kind != gen.RunEventKindGitPush || gp.Branch != "aep/m4" {
		t.Errorf("git_push = %+v", gp)
	}
	gh := one(t, `{"schemaVersion":1,"seq":15,"kind":"gh_action","command":"gh pr create","summary":"opened #7"}`)
	if gh.Kind != gen.RunEventKindGhAction || gh.Command != "gh pr create" {
		t.Errorf("gh_action = %+v", gh)
	}

	// A progress_item is a validation criterion — v1 had no other kind of item,
	// so `source` is a fact and not a guess.
	pi := one(t, `{"schemaVersion":1,"seq":16,"kind":"progress_item","itemId":"AC-003-a","status":"healing"}`)
	if pi.Kind != gen.RunEventKindWorkItem || pi.Source != gen.RunEventSourceCriterion ||
		pi.ItemID != "AC-003-a" || pi.ItemStatus != gen.RunEventItemStatusHealing {
		t.Errorf("progress_item = %+v", pi)
	}

	lg := one(t, `{"schemaVersion":1,"seq":17,"kind":"log","level":"warn","summary":"[watchdog] waiting on Bash"}`)
	if lg.Kind != gen.RunEventKindNotice || lg.Level != gen.RunEventLevelWarn || lg.Detail != "[watchdog] waiting on Bash" {
		t.Errorf("log = %+v, want a warn notice", lg)
	}
	// Prose carries no `code`: the enum is a closed set a consumer branches on,
	// and claiming a condition the producer never asserted is worse than silence.
	if lg.Code != "" {
		t.Errorf("log notice invented code %q", lg.Code)
	}

	rs := one(t, `{"schemaVersion":1,"seq":18,"kind":"result","status":"failure","error":"agent exited 1"}`)
	if rs.Kind != gen.RunEventKindRunSettled || rs.Outcome != gen.RunEventOutcomeFailure || rs.Error != "agent exited 1" {
		t.Errorf("result = %+v", rs)
	}

	// Every lifted event carries the envelope version, so a stored feed stays
	// readable years later.
	for _, ev := range []gen.RunEvent{ph, st, act, tu, tr, gc, gp, gh, pi, lg, rs} {
		if ev.V != gen.RunEventV2 {
			t.Errorf("%s event has v=%d, want 2", ev.Kind, ev.V)
		}
	}
}

// TestLiftWrapsWhatItCannotParse pins that nothing is dropped. Bootstrap stdout
// and stray library writes share the pod's fd with the envelopes, and a reader
// that dropped them would show a feed that goes silent for the part of a run
// people most want to see.
func TestLiftWrapsWhatItCannotParse(t *testing.T) {
	t.Parallel()

	boot := one(t, "[oneshot] materialised 3 skill(s)")
	if boot.Kind != gen.RunEventKindNotice || boot.Detail != "[oneshot] materialised 3 skill(s)" {
		t.Errorf("bootstrap stdout = %+v, want it wrapped as a notice", boot)
	}
	if boot.AgentID != leadAgentID {
		t.Errorf("wrapped line agentId = %q, want the lead", boot.AgentID)
	}
	// JSON that is not an envelope this build knows: kept verbatim, not guessed at.
	weird := one(t, `{"hello":"world"}`)
	if weird.Kind != gen.RunEventKindNotice || weird.Detail != `{"hello":"world"}` {
		t.Errorf("unknown json = %+v", weird)
	}
}

// TestParseRunEventTakesNativeV2 covers the other producer: once the runner
// emits v2 there is nothing to translate, and a line it cannot vouch for must
// not be forwarded as if it could.
func TestParseRunEventTakesNativeV2(t *testing.T) {
	t.Parallel()

	native := one(t, `{"v":2,"seq":5,"ts":"2026-09-04T09:25:39.580Z","kind":"heartbeat","agentId":"a1","waitingOn":"tool","ref":"t9","elapsedMs":120000}`)
	if native.Kind != gen.RunEventKindHeartbeat || native.AgentID != "a1" ||
		native.WaitingOn != gen.RunEventWaitingOnTool || native.ElapsedMs != 120000 {
		t.Errorf("native v2 = %+v, want it passed through whole", native)
	}
	// The producer's seq is dropped: the RECORDER numbers the feed, because a
	// producer's numbering cannot also number the seq-less lines interleaved with
	// it (see recordingSession.number). The number is not lost — the recorder
	// reads it off the raw line for dedupe and gap detection.
	if native.Seq != 0 {
		t.Errorf("native v2 seq = %d, want 0 — the lift does not number, the recorder does", native.Seq)
	}

	// A v2 envelope naming a kind this build cannot render is wrapped, not
	// forwarded: `kind` is what every consumer switches on, so passing an
	// unrenderable one through puts a blank row on a user's feed.
	future := one(t, `{"v":2,"seq":6,"kind":"telepathy","agentId":"a1"}`)
	if future.Kind != gen.RunEventKindNotice {
		t.Errorf("unknown v2 kind = %q, want it wrapped as a notice", future.Kind)
	}

	// A v2 line with no agentId still has to satisfy the contract's required
	// field; the lead is the only honest default.
	anon := one(t, `{"v":2,"seq":7,"kind":"tool_use","agentId":"","tool":"Read"}`)
	if anon.AgentID != leadAgentID {
		t.Errorf("v2 line with no agentId = %q, want the lead", anon.AgentID)
	}
}

// TestLiftAnnouncesEachAgentOnceAsInferred is the heart of the lift: v1 stamped
// lines with an emitterId and never said an agent had STARTED, so the reader has
// to deduce the agent from the first line that mentions it — and say that it
// deduced it.
func TestLiftAnnouncesEachAgentOnceAsInferred(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`{"schemaVersion":1,"seq":1,"kind":"phase","phase":"workspace_ready"}`,
		`{"schemaVersion":1,"seq":2,"kind":"activity","summary":"Build the SPA","emitter":"subagent","emitterId":"toolu_web","emitterLabel":"Build the SPA"}`,
		`{"schemaVersion":1,"seq":3,"kind":"tool_use","tool":"Bash","summary":"npm install","emitter":"subagent","emitterId":"toolu_web","emitterLabel":"Build the SPA"}`,
		`{"schemaVersion":1,"seq":4,"kind":"tool_use","tool":"Read","summary":"read api.bal","emitter":"subagent","emitterId":"toolu_api","emitterLabel":"Implement the API"}`,
	}, "\n")
	got := liftAll(t, body)

	var started []gen.RunEvent
	for _, ev := range got {
		if ev.Kind == gen.RunEventKindAgentStarted {
			started = append(started, ev)
		}
	}
	if len(started) != 2 {
		t.Fatalf("agent_started events = %d, want one per agent\n%+v", len(started), got)
	}
	for _, ev := range started {
		if ev.Role != roleInferred {
			t.Errorf("agent_started %s role = %q, want %q", ev.AgentID, ev.Role, roleInferred)
		}
		if ev.Depth != 1 {
			t.Errorf("agent_started %s depth = %d, want 1 (a v1 feed can only describe the lead's own children)", ev.AgentID, ev.Depth)
		}
	}
	if started[0].AgentID != "toolu_web" || started[0].Label != "Build the SPA" {
		t.Errorf("first announcement = %+v", started[0])
	}
	if started[1].AgentID != "toolu_api" || started[1].Label != "Implement the API" {
		t.Errorf("second announcement = %+v", started[1])
	}

	// The announcement comes BEFORE the line that revealed the agent. ORDER is
	// the whole guarantee the lift offers: it hands back a slice, the recorder
	// numbers that slice in place, so a client deduping on (attempt, seq) gets
	// two distinct rows without the lift ever inventing a number.
	if got[1].Kind != gen.RunEventKindAgentStarted || got[2].AgentID != "toolu_web" {
		t.Fatalf("announcement is not immediately before its line: %+v", got)
	}
	for i, ev := range got {
		if ev.Seq != 0 {
			t.Errorf("lifted event %d carries seq %d; the lift must leave numbering to the recorder", i, ev.Seq)
		}
	}
	// Ordinary work events carry the agent but NOT the tree fields — the contract
	// scopes label/depth to the three agent-lifecycle kinds.
	work := got[3]
	if work.Kind != gen.RunEventKindToolUse || work.AgentID != "toolu_web" || work.Label != "" || work.Depth != 0 {
		t.Errorf("subagent tool_use = %+v, want the agent id alone", work)
	}
}

// TestLiftFanOutResultBecomesAgentSettled pins the other inference: v1's
// end-of-subagent signal is a tool_result that answers the fan-out call itself,
// and it carries figures nothing else in the feed can reconstruct.
func TestLiftFanOutResultBecomesAgentSettled(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		// An ordinary result from INSIDE the subagent: same emitterId, a different
		// call. It must stay a tool result.
		`{"schemaVersion":1,"seq":1,"kind":"tool_result","tool":"Bash","ok":true,"toolUseId":"toolu_b1","emitter":"subagent","emitterId":"toolu_web","emitterLabel":"Build the SPA"}`,
		// The fan-out call's own result: toolUseId IS the emitterId.
		`{"schemaVersion":1,"seq":2,"kind":"tool_result","tool":"Agent","ok":true,"status":"completed","toolUseId":"toolu_web","durationMs":2471364,"toolCount":162,"linesAdded":2069,"linesRemoved":78,"emitter":"subagent","emitterId":"toolu_web","emitterLabel":"Build the SPA"}`,
	}, "\n")
	got := liftAll(t, body)

	if got[1].Kind != gen.RunEventKindToolResult {
		t.Errorf("a subagent's own tool result = %q, want it left a tool_result", got[1].Kind)
	}
	settled := got[len(got)-1]
	if settled.Kind != gen.RunEventKindAgentSettled {
		t.Fatalf("fan-out result = %q, want agent_settled\n%+v", settled.Kind, got)
	}
	if settled.AgentID != "toolu_web" || settled.Label != "Build the SPA" || settled.Depth != 1 {
		t.Errorf("agent_settled = %+v, want it attributed to the agent it ends", settled)
	}
	if settled.Status != gen.AgentStatusCompleted || settled.DurationMs != 2471364 ||
		settled.ToolCount != 162 || settled.LinesAdded != 2069 || settled.LinesRemoved != 78 {
		t.Errorf("agent_settled lost the runtime's totals: %+v", settled)
	}
}

// TestAgentStatusFallsBackToTheOkFlag covers a settle whose runner reported no
// verdict word — older runners did not always send one.
func TestAgentStatusFallsBackToTheOkFlag(t *testing.T) {
	t.Parallel()

	got := newLifter().line(`{"schemaVersion":1,"seq":1,"kind":"tool_result","tool":"Task","ok":false,"toolUseId":"a1","emitter":"subagent","emitterId":"a1"}`, "")
	settle := got[len(got)-1]
	if settle.Status != gen.AgentStatusFailed {
		t.Errorf("settle with ok=false and no word = %q, want failed", settle.Status)
	}
	got = newLifter().line(`{"schemaVersion":1,"seq":1,"kind":"tool_result","tool":"Task","ok":true,"toolUseId":"a1","emitter":"subagent","emitterId":"a1"}`, "")
	if s := got[len(got)-1].Status; s != gen.AgentStatusCompleted {
		t.Errorf("settle with ok=true and no word = %q, want completed", s)
	}
}

// TestLiftCapsProducerProse pins the contract's maxLength. The offending text is
// always somebody else's (an agent's summary, a kubelet message), and emitting an
// over-long field would put the platform in breach of its own published schema.
func TestLiftCapsProducerProse(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 900)
	tu := one(t, `{"schemaVersion":1,"seq":1,"kind":"tool_use","tool":"Bash","summary":"`+long+`"}`)
	if len([]rune(tu.Summary)) != capShortText {
		t.Errorf("summary length = %d, want the contract's %d", len([]rune(tu.Summary)), capShortText)
	}
	lg := one(t, `{"schemaVersion":1,"seq":2,"kind":"log","summary":"`+long+`"}`)
	if len([]rune(lg.Detail)) != capLongText {
		t.Errorf("detail length = %d, want the contract's %d", len([]rune(lg.Detail)), capLongText)
	}
	// A cut is visible, not silent.
	if !strings.HasSuffix(tu.Summary, "…") {
		t.Errorf("a truncated summary does not say so: %q", tu.Summary[len(tu.Summary)-8:])
	}
	// Runes, not bytes: a byte cut would slice a multi-byte character in half.
	multi := one(t, `{"schemaVersion":1,"seq":3,"kind":"tool_use","tool":"Bash","summary":"`+strings.Repeat("é", 900)+`"}`)
	if len([]rune(multi.Summary)) != capShortText {
		t.Errorf("multi-byte summary length = %d runes, want %d", len([]rune(multi.Summary)), capShortText)
	}
}

// TestLiftUsageRidesRunSettledUnpriced pins that the run's token usage reaches
// the feed and that the feed does not invent a price for it: USD is stamped at
// CAPTURE time from the rates then in force, and a second unstamped answer to
// "what did this run cost" is exactly what ADR-0011 forbids.
func TestLiftUsageRidesRunSettledUnpriced(t *testing.T) {
	t.Parallel()

	ev := one(t, `{"schemaVersion":1,"seq":1,"kind":"result","status":"success","usage":{"inputTokens":36,"outputTokens":8020,"cacheReadTokens":1285876,"cacheCreationTokens":72210,"model":"claude-sonnet-5"}}`)
	if ev.Usage == nil {
		t.Fatalf("run_settled carries no usage: %+v", ev)
	}
	if ev.Usage.InputTokens != 36 || ev.Usage.OutputTokens != 8020 ||
		ev.Usage.CacheReadTokens != 1285876 || ev.Usage.CacheCreationTokens != 72210 ||
		ev.Usage.Model != "claude-sonnet-5" {
		t.Errorf("usage = %+v", *ev.Usage)
	}
	if ev.Usage.CostUsd != nil {
		t.Errorf("the feed priced the run itself (costUsd=%v) — pricing is the capture path's", *ev.Usage.CostUsd)
	}
}

// TestUsageCannotRideTheV1WireShape is the structural half of the same rule.
// The usage used to sit ON contracts.ProgressEvent, where it serialised as an
// undocumented `usage` field on a shape three surfaces put on the wire — kept
// harmless only by the fact that nothing but the accounting parse ever filled it
// in. It now rides one level out, on the reader's own runnerLine, so the leak is
// impossible rather than merely unexercised.
func TestUsageCannotRideTheV1WireShape(t *testing.T) {
	t.Parallel()

	const raw = `{"schemaVersion":1,"seq":1,"kind":"result","status":"success","usage":{"inputTokens":36,"outputTokens":8020}}`
	ln := parseProgressLine(raw)
	if ln.Usage == nil || ln.Usage.OutputTokens != 8020 {
		t.Fatalf("the reader stopped seeing the usage: %+v", ln.Usage)
	}
	// …and the console-facing half of that same line carries none of it.
	b, err := json.Marshal(ln.ProgressEvent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "usage") {
		t.Errorf("contracts.ProgressEvent serialises a usage field: %s", b)
	}
}

// realRunRecording is a REAL 55-minute coding run, recorded 2026-09-04 by the
// runner image that still spoke v1.
//
// It is COPIED here rather than referenced across the repo at the runner's own
// copy, because the v1 producer no longer exists: the runner emits v2 and has
// deleted its v1 schema. Nothing over there will keep a v1 recording alive, and
// this reader has to go on lifting v1 for as long as a pre-cutover cycle can be
// replayed — so the historical shape belongs beside the historical reader. It is
// a frozen artifact, not a fixture anyone edits.
const realRunRecording = "testdata/run-2026-09-04-v1.ndjson"

// TestLiftARealV1Recording is the proof the lift works on output nobody wrote
// for it: 759 lines of a run that fanned out to two subagents, each settled by
// the SDK's own report. Synthetic fixtures agree with whatever the lift does;
// this one does not.
func TestLiftARealV1Recording(t *testing.T) {
	t.Parallel()

	f, err := os.Open(realRunRecording)
	if err != nil {
		t.Fatalf("open the real recording: %v", err)
	}
	defer func() { _ = f.Close() }()

	l := newLifter()
	var got []gen.RunEvent
	lines := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		lines++
		got = append(got, l.line(scanner.Text(), "")...)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read the real recording: %v", err)
	}
	if lines != 759 {
		t.Fatalf("recording has %d lines, want the 759 this test is written against", lines)
	}

	byKind := map[gen.RunEventKind]int{}
	for _, ev := range got {
		byKind[ev.Kind]++
		if ev.V != gen.RunEventV2 || ev.AgentID == "" || !ev.Kind.Valid() {
			t.Fatalf("lifted event breaks the contract's required fields: %+v", ev)
		}
	}

	// Two subagents ran, and BOTH are inferred — the v1 runner announced neither.
	var started, settled []gen.RunEvent
	for _, ev := range got {
		switch ev.Kind {
		case gen.RunEventKindAgentStarted:
			started = append(started, ev)
		case gen.RunEventKindAgentSettled:
			settled = append(settled, ev)
		}
	}
	if len(started) != 2 {
		t.Fatalf("agent_started = %d, want 2 (the run fanned out twice)", len(started))
	}
	for _, ev := range started {
		if ev.Role != roleInferred {
			t.Errorf("agent %s announced with role %q, want %q", ev.AgentID, ev.Role, roleInferred)
		}
	}
	if started[0].AgentID != "toolu_01H8anDGmAxeWNekRqPU4w8n" || started[0].Label != "Build onboarding-webapp React SPA" {
		t.Errorf("first agent = %+v", started[0])
	}
	if started[1].AgentID != "toolu_01FiKFBATWHmkQRRFCmVEmvN" || started[1].Label != "Walk onboarding-webapp in mock mode" {
		t.Errorf("second agent = %+v", started[1])
	}

	// Both settled, with the SDK's OWN totals — the only measure of a spawned
	// agent's work that exists, since its per-edit line counts never reach the feed.
	if len(settled) != 2 {
		t.Fatalf("agent_settled = %d, want 2", len(settled))
	}
	want := []gen.RunEvent{
		{AgentID: "toolu_01H8anDGmAxeWNekRqPU4w8n", Status: gen.AgentStatusCompleted, DurationMs: 2471364, ToolCount: 162, LinesAdded: 2069, LinesRemoved: 78},
		{AgentID: "toolu_01FiKFBATWHmkQRRFCmVEmvN", Status: gen.AgentStatusCompleted, DurationMs: 470175, ToolCount: 74, LinesAdded: 69, LinesRemoved: 37},
	}
	for i, w := range want {
		g := settled[i]
		if g.AgentID != w.AgentID || g.Status != w.Status || g.DurationMs != w.DurationMs ||
			g.ToolCount != w.ToolCount || g.LinesAdded != w.LinesAdded || g.LinesRemoved != w.LinesRemoved {
			t.Errorf("agent_settled[%d] = %+v\nwant agent=%s status=%s durationMs=%d toolCount=%d +%d/-%d",
				i, g, w.AgentID, w.Status, w.DurationMs, w.ToolCount, w.LinesAdded, w.LinesRemoved)
		}
	}

	// The whole recording, kind by kind: 759 lines in, 761 events out — the two
	// extra are the announcements, and nothing was dropped on the way.
	if len(got) != lines+2 {
		t.Errorf("events = %d, want %d (759 lines + 2 inferred agent_started)", len(got), lines+2)
	}
	wantKinds := map[gen.RunEventKind]int{
		gen.RunEventKindToolUse:       255,
		gen.RunEventKindToolResult:    255, // 257 results, less the 2 fan-out settles
		gen.RunEventKindAgentProgress: 238, // 236 activities + 2 non-session phases
		gen.RunEventKindNotice:        7,   // the log lines
		gen.RunEventKindRunStarted:    1,   // phase agent_started
		gen.RunEventKindRunSettled:    1,
		gen.RunEventKindAgentStarted:  2,
		gen.RunEventKindAgentSettled:  2,
	}
	for kind, n := range wantKinds {
		if byKind[kind] != n {
			t.Errorf("%s events = %d, want %d", kind, byKind[kind], n)
		}
	}
	for kind, n := range byKind {
		if wantKinds[kind] == 0 {
			t.Errorf("unexpected kind %s (%d) — the recording holds none of those", kind, n)
		}
	}

	// The run's terminal line carries the token usage, and it survives the lift.
	last := got[len(got)-1]
	if last.Kind != gen.RunEventKindRunSettled || last.Outcome != gen.RunEventOutcomeSuccess {
		t.Fatalf("last event = %+v, want a successful run_settled", last)
	}
	if last.Usage == nil || last.Usage.OutputTokens != 8020 || last.Usage.CacheReadTokens != 1285876 {
		t.Errorf("run_settled usage = %+v, want the recording's own figures", last.Usage)
	}

	// The lift numbers nothing, announcements included. What makes (attempt, seq)
	// a usable dedup key is the recorder stamping this slice in order, which
	// TestRecorder_EveryLineGetsItsOwnSeq covers end to end.
	for i, ev := range got {
		if ev.Seq != 0 {
			t.Fatalf("lifted event %d (%s) carries seq %d; the lift must leave numbering to the recorder",
				i, ev.Kind, ev.Seq)
		}
	}
}
