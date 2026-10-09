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

package webhook

// UNIT tier: the Router dispatch algebra — the (event, action) → handler
// lookup that decides which per-event handler (if any) a verified, dedup'd
// delivery reaches. Pure in-process; no HTTP, no DB.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// recordingHandler captures the (event, action, payload) a Dispatch delivered
// and returns the configured error, so a test asserts both invocation and
// error propagation.
//
// The receiver runs handlers off the request goroutine, so the recorder is
// safe for concurrent use: count/call/setErr read and write under its lock.
// When block is set, Handle waits on it (or on its own context) before
// returning, and reports what its context said at that moment in ctxErrs.
type recordingHandler struct {
	mu      sync.Mutex
	calls   []dispatchCall
	err     error
	block   chan struct{}
	ctxErrs []error
}

func (h *recordingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.calls)
}

func (h *recordingHandler) call(i int) dispatchCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls[i]
}

func (h *recordingHandler) setErr(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.err = err
}

type dispatchCall struct {
	event, action string
	payload       string
	// receivedAt is what ReceivedAt said in the handler's context.
	receivedAt time.Time
}

func (h *recordingHandler) Handle(ctx context.Context, event, action string, payload []byte) error {
	h.mu.Lock()
	receivedAt, _ := ReceivedAt(ctx)
	h.calls = append(h.calls, dispatchCall{event: event, action: action, payload: string(payload), receivedAt: receivedAt})
	block := h.block
	h.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ctxErrs = append(h.ctxErrs, ctx.Err())
	return h.err
}

func TestRouter_Dispatch_ActionSpecificHandlerWins(t *testing.T) {
	t.Parallel()
	r := NewRouter()
	specific := &recordingHandler{}
	fallback := &recordingHandler{}
	r.Register("pull_request", "opened", specific)
	r.Register("pull_request", "", fallback) // fallback for the event class

	body := []byte(`{"action":"opened","number":7}`)
	if err := r.Dispatch(context.Background(), "pull_request", body); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if len(specific.calls) != 1 {
		t.Fatalf("action-specific handler must fire exactly once, got %d", len(specific.calls))
	}
	if len(fallback.calls) != 0 {
		t.Fatalf("fallback must NOT fire when an action-specific handler matches, got %d", len(fallback.calls))
	}
	got := specific.calls[0]
	if got.event != "pull_request" || got.action != "opened" || got.payload != string(body) {
		t.Fatalf("handler received (event=%q action=%q payload=%q); want the parsed action + raw payload", got.event, got.action, got.payload)
	}
}

func TestRouter_Dispatch_FallsBackToEventHandler(t *testing.T) {
	t.Parallel()
	r := NewRouter()
	fallback := &recordingHandler{}
	r.Register("installation", "", fallback)

	// No handler registered for action "suspend"; the (event, "") fallback runs.
	if err := r.Dispatch(context.Background(), "installation", []byte(`{"action":"suspend"}`)); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(fallback.calls) != 1 {
		t.Fatalf("fallback must fire when no action-specific handler matches, got %d", len(fallback.calls))
	}
	// The parsed action still rides through to the fallback handler.
	if fallback.calls[0].action != "suspend" {
		t.Fatalf("fallback action = %q; want the parsed action %q", fallback.calls[0].action, "suspend")
	}
}

func TestRouter_Dispatch_UnregisteredEventIsSilentNoop(t *testing.T) {
	t.Parallel()
	r := NewRouter()
	handler := &recordingHandler{}
	r.Register("push", "", handler)

	// An event with no registered handler is intentionally swallowed: no error
	// (so the receiver acks 200) and no handler invoked.
	if err := r.Dispatch(context.Background(), "star", []byte(`{"action":"created"}`)); err != nil {
		t.Fatalf("unregistered event must be a no-op, got error %v", err)
	}
	if len(handler.calls) != 0 {
		t.Fatalf("no handler should fire for an unregistered event, got %d", len(handler.calls))
	}
}

func TestRouter_Dispatch_PropagatesHandlerError(t *testing.T) {
	t.Parallel()
	r := NewRouter()
	sentinel := errors.New("handler boom")
	r.Register("push", "", &recordingHandler{err: sentinel})

	// Dispatch returns the handler error verbatim — the receiver turns a non-nil
	// error into a 5xx so GitHub redelivers.
	if err := r.Dispatch(context.Background(), "push", []byte(`{}`)); !errors.Is(err, sentinel) {
		t.Fatalf("Dispatch must return the handler error verbatim, got %v", err)
	}
}

func TestRouter_Dispatch_EventHandlerFuncAdapter(t *testing.T) {
	t.Parallel()
	r := NewRouter()
	var fired bool
	r.Register("push", "", EventHandlerFunc(func(_ context.Context, event, _ string, _ []byte) error {
		fired = event == "push"
		return nil
	}))
	if err := r.Dispatch(context.Background(), "push", []byte(`{}`)); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !fired {
		t.Fatal("EventHandlerFunc adapter must invoke the wrapped function")
	}
}

func TestRouter_Dispatch_ChainsMultipleHandlers(t *testing.T) {
	t.Parallel()
	r := NewRouter()
	a := &recordingHandler{}
	b := &recordingHandler{}
	// Two features independently subscribe to issues/closed (e.g. task's noop +
	// provisioning's decline-reject).
	r.Register("issues", "closed", a)
	r.Register("issues", "closed", b)

	if err := r.Dispatch(context.Background(), "issues", []byte(`{"action":"closed"}`)); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(a.calls) != 1 || len(b.calls) != 1 {
		t.Fatalf("both registered handlers must fire once; got a=%d b=%d", len(a.calls), len(b.calls))
	}
}

func TestRouter_Dispatch_ChainAggregatesErrorsAndRunsAll(t *testing.T) {
	t.Parallel()
	r := NewRouter()
	boom := errors.New("boom")
	after := &recordingHandler{}
	r.Register("issues", "closed", &recordingHandler{err: boom})
	r.Register("issues", "closed", after)

	err := r.Dispatch(context.Background(), "issues", []byte(`{"action":"closed"}`))
	if !errors.Is(err, boom) {
		t.Fatalf("aggregate must preserve errors.Is on a failing handler, got %v", err)
	}
	if len(after.calls) != 1 {
		t.Fatalf("a handler error must NOT skip later handlers; got %d", len(after.calls))
	}
}

func TestParseAction(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		payload string
		want    string
	}{
		"present":      {`{"action":"opened"}`, "opened"},
		"absent":       {`{"number":7}`, ""},
		"invalid json": {`not json`, ""},
		"empty body":   {``, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := parseAction([]byte(tc.payload)); got != tc.want {
				t.Fatalf("parseAction(%q) = %q; want %q", tc.payload, got, tc.want)
			}
		})
	}
}

func TestRouterKey(t *testing.T) {
	t.Parallel()
	// action-less key is bare event; action-scoped key is event:action, so a
	// (push) fallback and a (pull_request:opened) specific never collide.
	if got := key("push", ""); got != "push" {
		t.Fatalf(`key("push","") = %q; want "push"`, got)
	}
	if got := key("pull_request", "opened"); got != "pull_request:opened" {
		t.Fatalf(`key("pull_request","opened") = %q; want "pull_request:opened"`, got)
	}
}
