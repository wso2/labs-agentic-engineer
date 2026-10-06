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

package aestudiotools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

const turnID = "6f1a2c1e-6c39-4f0e-9a51-7a1d0e5a6b10"

func startReq() TurnRequest {
	return TurnRequest{
		TurnID:  turnID,
		Project: "greeter",
		Kind:    "start",
		Credit:  Credit{UserID: "u-1", Name: "Ada", Email: "ada@example.com"},
		Text:    "a greeting service",
	}
}

// ndjsonServer answers POST /repos/acme/greeter/turns with lines, flushing
// each one, after recording the request body.
func ndjsonServer(t *testing.T, gotBody *map[string]any, lines ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/greeter/turns" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if gotBody != nil {
			_ = json.NewDecoder(r.Body).Decode(gotBody)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		for _, l := range lines {
			_, _ = io.WriteString(w, l+"\n")
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func collect(t *testing.T, seq iter.Seq2[TurnEvent, error]) ([]TurnEvent, error) {
	t.Helper()
	var evs []TurnEvent
	for ev, err := range seq {
		if err != nil {
			return evs, err
		}
		evs = append(evs, ev)
	}
	return evs, nil
}

func TestStartTurn_StreamsEventsInOrderKeepAlivesIncluded(t *testing.T) {
	var body map[string]any
	srv := ndjsonServer(t, &body,
		`{"type":"task-op","op":"plan","output":{"title":"one"}}`,
		`{"type":"keep-alive"}`,
		``,
		`{"type":"task-op","op":"update","output":{"number":2}}`,
		`{"type":"result","status":"failed","code":"stream-died","message":"agent went away"}`,
	)
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	seq, err := a.StartTurn(context.Background(), acmeGreeter, startReq())
	if err != nil {
		t.Fatal(err)
	}
	evs, err := collect(t, seq)
	if err != nil {
		t.Fatal(err)
	}
	want := []TurnEvent{
		{Type: "task-op", Op: "plan", Output: json.RawMessage(`{"title":"one"}`)},
		{Type: "keep-alive"},
		{Type: "task-op", Op: "update", Output: json.RawMessage(`{"number":2}`)},
		{Type: "result", Status: "failed", Code: "stream-died"},
	}
	if len(evs) != len(want) {
		t.Fatalf("events = %+v, want %d (keep-alive yielded, D-2)", evs, len(want))
	}
	for i := range want {
		if evs[i].Type != want[i].Type || evs[i].Op != want[i].Op || string(evs[i].Output) != string(want[i].Output) ||
			evs[i].Status != want[i].Status || evs[i].Code != want[i].Code {
			t.Fatalf("event %d = %+v, want %+v", i, evs[i], want[i])
		}
	}
	// The result's free-text message is the agent's (possibly a provider's
	// error body): the adapter does not read it, so it cannot reach an error,
	// a log line or Temporal history.
	if strings.Contains(fmt.Sprintf("%+v", evs[3]), "agent went away") {
		t.Fatalf("result event %+v carries the pod's free-text message", evs[3])
	}
	if body["turnId"] != turnID || body["project"] != "greeter" || body["kind"] != "start" || body["text"] != "a greeting service" {
		t.Fatalf("request body = %v", body)
	}
	if c, _ := body["credit"].(map[string]any); c["userId"] != "u-1" || c["name"] != "Ada" || c["email"] != "ada@example.com" {
		t.Fatalf("credit = %v", body["credit"])
	}
	for _, absent := range []string{"scope", "taskContext"} {
		if _, ok := body[absent]; ok {
			t.Fatalf("a start turn must not send %q (the spec refuses an empty scope): %v", absent, body)
		}
	}
}

func TestStartTurn_PlanSendsScopeAndTaskContext(t *testing.T) {
	var body map[string]any
	srv := ndjsonServer(t, &body, `{"type":"result","status":"completed"}`)
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	req := TurnRequest{
		TurnID: turnID, Project: "greeter", Kind: "plan",
		Scope:       &PlanScope{Tag: "m1", Stories: []PlanStory{{Number: 1, Title: "Greet", Covered: true}, {Number: 2}}},
		TaskContext: []PlanContextFile{{Path: "tasks/1.md", Body: "# one"}},
	}
	seq, err := a.StartTurn(context.Background(), acmeGreeter, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collect(t, seq); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body)
	for _, want := range []string{
		`"scope":{"stories":[{"covered":true,"number":1,"title":"Greet"},{"covered":false,"number":2}],"tag":"m1"}`,
		`"taskContext":[{"body":"# one","path":"tasks/1.md"}]`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("body %s lacks %s", raw, want)
		}
	}
}

func TestStartTurn_Refusals(t *testing.T) {
	for _, tc := range []struct {
		name      string
		reply     func(w http.ResponseWriter)
		want      error
		code      string
		permanent bool
	}{
		{name: "409 turn_in_progress", reply: func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"turn_in_progress","activeTurnId":"0d1f8a8e-1f2a-4c1e-9a51-7a1d0e5a6b10"}`))
		}, want: ErrTurnInProgress},
		{name: "503 disk_full", reply: func(w http.ResponseWriter) { writeProblem(w, 503, "disk_full", "") }, want: sourcecontrol.ErrAEStudioUnavailable},
		{name: "503 agent_unavailable", reply: func(w http.ResponseWriter) { writeProblem(w, 503, "agent_unavailable", "") }, want: sourcecontrol.ErrAEStudioUnavailable},
		{name: "502 from the gateway", reply: func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) }, want: sourcecontrol.ErrAEStudioUnavailable},
		{name: "404 project_unknown", reply: func(w http.ResponseWriter) { writeProblem(w, 404, "project_unknown", "") }, want: sourcecontrol.ErrRepoNotFound, permanent: true},
		{name: "409 no_default_key", reply: func(w http.ResponseWriter) { writeProblem(w, 409, "no_default_key", "") }, code: "no_default_key", permanent: true},
		{name: "400 validation_failed", reply: func(w http.ResponseWriter) { writeProblem(w, 400, "validation_failed", "kind") }, code: "validation_failed", permanent: true},
		{name: "502 agent_error", reply: func(w http.ResponseWriter) { writeProblem(w, 502, "agent_error", "") }, code: "agent_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { tc.reply(w) }))
			defer srv.Close()
			a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
			seq, err := a.StartTurn(context.Background(), acmeGreeter, startReq())
			if seq != nil {
				t.Fatal("a refused turn has no stream")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.code != "" {
				var se *StatusError
				if !errors.As(err, &se) || se.Code != tc.code {
					t.Fatalf("err = %v, want a StatusError with code %s", err, tc.code)
				}
			}
			if sourcecontrol.IsPermanent(err) != tc.permanent {
				t.Fatalf("sourcecontrol.IsPermanent(%v) = %v, want %v", err, sourcecontrol.IsPermanent(err), tc.permanent)
			}
		})
	}
}

func TestStartTurn_StreamCutBeforeResultIsUnavailable(t *testing.T) {
	srv := ndjsonServer(t, nil, `{"type":"task-op","op":"plan","output":{}}`)
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	seq, err := a.StartTurn(context.Background(), acmeGreeter, startReq())
	if err != nil {
		t.Fatal(err)
	}
	evs, err := collect(t, seq)
	if len(evs) != 1 || !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Fatalf("events=%d err=%v, want the one event then sourcecontrol.ErrAEStudioUnavailable", len(evs), err)
	}
}

func TestStartTurn_MalformedLineEndsTheStream(t *testing.T) {
	srv := ndjsonServer(t, nil, `{"type":"task-op"`, `{"type":"result","status":"completed"}`)
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	seq, err := a.StartTurn(context.Background(), acmeGreeter, startReq())
	if err != nil {
		t.Fatal(err)
	}
	if evs, err := collect(t, seq); len(evs) != 0 || err == nil {
		t.Fatalf("events=%v err=%v, want an error on the malformed line", evs, err)
	}
}

// The stream runs up to 30 min; the per-call timeout of the unary
// ops must not cut it, only the caller's ctx may.
func TestStartTurn_StreamOutlivesTheCallTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		for range 4 {
			_, _ = io.WriteString(w, `{"type":"keep-alive"}`+"\n")
			w.(http.Flusher).Flush()
			time.Sleep(500 * time.Millisecond)
		}
		_, _ = io.WriteString(w, `{"type":"result","status":"completed"}`+"\n")
	}))
	defer srv.Close()
	a := New(Config{Endpoints: fixedTarget(srv.URL, "ou-123"), Tokens: &countingTokens{}, CallTimeout: time.Second})
	seq, err := a.StartTurn(context.Background(), acmeGreeter, startReq())
	if err != nil {
		t.Fatal(err)
	}
	evs, err := collect(t, seq)
	if err != nil || len(evs) != 5 || evs[4].Type != "result" {
		t.Fatalf("events=%d err=%v, want the whole 2 s stream", len(evs), err)
	}
}

func TestStartTurn_BreakingOffClosesTheStream(t *testing.T) {
	gone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, `{"type":"keep-alive"}`+"\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(gone)
	}))
	defer srv.Close()
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	seq, err := a.StartTurn(context.Background(), acmeGreeter, startReq())
	if err != nil {
		t.Fatal(err)
	}
	for range seq {
		break
	}
	select {
	case <-gone:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection stayed open after the caller stopped reading")
	}
}

func TestStartTurn_RejectsABadRequestWithoutACall(t *testing.T) {
	srv := identityServer(t, func(http.ResponseWriter, *http.Request, int) { t.Error("no call for a bad request") })
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	bad := startReq()
	bad.TurnID = "not-a-uuid"
	if _, err := a.StartTurn(context.Background(), acmeGreeter, bad); err == nil {
		t.Fatal("want an error for a non-uuid turnId")
	}
	if _, err := a.StartTurn(context.Background(), RepoRef{Org: "default", Owner: "acme"}, startReq()); err == nil {
		t.Fatal("want an error for a RepoRef without a repo")
	}
}

// The caller's own deadline ending a stream (the kickoff's 20 s budget, an
// activity's timeout) is the caller's: the pod answered, so its Target stays
// cached and the error is the context's, never ErrAEStudioUnavailable.
func TestStartTurn_TheCallersDeadlineIsNotUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, `{"type":"keep-alive"}`+"\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ep := fixedTarget(srv.URL, "ou-123")
	a := newAdapter(t, ep, &countingTokens{})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	seq, err := a.StartTurn(ctx, acmeGreeter, startReq())
	if err != nil {
		t.Fatal(err)
	}
	evs, err := collect(t, seq)
	if len(evs) != 1 || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Fatalf("events=%d err=%v, want the keep-alive then the caller's deadline", len(evs), err)
	}
	seq, err = a.StartTurn(context.Background(), acmeGreeter, startReq())
	if err != nil {
		t.Fatal(err)
	}
	for range seq {
		break
	}
	if ep.count() != 1 {
		t.Fatalf("resolve calls = %d, want 1 (the Target survives the caller's deadline)", ep.count())
	}
}

// A provider_limit result may say when the provider's limit resets. The
// adapter reads it, so the planning activity can wait that long rather than
// a fixed delay; a value that is not an RFC 3339 time is dropped (the fixed
// delay applies), never a broken stream.
func TestStartTurn_TheResultCarriesTheProvidersResetTime(t *testing.T) {
	for name, tc := range map[string]struct {
		line string
		want time.Time
	}{
		"stated":    {`{"type":"result","status":"failed","code":"provider_limit","resetAt":"2026-10-04T10:15:00Z"}`, time.Date(2026, 10, 4, 10, 15, 0, 0, time.UTC)},
		"absent":    {`{"type":"result","status":"failed","code":"provider_limit"}`, time.Time{}},
		"malformed": {`{"type":"result","status":"failed","code":"provider_limit","resetAt":"soon"}`, time.Time{}},
	} {
		t.Run(name, func(t *testing.T) {
			srv := ndjsonServer(t, nil, tc.line)
			a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
			seq, err := a.StartTurn(context.Background(), acmeGreeter, startReq())
			if err != nil {
				t.Fatal(err)
			}
			evs, err := collect(t, seq)
			if err != nil {
				t.Fatal(err)
			}
			if len(evs) != 1 || evs[0].Code != TurnCodeProviderLimit || !evs[0].ResetAt.Equal(tc.want) {
				t.Fatalf("events = %+v, want one provider_limit result with resetAt %v", evs, tc.want)
			}
		})
	}
}
