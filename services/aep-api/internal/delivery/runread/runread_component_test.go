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

// Component tier for the milestone run read surface: the REAL contract-first
// strict handler (via componenttest, tenant gate in ENFORCE) over the version
// runs read, the per-run progress stream and cancel — with the repositories,
// the pod-log reader and the supervisor faked.
//
// It proves the HTTP contract rather than the wiring: the JSON shapes, the
// text/event-stream framing (a TERMINAL run is a finite stream the recorder
// captures whole), the no-claims 401, and the org fence — a caller scoped to
// another org resolves nil through the scoped read and surfaces as 404, never
// 403, so a probe cannot learn that a run exists.
package runread_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
	deliveryhttpapi "github.com/wso2/aep/aep-api/internal/delivery/httpapi"
	"github.com/wso2/aep/aep-api/internal/delivery/runread"
	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
)

// ---- fakes ------------------------------------------------------------------

// fakeRuns fences every read to one org, missing with (nil, nil) exactly as the
// repository does — which is what turns a cross-org read into a 404.
type fakeRuns struct {
	org  string
	rows []delivery.MilestoneRun
}

func (f fakeRuns) MilestoneNumberForTag(_ context.Context, orgID, projectID, tag string) (int, bool, error) {
	if orgID != f.org {
		return 0, false, nil
	}
	for i := range f.rows {
		if f.rows[i].ProjectID == projectID && f.rows[i].MilestoneTitle == tag {
			return f.rows[i].MilestoneNumber, true, nil
		}
	}
	return 0, false, nil
}

func (f fakeRuns) ListByMilestone(_ context.Context, orgID, projectID string, number int) ([]delivery.MilestoneRun, error) {
	var out []delivery.MilestoneRun
	if orgID != f.org {
		return out, nil
	}
	for i := range f.rows {
		if f.rows[i].ProjectID == projectID && f.rows[i].MilestoneNumber == number {
			out = append(out, f.rows[i])
		}
	}
	return out, nil
}

func (f fakeRuns) GetByIDScoped(_ context.Context, orgID, id string) (*delivery.MilestoneRun, error) {
	if orgID != f.org {
		return nil, nil
	}
	for i := range f.rows {
		if f.rows[i].ID == id {
			row := f.rows[i]
			return &row, nil
		}
	}
	return nil, nil
}

type fakeCycles struct {
	byRun map[string][]delivery.RunCycle
}

func (f fakeCycles) ListByRun(_ context.Context, _, runID string) ([]delivery.RunCycle, error) {
	return f.byRun[runID], nil
}

// fakeCycleLogs replays a fixed feed per cycle, in whichever envelope version the
// stream under test reads: v2 events for the run stream, v1 lines for the version
// stream. It drains on the first derive (a non-zero cursor answers empty) — the
// same shape the captured snapshot has.
type fakeCycleLogs struct {
	byCycle   map[string][]contracts.ProgressEvent
	eventsFor map[string][]gen.RunEvent
}

func (f fakeCycleLogs) CycleProgress(_ context.Context, c *delivery.RunCycle, since int64) (*contracts.ProgressResponse, error) {
	if since > 0 {
		return &contracts.ProgressResponse{Lines: nil, CursorMillis: since, Final: true}, nil
	}
	return &contracts.ProgressResponse{
		Lines:        f.byCycle[c.ID],
		CursorMillis: 1,
		Final:        true,
	}, nil
}

func (f fakeCycleLogs) CycleEvents(_ context.Context, c *delivery.RunCycle, cursor string) ([]gen.RunEvent, int, string, error) {
	if cursor != "" {
		return nil, c.Attempts, cursor, nil
	}
	return f.eventsFor[c.ID], c.Attempts, "drained", nil
}

// fakeCanceller records the run it was asked to cancel and can answer with the
// engine-down sentinel.
type fakeCanceller struct {
	err     error
	calls   []string
	cancels int
	// order, when set, records this step's position among record/signal/reap.
	order *[]string
}

func (f *fakeCanceller) CancelRun(_ context.Context, row *delivery.MilestoneRun) error {
	if f.order != nil {
		*f.order = append(*f.order, "signal")
	}
	f.cancels++
	f.calls = append(f.calls, row.ID)
	return f.err
}

// ---- fixtures ---------------------------------------------------------------

func specRun(id, state string) delivery.MilestoneRun {
	started := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	row := delivery.MilestoneRun{
		ID: id, OrgID: "acme", ProjectID: "widgets",
		MilestoneNumber: 4, MilestoneTitle: "v3",
		Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild, State: state,
		CyclesTotal: 2, CycleCeiling: delivery.RunDefaultCycleCeiling, FixCycles: 1,
		CreatedAt: started, StartedAt: &started,
	}
	if delivery.IsTerminalRunState(state) {
		ended := started.Add(time.Hour)
		row.EndedAt = &ended
		row.ValidationVerdict = delivery.ValidationVerdictPassed
	}
	return row
}

func cycle(id, kind string, ended bool) delivery.RunCycle {
	c := delivery.RunCycle{
		ID: id, OrgID: "acme", ProjectID: "widgets", RunID: "r1",
		Kind: kind, Attempts: 1, JobRef: "ca-" + id,
		Branch: "aep/m4-" + id, PRNumber: 42, PRURL: cyclePRPage,
		CreatedAt: time.Date(2026, 7, 1, 10, 5, 0, 0, time.UTC),
	}
	if ended {
		t := c.CreatedAt.Add(10 * time.Minute)
		c.EndedAt = &t
	}
	return c
}

func line(kind, summary, emitter string) contracts.ProgressEvent {
	return contracts.ProgressEvent{SchemaVersion: 1, Seq: 1, Kind: kind, Summary: summary, Emitter: emitter}
}

// event is a minimal v2 feed entry: the four fields the contract makes required,
// plus whatever the test is about.
func event(seq int64, kind gen.RunEventKind, agentID string) gen.RunEvent {
	return gen.RunEvent{
		V: gen.RunEventV2, Seq: seq, TS: time.Date(2026, 7, 1, 10, 6, 0, 0, time.UTC),
		Kind: kind, AgentID: agentID,
	}
}

// fakeProjectBuilds is the cluster, as the cycle-build read sees it: every
// WorkflowRun the project has, of any commit. It stores nothing per cycle —
// modelling the real contract, where the runs themselves ARE the record and the
// read recovers a merge's fan-out by filtering them.
type fakeProjectBuilds struct {
	runs []delivery.MergeBuild
	err  error
}

func (f fakeProjectBuilds) ListProjectBuildRuns(_ context.Context, _, _ string) ([]delivery.MergeBuild, error) {
	return f.runs, f.err
}

// newHarness wires the real runread services behind componenttest.
func newHarness(t *testing.T, rows []delivery.MilestoneRun, cycles map[string][]delivery.RunCycle,
	logs map[string][]contracts.ProgressEvent, canceller runread.RunCanceller) *componenttest.Harness {
	t.Helper()
	return newHarnessWithBuilds(t, rows, cycles, logs, canceller, nil)
}

// newHarnessWithBuilds is newHarness plus a cluster to derive cycle builds from.
// A nil lister is the degraded boot without the OpenChoreo client.
func newHarnessWithBuilds(t *testing.T, rows []delivery.MilestoneRun, cycles map[string][]delivery.RunCycle,
	logs map[string][]contracts.ProgressEvent, canceller runread.RunCanceller,
	builds runread.ProjectBuildLister) *componenttest.Harness {
	t.Helper()
	var logReader runread.CycleLogReader
	if logs != nil {
		logReader = fakeCycleLogs{byCycle: logs}
	}
	return assemble(t, rows, cycles, logReader, canceller, builds)
}

// newEventHarness is newHarness for the RUN progress stream, whose feed is v2
// RunEvents.
func newEventHarness(t *testing.T, rows []delivery.MilestoneRun, cycles map[string][]delivery.RunCycle,
	events map[string][]gen.RunEvent) *componenttest.Harness {
	t.Helper()
	return assemble(t, rows, cycles, fakeCycleLogs{eventsFor: events}, nil, nil)
}

// assemble wires the real runread services behind componenttest.
func assemble(t *testing.T, rows []delivery.MilestoneRun, cycles map[string][]delivery.RunCycle,
	logReader runread.CycleLogReader, canceller runread.RunCanceller,
	builds runread.ProjectBuildLister) *componenttest.Harness {
	t.Helper()
	runs := fakeRuns{org: "acme", rows: rows}
	cyc := fakeCycles{byRun: cycles}
	// Every cycle in this harness has a COMPLETE recording, so the read and the
	// stream both carry a real `recording` — the field a console has to see
	// before it presents a feed as the story of a cycle.
	recordings := fakeRecordings{}
	for _, rows := range cycles {
		for i := range rows {
			recordings[rows[i].ID] = gen.RunCycleViewRecordingComplete
		}
	}
	handlers, err := deliveryhttpapi.New(deliveryhttpapi.Deps{
		RunReads:       runread.NewReads(runs, cyc).WithRecordings(recordings),
		RunProgress:    runread.NewProgressService(runs, cyc, logReader).WithRecordings(recordings),
		RunCommands:    runread.NewCommands(runs, &fakeRecorder{}, canceller, nil),
		RunCycleBuilds: runread.NewCycleBuilds(runs, cyc, builds),
	})
	if err != nil {
		t.Fatalf("assemble delivery aggregator: %v", err)
	}
	return componenttest.New(t, componenttest.Options{Deps: edge.Deps{Delivery: handlers}})
}

// cyclePRPage is a cycle's recorded pull request page — the HOST's own link, as
// the webhook reported it. The read must carry it verbatim: the console links it
// and composes no URL of its own.
const cyclePRPage = "https://github.com/acme/widgets/pull/42"

const (
	runsPath     = "/api/v1/projects/widgets/builds/v3/runs"
	progressPath = "/api/v1/projects/widgets/runs/r1/progress"
	cancelPath   = "/api/v1/projects/widgets/runs/r1/cancel"
)

// ---- GET /builds/{tag}/runs -------------------------------------------------

func TestListBuildRuns_ResolvesTheTagThroughRunRows(t *testing.T) {
	h := newHarness(t,
		[]delivery.MilestoneRun{specRun("r1", delivery.RunStateSucceeded)},
		map[string][]delivery.RunCycle{"r1": {cycle("c1", delivery.CycleKindCoding, true), cycle("c2", delivery.CycleKindFix, true)}},
		nil, nil)

	rec := h.AsOrg("acme").Get(runsPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d (%s)", rec.Code, rec.Body.String())
	}
	var got gen.BuildRunList
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v — %s", err, rec.Body.String())
	}
	if got.Tag != "v3" || got.MilestoneNumber != 4 {
		t.Errorf("tag/milestone = %q/%d, want v3/4", got.Tag, got.MilestoneNumber)
	}
	if len(got.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(got.Runs))
	}
	run := got.Runs[0]
	if run.State != "succeeded" || run.Origin != "spec-build" {
		t.Errorf("run state/origin = %q/%q", run.State, run.Origin)
	}
	if run.Budgets.CycleCeiling != int64(delivery.RunDefaultCycleCeiling) || run.Budgets.FixCycles != 1 {
		t.Errorf("budgets not carried: %+v", run.Budgets)
	}
	// The deployment surface reads the verdict HERE — with a link to the report.
	if run.Validation.Verdict != "passed" || run.Validation.ReportPath == "" {
		t.Errorf("validation = %+v, want the verdict plus a report link", run.Validation)
	}
	if len(run.Cycles) != 2 || run.Cycles[0].Kind != "coding" || run.Cycles[1].Kind != "fix" {
		t.Errorf("cycles not carried in dispatch order: %+v", run.Cycles)
	}
	// What the platform can SERVE of each cycle's feed rides the same read. It is
	// a different question from what the cycle did, and the console has to ask it
	// before it presents a feed as the whole story.
	if run.Cycles[0].Recording != gen.RunCycleViewRecordingComplete {
		t.Errorf("cycle recording = %q, want complete", run.Cycles[0].Recording)
	}
	// The pull request travels as the host's own page, not as a number the
	// console would have to turn into a link itself.
	if run.Cycles[0].PrNumber != 42 || run.Cycles[0].PrURL != cyclePRPage {
		t.Errorf("cycle pull request = (#%d, %q), want (#42, %q)",
			run.Cycles[0].PrNumber, run.Cycles[0].PrURL, cyclePRPage)
	}
}

// A tag with no run row is a 404 — "no such version" and "never built" are the
// same answer, and neither invents an empty list.
func TestListBuildRuns_UnknownTag_404(t *testing.T) {
	h := newHarness(t, []delivery.MilestoneRun{specRun("r1", delivery.RunStateRunning)}, nil, nil, nil)
	if rec := h.AsOrg("acme").Get("/api/v1/projects/widgets/builds/v99/runs"); rec.Code != http.StatusNotFound {
		t.Fatalf("code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}

func TestListBuildRuns_CrossTenant_404(t *testing.T) {
	h := newHarness(t, []delivery.MilestoneRun{specRun("r1", delivery.RunStateRunning)}, nil, nil, nil)
	if rec := h.AsOrg("evil").Get(runsPath); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant read: code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}

func TestListBuildRuns_NoAuth_401(t *testing.T) {
	h := newHarness(t, []delivery.MilestoneRun{specRun("r1", delivery.RunStateRunning)}, nil, nil, nil)
	if rec := h.NoAuth().Get(runsPath); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-auth read: code %d, want 401", rec.Code)
	}
}

// ---- GET /runs/{runId}/progress (SSE) ---------------------------------------

// A TERMINAL run streams its whole history then ends. That finiteness is what
// lets the recorder capture the body — and it is the contract itself: only a
// terminal run settles the stream.
func TestRunProgress_TerminalRun_StreamsCyclesAndEventsThenDone(t *testing.T) {
	read := event(2, gen.RunEventKindToolUse, "lead")
	read.Tool, read.Summary, read.ToolUseID = "Read", "read api.go", "toolu_r1"
	spawned := event(4, gen.RunEventKindAgentProgress, "toolu_web")
	spawned.Phrase, spawned.Label, spawned.Depth = "Editing web.tsx", "Build the SPA", 1
	settled := event(6, gen.RunEventKindRunSettled, "lead")
	settled.Outcome = gen.RunEventOutcomeSuccess

	h := newEventHarness(t,
		[]delivery.MilestoneRun{specRun("r1", delivery.RunStateSucceeded)},
		map[string][]delivery.RunCycle{"r1": {cycle("c1", delivery.CycleKindCoding, true), cycle("c2", delivery.CycleKindValidation, true)}},
		map[string][]gen.RunEvent{"c1": {read, spawned}, "c2": {settled}})

	rec := h.AsOrg("acme").Get(progressPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("stream: code %d (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{`"type":"cycle"`, `"type":"event"`, `"type":"done"`, `"state":"succeeded"`, "data: [DONE]"} {
		if !strings.Contains(body, want) {
			t.Errorf("stream body missing %q\n---\n%s", want, body)
		}
	}
	// The frame kind lives INSIDE the data payload — never as an SSE event: name,
	// because the console's shared parser keeps only `data:` lines.
	if strings.Contains(body, "event:") {
		t.Errorf("frames must not use SSE event: names\n---\n%s", body)
	}
	// This stream is v2 ONLY. A `line` frame here would mean a console had to
	// dedup one cycle's feed across two incompatible seq spaces.
	if strings.Contains(body, `"type":"line"`) {
		t.Errorf("run stream emitted a v1 line frame\n---\n%s", body)
	}

	// Grouping: every event names the cycle AND the attempt that produced it, on
	// the frame — a runner knows what it is doing but not which cycle of which run
	// it turned out to be, and a seq is monotonic only within one attempt.
	frames := parseFrames(t, body)
	var events []map[string]any
	for _, f := range frames {
		if f["type"] == "event" {
			events = append(events, f)
		}
	}
	if len(events) != 3 {
		t.Fatalf("event frames = %d, want 3\n%s", len(events), body)
	}
	if events[0]["cycleId"] != "c1" || events[0]["attempt"] != float64(1) {
		t.Errorf("first event not stamped with its cycle/attempt: %+v", events[0])
	}
	if events[2]["cycleId"] != "c2" {
		t.Errorf("last event not stamped with cycle c2: %+v", events[2])
	}
	// The RunEvent itself is carried whole, kind and all.
	first := events[0]["event"].(map[string]any)
	if first["kind"] != "tool_use" || first["v"] != float64(2) || first["agentId"] != "lead" {
		t.Errorf("event payload = %+v, want the v2 envelope intact", first)
	}
	// Attribution is per AGENT now, not a main/subagent chip: an agent's own
	// events name it, and carry the label and depth a console indents on.
	second := events[1]["event"].(map[string]any)
	if second["agentId"] != "toolu_web" || second["label"] != "Build the SPA" || second["depth"] != float64(1) {
		t.Errorf("spawned agent's event lost its identity: %+v", second)
	}

	// A cycle frame precedes its own events, so the console can open the section
	// before filling it.
	if frames[0]["type"] != "cycle" {
		t.Errorf("first frame = %v, want a cycle frame", frames[0]["type"])
	}
}

// TestRunProgress_CarriesAgentIdentityAndOutcomes pins that the fields a console
// needs to tell one agent from another — and a failed tool call from a
// successful one — survive the frame walk. The walk is where the platform stamps
// its own attribution onto somebody else's event, so it is exactly the place
// where the rest of it could be dropped without anything failing to compile.
func TestRunProgress_CarriesAgentIdentityAndOutcomes(t *testing.T) {
	const agent = "toolu_api"
	started := event(1, gen.RunEventKindAgentStarted, agent)
	started.Label, started.Depth, started.Role = "Implement todo-api (issue #3)", 1, "inferred"

	work := event(2, gen.RunEventKindToolUse, agent)
	work.Tool, work.Summary, work.ToolUseID = "Bash", "bal build", "toolu_b1"

	failed, exitCode := false, 1
	outcome := event(4, gen.RunEventKindToolResult, agent)
	outcome.Tool, outcome.Summary, outcome.ToolUseID = "Bash", "error: compilation contains errors", "toolu_b1"
	outcome.Ok, outcome.DurationMs, outcome.ExitCode = &failed, 172000, &exitCode

	// The agent's own closing report, with the figures only the runtime has.
	report := event(6, gen.RunEventKindAgentSettled, agent)
	report.Status, report.DurationMs = gen.AgentStatusCompleted, 209158
	report.ToolCount, report.LinesAdded, report.LinesRemoved = 19, 553, 4
	report.Label, report.Depth = "Implement todo-api (issue #3)", 1

	h := newEventHarness(t,
		[]delivery.MilestoneRun{specRun("r1", delivery.RunStateSucceeded)},
		map[string][]delivery.RunCycle{"r1": {cycle("c1", delivery.CycleKindCoding, true)}},
		map[string][]gen.RunEvent{"c1": {started, work, outcome, report}})

	rec := h.AsOrg("acme").Get(progressPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("stream: code %d (%s)", rec.Code, rec.Body.String())
	}
	var events []map[string]any
	for _, f := range parseFrames(t, rec.Body.String()) {
		if f["type"] == "event" {
			events = append(events, f["event"].(map[string]any))
		}
	}
	if len(events) != 4 {
		t.Fatalf("event frames = %d, want 4", len(events))
	}
	for i, e := range events {
		if e["agentId"] != agent {
			t.Errorf("event %d lost its agent: %+v", i, e)
		}
	}
	// An INFERRED agent must say so: a v1 cycle never announced its subagents, and
	// a reader has to be able to tell a deduced tree from an observed one.
	if events[0]["role"] != "inferred" || events[0]["label"] != "Implement todo-api (issue #3)" {
		t.Errorf("agent_started = %+v, want the inferred role and the label", events[0])
	}
	// The action and its outcome share a call id — that pairing is what lets the
	// console put the outcome back on the action's own row.
	if events[1]["toolUseId"] != "toolu_b1" || events[2]["toolUseId"] != "toolu_b1" {
		t.Errorf("action/outcome pairing lost: %+v / %+v", events[1], events[2])
	}

	// The failure must arrive AS a failure. `ok` is a pointer precisely so
	// `false` survives the wire; a plain bool would omitempty it away here and
	// the console would render a failed build as a success.
	res := events[2]
	if got, ok := res["ok"].(bool); !ok || got {
		t.Errorf("tool_result ok = %v, want an explicit false", res["ok"])
	}
	if res["durationMs"] != float64(172000) {
		t.Errorf("tool_result durationMs = %v, want 172000", res["durationMs"])
	}
	// …and an event that is NOT a tool result carries no `ok` at all, so absence
	// can never be mistaken for success.
	if _, present := events[1]["ok"]; present {
		t.Errorf("tool_use carries an ok field: %+v", events[1])
	}

	// The exit code is the honest per-step signal: it says THIS command broke.
	if res["exitCode"] != float64(1) {
		t.Errorf("tool_result exitCode = %v, want 1", res["exitCode"])
	}
	// A tool that reports no code must not gain a zero on the way through,
	// which would read as "exited 0" on a failed call.
	if _, present := events[1]["exitCode"]; present {
		t.Errorf("tool_use carries an exitCode: %+v", events[1])
	}

	// The agent totals are what a collapsed section reports, so losing them here
	// means a settled agent says nothing about what it produced.
	settled := events[3]
	if settled["toolCount"] != float64(19) || settled["linesAdded"] != float64(553) || settled["linesRemoved"] != float64(4) {
		t.Errorf("agent report lost its totals: %+v", settled)
	}
	if settled["status"] != "completed" || settled["durationMs"] != float64(209158) {
		t.Errorf("agent report lost its verdict or duration: %+v", settled)
	}
}

func TestRunProgress_CrossTenant_404(t *testing.T) {
	h := newHarness(t, []delivery.MilestoneRun{specRun("r1", delivery.RunStateSucceeded)}, nil, nil, nil)
	rec := h.AsOrg("evil").Get(progressPath)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant stream: code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	// The fence ran BEFORE any byte, so the client gets a JSON envelope rather
	// than a broken half-stream.
	if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("a refused stream must not open one; content-type = %q", ct)
	}
}

// A run in another PROJECT of the same org is equally a 404 — the id alone is
// not an authorization.
func TestRunProgress_WrongProject_404(t *testing.T) {
	row := specRun("r1", delivery.RunStateSucceeded)
	row.ProjectID = "other"
	h := newHarness(t, []delivery.MilestoneRun{row}, nil, nil, nil)
	if rec := h.AsOrg("acme").Get(progressPath); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-project stream: code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}

func TestRunProgress_NoAuth_401(t *testing.T) {
	h := newHarness(t, []delivery.MilestoneRun{specRun("r1", delivery.RunStateSucceeded)}, nil, nil, nil)
	if rec := h.NoAuth().Get(progressPath); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-auth stream: code %d, want 401", rec.Code)
	}
}

// ---- POST /runs/{runId}/cancel ----------------------------------------------

func TestCancelRun_SignalsTheSupervisor_202(t *testing.T) {
	canceller := &fakeCanceller{}
	h := newHarness(t, []delivery.MilestoneRun{specRun("r1", delivery.RunStateWaiting)}, nil, nil, canceller)

	rec := h.AsOrg("acme").Post(cancelPath, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("cancel: code %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	if canceller.cancels != 1 || canceller.calls[0] != "r1" {
		t.Errorf("supervisor not signalled once for r1: %+v", canceller.calls)
	}
}

// The engine being down means nothing was cancelled and the caller may retry —
// a 503, never a 500 and never a silent 202.
func TestCancelRun_TemporalDown_503(t *testing.T) {
	canceller := &fakeCanceller{err: delivery.ErrTemporalUnavailable}
	h := newHarness(t, []delivery.MilestoneRun{specRun("r1", delivery.RunStateWaiting)}, nil, nil, canceller)

	if rec := h.AsOrg("acme").Post(cancelPath, ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("cancel with the engine down: code %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
}

func TestCancelRun_CrossTenant_404_AndNeverSignals(t *testing.T) {
	canceller := &fakeCanceller{}
	h := newHarness(t, []delivery.MilestoneRun{specRun("r1", delivery.RunStateWaiting)}, nil, nil, canceller)

	if rec := h.AsOrg("evil").Post(cancelPath, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant cancel: code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	if canceller.cancels != 0 {
		t.Error("a cross-tenant cancel must never reach the supervisor")
	}
}

func TestCancelRun_NoAuth_401(t *testing.T) {
	canceller := &fakeCanceller{}
	h := newHarness(t, []delivery.MilestoneRun{specRun("r1", delivery.RunStateWaiting)}, nil, nil, canceller)
	if rec := h.NoAuth().Post(cancelPath, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-auth cancel: code %d, want 401", rec.Code)
	}
}

// ---- helpers ----------------------------------------------------------------

// parseFrames decodes the `data:` JSON payloads of a finished SSE body, in
// order, skipping the [DONE] sentinel.
func parseFrames(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, block := range strings.Split(body, "\n\n") {
		for _, ln := range strings.Split(block, "\n") {
			payload, ok := strings.CutPrefix(ln, "data: ")
			if !ok || payload == "[DONE]" {
				continue
			}
			var frame map[string]any
			if err := json.Unmarshal([]byte(payload), &frame); err != nil {
				t.Fatalf("frame %q: %v", payload, err)
			}
			out = append(out, frame)
		}
	}
	return out
}

// ---- GET /builds/{tag}/cycles/{cycleId}/builds --------------------------------

// mergedCycle is a cycle whose pull request landed — the only kind that has
// builds to report.
func mergedCycle(id, sha string) delivery.RunCycle {
	c := cycle(id, delivery.CycleKindCoding, true)
	c.Branch = "aep/m4-c1"
	c.PRNumber = 12
	c.MergeSHA = sha
	return c
}

func TestListCycleBuilds_DerivesTheFanOutFromTheMergeSHA(t *testing.T) {
	const sha = "4a91c2f8ab3199ff"
	h := newHarnessWithBuilds(t,
		[]delivery.MilestoneRun{specRun("r1", delivery.RunStateRunning)},
		map[string][]delivery.RunCycle{"r1": {mergedCycle("c1", sha)}},
		nil, nil,
		fakeProjectBuilds{runs: []delivery.MergeBuild{
			{Component: "webapp", RunName: delivery.BuildRunName("widgets", "webapp", sha, 1), Status: "Running"},
			{Component: "api", RunName: delivery.BuildRunName("widgets", "api", sha, 1), Status: "Succeeded", Completed: true},
			// A build of a DIFFERENT commit, which this cycle must not claim.
			{Component: "api", RunName: delivery.BuildRunName("widgets", "api", "ffffffffffff", 1), Status: "Failed", Completed: true},
		}})

	rec := h.AsOrg("acme").Get("/api/v1/projects/widgets/builds/v3/cycles/c1/builds")
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d (%s)", rec.Code, rec.Body.String())
	}
	var got gen.CycleBuildList
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v — %s", err, rec.Body.String())
	}
	if len(got.Items) != 2 {
		t.Fatalf("builds = %+v, want only this merge's two", got.Items)
	}
	if got.Items[0].Component != "api" || !got.Items[0].Completed || got.Items[0].Status != "Succeeded" {
		t.Errorf("api build = %+v, want the completed one carried verbatim", got.Items[0])
	}
	if got.Items[1].Component != "webapp" || got.Items[1].Completed {
		t.Errorf("webapp build = %+v, want the still-running one", got.Items[1])
	}
	if got.Items[0].BuildName != delivery.BuildRunName("widgets", "api", sha, 1) {
		// The name is the client's key into get-build-logs; it must be handed back
		// verbatim so nothing client-side reconstructs it.
		t.Errorf("buildName = %q, want the WorkflowRun's own name", got.Items[0].BuildName)
	}
}

// A cycle still working has no merge, so nothing has been built. That is the
// ordinary mid-cycle answer and must not read as an error.
func TestListCycleBuilds_UnmergedCycle_Empty(t *testing.T) {
	h := newHarnessWithBuilds(t,
		[]delivery.MilestoneRun{specRun("r1", delivery.RunStateRunning)},
		map[string][]delivery.RunCycle{"r1": {cycle("c1", delivery.CycleKindCoding, false)}},
		nil, nil,
		fakeProjectBuilds{runs: []delivery.MergeBuild{
			{Component: "api", RunName: delivery.BuildRunName("widgets", "api", "4a91c2f8ab31", 1)},
		}})

	rec := h.AsOrg("acme").Get("/api/v1/projects/widgets/builds/v3/cycles/c1/builds")
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d (%s)", rec.Code, rec.Body.String())
	}
	var got gen.CycleBuildList
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("builds = %+v, want none before the merge", got.Items)
	}
}

// A boot without the OpenChoreo client answers empty rather than failing — the
// same answer a project with no builds gives.
func TestListCycleBuilds_NoClusterClient_Empty(t *testing.T) {
	h := newHarnessWithBuilds(t,
		[]delivery.MilestoneRun{specRun("r1", delivery.RunStateRunning)},
		map[string][]delivery.RunCycle{"r1": {mergedCycle("c1", "4a91c2f8ab31")}},
		nil, nil, nil)

	if rec := h.AsOrg("acme").Get("/api/v1/projects/widgets/builds/v3/cycles/c1/builds"); rec.Code != http.StatusOK {
		t.Fatalf("code %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

// The cycle is looked up WITHIN the version's own runs, so an id that exists
// elsewhere is not readable by guessing it here.
func TestListCycleBuilds_UnknownCycle_404(t *testing.T) {
	h := newHarnessWithBuilds(t,
		[]delivery.MilestoneRun{specRun("r1", delivery.RunStateRunning)},
		map[string][]delivery.RunCycle{"r1": {mergedCycle("c1", "4a91c2f8ab31")}},
		nil, nil, fakeProjectBuilds{})

	if rec := h.AsOrg("acme").Get("/api/v1/projects/widgets/builds/v3/cycles/nope/builds"); rec.Code != http.StatusNotFound {
		t.Fatalf("code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}

func TestListCycleBuilds_CrossTenant_404(t *testing.T) {
	h := newHarnessWithBuilds(t,
		[]delivery.MilestoneRun{specRun("r1", delivery.RunStateRunning)},
		map[string][]delivery.RunCycle{"r1": {mergedCycle("c1", "4a91c2f8ab31")}},
		nil, nil, fakeProjectBuilds{})

	if rec := h.AsOrg("evil").Get("/api/v1/projects/widgets/builds/v3/cycles/c1/builds"); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant read: code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}

func TestListCycleBuilds_NoAuth_401(t *testing.T) {
	h := newHarnessWithBuilds(t,
		[]delivery.MilestoneRun{specRun("r1", delivery.RunStateRunning)},
		map[string][]delivery.RunCycle{"r1": {mergedCycle("c1", "4a91c2f8ab31")}},
		nil, nil, fakeProjectBuilds{})

	if rec := h.NoAuth().Get("/api/v1/projects/widgets/builds/v3/cycles/c1/builds"); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-auth read: code %d, want 401", rec.Code)
	}
}
