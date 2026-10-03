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

// Package turnstest is a fake Turn socket (packages/contracts/sockets/ae-studio/turn),
// the one ae-design-agent serves, for the tests of its callers. It follows the
// spec's turn rules: a turn runs on its own, whether or not anyone is reading
// its stream; a request with a turnId the fake has seen reattaches (it is
// streamed every frame so far, then follows the turn, or gets the finished
// stream whole); a different turnId while a turn runs for the project is 409
// turn_in_progress {activeTurnId}.
//
// The streams it plays are the golden lines kept beside the spec
// (packages/contracts/sockets/ae-studio/turn/golden/*.ndjson, Golden), which
// ae-design-agent's own Turn socket test asserts it emits.
package turnstest

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// CompletedResult is the result frame a Script ends with unless it names one.
const CompletedResult = `{"type":"result","status":"completed"}`

// Script is what every turn the fake starts plays: Events in order, then,
// after Hold, Result.
type Script struct {
	Events []string
	// Result is the last frame; CompletedResult when empty.
	Result string
	// Hold is how long the turn runs between its last event and its result.
	Hold time.Duration
}

// Server is a running fake Turn socket.
type Server struct {
	path   string
	script Script

	mu      sync.Mutex
	started int
	turns   map[string]*turn  // by turnId, kept after the turn ends
	active  map[string]string // project → its running turnId
	running sync.WaitGroup
}

// turn is one turn's frames so far. changed is closed and replaced on every
// append, so a stream can wait for the next frame.
type turn struct {
	mu      sync.Mutex
	frames  []string
	done    bool
	changed chan struct{}
}

// New serves the fake on a fresh socket until the test ends.
func New(t *testing.T, s Script) *Server {
	t.Helper()
	if s.Result == "" {
		s.Result = CompletedResult
	}
	srv := &Server{script: s, turns: map[string]*turn{}, active: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /turns", srv.startTurn)
	srv.path = ServeHandler(t, mux)
	t.Cleanup(srv.running.Wait)
	return srv
}

// Path is the socket's path.
func (s *Server) Path() string { return s.path }

// Started counts the turns the fake started (reattaches not counted).
func (s *Server) Started() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// ServeHandler serves h on a fresh Unix socket until the test ends and
// answers its path. The directory is short: a socket path is limited to about
// 100 bytes, which t.TempDir() can exceed.
func ServeHandler(t *testing.T, h http.Handler) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "aets")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "turn.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return path
}

// Golden answers the frames of a golden stream, one per line.
func Golden(t *testing.T, name string) []string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("turnstest: cannot locate its own source")
	}
	// turnstest → turns → internal → ae-studio-tools → ae-studio →
	// ae-system-project → dataplane → components → repository root.
	path := filepath.Join(filepath.Dir(self), "../../../../../../../..", GoldenDir, name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

// GoldenDir is where the golden streams live, from the repository root.
const GoldenDir = "packages/contracts/sockets/ae-studio/turn/golden"

// GoldenScript plays a golden stream: its last line is the result, held back
// by hold.
func GoldenScript(t *testing.T, name string, hold time.Duration) Script {
	t.Helper()
	lines := Golden(t, name)
	if len(lines) == 0 {
		t.Fatalf("golden %s is empty", name)
	}
	return Script{Events: lines[:len(lines)-1], Result: lines[len(lines)-1], Hold: hold}
}

func (s *Server) startTurn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TurnID  string `json:"turnId"`
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TurnID == "" || req.Project == "" {
		writeProblem(w, http.StatusBadRequest, "invalid_turn")
		return
	}
	s.mu.Lock()
	tn, seen := s.turns[req.TurnID]
	if !seen {
		if activeID := s.active[req.Project]; activeID != "" {
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "turn_in_progress", "activeTurnId": activeID})
			return
		}
		tn = &turn{changed: make(chan struct{})}
		s.turns[req.TurnID] = tn
		s.active[req.Project] = req.TurnID
		s.started++
		s.running.Add(1)
		go s.play(req.Project, tn)
	}
	s.mu.Unlock()
	stream(w, r, tn)
}

// play runs one turn to its end, independent of any request.
func (s *Server) play(project string, tn *turn) {
	defer s.running.Done()
	for _, e := range s.script.Events {
		tn.append(e, false)
	}
	time.Sleep(s.script.Hold)
	s.mu.Lock()
	delete(s.active, project)
	s.mu.Unlock()
	tn.append(s.script.Result, true)
}

func (tn *turn) append(frame string, last bool) {
	tn.mu.Lock()
	defer tn.mu.Unlock()
	tn.frames = append(tn.frames, frame)
	tn.done = last
	close(tn.changed)
	tn.changed = make(chan struct{})
}

// stream writes the turn's frames from the first, following it until its
// result or until the caller goes away.
func stream(w http.ResponseWriter, r *http.Request, tn *turn) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	_ = rc.Flush()
	next := 0
	for {
		tn.mu.Lock()
		pending := tn.frames[next:]
		done, changed := tn.done, tn.changed
		tn.mu.Unlock()
		for _, f := range pending {
			if _, err := w.Write([]byte(f + "\n")); err != nil {
				return
			}
			next++
		}
		_ = rc.Flush()
		if done {
			return
		}
		select {
		case <-changed:
		case <-r.Context().Done():
			return
		}
	}
}

func writeProblem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code,
	})
}
