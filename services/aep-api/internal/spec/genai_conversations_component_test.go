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

// Component tier for project-scoped conversations (#430): the resolve/rotate
// endpoints and the single-era conversation_rotated fence on create-turn,
// through the real edge chain with an in-memory thread store (the store's
// SQL semantics — partial-unique race, demote-then-insert — are pinned at the
// DB tier in repository_conversation_dbtest_test.go).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/spec"
)

func conversationsPath() string {
	return "/api/v1/projects/" + testProj + "/agents/conversations"
}

// memConversationRepo is the in-memory ConversationRepository for the
// component tier — single-flight semantics without SQL.
type memConversationRepo struct {
	mu      sync.Mutex
	rows    map[string]*spec.ProjectConversation // scope key → current row
	demoted []string                             // rotated-away ids (still Exists)
	n       int
}

func (m *memConversationRepo) key(org, project, useCase string) string {
	return org + "/" + project + "/" + useCase
}

func (m *memConversationRepo) ResolveCurrent(_ context.Context, org, project, useCase, createdBy string) (*spec.ProjectConversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows == nil {
		m.rows = map[string]*spec.ProjectConversation{}
	}
	k := m.key(org, project, useCase)
	if row, ok := m.rows[k]; ok {
		return row, nil
	}
	m.n++
	row := &spec.ProjectConversation{
		ID:    fmt.Sprintf("00000000-0000-4000-8000-%012d", m.n),
		OrgID: org, ProjectID: project, UseCase: useCase,
		Current: true, CreatedBy: createdBy,
	}
	m.rows[k] = row
	return row, nil
}

func (m *memConversationRepo) Rotate(ctx context.Context, org, project, useCase, createdBy string) (*spec.ProjectConversation, error) {
	m.mu.Lock()
	if m.rows == nil {
		m.rows = map[string]*spec.ProjectConversation{}
	}
	if old, ok := m.rows[m.key(org, project, useCase)]; ok {
		m.demoted = append(m.demoted, old.ID)
	}
	delete(m.rows, m.key(org, project, useCase))
	m.mu.Unlock()
	return m.ResolveCurrent(ctx, org, project, useCase, createdBy)
}

func (m *memConversationRepo) IsCurrent(_ context.Context, org, project, useCase, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[m.key(org, project, useCase)]
	return ok && row.ID == id, nil
}

func (m *memConversationRepo) Exists(_ context.Context, org, project, useCase, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row, ok := m.rows[m.key(org, project, useCase)]; ok && row.ID == id {
		return true, nil
	}
	for _, d := range m.demoted {
		if d == id {
			return true, nil
		}
	}
	return false, nil
}

type conversationViewBody struct {
	ConversationID string `json:"conversationId"`
	Current        bool   `json:"current"`
}

func listConversations(t *testing.T, r *genaiRig) []conversationViewBody {
	t.Helper()
	rec := r.h.AsOrg(testOrg).Get(conversationsPath())
	if rec.Code != http.StatusOK {
		t.Fatalf("GET conversations: code %d (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Conversations []conversationViewBody `json:"conversations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("list body: %v (%s)", err, rec.Body.String())
	}
	return out.Conversations
}

func TestConversations_ResolveIsLazyAndStable(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}))

	first := listConversations(t, r)
	if len(first) != 1 || first[0].ConversationID == "" || !first[0].Current {
		t.Fatalf("first list = %+v, want one current thread", first)
	}
	again := listConversations(t, r)
	if again[0].ConversationID != first[0].ConversationID {
		t.Fatalf("list minted a second thread: %q then %q", first[0].ConversationID, again[0].ConversationID)
	}
}

func TestConversations_TurnFenceAndRotation(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}))

	current := listConversations(t, r)[0].ConversationID

	// A turn addressed to a NON-current id — a stale localStorage uuid, or a
	// resolved id a teammate rotated away — is refused with the pinned body,
	// and nothing is dispatched.
	rec := r.post(t, convUUID, "general", "hello")
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale-id POST: code %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "conversation_rotated") {
		t.Fatalf("409 body = %s, want code conversation_rotated", rec.Body.String())
	}
	if r.fake.turns(t) != 0 {
		t.Error("agents dispatched despite the rotated-conversation fence")
	}

	// The current id passes the fence and runs a real (no-op) turn: the fake
	// streams no file parts, so the manifest names no files and the fold
	// completes with no changes.
	m := manifestPart(map[string]string{}, nil)
	r.fake.manifest = &m
	turnID := r.startTurn(t, current, "general", "hello")
	if st := r.waitTerminal(t, turnID); st.Status != "completed" {
		t.Fatalf("turn on current thread = %q, want completed (%s)", st.Status, st.Message)
	}

	// Rotate: 201 with a fresh current thread; the old id now 409s.
	rrec := r.h.AsOrg(testOrg).Post(conversationsPath(), "")
	if rrec.Code != http.StatusCreated {
		t.Fatalf("POST rotate: code %d (%s)", rrec.Code, rrec.Body.String())
	}
	var rotated conversationViewBody
	if err := json.Unmarshal(rrec.Body.Bytes(), &rotated); err != nil || rotated.ConversationID == "" {
		t.Fatalf("rotate body = %s (err %v)", rrec.Body.String(), err)
	}
	if rotated.ConversationID == current {
		t.Fatal("rotate returned the demoted thread")
	}
	if got := listConversations(t, r)[0].ConversationID; got != rotated.ConversationID {
		t.Fatalf("list after rotate = %q, want %q", got, rotated.ConversationID)
	}
	if rec := r.post(t, current, "general", "hello again"); rec.Code != http.StatusConflict {
		t.Fatalf("demoted-id POST: code %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
}

// A thread the BFF minted has no agents-store row until its first turn (and
// the store's TTL sweep can reap an idle one): its rehydrate answers 200 with
// EMPTY history. 404 is reserved for genuinely unknown ids — the console
// treats 404-class failures as "keep the local cache", so an empty-thread 404
// would leave a re-created project's stale log immortal.
func TestConversations_EmptyThreadRehydratesEmptyNot404(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}))
	// The agents store has no row for a turn-less thread — its GET 404s.
	r.fake.mu.Lock()
	r.fake.convStatus = http.StatusNotFound
	r.fake.mu.Unlock()

	current := listConversations(t, r)[0].ConversationID

	rec := r.h.AsOrg(testOrg).Get(convPath(current))
	if rec.Code != http.StatusOK {
		t.Fatalf("fresh-thread rehydrate: code %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Messages []any `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Messages == nil {
		t.Fatalf("rehydrate body = %s (err %v), want {\"messages\":[]}", rec.Body.String(), err)
	}
	if len(body.Messages) != 0 {
		t.Fatalf("fresh thread has %d messages, want 0", len(body.Messages))
	}

	// An id no thread ever had stays a real 404.
	if rec := r.h.AsOrg(testOrg).Get(convPath("11111111-1111-4111-8111-111111111111")); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown-id rehydrate: code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}
