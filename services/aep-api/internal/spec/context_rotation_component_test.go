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

package spec_test

// Component tier for rotation near a smaller context window: the turn records
// its closing context off the last finish-step, and the next send to a
// conversation past 80% of the connection's window rotates the project's
// thread and answers 409 conversation_rotated. The store's conditional
// rotate under concurrency is pinned at the DB tier
// (repository_conversation_dbtest_test.go).

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// finishStepPart is the AI SDK's finish-step part as the agents service
// relays it: that one step's usage, whose inputTokens is the whole prompt.
func finishStepPart(inputTokens, outputTokens int64) string {
	b, _ := json.Marshal(map[string]any{
		"type": "finish-step", "finishReason": "stop",
		"usage": map[string]any{
			"inputTokens":       inputTokens,
			"inputTokenDetails": map[string]any{"noCacheTokens": inputTokens},
			"outputTokens":      outputTokens,
		},
	})
	return string(b)
}

func newRotationRig(t *testing.T) *genaiRig {
	t.Helper()
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}))
	m := manifestPart(map[string]string{}, nil)
	r.fake.manifest = &m
	return r
}

// runMeasuredTurn runs one completed turn whose steps end at the given
// contexts, and returns the context the row recorded.
func runMeasuredTurn(t *testing.T, r *genaiRig, conversationID string, steps ...[2]int64) *int64 {
	t.Helper()
	r.fake.mu.Lock()
	r.fake.parts = nil
	for _, s := range steps {
		r.fake.parts = append(r.fake.parts, finishStepPart(s[0], s[1]))
	}
	r.fake.mu.Unlock()
	turnID := r.startTurn(t, conversationID, "general", "hello")
	if st := r.waitTerminal(t, turnID); st.Status != "completed" {
		t.Fatalf("measured turn = %q, want completed (%s)", st.Status, st.Message)
	}
	return r.turns.row(t, turnID).ContextTokens
}

func window(n int) *int { return &n }

func TestContextRotation_RotatesPastEightyPercent(t *testing.T) {
	r := newRotationRig(t)
	r.contextWindow = window(100_000)
	current := listConversations(t, r)[0].ConversationID

	// The LAST step is the measure, not the sum of steps.
	got := runMeasuredTurn(t, r, current, [2]int64{40_000, 1_000}, [2]int64{68_000, 2_000})
	if got == nil || *got != 70_000 {
		t.Fatalf("recorded context = %v, want 70000 (the last step's prompt + output)", got)
	}
	// 70% of the window: the next send runs on the same thread and ends at 85%.
	if got := runMeasuredTurn(t, r, current, [2]int64{83_000, 2_000}); got == nil || *got != 85_000 {
		t.Fatalf("recorded context = %v, want 85000", got)
	}

	dispatched := r.fake.turns(t)
	rec := r.post(t, current, "general", "and another thing")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "conversation_rotated") {
		t.Fatalf("send to a full thread: code %d (%s), want 409 conversation_rotated", rec.Code, rec.Body.String())
	}
	if r.fake.turns(t) != dispatched {
		t.Error("the send to a full thread was dispatched")
	}
	fresh := listConversations(t, r)[0].ConversationID
	if fresh == current {
		t.Fatal("the full thread is still current")
	}
	// The fresh thread has no measured turn: the resend runs.
	runMeasuredTurn(t, r, fresh, [2]int64{5_000, 500})
}

func TestContextRotation_AtEightyPercentDoesNotRotate(t *testing.T) {
	r := newRotationRig(t)
	r.contextWindow = window(100_000)
	current := listConversations(t, r)[0].ConversationID

	runMeasuredTurn(t, r, current, [2]int64{79_000, 1_000})
	// Exactly 80% is not past it.
	runMeasuredTurn(t, r, current, [2]int64{1_000, 100})
	if got := listConversations(t, r)[0].ConversationID; got != current {
		t.Fatalf("thread rotated at exactly 80%%: now %q", got)
	}
}

// First-party Anthropic states no window: the conversation never rotates,
// and the check issues no query at all.
func TestContextRotation_NoWindowNeverRotatesOrQueries(t *testing.T) {
	r := newRotationRig(t)
	current := listConversations(t, r)[0].ConversationID

	runMeasuredTurn(t, r, current, [2]int64{990_000, 9_000})
	runMeasuredTurn(t, r, current, [2]int64{995_000, 4_000})
	if got := listConversations(t, r)[0].ConversationID; got != current {
		t.Fatalf("thread rotated with no context window: now %q", got)
	}
	r.turns.mu.Lock()
	reads := r.turns.contextReads
	r.turns.mu.Unlock()
	if reads != 0 {
		t.Fatalf("LastContextTokens called %d time(s) on a connection with no window, want 0", reads)
	}
}

// A turn that ends without a manifest was not saved into the conversation, so
// however far its steps got, it measures nothing.
func TestContextRotation_UnvouchedTurnMeasuresNothing(t *testing.T) {
	r := newRotationRig(t)
	r.contextWindow = window(100_000)
	current := listConversations(t, r)[0].ConversationID

	r.fake.mu.Lock()
	r.fake.parts = []string{finishStepPart(95_000, 1_000)}
	r.fake.sever = true
	r.fake.mu.Unlock()
	turnID := r.startTurn(t, current, "general", "hello")
	if st := r.waitTerminal(t, turnID); st.Status != "failed" {
		t.Fatalf("severed turn = %q, want failed", st.Status)
	}
	if got := r.turns.row(t, turnID).ContextTokens; got != nil {
		t.Fatalf("severed turn recorded context %d, want none", *got)
	}
	r.fake.mu.Lock()
	r.fake.sever = false
	r.fake.mu.Unlock()
	runMeasuredTurn(t, r, current, [2]int64{1_000, 100})
	if got := listConversations(t, r)[0].ConversationID; got != current {
		t.Fatalf("an unvouched turn rotated the thread: now %q", got)
	}
}

// A send to a thread that is no longer current is refused by the fence before
// the rotation check: it must not rotate the thread that IS current.
func TestContextRotation_NonCurrentThreadDoesNotRotate(t *testing.T) {
	r := newRotationRig(t)
	r.contextWindow = window(100_000)
	full := listConversations(t, r)[0].ConversationID
	runMeasuredTurn(t, r, full, [2]int64{90_000, 1_000})

	if rrec := r.h.AsOrg(testOrg).Post(conversationsPath(), ""); rrec.Code != http.StatusCreated {
		t.Fatalf("POST rotate: code %d (%s)", rrec.Code, rrec.Body.String())
	}
	current := listConversations(t, r)[0].ConversationID

	if rec := r.post(t, full, "general", "late send"); rec.Code != http.StatusConflict {
		t.Fatalf("send to the demoted full thread: code %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	if got := listConversations(t, r)[0].ConversationID; got != current {
		t.Fatalf("a send to a demoted thread rotated the current one: %q -> %q", current, got)
	}
}

// A full thread with a turn still running in it is not rotated under that
// turn: the send gets the turn_in_progress answer it would have got anyway.
func TestContextRotation_RunningTurnBlocksRotation(t *testing.T) {
	r := newRotationRig(t)
	current := listConversations(t, r)[0].ConversationID
	runMeasuredTurn(t, r, current, [2]int64{90_000, 1_000})

	r.fake.mu.Lock()
	r.fake.parts = nil
	r.fake.gated = true
	r.fake.mu.Unlock()
	running := r.startTurn(t, current, "general", "still going")
	<-r.fake.entered

	r.contextWindow = window(100_000)
	rec := r.post(t, current, "general", "one more")
	close(r.fake.release)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "turn_in_progress") {
		t.Fatalf("send during a running turn: code %d (%s), want 409 turn_in_progress", rec.Code, rec.Body.String())
	}
	if got := listConversations(t, r)[0].ConversationID; got != current {
		t.Fatalf("thread rotated under a running turn: now %q", got)
	}
	r.waitTerminal(t, running)
}
