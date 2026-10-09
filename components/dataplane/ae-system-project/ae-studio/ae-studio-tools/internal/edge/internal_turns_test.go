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

package edge

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/projects"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/repotest"
	"github.com/wso2/aep/ae-studio-tools/internal/turns/turnstest"
)

const turnsPath = "/internal/v1/repos/acme-gh/greeter/turns"

// postStream posts body to path over real HTTP (the relay streams and
// notices the caller leaving), with the AE-only token and the pod's org.
// The caller closes the response.
func (h *harness) postStream(path, token, body string) *http.Response {
	h.t.Helper()
	if h.server == nil {
		h.server = httptest.NewServer(h.handler)
		h.t.Cleanup(h.server.Close)
	}
	req, err := http.NewRequest(http.MethodPost, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-Impersonate-Org", "ou-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// logCount counts the log lines carrying every fragment.
func (h *harness) logCount(fragments ...string) int {
	n := 0
	for _, line := range strings.Split(h.logs(), "\n") {
		all := line != ""
		for _, f := range fragments {
			all = all && strings.Contains(line, f)
		}
		if all {
			n++
		}
	}
	return n
}

func readLine(t *testing.T, resp *http.Response) string {
	t.Helper()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Fatalf("read line: %v", err)
	}
	return line
}

func lastLine(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return lines[len(lines)-1]
}

func responseCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	var p struct{ Code string }
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	return p.Code
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func turnRequest(turnID string) string {
	return `{"turnId":"` + turnID + `","project":"greeter","kind":"start","credit":{"userId":"u1","name":"Ann","email":"ann@x"}}`
}

var greeterRepo = map[string]projects.Repository{"greeter": {Owner: "acme-gh", Repo: "greeter"}}

// TestTurnsRelay_DrainsAfterCallerLeavesAndReattaches:
// the caller leaves mid-stream, the turn still completes and turns.result
// is logged; the same turnId again reattaches; the path must name the
// project's own repository.
func TestTurnsRelay_DrainsAfterCallerLeavesAndReattaches(t *testing.T) {
	sock := turnstest.New(t, turnstest.Script{Events: []string{`{"type":"task-op","op":"plan","output":{"ok":true}}`}, Hold: 200 * time.Millisecond})
	h := newHarness(t, withTurnSocket(sock), withProjects(greeterRepo))
	m := h.m2m() // AE-only, X-Impersonate-Org: the pod's org

	body := turnRequest("11111111-1111-5111-8111-111111111111")
	resp := h.postStream(turnsPath, m, body)
	first := readLine(t, resp) // task-op
	if !strings.Contains(first, `"task-op"`) {
		t.Fatalf("first line %q", first)
	}
	_ = resp.Body.Close() // caller leaves (aep-api kickoff budget)

	waitFor(t, func() bool {
		return h.logCount(`"msg":"turns.result"`, `"turnId":"11111111-1111-5111-8111-111111111111"`, `"status":"completed"`) == 1
	}, "turns.result not logged after the caller left")

	again := h.postStream(turnsPath, m, body)
	if again.StatusCode != http.StatusOK {
		t.Fatalf("retry = %d", again.StatusCode)
	}
	if last := lastLine(t, again); !strings.Contains(last, `"type":"result","status":"completed"`) {
		t.Fatalf("retry last line %q", last)
	}
	if sock.Started() != 1 {
		t.Fatalf("started = %d: same turnId never starts a second turn", sock.Started())
	}

	mismatch := h.postStream("/internal/v1/repos/other/greeter/turns", m, strings.Replace(body, "1111-5111", "2222-5222", 1))
	if mismatch.StatusCode != http.StatusForbidden {
		t.Fatalf("mismatch = %d: an owner other than the connected account is refused", mismatch.StatusCode)
	}
	if code := responseCode(t, mismatch); code != "owner_not_allowed" {
		t.Fatalf("mismatch code %q", code)
	}
	if sock.Started() != 1 {
		t.Fatalf("started = %d after a refused path", sock.Started())
	}
}

// TestTurnsRelay_BodyCapIsTheAgents: start-repo-turn takes what the
// agent's Turn socket takes (4 MiB), not the group's 1 MiB default. A re-plan's
// taskContext carries the open Tasks' bodies, and a 413 here is permanent to
// aep-api, which would fail the version's plan outright.
func TestTurnsRelay_BodyCapIsTheAgents(t *testing.T) {
	sock := turnstest.New(t, turnstest.Script{})
	h := newHarness(t, withTurnSocket(sock), withProjects(greeterRepo))
	m := h.m2m()
	withText := func(id string, n int) string {
		return strings.TrimSuffix(turnRequest(id), "}") + `,"text":"` + strings.Repeat("a", n) + `"}`
	}

	big := h.postStream(turnsPath, m, withText("11111111-1111-5111-8111-111111111111", 2<<20))
	if big.StatusCode != http.StatusOK {
		t.Fatalf("a 2 MiB turn = %d, want 200 (under the agent's 4 MiB cap)", big.StatusCode)
	}
	_ = lastLine(t, big)

	over := h.postStream(turnsPath, m, withText("22222222-2222-5222-8222-222222222222", 4<<20))
	defer func() { _ = over.Body.Close() }()
	if over.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("a turn over 4 MiB = %d, want 413", over.StatusCode)
	}
}

// TestTurnsRelay_DifferentTurnWhileRunningIs409: the third
// case.
func TestTurnsRelay_DifferentTurnWhileRunningIs409(t *testing.T) {
	sock := turnstest.New(t, turnstest.Script{Hold: 500 * time.Millisecond})
	h := newHarness(t, withTurnSocket(sock), withProjects(greeterRepo))
	m := h.m2m()
	running := h.postStream(turnsPath, m, turnRequest("11111111-1111-5111-8111-111111111111"))
	if running.StatusCode != http.StatusOK {
		t.Fatalf("start = %d", running.StatusCode)
	}

	other := h.postStream(turnsPath, m, turnRequest("22222222-2222-5222-8222-222222222222"))
	b, _ := io.ReadAll(other.Body)
	if other.StatusCode != http.StatusConflict || other.Header.Get("Content-Type") != "application/json" ||
		!strings.Contains(string(b), `"activeTurnId":"11111111-1111-5111-8111-111111111111"`) ||
		!strings.Contains(string(b), `"code":"turn_in_progress"`) {
		t.Fatalf("got %d %q %s", other.StatusCode, other.Header.Get("Content-Type"), b)
	}
	if h.logCount(`"msg":"turns.start"`, `"turnId":"22222222-2222-5222-8222-222222222222"`) != 0 {
		t.Fatal("a refused turn is logged as started")
	}
}

// TestTurnsRelay_Logs: turns.start and turns.result carry kind, project,
// turnId (and status) only, never the request's values.
func TestTurnsRelay_Logs(t *testing.T) {
	sock := turnstest.New(t, turnstest.GoldenScript(t, "completed.ndjson", 0))
	h := newHarness(t, withTurnSocket(sock), withProjects(greeterRepo))
	resp := h.postStream(turnsPath, h.m2m(), turnRequest("11111111-1111-5111-8111-111111111111"))
	if resp.Header.Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("content-type %q", resp.Header.Get("Content-Type"))
	}
	_ = lastLine(t, resp)
	waitFor(t, func() bool { return h.logCount(`"msg":"turns.result"`) == 1 }, "no turns.result")
	for msg, want := range map[string][]string{
		"turns.start":  {"kind", "project", "turnId"},
		"turns.result": {"kind", "project", "turnId", "status"},
	} {
		line := h.logLine(msg)
		for _, k := range []string{"time", "level", "msg"} {
			delete(line, k)
		}
		if len(line) != len(want) || line["kind"] != "start" || line["project"] != "greeter" ||
			line["turnId"] != "11111111-1111-5111-8111-111111111111" {
			t.Fatalf("%s = %v, want exactly %v", msg, line, want)
		}
	}
	if logs := h.logs(); strings.Contains(logs, "Ann") || strings.Contains(logs, "ann@x") || strings.Contains(logs, "tasks") {
		t.Fatalf("logs carry request or frame values: %s", logs)
	}
}

func TestTurnsRelay_Refusals(t *testing.T) {
	sock := turnstest.New(t, turnstest.Script{})
	h := newHarness(t, withTurnSocket(sock), withProjects(greeterRepo))
	m := h.m2m()
	id := "11111111-1111-5111-8111-111111111111"
	cases := []struct {
		name, path, token, body string
		status                  int
		code                    string
	}{
		{"user JWT", turnsPath, h.user("default", "ou-1"), turnRequest(id), 401, ""},
		{"unknown project", turnsPath, m, strings.Replace(turnRequest(id), `"greeter"`, `"nope"`, 1), 404, "project_unknown"},
		{"other repo of the owner", "/internal/v1/repos/acme-gh/other/turns", m, turnRequest(id), 404, "project_unknown"},
		{"no credit", turnsPath, m, `{"turnId":"` + id + `","project":"greeter","kind":"start"}`, 400, "validation_failed"},
		{"unknown field", turnsPath, m, strings.Replace(turnRequest(id), `"kind"`, `"x":1,"kind"`, 1), 400, "validation_failed"},
		{"turnId not a uuid", turnsPath, m, turnRequest("t-1"), 400, "validation_failed"},
		{"kind not start or plan", turnsPath, m, strings.Replace(turnRequest(id), `"start"`, `"browser"`, 1), 400, "validation_failed"},
		{"repo name the store refuses", "/internal/v1/repos/acme-gh/bad~repo/turns", m, turnRequest(id), 400, "validation_failed"},
	}
	for _, c := range cases {
		resp := h.postStream(c.path, c.token, c.body)
		if resp.StatusCode != c.status {
			t.Errorf("%s: got %d, want %d", c.name, resp.StatusCode, c.status)
			continue
		}
		if c.code != "" {
			if got := responseCode(t, resp); got != c.code {
				t.Errorf("%s: code %q, want %q", c.name, got, c.code)
			}
		}
	}
	if sock.Started() != 0 {
		t.Fatalf("started = %d: no refusal reaches the agent", sock.Started())
	}

	h.projects.SetErr(projects.ErrUnavailable)
	resp := h.postStream(turnsPath, m, turnRequest(id))
	if resp.StatusCode != http.StatusServiceUnavailable || responseCode(t, resp) != "aep_api_unavailable" {
		t.Fatalf("resolver down: %d", resp.StatusCode)
	}
}

func TestTurnsRelay_AgentUnreachable(t *testing.T) {
	h := newHarness(t, withProjects(greeterRepo)) // no Turn socket bound
	resp := h.postStream(turnsPath, h.m2m(), turnRequest("11111111-1111-5111-8111-111111111111"))
	if resp.StatusCode != http.StatusServiceUnavailable || responseCode(t, resp) != "agent_unavailable" {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

// TestInternal_RepoPathParamPattern: an owner or repo the store's segment
// rule refuses is 400 at the validator on both repo ops, not 500 later.
func TestInternal_RepoPathParamPattern(t *testing.T) {
	h := newHarness(t, withProjects(greeterRepo))
	for _, path := range []string{
		"/internal/v1/repos/acme-gh/bad~repo/references",
		"/internal/v1/repos/acme%20gh/greeter/references",
		"/internal/v1/repos/acme-gh/" + strings.Repeat("r", 201) + "/references",
	} {
		rec := h.putReferences(path, h.m2m(), false, refFile("notes.md", 10))
		wantProblem(t, rec, http.StatusBadRequest, "validation_failed")
	}
	if !h.referencesStoreEmpty() {
		t.Fatal("a refused path stored references")
	}
}

// planRequest is a plan turn of greeter with the contract's full PlanScope
// shape; at is left out when empty.
func planRequest(turnID, at string) string {
	pin := ""
	if at != "" {
		pin = `"at":"` + at + `",`
	}
	return `{"turnId":"` + turnID + `","project":"greeter","kind":"plan",` + pin +
		`"credit":{"userId":"u1","name":"Ann","email":"ann@x"},` +
		`"scope":{"tag":"v1","stories":[{"id":"F1.1","title":"Sign in","covered":false},{"id":"F2.3","covered":true}],` +
		`"features":[{"id":"F1","name":"Accounts"},{"id":"F2","needs":["F1"]}],` +
		`"productWide":[{"id":"P1","text":"Audit every write","appliesTo":["all"]},{"id":"P2","appliesTo":["F1"]}]},` +
		`"taskContext":[{"path":"tasks/1.md","body":"open"}]}`
}

// relayedRequest decodes the one body the Turn socket received.
func relayedRequest(t *testing.T, sock *turnstest.Server) map[string]any {
	t.Helper()
	reqs := sock.Requests()
	if len(reqs) != 1 {
		t.Fatalf("turn socket received %d requests, want 1", len(reqs))
	}
	var m map[string]any
	if err := json.Unmarshal(reqs[0], &m); err != nil {
		t.Fatalf("relayed body %s: %v", reqs[0], err)
	}
	return m
}

// TestTurnsRelay_PlanAtIsResolvedBeforeTheTurn: a plan turn's at (a tag or a
// sha) reaches the Turn socket as the commit it names, the rest of the
// request unchanged, so the agent never resolves a ref and an edit after the
// tag is not planned.
func TestTurnsRelay_PlanAtIsResolvedBeforeTheTurn(t *testing.T) {
	origin := repotest.NewOrigin(t, map[string]string{"specs/a.md": "v1"})
	tagged := origin.HeadSHA(t)
	origin.Tag(t, "v1", "Spec v1")
	origin.Commit(t, map[string]string{"specs/a.md": "after v1"}, "edit after the version")
	const id = "11111111-1111-5111-8111-111111111111"

	for name, at := range map[string]string{"a tag": "tags/v1", "a sha": tagged} {
		t.Run(name, func(t *testing.T) {
			sock := turnstest.New(t, turnstest.Script{})
			h := newHarness(t, withTurnSocket(sock), withProjects(greeterRepo), withGitOrigin(origin))
			resp := h.postStream(turnsPath, h.m2m(), planRequest(id, at))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("got %d", resp.StatusCode)
			}
			_ = lastLine(t, resp)

			got := relayedRequest(t, sock)
			if got["at"] != tagged {
				t.Fatalf("relayed at = %v, want the tagged commit %s", got["at"], tagged)
			}
			var sent map[string]any
			if err := json.Unmarshal([]byte(planRequest(id, at)), &sent); err != nil {
				t.Fatal(err)
			}
			delete(got, "at")
			delete(sent, "at")
			if !reflect.DeepEqual(got, sent) {
				t.Fatalf("relayed %v, want the request unchanged but for at: %v", got, sent)
			}
		})
	}

	t.Run("no at is relayed without one", func(t *testing.T) {
		sock := turnstest.New(t, turnstest.Script{})
		h := newHarness(t, withTurnSocket(sock), withProjects(greeterRepo), withGitOrigin(origin))
		resp := h.postStream(turnsPath, h.m2m(), planRequest(id, ""))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got %d", resp.StatusCode)
		}
		_ = lastLine(t, resp)
		if got := string(sock.Requests()[0]); got != planRequest(id, "") {
			t.Fatalf("relayed %s, want the request byte for byte", got)
		}
	})
}

// TestTurnsRelay_PlanAtRefusals: an at the repository lacks is a permanent
// 404 ref_not_found, a start turn may not pin, and a scope off the contract's
// shape is refused; none starts a turn.
func TestTurnsRelay_PlanAtRefusals(t *testing.T) {
	origin := repotest.NewOrigin(t, map[string]string{"specs/a.md": "v1"})
	origin.Tag(t, "v1", "Spec v1")
	sock := turnstest.New(t, turnstest.Script{})
	h := newHarness(t, withTurnSocket(sock), withProjects(greeterRepo), withGitOrigin(origin))
	const id = "11111111-1111-5111-8111-111111111111"
	start := strings.TrimSuffix(turnRequest(id), "}") + `,"at":"tags/v1"}`
	for _, c := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"unknown tag", planRequest(id, "tags/v9"), 404, "ref_not_found"},
		{"unknown sha", planRequest(id, strings.Repeat("ab", 20)), 404, "ref_not_found"},
		{"a start turn with at", start, 400, "validation_failed"},
		{"at neither a tag nor a sha", planRequest(id, "main"), 400, "validation_failed"},
		{"a numbered story", strings.Replace(planRequest(id, "tags/v1"), `"id":"F1.1"`, `"number":1`, 1), 400, "validation_failed"},
		{"a story id off the pattern", strings.Replace(planRequest(id, "tags/v1"), `"F1.1"`, `"1.1"`, 1), 400, "validation_failed"},
		{"a feature id off the pattern", strings.Replace(planRequest(id, "tags/v1"), `"id":"F1","name"`, `"id":"f1","name"`, 1), 400, "validation_failed"},
		{"an appliesTo off the pattern", strings.Replace(planRequest(id, "tags/v1"), `["all"]`, `["everything"]`, 1), 400, "validation_failed"},
		{"an unknown scope field", strings.Replace(planRequest(id, "tags/v1"), `"tag":"v1",`, `"tag":"v1","x":1,`, 1), 400, "validation_failed"},
	} {
		resp := h.postStream(turnsPath, h.m2m(), c.body)
		if resp.StatusCode != c.status {
			t.Errorf("%s: got %d, want %d", c.name, resp.StatusCode, c.status)
			continue
		}
		if got := responseCode(t, resp); got != c.code {
			t.Errorf("%s: code %q, want %q", c.name, got, c.code)
		}
	}
	if sock.Started() != 0 || len(sock.Requests()) != 0 {
		t.Fatalf("started = %d, requests = %d: no refusal reaches the agent", sock.Started(), len(sock.Requests()))
	}
}
