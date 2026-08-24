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

// Component tier for the VERSION progress stream (GET
// /projects/{p}/builds/{tag}/progress), over the same real strict handler and
// tenant gate as the rest of this package's tests. It shares their fixtures.
//
// Every case here drives a milestone whose runs have ALL settled, because that is
// what makes the stream finite and capturable by an httptest recorder. The rule
// that a live run holds it open is a live-loop fact and lives in
// build_progress_settle_test.go.
package runread_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
)

const buildProgressPath = "/api/v1/projects/widgets/builds/v3/progress"

// runOfKind is specRun with a KIND and a creation time — the two facts the
// version stream orders and labels by. The origin follows the kind because a row
// where they disagree is one the platform never writes.
func runOfKind(id, kind, state string, minute int) delivery.MilestoneRun {
	row := specRun(id, state)
	row.Kind = kind
	switch kind {
	case delivery.RunKindTask:
		row.Origin = delivery.RunOriginIncidentAdoption
	case delivery.RunKindValidation:
		row.Origin = delivery.RunOriginRevalidate
	}
	created := time.Date(2026, 7, 1, 10, minute, 0, 0, time.UTC)
	row.CreatedAt = created
	row.StartedAt = &created
	return row
}

// versionFrames drives the stream and returns its frames, asserting the response
// itself is a well-formed event stream.
func versionFrames(t *testing.T, req *componenttest.Req) []map[string]any {
	t.Helper()
	rec := req.Get(buildProgressPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("stream: code %d (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}
	return parseFrames(t, rec.Body.String())
}

// frameRun pulls a frame's run attribution, failing if it is absent — every
// cycle and line frame on this stream owes one, because a cycle is not
// identified until you know which run opened it.
func frameRun(t *testing.T, frame map[string]any) (id, kind string, index float64) {
	t.Helper()
	run, ok := frame["run"].(map[string]any)
	if !ok {
		t.Fatalf("frame carries no run attribution: %+v", frame)
	}
	id, _ = run["id"].(string)
	kind, _ = run["kind"].(string)
	index, _ = run["index"].(float64)
	if id == "" || kind == "" || index == 0 {
		t.Fatalf("incomplete run attribution: %+v", run)
	}
	return id, kind, index
}

// TestBuildProgress_StitchesEveryRunOldestFirst is the endpoint's whole point:
// three executions, one narrative. The dev run that delivered the version leads,
// the task run that repaired it follows, the validation run that re-judged it
// comes last — regardless of the order the repository answers in.
func TestBuildProgress_StitchesEveryRunOldestFirst(t *testing.T) {
	// Handed to the harness NEWEST FIRST, which is how the repository answers, so
	// a stream that simply forwarded that order would fail here.
	rows := []delivery.MilestoneRun{
		runOfKind("r3", delivery.RunKindValidation, delivery.RunStateSucceeded, 40),
		runOfKind("r2", delivery.RunKindTask, delivery.RunStateSucceeded, 20),
		runOfKind("r1", delivery.RunKindDev, delivery.RunStateSucceeded, 0),
	}
	h := newHarness(t, rows,
		map[string][]delivery.RunCycle{
			"r1": {cycle("c1", delivery.CycleKindCoding, true), cycle("c2", delivery.CycleKindFix, true)},
			"r2": {cycle("c3", delivery.CycleKindCoding, true)},
			"r3": {cycle("c4", delivery.CycleKindValidation, true)},
		},
		map[string][]contracts.ProgressEvent{
			"c1": {line("tool_use", "read api.go", "")},
			"c2": {line("tool_use", "fix api.go", "subagent")},
			"c3": {line("tool_use", "patch the defect", "")},
			"c4": {line("result", "validation done", "")},
		}, nil)

	frames := versionFrames(t, h.AsOrg("acme"))

	// The narrative reads forwards: every frame of r1, then r2, then r3.
	var order []string
	for _, f := range frames {
		if f["type"] == "done" {
			continue
		}
		id, _, _ := frameRun(t, f)
		if len(order) == 0 || order[len(order)-1] != id {
			order = append(order, id)
		}
	}
	if strings.Join(order, ",") != "r1,r2,r3" {
		t.Errorf("run order = %v, want r1,r2,r3", order)
	}

	// Each run's KIND rides on its frames — that is the section marker — and its
	// 1-based chronological index labels the section without the console holding
	// the run list.
	wantKind := map[string]string{"r1": "dev", "r2": "task", "r3": "validation"}
	wantIndex := map[string]float64{"r1": 1, "r2": 2, "r3": 3}
	for _, f := range frames {
		if f["type"] == "done" {
			continue
		}
		id, kind, index := frameRun(t, f)
		if kind != wantKind[id] || index != wantIndex[id] {
			t.Errorf("run %s tagged kind=%q index=%v, want %q/%v", id, kind, index, wantKind[id], wantIndex[id])
		}
	}

	// cycleIndex stays RUN-RELATIVE: r1's cycles are 1 and 2, and r2's single
	// cycle is 1 again rather than 3. The pair (run.id, cycleIndex) is the key.
	seen := map[string]float64{}
	for _, f := range frames {
		if f["type"] != "line" {
			continue
		}
		l, _ := f["line"].(map[string]any)
		id, _, _ := frameRun(t, f)
		cid, _ := l["cycleId"].(string)
		idx, _ := l["cycleIndex"].(float64)
		seen[id+"/"+cid] = idx
	}
	want := map[string]float64{"r1/c1": 1, "r1/c2": 2, "r2/c3": 1, "r3/c4": 1}
	for k, v := range want {
		if seen[k] != v {
			t.Errorf("cycleIndex for %s = %v, want %v (run-relative)", k, seen[k], v)
		}
	}

	// A cycle frame precedes its own lines, so the console can open a section
	// before filling it — the same guarantee the run stream gives.
	if frames[0]["type"] != "cycle" {
		t.Errorf("first frame = %v, want a cycle frame", frames[0]["type"])
	}

	// And the ending says only what it can: no run is live. Never a run state —
	// a settled dev run is not a finished version, which is exactly what the
	// split makes ordinary.
	last := frames[len(frames)-1]
	if last["type"] != "done" || last["reason"] != "no_live_run" {
		t.Errorf("last frame = %+v, want a done frame reasoned no_live_run", last)
	}
	if _, present := last["state"]; present {
		t.Errorf("the done frame carries a run state: %+v", last)
	}
}

// TestBuildProgress_OneRun_IsStillAVersion: the ordinary case. A version worked
// by a single dev run reads as one section, with the same frame shape — nothing
// about the attribution is conditional on there being several runs.
func TestBuildProgress_OneRun_IsStillAVersion(t *testing.T) {
	h := newHarness(t,
		[]delivery.MilestoneRun{runOfKind("r1", delivery.RunKindDev, delivery.RunStateSucceeded, 0)},
		map[string][]delivery.RunCycle{"r1": {cycle("c1", delivery.CycleKindCoding, true)}},
		map[string][]contracts.ProgressEvent{"c1": {line("tool_use", "read api.go", "")}}, nil)

	frames := versionFrames(t, h.AsOrg("acme"))
	if len(frames) < 3 {
		t.Fatalf("frames = %d, want a cycle, a line and a done", len(frames))
	}
	id, kind, index := frameRun(t, frames[0])
	if id != "r1" || kind != "dev" || index != 1 {
		t.Errorf("attribution = %s/%s/%v, want r1/dev/1", id, kind, index)
	}
	if frames[len(frames)-1]["reason"] != "no_live_run" {
		t.Errorf("last frame = %+v, want reason no_live_run", frames[len(frames)-1])
	}
}

// TestBuildProgress_ReconnectRederivesIdentically pins the client-dedup contract
// from the server's side: the server keeps NO cursor, so a reconnect replays the
// same frames in the same order, and every line within one connection carries a
// distinct (run, cycle, seq) key for the client to dedup on. A server that
// renumbered anything per connection would break that dedup silently.
func TestBuildProgress_ReconnectRederivesIdentically(t *testing.T) {
	rows := []delivery.MilestoneRun{
		runOfKind("r1", delivery.RunKindDev, delivery.RunStateSucceeded, 0),
		runOfKind("r2", delivery.RunKindTask, delivery.RunStateSucceeded, 20),
	}
	cycles := map[string][]delivery.RunCycle{
		"r1": {cycle("c1", delivery.CycleKindCoding, true)},
		"r2": {cycle("c2", delivery.CycleKindCoding, true)},
	}
	logs := map[string][]contracts.ProgressEvent{
		"c1": {line("tool_use", "read api.go", "")},
		"c2": {line("tool_use", "patch the defect", "")},
	}
	h := newHarness(t, rows, cycles, logs, nil)

	first := h.AsOrg("acme").Get(buildProgressPath).Body.String()
	second := h.AsOrg("acme").Get(buildProgressPath).Body.String()
	if first != second {
		t.Errorf("a reconnect must re-derive the same stream\n--- first\n%s\n--- second\n%s", first, second)
	}

	keys := map[string]bool{}
	for _, f := range parseFrames(t, first) {
		if f["type"] != "line" {
			continue
		}
		l, _ := f["line"].(map[string]any)
		id, _, _ := frameRun(t, f)
		b, _ := json.Marshal([]any{id, l["cycleId"], l["seq"]})
		if keys[string(b)] {
			t.Errorf("duplicate line key %s within one connection", b)
		}
		keys[string(b)] = true
	}
}

// TestBuildProgress_CrossTenant_404: a version of another org resolves to
// not-found through the org-scoped tag read, so the probe cannot learn it exists.
func TestBuildProgress_CrossTenant_404(t *testing.T) {
	h := newHarness(t,
		[]delivery.MilestoneRun{runOfKind("r1", delivery.RunKindDev, delivery.RunStateSucceeded, 0)}, nil, nil, nil)
	rec := h.AsOrg("evil").Get(buildProgressPath)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant stream: code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	// The fence ran BEFORE any byte, so the client gets a JSON envelope rather
	// than a broken half-stream.
	if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("a refused stream must not open one; content-type = %q", ct)
	}
	if rec.Body.Len() == 0 || strings.Contains(rec.Body.String(), "data:") {
		t.Errorf("body = %q, want an error envelope", rec.Body.String())
	}
}

// A version in another PROJECT of the same org is equally a 404 — the tag alone
// is not an authorization.
func TestBuildProgress_WrongProject_404(t *testing.T) {
	row := runOfKind("r1", delivery.RunKindDev, delivery.RunStateSucceeded, 0)
	row.ProjectID = "other"
	h := newHarness(t, []delivery.MilestoneRun{row}, nil, nil, nil)
	rec := h.AsOrg("acme").Get(buildProgressPath)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-project stream: code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("a refused stream must not open one; content-type = %q", ct)
	}
}

// A tag with no run row is a 404 — "no such version" and "never built" are the
// same answer, and neither opens an empty stream.
func TestBuildProgress_UnknownTag_404(t *testing.T) {
	h := newHarness(t,
		[]delivery.MilestoneRun{runOfKind("r1", delivery.RunKindDev, delivery.RunStateSucceeded, 0)}, nil, nil, nil)
	if rec := h.AsOrg("acme").Get("/api/v1/projects/widgets/builds/v99/progress"); rec.Code != http.StatusNotFound {
		t.Fatalf("code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}

func TestBuildProgress_NoAuth_401(t *testing.T) {
	h := newHarness(t,
		[]delivery.MilestoneRun{runOfKind("r1", delivery.RunKindDev, delivery.RunStateSucceeded, 0)}, nil, nil, nil)
	if rec := h.NoAuth().Get(buildProgressPath); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-auth stream: code %d, want 401", rec.Code)
	}
}
