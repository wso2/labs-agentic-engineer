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

package turns

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/turns/turnstest"
)

const testTurnID = "11111111-1111-5111-8111-111111111111"

// syncBuffer is a log sink safe for the relay's goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type relayHarness struct {
	t    *testing.T
	url  string
	logs *syncBuffer
}

// newRelayHarness serves Relay over the Turn socket at sock, for the turn
// the request body names (kind start, project greeter).
func newRelayHarness(t *testing.T, sock string) *relayHarness {
	t.Helper()
	logs := &syncBuffer{}
	rl := Relay{Turns: NewClient(sock), Log: slog.New(slog.NewJSONHandler(logs, nil))}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct{ TurnID, Project, Kind string }
		_ = json.Unmarshal(body, &req)
		rl.Serve(w, r, Turn{ID: req.TurnID, Project: req.Project, Kind: req.Kind, Body: body})
	}))
	t.Cleanup(srv.Close)
	return &relayHarness{t: t, url: srv.URL, logs: logs}
}

func turnBody(id string) string {
	return `{"turnId":"` + id + `","project":"greeter","kind":"start","credit":{"userId":"u1","name":"Ann","email":"ann@x"}}`
}

func (h *relayHarness) post(body string) *http.Response {
	h.t.Helper()
	resp, err := http.Post(h.url, "application/json", strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func readAllLines(t *testing.T, resp *http.Response) []string {
	t.Helper()
	var lines []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

// logCount counts the log lines carrying every fragment.
func (h *relayHarness) logCount(fragments ...string) int {
	n := 0
	for _, line := range strings.Split(h.logs.String(), "\n") {
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

func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRelay_StreamsEveryFrameUnchanged(t *testing.T) {
	golden := turnstest.Golden(t, "completed.ndjson")
	sock := turnstest.New(t, turnstest.GoldenScript(t, "completed.ndjson", 0))
	h := newRelayHarness(t, sock.Path())

	resp := h.post(turnBody(testTurnID))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("got %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	got := readAllLines(t, resp)
	if strings.Join(got, "\n") != strings.Join(golden, "\n") {
		t.Fatalf("relayed\n%s\nwant (keep-alive lines included, D-2)\n%s", strings.Join(got, "\n"), strings.Join(golden, "\n"))
	}
	eventually(t, func() bool {
		return h.logCount(`"msg":"turns.result"`, `"turnId":"`+testTurnID+`"`, `"status":"completed"`, `"kind":"start"`, `"project":"greeter"`) == 1
	}, "turns.result {kind, project, turnId, status} not logged")
	if h.logCount(`"msg":"turns.start"`, `"turnId":"`+testTurnID+`"`, `"kind":"start"`, `"project":"greeter"`) != 1 {
		t.Fatalf("turns.start not logged: %s", h.logs)
	}
	if strings.Contains(h.logs.String(), "Ann") || strings.Contains(h.logs.String(), "ann@x") || strings.Contains(h.logs.String(), "tasks") {
		t.Fatalf("logs carry request or frame values: %s", h.logs)
	}
}

func TestRelay_CallerLeavesTurnStillDrainedAndLogged(t *testing.T) {
	sock := turnstest.New(t, turnstest.Script{
		Events: []string{`{"type":"task-op","op":"plan","output":{"ok":true}}`},
		Hold:   300 * time.Millisecond,
	})
	h := newRelayHarness(t, sock.Path())

	resp := h.post(turnBody(testTurnID))
	first, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || !strings.Contains(first, `"task-op"`) {
		t.Fatalf("first line %q, %v", first, err)
	}
	_ = resp.Body.Close() // the caller leaves before the result
	eventually(t, func() bool {
		return h.logCount(`"msg":"turns.result"`, `"turnId":"`+testTurnID+`"`, `"status":"completed"`) == 1
	}, "the relay stopped draining when the caller left")
	if sock.Started() != 1 {
		t.Fatalf("started = %d", sock.Started())
	}
}

func TestRelay_FailedResultIsLoggedAsFailed(t *testing.T) {
	sock := turnstest.New(t, turnstest.GoldenScript(t, "shutdown.ndjson", 0))
	h := newRelayHarness(t, sock.Path())
	lines := readAllLines(t, h.post(turnBody(testTurnID)))
	if last := lines[len(lines)-1]; !strings.Contains(last, `"code":"shutdown"`) {
		t.Fatalf("last line %q", last)
	}
	eventually(t, func() bool {
		return h.logCount(`"msg":"turns.result"`, `"status":"failed"`) == 1
	}, "failed result not logged")
}

func TestRelay_StreamCutBeforeResult(t *testing.T) {
	sock := turnstest.ServeHandler(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, `{"type":"task-op","op":"plan","output":{}}`+"\n")
		// The agent goes away with no result frame.
	}))
	h := newRelayHarness(t, sock)
	lines := readAllLines(t, h.post(turnBody(testTurnID)))
	if len(lines) != 2 || lines[1] != `{"type":"result","status":"failed","code":"stream-died"}` {
		t.Fatalf("lines = %q, want the frame then a stream-died result", lines)
	}
	eventually(t, func() bool {
		return h.logCount(`"msg":"turns.result"`, `"status":"failed"`) == 1
	}, "a cut stream is logged as failed")
}

func TestRelay_SocketRefusals(t *testing.T) {
	const active = "22222222-2222-5222-8222-222222222222"
	cases := []struct {
		name               string
		status             int
		contentType, body  string
		wantStatus         int
		wantCT, wantInBody string
	}{
		{"turn in progress", 409, "application/json", `{"code":"turn_in_progress","activeTurnId":"` + active + `"}`,
			409, "application/json", `{"activeTurnId":"` + active + `","code":"turn_in_progress"}`},
		{"no default key", 409, "application/problem+json", `{"type":"about:blank","title":"Conflict","status":409,"code":"no_default_key"}`,
			409, "application/problem+json", `"code":"no_default_key"`},
		{"project unknown", 404, "application/problem+json", `{"type":"about:blank","title":"Not Found","status":404,"code":"project_unknown"}`,
			404, "application/problem+json", `"code":"project_unknown"`},
		{"shutting down", 503, "application/problem+json", `{"type":"about:blank","title":"Service Unavailable","status":503,"code":"shutting_down"}`,
			503, "application/problem+json", `"code":"shutting_down"`},
		{"invalid turn", 400, "application/problem+json", `{"type":"about:blank","title":"Bad Request","status":400,"code":"invalid_turn"}`,
			400, "application/problem+json", `"code":"invalid_turn"`},
		{"undeclared status", 500, "text/plain", `boom`,
			502, "application/problem+json", `"code":"agent_error"`},
		{"409 that is neither shape", 409, "application/json", `{"nope":true}`,
			502, "application/problem+json", `"code":"agent_error"`},
		{"problem without a code", 503, "application/problem+json", `{"status":503}`,
			502, "application/problem+json", `"code":"agent_error"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sock := turnstest.ServeHandler(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", c.contentType)
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			}))
			h := newRelayHarness(t, sock)
			resp := h.post(turnBody(testTurnID))
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != c.wantStatus || resp.Header.Get("Content-Type") != c.wantCT || !strings.Contains(string(body), c.wantInBody) {
				t.Fatalf("got %d %q %s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
			}
			if h.logCount(`"msg":"turns.start"`) != 0 || h.logCount(`"msg":"turns.result"`) != 0 {
				t.Fatalf("a refused turn is not logged as started: %s", h.logs)
			}
		})
	}
}

func TestRelay_SocketUnreachable(t *testing.T) {
	h := newRelayHarness(t, filepath.Join(t.TempDir(), "absent.sock"))
	resp := h.post(turnBody(testTurnID))
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), `"code":"agent_unavailable"`) {
		t.Fatalf("got %d %s", resp.StatusCode, body)
	}
	if h.logCount(`"msg":"turns.socket_failed"`) != 1 {
		t.Fatalf("socket failure not logged: %s", h.logs)
	}
}
