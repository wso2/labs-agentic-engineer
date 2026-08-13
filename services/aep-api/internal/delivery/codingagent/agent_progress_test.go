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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// TestParseProgressLine covers the runner NDJSON → event decode and the
// non-envelope fallback to a `log` event.
func TestParseProgressLine(t *testing.T) {
	t.Parallel()

	phase := parseProgressLine(`{"schemaVersion":1,"ts":"2026-07-08T10:14:17.210Z","seq":2,"kind":"phase","phase":"workspace_ready"}`)
	if phase.Kind != "phase" || phase.Phase != "workspace_ready" || phase.Seq != 2 {
		t.Errorf("phase envelope = %+v", phase)
	}

	tool := parseProgressLine(`{"schemaVersion":1,"seq":15,"kind":"tool_use","tool":"Read","summary":"reading design"}`)
	if tool.Kind != "tool_use" || tool.Tool != "Read" || tool.Summary != "reading design" {
		t.Errorf("tool_use envelope = %+v", tool)
	}

	// Bootstrap / library stdout is not JSON → wrapped as a log event verbatim.
	boot := parseProgressLine("[oneshot] materialised 3 skill(s); preload=1 org skill(s)")
	if boot.Kind != "log" || boot.Summary != "[oneshot] materialised 3 skill(s); preload=1 org skill(s)" {
		t.Errorf("bootstrap line = %+v, want kind=log with raw summary", boot)
	}

	// JSON that isn't a recognised envelope (no kind) → log fallback.
	weird := parseProgressLine(`{"hello":"world"}`)
	if weird.Kind != "log" {
		t.Errorf("unrecognised json = %+v, want kind=log", weird)
	}

	// Wrong schema version → log fallback (forward-compat guard).
	future := parseProgressLine(`{"schemaVersion":2,"kind":"phase","phase":"x"}`)
	if future.Kind != "log" {
		t.Errorf("future schema = %+v, want kind=log", future)
	}
}

// TestParseProgressLineAttribution pins the fields that let a reader tell one
// subagent's work from another's, and a failed tool call from a successful one.
// A cycle fans out to several subagents at once and their lines interleave, so
// dropping any of these silently collapses the feed back to unattributable.
func TestParseProgressLineAttribution(t *testing.T) {
	t.Parallel()

	sub := parseProgressLine(`{"schemaVersion":1,"seq":9,"kind":"tool_use","tool":"Bash","summary":"bal build",` +
		`"emitter":"subagent","emitterId":"toolu_api","emitterLabel":"Implement todo-api service (issue #3)",` +
		`"toolUseId":"toolu_b1"}`)
	if sub.Emitter != "subagent" || sub.EmitterID != "toolu_api" {
		t.Errorf("subagent attribution = %+v", sub)
	}
	if sub.EmitterLabel != "Implement todo-api service (issue #3)" || sub.ToolUseID != "toolu_b1" {
		t.Errorf("subagent identity = %+v", sub)
	}

	// A FAILED call. `ok` is a pointer precisely so this survives: a plain bool
	// with omitempty would drop `false` on the way out and the console would
	// render the failure as an ordinary success.
	failed := parseProgressLine(`{"schemaVersion":1,"seq":10,"kind":"tool_result","tool":"Bash","ok":false,` +
		`"durationMs":172000,"summary":"exit 1: compilation failed","toolUseId":"toolu_b1"}`)
	if failed.Kind != "tool_result" || failed.OK == nil || *failed.OK {
		t.Fatalf("failed tool_result = %+v, want ok=false", failed)
	}
	if failed.DurationMs != 172000 {
		t.Errorf("durationMs = %d, want 172000", failed.DurationMs)
	}

	// …and it must still be false after a round trip to the wire.
	encoded, err := json.Marshal(failed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"ok":false`) {
		t.Errorf("re-encoded line = %s, want it to still carry ok:false", encoded)
	}

	// Every other kind carries no `ok` at all, so absence must never read as
	// success.
	if phase := parseProgressLine(`{"schemaVersion":1,"kind":"phase","phase":"x"}`); phase.OK != nil {
		t.Errorf("phase event carries ok = %v, want nil", *phase.OK)
	}
}

// TestParseProgressLineFailureDetail pins the per-step failure signal and the
// per-subagent totals. Both are figures the runner is the only source of — the
// exit code because it parses the SDK's own line, the totals because the SDK
// reports them and nothing downstream can reconstruct them.
func TestParseProgressLineFailureDetail(t *testing.T) {
	t.Parallel()

	shell := parseProgressLine(`{"schemaVersion":1,"seq":11,"kind":"tool_result","tool":"Bash","ok":false,` +
		`"exitCode":2,"summary":"ls: cannot access 'todo-api/'","toolUseId":"toolu_b1"}`)
	if shell.ExitCode == nil || *shell.ExitCode != 2 {
		t.Fatalf("exitCode = %v, want 2", shell.ExitCode)
	}

	// A non-shell tool reports no code. Nil has to stay nil on the way out: a
	// zero would read as "exited 0", i.e. as a success on a failed call.
	other := parseProgressLine(`{"schemaVersion":1,"seq":12,"kind":"tool_result","tool":"Read","ok":false,` +
		`"summary":"File does not exist"}`)
	if other.ExitCode != nil {
		t.Errorf("exitCode = %d on a tool that reports none, want nil", *other.ExitCode)
	}
	encoded, err := json.Marshal(other)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "exitCode") {
		t.Errorf("re-encoded line = %s, want no exitCode field at all", encoded)
	}

	// A fan-out call's result: the SDK's own totals for a whole subagent.
	settle := parseProgressLine(`{"schemaVersion":1,"seq":13,"kind":"tool_result","tool":"Agent","ok":true,` +
		`"status":"completed","summary":"todo-api","durationMs":209158,"toolCount":19,"linesAdded":553,` +
		`"linesRemoved":4,"toolUseId":"toolu_spawn","emitter":"subagent","emitterId":"toolu_spawn"}`)
	if settle.ToolCount != 19 || settle.LinesAdded != 553 || settle.LinesRemoved != 4 {
		t.Errorf("subagent totals = %+v, want 19 tools +553/-4", settle)
	}
	if settle.Status != "completed" || settle.DurationMs != 209158 {
		t.Errorf("subagent verdict = %q after %dms, want completed after 209158", settle.Status, settle.DurationMs)
	}

	// A subagent's narration is its own kind, so a reader can drop it from the
	// rows without having to infer intent from an empty tool name.
	if act := parseProgressLine(`{"schemaVersion":1,"seq":14,"kind":"activity","summary":"Writing service.bal","toolCount":12}`); act.Kind != "activity" || act.ToolCount != 12 {
		t.Errorf("activity = %+v, want kind=activity toolCount=12", act)
	}
}

// TestTextToProgressEvents pins the K8s timestamp-prefix split, envelope-ts
// preference, and the newest-window cap.
func TestTextToProgressEvents(t *testing.T) {
	t.Parallel()

	// K8s `timestamps=true` prefix + envelope carrying its own ts → envelope ts wins.
	withEnvTs := "2026-07-08T10:14:17.300000000Z " +
		`{"schemaVersion":1,"ts":"2026-07-08T10:14:17.210Z","seq":2,"kind":"phase","phase":"workspace_ready"}`
	// K8s prefix + a bootstrap (non-JSON) line → prefix used as the event ts.
	bootWithPrefix := "2026-07-08T10:14:18.000000000Z [oneshot] materialised 3 skill(s)"

	events, _ := textToProgressEvents(withEnvTs + "\n" + bootWithPrefix + "\n")
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}
	if events[0].Kind != "phase" || events[0].Ts != "2026-07-08T10:14:17.210Z" {
		t.Errorf("event0 = %+v, want phase with envelope ts", events[0])
	}
	if events[1].Kind != "log" || events[1].Ts != "2026-07-08T10:14:18.000000000Z" {
		t.Errorf("event1 = %+v, want log with k8s-prefix ts", events[1])
	}
	for _, e := range events {
		if e.SchemaVersion != progressSchemaVersion {
			t.Errorf("schemaVersion not stamped: %+v", e)
		}
	}

	if got, _ := textToProgressEvents(""); len(got) != 0 {
		t.Errorf("empty text → %d events, want 0", len(got))
	}

	// A pod log read that ends mid-envelope must not surface the fragment. The
	// complete line before it still does.
	cut := `{"schemaVersion":1,"seq":8,"kind":"phase","phase":"coding"}` + "\n" +
		`{"schemaVersion":1,"ts":"2026-`
	got, _ := textToProgressEvents(cut)
	if len(got) != 1 {
		t.Fatalf("truncated tail → %d events, want 1: %+v", len(got), got)
	}
	if got[0].Kind != "phase" {
		t.Errorf("kept the wrong event: %+v", got[0])
	}

	// Over-cap input keeps the NEWEST window (live-tail freshness).
	var b strings.Builder
	for i := 0; i < defaultProgressLimit+50; i++ {
		b.WriteString(`{"schemaVersion":1,"kind":"log","summary":"line"}` + "\n")
	}
	capped, cappedTruncated := textToProgressEvents(b.String())
	if len(capped) != defaultProgressLimit {
		t.Errorf("capped len = %d, want %d", len(capped), defaultProgressLimit)
	}
	if !cappedTruncated {
		t.Error("an over-cap page must report truncated")
	}
	// …and it must SAY so, because nothing consumes the truncated flag.
	if capped[0].Seq != seqHeadDropped || !strings.Contains(capped[0].Summary, "earlier line(s) omitted") {
		t.Errorf("first event must be the omission marker, got %+v", capped[0])
	}
	if capped[0].Ts != "" {
		t.Errorf("the marker must stay untimestamped so it cannot advance the cursor, got %q", capped[0].Ts)
	}
	if last := capped[len(capped)-1]; last.Seq == seqHeadDropped {
		t.Error("the marker belongs at the head of the window, not the tail")
	}
}

// TestFilterEventsAfter pins the half-open (sinceMillis, +∞) cursor filter and
// the keep-untimestamped rule.
func TestFilterEventsAfter(t *testing.T) {
	t.Parallel()

	// ts = 2026-07-08T10:14:17.210Z → 1783505657210 ms.
	const tsA = "2026-07-08T10:14:17.210Z" // older
	const tsB = "2026-07-08T10:14:18.500Z" // newer
	msA := int64(1783505657210)

	events, _ := textToProgressEvents(
		`{"schemaVersion":1,"ts":"` + tsA + `","kind":"phase","phase":"a"}` + "\n" +
			`{"schemaVersion":1,"ts":"` + tsB + `","kind":"phase","phase":"b"}` + "\n" +
			`{"schemaVersion":1,"kind":"log","summary":"no-ts"}` + "\n",
	)

	// sinceMillis at tsA drops A (== boundary), keeps B (newer) and the no-ts line.
	got := filterEventsAfter(events, msA)
	var phases []string
	for _, e := range got {
		if e.Kind == "phase" {
			phases = append(phases, e.Phase)
		}
	}
	if len(phases) != 1 || phases[0] != "b" {
		t.Errorf("phases after filter = %v, want [b] (a dropped at boundary)", phases)
	}
	// The untimestamped line is always kept.
	kept := false
	for _, e := range got {
		if e.Summary == "no-ts" {
			kept = true
		}
	}
	if !kept {
		t.Error("untimestamped event must be kept")
	}

	// sinceMillis<=0 → no filtering.
	if got := filterEventsAfter(events, 0); len(got) != len(events) {
		t.Errorf("sinceMillis=0 filtered %d→%d, want no-op", len(events), len(got))
	}
}

// TestLastEventMillis returns the max parseable ts, ignoring untimestamped lines.
func TestLastEventMillis(t *testing.T) {
	t.Parallel()
	events, _ := textToProgressEvents(
		`{"schemaVersion":1,"ts":"2026-07-08T10:14:17.210Z","kind":"log","summary":"a"}` + "\n" +
			`{"schemaVersion":1,"ts":"2026-07-08T10:14:18.500Z","kind":"log","summary":"b"}` + "\n" +
			`{"schemaVersion":1,"kind":"log","summary":"no-ts"}` + "\n",
	)
	if got := lastEventMillis(events); got != 1783505658500 {
		t.Errorf("lastEventMillis = %d, want 1783505658500 (tsB)", got)
	}
	if got := lastEventMillis(nil); got != 0 {
		t.Errorf("lastEventMillis(nil) = %d, want 0", got)
	}
}

// TestBootstrapEvent pins the pre-stdout "dark zone" state → synthetic line
// mapping: every state is a kind=phase event with a STABLE negative seq (so the
// console dedups re-polls yet shows each transition) and an empty ts (a
// transient marker, not a wall-clock log line).
func TestBootstrapEvent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		podFound      bool
		phase         string
		waitingReason string
		message       string
		wantSeq       int64
		wantPhase     string
		wantInSummary string
	}{
		{"no pod yet", false, "", "", "", seqBootScheduling, "runner_scheduling", ""},
		{"container creating", true, "Pending", "ContainerCreating", "", seqBootPulling, "runner_pulling_image", ""},
		{"pod initializing", true, "Pending", "PodInitializing", "", seqBootPulling, "runner_pulling_image", ""},
		{"image pull backoff", true, "Pending", "ImagePullBackOff", "", seqBootBackoff, "runner_image_pull_backoff", ""},
		{"err image pull", true, "Pending", "ErrImagePull", "", seqBootBackoff, "runner_image_pull_backoff", ""},
		{"config error", true, "Pending", "CreateContainerConfigError", "", seqBootConfig, "runner_config_error", ""},
		{"invalid image name", true, "Pending", "InvalidImageName", "", seqBootConfig, "runner_config_error", ""},
		{"unschedulable capacity", true, "Pending", "Unschedulable", "0/5 nodes are available: 5 Too many pods.", seqBootUnschedulable, "runner_unschedulable", "Too many pods"},
		{"scheduler error", true, "Pending", "SchedulerError", "", seqBootUnschedulable, "runner_unschedulable", "No capacity"},
		{"running no output", true, "Running", "", "", seqBootStarting, "runner_starting", ""},
		{"pending no reason", true, "Pending", "", "", seqBootScheduling, "runner_scheduling", ""},
		{"unknown reason", true, "Pending", "SomethingWeird", "", seqBootPulling, "runner_pulling_image", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := bootstrapEvent(tc.podFound, tc.phase, tc.waitingReason, tc.message)
			if ev.Kind != "phase" {
				t.Errorf("kind = %q, want phase", ev.Kind)
			}
			if ev.Seq != tc.wantSeq {
				t.Errorf("seq = %d, want %d", ev.Seq, tc.wantSeq)
			}
			if ev.Phase != tc.wantPhase {
				t.Errorf("phase = %q, want %q", ev.Phase, tc.wantPhase)
			}
			if ev.Ts != "" {
				t.Errorf("ts = %q, want empty (synthetic marker)", ev.Ts)
			}
			if ev.SchemaVersion != progressSchemaVersion {
				t.Errorf("schemaVersion = %d, want %d", ev.SchemaVersion, progressSchemaVersion)
			}
			if ev.Summary == "" {
				t.Error("summary must be a non-empty human fallback")
			}
			if tc.wantInSummary != "" && !strings.Contains(ev.Summary, tc.wantInSummary) {
				t.Errorf("summary = %q, want it to contain %q", ev.Summary, tc.wantInSummary)
			}
		})
	}

	// An unrecognised reason is surfaced verbatim so nothing hides.
	if ev := bootstrapEvent(true, "Pending", "SomethingWeird", ""); !strings.Contains(ev.Summary, "SomethingWeird") {
		t.Errorf("unknown reason summary = %q, want it to contain the raw reason", ev.Summary)
	}

	// Distinct states must carry distinct seqs (else the console dedups two
	// different transitions into one row).
	seqs := map[int64]bool{
		seqBootScheduling: true, seqBootPulling: true, seqBootBackoff: true,
		seqBootConfig: true, seqBootStarting: true, seqBootUnschedulable: true,
	}
	if len(seqs) != 6 {
		t.Errorf("bootstrap seqs collide: %v", seqs)
	}
}

// TestPageEvents caps and flags truncation.
func TestPageEvents(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < defaultProgressLimit+10; i++ {
		b.WriteString(`{"schemaVersion":1,"kind":"log","summary":"x"}` + "\n")
	}
	lines, truncated, _ := pageEvents(b.String(), 0)
	if len(lines) != defaultProgressLimit || !truncated {
		t.Errorf("pageEvents over-cap = (%d, %v), want (%d, true)", len(lines), truncated, defaultProgressLimit)
	}

	lines, truncated, _ = pageEvents(`{"schemaVersion":1,"kind":"phase","phase":"p"}`+"\n", 0)
	if len(lines) != 1 || truncated {
		t.Errorf("pageEvents under-cap = (%d, %v), want (1, false)", len(lines), truncated)
	}
}

// TestPageEventsHadOutput pins the distinction that keeps the dark-zone
// narration out of a live stream: hadOutput is a fact about the POD LOG, not
// about the cursor window. A tail carrying only already-seen lines yields no new
// lines yet still had output, so the caller must not re-narrate "Starting the
// agent…" — the console dedups bootstrap lines on a stable seq, so one such
// mid-stream emission is pinned in place for the rest of the run.
func TestPageEventsHadOutput(t *testing.T) {
	t.Parallel()

	const seen = `{"schemaVersion":1,"kind":"log","summary":"first","ts":"2026-07-28T10:00:00.000000000Z"}` + "\n"

	if _, _, hadOutput := pageEvents("", 0); hadOutput {
		t.Error("empty page: hadOutput = true, want false (the runner has not spoken)")
	}

	// The agent is mid-thought: the tail still holds the line we already emitted.
	cursor := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC).UnixMilli()
	lines, _, hadOutput := pageEvents(seen, cursor)
	if len(lines) != 0 {
		t.Errorf("already-seen page: lines = %d, want 0", len(lines))
	}
	if !hadOutput {
		t.Error("already-seen page: hadOutput = false, want true (the pod HAS produced output)")
	}
}

func liveCycle(id string) *delivery.RunCycle {
	return &delivery.RunCycle{
		ID: id, OrgID: "acme", ProjectID: "shop", RunID: "run-1",
		Kind: delivery.CycleKindCoding, JobRef: "ca-" + id + "-2608061000",
		CreatedAt: time.Now().UTC().Add(-10 * time.Minute),
	}
}

type stubLive struct {
	tail LiveTail
	err  error
}

func (s *stubLive) Tail(context.Context, string, string, string, int) (LiveTail, error) {
	return s.tail, s.err
}

type stubArchive struct {
	text string
	err  error
}

func (s *stubArchive) CycleArchive(context.Context, ArchiveScope) (string, error) {
	return s.text, s.err
}

func TestCycleProgress_LiveTailIsServedWhileTheComponentExists(t *testing.T) {
	live := &stubLive{tail: LiveTail{
		Text: "2026-08-06T10:00:01Z hello\n",
		Pod:  openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"},
	}}

	resp, err := NewAgentProgressReader(live, nil).CycleProgress(context.Background(), liveCycle("c1"), 0)
	if err != nil {
		t.Fatalf("CycleProgress: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Summary != "hello" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
	if resp.Final {
		t.Error("a live tail is never final")
	}
}

func TestCycleProgress_UnscheduledPodNarratesTheDarkZone(t *testing.T) {
	live := &stubLive{tail: LiveTail{Pod: openchoreo.RuntimePod{}}}

	resp, err := NewAgentProgressReader(live, nil).CycleProgress(context.Background(), liveCycle("c2"), 0)
	if err != nil {
		t.Fatalf("CycleProgress: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Phase != "runner_scheduling" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
}

func TestCycleProgress_PendingPodNarratesItsWaitingReason(t *testing.T) {
	live := &stubLive{tail: LiveTail{Pod: openchoreo.RuntimePod{
		Found: true, Name: "p1", Phase: "Pending", WaitingReason: "ImagePullBackOff",
	}}}

	resp, err := NewAgentProgressReader(live, nil).CycleProgress(context.Background(), liveCycle("c3"), 0)
	if err != nil {
		t.Fatalf("CycleProgress: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Phase != "runner_image_pull_backoff" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
}

// The component is gone but the archive still has the log: that is the whole
// point of retaining components after a cycle ends.
func TestCycleProgress_FallsBackToTheArchiveWhenThePodIsGone(t *testing.T) {
	live := &stubLive{err: fmt.Errorf("%w: ca-c4", ErrComponentGone)}
	archive := &stubArchive{text: "2026-08-06T10:00:01Z archived line\n"}
	cycle := liveCycle("c4")
	ended := time.Now().UTC()
	cycle.EndedAt = &ended

	resp, err := NewAgentProgressReader(live, nil).WithArchive(archive).
		CycleProgress(context.Background(), cycle, 0)
	if err != nil {
		t.Fatalf("CycleProgress: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Summary != "archived line" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
	if !resp.Final {
		t.Error("a closed cycle served from the archive is final")
	}
}

// A finished cycle whose archive exceeds one progress page leads with an honest
// banner so the console never looks like the whole run was this short.
func TestCycleProgress_ClosedArchiveOverCapLeadsWithTruncatedBanner(t *testing.T) {
	var b strings.Builder
	for i := 0; i < defaultProgressLimit+25; i++ {
		fmt.Fprintf(&b, "{\"schemaVersion\":1,\"kind\":\"log\",\"summary\":\"line-%d\",\"ts\":\"2026-08-06T10:00:00.%09dZ\"}\n", i, i)
	}
	live := &stubLive{err: fmt.Errorf("%w: ca-trunc", ErrComponentGone)}
	archive := &stubArchive{text: b.String()}
	cycle := liveCycle("trunc")
	ended := time.Now().UTC()
	cycle.EndedAt = &ended

	resp, err := NewAgentProgressReader(live, nil).WithArchive(archive).
		CycleProgress(context.Background(), cycle, 0)
	if err != nil {
		t.Fatalf("CycleProgress: %v", err)
	}
	if len(resp.Lines) < 2 || resp.Lines[0].Phase != "logs_truncated" {
		t.Fatalf("want leading logs_truncated banner, got %+v", resp.Lines)
	}
	if resp.Lines[0].Seq != seqLogsTruncated {
		t.Fatalf("Seq = %d, want %d", resp.Lines[0].Seq, seqLogsTruncated)
	}
	if !resp.Truncated || !resp.Final {
		t.Fatalf("Truncated=%v Final=%v, want both true", resp.Truncated, resp.Final)
	}
	if got := len(resp.Lines) - 1; got != defaultProgressLimit {
		t.Fatalf("content lines = %d, want %d", got, defaultProgressLimit)
	}
}

// Nothing left to read must SAY so. An empty stream reads like an agent that
// never spoke, which is a different and much more alarming thing.
func TestCycleProgress_UnavailableWhenComponentAndArchiveAreBothGone(t *testing.T) {
	live := &stubLive{err: fmt.Errorf("%w: ca-c5", ErrComponentGone)}
	archive := &stubArchive{err: fmt.Errorf("%w: ca-c5", ErrComponentGone)}
	cycle := liveCycle("c5")
	ended := time.Now().UTC()
	cycle.EndedAt = &ended

	resp, err := NewAgentProgressReader(live, nil).WithArchive(archive).
		CycleProgress(context.Background(), cycle, 0)
	if err != nil {
		t.Fatalf("CycleProgress: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Phase != "logs_unavailable" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
	if resp.Lines[0].Seq != seqLogsUnavailable {
		t.Fatalf("Seq = %d, want the stable %d", resp.Lines[0].Seq, seqLogsUnavailable)
	}
	if !resp.Final {
		t.Error("an unavailable log is a settled answer")
	}
}

// A deployment with no observability plane must degrade to the same honest
// empty state rather than erroring the page.
func TestCycleProgress_UnavailableWithNoObserverConfigured(t *testing.T) {
	live := &stubLive{err: fmt.Errorf("%w: ca-c6", ErrComponentGone)}
	cycle := liveCycle("c6")
	ended := time.Now().UTC()
	cycle.EndedAt = &ended

	resp, err := NewAgentProgressReader(live, nil).CycleProgress(context.Background(), cycle, 0)
	if err != nil {
		t.Fatalf("CycleProgress: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Phase != "logs_unavailable" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
}

// An observer having a bad minute is not an answer: keep the stream open and
// let the next poll try again.
func TestCycleProgress_ArchiveErrorOnAnOpenCycleStaysNonFinal(t *testing.T) {
	live := &stubLive{err: fmt.Errorf("%w: ca-c7", ErrComponentGone)}
	archive := &stubArchive{err: fmt.Errorf("%w: 503", ErrArchiveUnavailable)}

	resp, err := NewAgentProgressReader(live, nil).WithArchive(archive).
		CycleProgress(context.Background(), liveCycle("c7"), 0)
	if err != nil {
		t.Fatalf("CycleProgress: %v", err)
	}
	if resp.Final {
		t.Error("an open cycle whose archive hiccuped must keep polling")
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Phase != "logs_unavailable" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
}

func TestCycleProgress_ATransportFailureIsAnError(t *testing.T) {
	live := &stubLive{err: errors.New("dial tcp: connection refused")}

	if _, err := NewAgentProgressReader(live, nil).CycleProgress(context.Background(), liveCycle("c8"), 0); err == nil {
		t.Fatal("a transport failure must surface so the caller can degrade")
	}
}

// Closed cycle + live empty success (Component retained, pod gone — not
// ErrComponentGone) must fall through to the archive.
func TestCycleProgress_ClosedEmptyLiveFallsThroughToArchive(t *testing.T) {
	live := &stubLive{tail: LiveTail{Pod: openchoreo.RuntimePod{Found: false}}}
	archive := &stubArchive{text: "2026-08-06T10:00:01Z archived after empty live\n"}
	cycle := liveCycle("c9")
	ended := time.Now().UTC()
	cycle.EndedAt = &ended

	resp, err := NewAgentProgressReader(live, nil).WithArchive(archive).
		CycleProgress(context.Background(), cycle, 0)
	if err != nil {
		t.Fatalf("CycleProgress: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Summary != "archived after empty live" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
	if !resp.Final {
		t.Error("closed cycle served from archive after empty live must be final")
	}
}

// Closed cycle + live empty success + no archive → settled unavailable.
func TestCycleProgress_ClosedEmptyLiveWithNoArchiveIsUnavailable(t *testing.T) {
	live := &stubLive{tail: LiveTail{Text: "", Pod: openchoreo.RuntimePod{Found: true, Phase: "Succeeded"}}}
	cycle := liveCycle("c10")
	ended := time.Now().UTC()
	cycle.EndedAt = &ended

	resp, err := NewAgentProgressReader(live, nil).CycleProgress(context.Background(), cycle, 0)
	if err != nil {
		t.Fatalf("CycleProgress: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Phase != "logs_unavailable" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
	if !resp.Final {
		t.Error("closed cycle with nowhere to read must be final")
	}
}

// Open cycle + live empty success with no archive lines is still the dark
// zone — the attempt may still be scheduling. An archive that already has
// lines means the pod finished and was reaped while the cycle stays open
// (PR webhook pending); that falls through in
// TestCycleProgress_OpenEmptyLiveFallsThroughToArchiveWhenArchiveHasLines.
func TestCycleProgress_OpenEmptyLiveStaysOnDarkZone(t *testing.T) {
	live := &stubLive{tail: LiveTail{Pod: openchoreo.RuntimePod{}}}
	archive := &stubArchive{text: ""}

	resp, err := NewAgentProgressReader(live, nil).WithArchive(archive).
		CycleProgress(context.Background(), liveCycle("c11"), 0)
	if err != nil {
		t.Fatalf("CycleProgress: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Phase != "runner_scheduling" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
	if resp.Final {
		t.Error("open cycle empty live must keep polling")
	}
	if resp.Lines[0].Phase == "logs_unavailable" {
		t.Fatal("open empty live must not report unavailable")
	}
}

func TestCycleProgress_OpenEmptyLiveFallsThroughToArchiveWhenArchiveHasLines(t *testing.T) {
	live := &stubLive{tail: LiveTail{Pod: openchoreo.RuntimePod{}}}
	archive := &stubArchive{text: "2026-08-06T10:00:01Z archived while cycle still open\n"}

	resp, err := NewAgentProgressReader(live, nil).WithArchive(archive).
		CycleProgress(context.Background(), liveCycle("c12"), 0)
	if err != nil {
		t.Fatalf("CycleProgress: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Summary != "archived while cycle still open" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
	if resp.Final {
		t.Error("open cycle served from archive must keep polling until the run settles")
	}
}

func TestCycleProgress_OpenEmptyLiveOnTerminalPodFallsThroughToArchive(t *testing.T) {
	live := &stubLive{tail: LiveTail{Pod: openchoreo.RuntimePod{Found: true, Phase: "Succeeded"}}}
	archive := &stubArchive{text: "2026-08-06T10:00:01Z archived after succeeded pod\n"}

	resp, err := NewAgentProgressReader(live, nil).WithArchive(archive).
		CycleProgress(context.Background(), liveCycle("c13"), 0)
	if err != nil {
		t.Fatalf("CycleProgress: %v", err)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].Summary != "archived after succeeded pod" {
		t.Fatalf("unexpected lines: %+v", resp.Lines)
	}
}

// TestDropTruncatedTail pins exactly which trailing lines are treated as cut
// mid-write. The rule has to be narrow: dropping a complete final line would
// lose the runner's terminal `result` event, which is how the feed reports the
// run's outcome.
func TestDropTruncatedTail(t *testing.T) {
	t.Parallel()

	const envelope = `{"schemaVersion":1,"seq":1,"kind":"phase","phase":"coding"}`
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"newline-terminated is always complete", envelope + "\n", envelope + "\n"},
		{"unterminated but valid envelope is kept", envelope, envelope},
		{"unterminated prose is kept", "[oneshot] materialised 3 skill(s)", "[oneshot] materialised 3 skill(s)"},
		{"unterminated brace-less JSON-ish text is kept", `result: {"ok":true`, `result: {"ok":true`},
		{"mid-envelope cut is dropped", envelope + "\n" + `{"schemaVersion":1,"ts":"2026-`, envelope + "\n"},
		{"a lone fragment leaves nothing", `{"schemaVersion":1,"ts":"2026-`, ""},
		{
			"k8s timestamp prefix does not hide the cut",
			"2026-08-07T08:20:30.880659137Z " + `{"schemaVersion":1,"ts":"2026-`,
			"",
		},
		{
			"a complete final line keeps its terminal result",
			`{"schemaVersion":1,"seq":9,"kind":"result","usage":{"inputTokens":1}}`,
			`{"schemaVersion":1,"seq":9,"kind":"result","usage":{"inputTokens":1}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := dropTruncatedTail(tc.in); got != tc.want {
				t.Errorf("dropTruncatedTail(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestTextToProgressEventsSurfacesScanFailure pins that a line past the scanner
// buffer names itself in the feed. It used to end the scan silently, dropping
// the rest of the page with no signal to the reader.
func TestTextToProgressEventsSurfacesScanFailure(t *testing.T) {
	t.Parallel()

	huge := `{"schemaVersion":1,"kind":"log","summary":"` + strings.Repeat("x", 2*1024*1024) + `"}`
	text := `{"schemaVersion":1,"seq":1,"kind":"phase","phase":"coding"}` + "\n" + huge + "\n" +
		`{"schemaVersion":1,"seq":2,"kind":"phase","phase":"done"}` + "\n"

	events, truncated := textToProgressEvents(text)
	if !truncated {
		t.Error("a scan that stopped early must report truncated")
	}
	if len(events) == 0 {
		t.Fatal("no events")
	}
	last := events[len(events)-1]
	if last.Kind != "log" || !strings.Contains(last.Summary, "size cap") {
		t.Errorf("last event must name the dropped page, got %+v", last)
	}
}
