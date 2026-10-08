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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"

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
	demoted []string                             // rotated-away scope key + "/" + id (still Exists)
	created map[string]time.Time                 // scope key + "/" + id → creation, current or demoted
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
	row.CreatedAt = time.Now().UTC()
	if m.created == nil {
		m.created = map[string]time.Time{}
	}
	m.created[k+"/"+row.ID] = row.CreatedAt
	m.rows[k] = row
	return row, nil
}

func (m *memConversationRepo) Rotate(ctx context.Context, org, project, useCase, createdBy string) (*spec.ProjectConversation, error) {
	m.mu.Lock()
	if m.rows == nil {
		m.rows = map[string]*spec.ProjectConversation{}
	}
	if old, ok := m.rows[m.key(org, project, useCase)]; ok {
		m.demoted = append(m.demoted, m.key(org, project, useCase)+"/"+old.ID)
	}
	delete(m.rows, m.key(org, project, useCase))
	m.mu.Unlock()
	return m.ResolveCurrent(ctx, org, project, useCase, createdBy)
}

func (m *memConversationRepo) RotateIfCurrent(ctx context.Context, org, project, useCase, id, createdBy string) (*spec.ProjectConversation, error) {
	if ok, _ := m.IsCurrent(ctx, org, project, useCase, id); !ok {
		return nil, nil
	}
	return m.Rotate(ctx, org, project, useCase, createdBy)
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
		if d == m.key(org, project, useCase)+"/"+id {
			return true, nil
		}
	}
	return false, nil
}

func (m *memConversationRepo) CreatedAt(_ context.Context, org, project, useCase, id string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.created[m.key(org, project, useCase)+"/"+id], nil
}

func (m *memConversationRepo) UseCaseOf(_ context.Context, org, project, id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := org + "/" + project + "/"
	for k := range m.created {
		scope, rowID, ok := strings.Cut(strings.TrimPrefix(k, prefix), "/")
		if ok && rowID == id && strings.HasPrefix(k, prefix) {
			return scope, nil
		}
	}
	return "", nil
}

func (m *memConversationRepo) DeleteUseCase(_ context.Context, org, project, useCase string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	scope := m.key(org, project, useCase) + "/"
	var ids []string
	for k := range m.created {
		if id, ok := strings.CutPrefix(k, scope); ok {
			ids = append(ids, id)
			delete(m.created, k)
		}
	}
	delete(m.rows, m.key(org, project, useCase))
	kept := m.demoted[:0]
	for _, d := range m.demoted {
		if !strings.HasPrefix(d, scope) {
			kept = append(kept, d)
		}
	}
	m.demoted = kept
	return ids, nil
}

type conversationViewBody struct {
	ConversationID string `json:"conversationId"`
	Current        bool   `json:"current"`
}

func listConversations(t *testing.T, r *genaiRig) []conversationViewBody {
	t.Helper()
	return listConversationsAt(t, r, conversationsPath())
}

func listConversationsAt(t *testing.T, r *genaiRig, path string) []conversationViewBody {
	t.Helper()
	rec := r.h.AsOrg(testOrg).Get(path)
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

// ---- chat views ----------------------------------------------------------------

// postTurnBody POSTs an arbitrary JSON create-turn body.
func postTurnBody(t *testing.T, r *genaiRig, uuid string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	return r.h.AsOrg(testOrg).Post(turnsPath(uuid), string(raw))
}

// acceptedTurnID reads the turnId off a 202.
func acceptedTurnID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST turn: code %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		TurnID string `json:"turnId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.TurnID == "" {
		t.Fatalf("202 body = %s (err %v)", rec.Body.String(), err)
	}
	return out.TurnID
}

// activeTurnID GETs the active turn (query is "" or "?view=…") and returns its id.
func activeTurnID(t *testing.T, r *genaiRig, query string) string {
	t.Helper()
	rec := r.h.AsOrg(testOrg).Get(turnPath("active") + query)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET active%s: code %d, want 200 (%s)", query, rec.Code, rec.Body.String())
	}
	var st spec.TurnStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("active body: %v (%s)", err, rec.Body.String())
	}
	return st.TurnID
}

// The Issues view has its own thread and its own active-turn slot: a turn in
// it is admitted while the spec chat is busy, the spec chat's thread id is not
// a valid address for it, and each view reads back its own running turn.
func TestIssuesView_OwnThreadAndActiveTurn(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}))

	main := listConversations(t, r)[0].ConversationID
	issues := listConversationsAt(t, r, conversationsPath()+"?view=issues")
	if len(issues) != 1 || !issues[0].Current || issues[0].ConversationID == "" {
		t.Fatalf("issues list = %+v, want one current thread", issues)
	}
	issuesThread := issues[0].ConversationID
	if issuesThread == main {
		t.Fatalf("the issues view resolved the main thread %q", main)
	}
	if again := listConversationsAt(t, r, conversationsPath()+"?view=issues")[0].ConversationID; again != issuesThread {
		t.Fatalf("issues list minted a second thread: %q then %q", issuesThread, again)
	}

	m := manifestPart(map[string]string{}, nil)
	r.fake.manifest = &m
	r.fake.gated = true

	generalTurn := r.startTurn(t, main, "general", "hello")
	<-r.fake.entered

	// The spec chat is busy; the Issues chat is not blocked by it.
	issuesTurn := acceptedTurnID(t, postTurnBody(t, r, issuesThread, map[string]any{
		"instruction": "the save button does nothing", "view": "issues",
	}))
	<-r.fake.entered

	// A second Issues turn IS blocked — by the Issues turn, not the spec one.
	rec := postTurnBody(t, r, issuesThread, map[string]any{"instruction": "and another", "view": "issues"})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), issuesTurn) {
		t.Fatalf("second issues POST: code %d body %s, want 409 naming %s", rec.Code, rec.Body.String(), issuesTurn)
	}

	// The main thread is not the Issues view's current thread.
	rec = postTurnBody(t, r, main, map[string]any{"instruction": "hi", "view": "issues"})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "conversation_rotated") {
		t.Fatalf("issues POST to the main thread: code %d body %s, want 409 conversation_rotated", rec.Code, rec.Body.String())
	}

	if got := activeTurnID(t, r, "?view=issues"); got != issuesTurn {
		t.Errorf("active?view=issues = %s, want the issues turn %s", got, issuesTurn)
	}
	if got := activeTurnID(t, r, ""); got != generalTurn {
		t.Errorf("active (no view) = %s, want the general turn %s", got, generalTurn)
	}

	// The Issues turn reached the agents service under its own namespace.
	sent := r.fake.sentTurn(t, 1)
	wantConv := "org_" + testOrg + "--proj_" + testProj + "--issues--" + issuesThread
	if sent.req.Workspace.ConversationID != wantConv {
		t.Errorf("issues turn conversation = %q, want %q", sent.req.Workspace.ConversationID, wantConv)
	}

	close(r.fake.release)
	if st := r.waitTerminal(t, issuesTurn); st.Status != "completed" || st.UseCase != "issues" {
		t.Errorf("issues turn terminal = %+v, want completed under use case issues", st)
	}
	if st := r.waitTerminal(t, generalTurn); st.Status != "completed" || st.UseCase != "general" {
		t.Errorf("general turn terminal = %+v, want completed under use case general", st)
	}
}

// An Issues turn is always a plain chat turn: a `/<skill>` instruction is not
// recognised as a flow, and a collab flag joins no spec room — the Issues agent
// files issues, it does not edit the spec.
func TestIssuesView_TurnIsAlwaysPlainChat(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}))
	issuesThread := listConversationsAt(t, r, conversationsPath()+"?view=issues")[0].ConversationID
	m := manifestPart(map[string]string{}, nil)
	r.fake.manifest = &m

	turnID := acceptedTurnID(t, postTurnBody(t, r, issuesThread, map[string]any{
		"instruction": "/design", "view": "issues", "collab": true,
	}))
	r.waitTerminal(t, turnID)

	sent := r.fake.sentTurn(t, 0)
	if sent.req.Turn.Kind != agentsvc.TurnKindChat || sent.req.Turn.Text != "/design" {
		t.Errorf("turn = %+v, want the instruction verbatim as a chat turn", sent.req.Turn)
	}
	if sent.req.Collab != nil {
		t.Errorf("collab = %+v, want no room for an issues turn", sent.req.Collab)
	}
	if sent.req.WebSearch {
		t.Error("an issues turn asked for web search")
	}
	if row := r.turns.row(t, turnID); row.Flow != "" || row.UseCase != "issues" {
		t.Errorf("row flow/useCase = %q/%q, want \"\"/issues", row.Flow, row.UseCase)
	}
}

// What an Issues turn cannot carry is refused before any turn exists: a spec
// scope and prototype feedback belong to the spec chat, and a view outside the
// enum is a client bug on every endpoint that takes one.
func TestIssuesView_Rejections(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}))
	issuesThread := listConversationsAt(t, r, conversationsPath()+"?view=issues")[0].ConversationID

	cases := []struct {
		name string
		body map[string]any
	}{
		{"scope", map[string]any{"instruction": "x", "view": "issues", "scope": map[string]any{"kind": "design-review"}}},
		{"prototype feedback", map[string]any{"instruction": "/prototype", "view": "issues", "collab": true, "prototypeFeedback": prototypeFeedbackFixture()}},
		{"unknown view", map[string]any{"instruction": "x", "view": "boards"}},
	}
	for _, tc := range cases {
		if rec := postTurnBody(t, r, issuesThread, tc.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: code %d, want 400 (%s)", tc.name, rec.Code, rec.Body.String())
		}
	}
	// The multipart arm is parsed by hand, past the request validator: the
	// service's own check is what refuses a view outside the enum there.
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("instruction", "x")
	_ = w.WriteField("view", "boards")
	_ = w.Close()
	if rec := r.h.AsOrg(testOrg).PostRaw(turnsPath(issuesThread), w.FormDataContentType(), buf.Bytes()); rec.Code != http.StatusBadRequest {
		t.Errorf("multipart unknown view: code %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	for _, path := range []string{conversationsPath() + "?view=boards", turnPath("active") + "?view=boards"} {
		if rec := r.h.AsOrg(testOrg).Get(path); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: code %d, want 400 (%s)", path, rec.Code, rec.Body.String())
		}
	}
	if r.fake.turns(t) != 0 {
		t.Error("agents dispatched despite a refused issues turn")
	}
}

// The Issues thread rehydrates from its own namespace — the history the
// agents service stored its turns under — not the main chat's.
func TestIssuesView_RehydratesItsOwnThread(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}))
	issuesThread := listConversationsAt(t, r, conversationsPath()+"?view=issues")[0].ConversationID

	if rec := r.h.AsOrg(testOrg).Get(convPath(issuesThread)); rec.Code != http.StatusOK {
		t.Fatalf("rehydrate issues thread: code %d (%s)", rec.Code, rec.Body.String())
	}
	r.fake.mu.Lock()
	got := r.fake.lastConvPath
	r.fake.mu.Unlock()
	if want := "--issues--" + issuesThread; !strings.Contains(got, want) {
		t.Errorf("agents conversation path = %q, want it to contain %q", got, want)
	}
}
