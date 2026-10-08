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

// Component tier for the issue view: every open issue in a project has its own
// chat thread (use case issue-<n>) and its own active-turn slot, resolved,
// run and rehydrated through the real edge chain with an in-memory thread
// store and a stub issue reader.

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// stubIssues is the project's issues by number → state ("open" | "closed"); a
// number it does not hold is not found, as GitHub answers it.
type stubIssues map[int]string

func (s stubIssues) GetIssue(_ context.Context, _, _ string, n int) (*sourcecontrol.IssueInfo, error) {
	state, ok := s[n]
	if !ok {
		return nil, fmt.Errorf("get issue %d: %w", n, sourcecontrol.ErrIssueNotFound)
	}
	return &sourcecontrol.IssueInfo{Number: n, State: state}, nil
}

func issueQuery(n int) string { return fmt.Sprintf("?view=issue&issueNumber=%d", n) }

func newIssueRig(t *testing.T, convs *memConversationRepo, opts ...rigOption) *genaiRig {
	t.Helper()
	base := []rigOption{
		withConversations(convs),
		withIssues(stubIssues{7: "open", 8: "open", 9: "closed", 10: ""}),
	}
	return newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, append(base, opts...)...)
}

// An issue's thread is its own: distinct from the main chat's, the Issues
// chat's and every other issue's, and stable across reads.
func TestIssueView_OwnThreadPerIssue(t *testing.T) {
	r := newIssueRig(t, &memConversationRepo{})

	main := listConversations(t, r)[0].ConversationID
	issues := listConversationsAt(t, r, conversationsPath()+"?view=issues")[0].ConversationID
	seven := listConversationsAt(t, r, conversationsPath()+issueQuery(7))
	if len(seven) != 1 || !seven[0].Current || seven[0].ConversationID == "" {
		t.Fatalf("issue 7 list = %+v, want one current thread", seven)
	}
	eight := listConversationsAt(t, r, conversationsPath()+issueQuery(8))[0].ConversationID
	for name, other := range map[string]string{"main": main, "issues": issues, "issue 8": eight} {
		if seven[0].ConversationID == other {
			t.Errorf("issue 7 resolved the %s thread %q", name, other)
		}
	}
	if again := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID; again != seven[0].ConversationID {
		t.Errorf("issue 7 list minted a second thread: %q then %q", seven[0].ConversationID, again)
	}
}

// An issue turn runs in its own slot beside a running Issues turn, reaches the
// agents service as the issue view with its number under its own namespace,
// and is a plain chat turn: no flow, no room, no discovery tools — only the
// issue tools, on a token fenced to its issue.
func TestIssueView_TurnRunsBesideIssuesTurn(t *testing.T) {
	r := newIssueRig(t, &memConversationRepo{}, withMCP())
	issuesThread := listConversationsAt(t, r, conversationsPath()+"?view=issues")[0].ConversationID
	thread := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID

	m := manifestPart(map[string]string{}, nil)
	r.fake.manifest = &m
	r.fake.gated = true

	issuesTurn := acceptedTurnID(t, postTurnBody(t, r, issuesThread, map[string]any{
		"instruction": "file a bug", "view": "issues",
	}))
	<-r.fake.entered
	issueTurn := acceptedTurnID(t, postTurnBody(t, r, thread, map[string]any{
		"instruction": "/design", "view": "issue", "issueNumber": 7, "collab": true,
	}))
	<-r.fake.entered

	// A second turn on issue 7 is blocked by issue 7's turn alone.
	rec := postTurnBody(t, r, thread, map[string]any{"instruction": "again", "view": "issue", "issueNumber": 7})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), issueTurn) {
		t.Fatalf("second issue-7 POST: code %d body %s, want 409 naming %s", rec.Code, rec.Body.String(), issueTurn)
	}
	// Issue 8's thread id is not issue 7's.
	eight := listConversationsAt(t, r, conversationsPath()+issueQuery(8))[0].ConversationID
	rec = postTurnBody(t, r, eight, map[string]any{"instruction": "hi", "view": "issue", "issueNumber": 7})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "conversation_rotated") {
		t.Fatalf("issue-7 POST to issue 8's thread: code %d body %s, want 409 conversation_rotated", rec.Code, rec.Body.String())
	}

	if got := activeTurnID(t, r, issueQuery(7)); got != issueTurn {
		t.Errorf("active%s = %s, want the issue turn %s", issueQuery(7), got, issueTurn)
	}
	if got := activeTurnID(t, r, "?view=issues"); got != issuesTurn {
		t.Errorf("active?view=issues = %s, want the issues turn %s", got, issuesTurn)
	}
	if rec := r.h.AsOrg(testOrg).Get(turnPath("active") + issueQuery(8)); rec.Code != http.StatusNoContent {
		t.Errorf("active%s: code %d, want 204", issueQuery(8), rec.Code)
	}

	sent := r.fake.sentTurn(t, 1)
	if sent.req.View != "issue" || sent.req.IssueNumber != 7 {
		t.Errorf("sent view/issueNumber = %q/%d, want issue/7", sent.req.View, sent.req.IssueNumber)
	}
	wantConv := "org_" + testOrg + "--proj_" + testProj + "--issue-7--" + thread
	if sent.req.Workspace.ConversationID != wantConv {
		t.Errorf("issue turn conversation = %q, want %q", sent.req.Workspace.ConversationID, wantConv)
	}
	if sent.req.Turn.Kind != agentsvc.TurnKindChat || sent.req.Turn.Text != "/design" {
		t.Errorf("turn = %+v, want the instruction verbatim as a chat turn", sent.req.Turn)
	}
	if sent.req.Collab != nil || sent.req.WebSearch || len(sent.req.BranchNotes) != 0 {
		t.Errorf("issue turn carried collab/webSearch/branchNotes: %+v %v %+v",
			sent.req.Collab, sent.req.WebSearch, sent.req.BranchNotes)
	}
	wantMCP := agentsvc.MCPBlock{URL: testMCPBaseURL + "/internal/v1/issues/mcp", Token: "issue:" + testOrg + "/" + testProj + "#7"}
	if sent.req.MCP == nil || *sent.req.MCP != wantMCP {
		t.Errorf("issue turn MCP = %+v, want the issue-fenced block %+v", sent.req.MCP, wantMCP)
	}
	if issuesSent := r.fake.sentTurn(t, 0); issuesSent.req.IssueNumber != 0 {
		t.Errorf("issues turn carried issueNumber %d", issuesSent.req.IssueNumber)
	}

	close(r.fake.release)
	if st := r.waitTerminal(t, issueTurn); st.Status != "completed" || st.UseCase != "issue-7" {
		t.Errorf("issue turn terminal = %+v, want completed under use case issue-7", st)
	}
	if row := r.turns.row(t, issueTurn); row.Flow != "" {
		t.Errorf("issue turn flow = %q, want none", row.Flow)
	}
	r.waitTerminal(t, issuesTurn)
}

// A closed issue has no thread: resolving and sending answer 409 issue_closed,
// and nothing is minted or dispatched — nor for an issue whose state is not
// known to be open. An issue the project does not have is a 404.
func TestIssueView_ClosedOrMissingIssue(t *testing.T) {
	convs := &memConversationRepo{}
	r := newIssueRig(t, convs)
	const anyThread = "00000000-0000-4000-8000-000000000099"

	for _, n := range []int{9, 10} {
		rec := r.h.AsOrg(testOrg).Get(conversationsPath() + issueQuery(n))
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "issue_closed") {
			t.Errorf("GET conversations%s: code %d body %s, want 409 issue_closed", issueQuery(n), rec.Code, rec.Body.String())
		}
		rec = postTurnBody(t, r, anyThread, map[string]any{"instruction": "hi", "view": "issue", "issueNumber": n})
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "issue_closed") {
			t.Errorf("POST to issue %d: code %d body %s, want 409 issue_closed", n, rec.Code, rec.Body.String())
		}
	}
	// The active-turn read is read-only: a closed issue's slot answers, empty.
	if rec := r.h.AsOrg(testOrg).Get(turnPath("active") + issueQuery(9)); rec.Code != http.StatusNoContent {
		t.Errorf("GET active%s: code %d, want 204 (%s)", issueQuery(9), rec.Code, rec.Body.String())
	}

	if rec := r.h.AsOrg(testOrg).Get(conversationsPath() + issueQuery(404)); rec.Code != http.StatusNotFound {
		t.Errorf("GET conversations%s: code %d, want 404 (%s)", issueQuery(404), rec.Code, rec.Body.String())
	}
	if rec := postTurnBody(t, r, anyThread, map[string]any{"instruction": "hi", "view": "issue", "issueNumber": 404}); rec.Code != http.StatusNotFound {
		t.Errorf("POST to missing issue: code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}

	convs.mu.Lock()
	minted := len(convs.rows)
	convs.mu.Unlock()
	if minted != 0 {
		t.Errorf("thread store holds %d row(s), want none for a closed or missing issue", minted)
	}
	if r.fake.turns(t) != 0 {
		t.Error("agents dispatched for a closed or missing issue")
	}
}

// A turn still running when its issue closes (the issue agent closed its own
// issue) stays reachable through the active-turn read, so a reload reattaches.
func TestIssueView_ActiveTurnOutlivesClosure(t *testing.T) {
	issues := stubIssues{7: "open"}
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}), withIssues(issues))
	thread := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID
	m := manifestPart(map[string]string{}, nil)
	r.fake.manifest = &m
	r.fake.gated = true

	turnID := acceptedTurnID(t, postTurnBody(t, r, thread, map[string]any{
		"instruction": "close it", "view": "issue", "issueNumber": 7,
	}))
	<-r.fake.entered
	issues[7] = "closed"

	if got := activeTurnID(t, r, issueQuery(7)); got != turnID {
		t.Errorf("active%s after closure = %s, want the running turn %s", issueQuery(7), got, turnID)
	}
	close(r.fake.release)
	r.waitTerminal(t, turnID)
}

// errIssues answers every read with one error.
type errIssues struct{ err error }

func (e errIssues) GetIssue(context.Context, string, string, int) (*sourcecontrol.IssueInfo, error) {
	return nil, e.err
}

// The issue view's reads fail as what they are: a project with no repository
// is a 404, a service assembled without an issue reader a 503.
func TestIssueView_ReaderFailures(t *testing.T) {
	seed := map[string]string{"specs/requirements/prd.md": "# Reqs\n"}
	noRepo := newGenaiRig(t, seed, withConversations(&memConversationRepo{}),
		withIssues(errIssues{fmt.Errorf("resolve: %w", sourcecontrol.ErrRepoNotFound)}))
	if rec := noRepo.h.AsOrg(testOrg).Get(conversationsPath() + issueQuery(7)); rec.Code != http.StatusNotFound {
		t.Errorf("repo not found: code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	unwired := newGenaiRig(t, seed, withConversations(&memConversationRepo{}))
	if rec := unwired.h.AsOrg(testOrg).Get(conversationsPath() + issueQuery(7)); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no issue reader: code %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
}

// The issue number is required on the issue view and refused on every other,
// on each endpoint that takes a view, and an issue turn carries no spec scope.
func TestIssueView_Rejections(t *testing.T) {
	r := newIssueRig(t, &memConversationRepo{})
	thread := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID

	for _, q := range []string{"?view=issue", "?view=issue&issueNumber=0", "?view=issues&issueNumber=7", "?issueNumber=7"} {
		for _, path := range []string{conversationsPath() + q, turnPath("active") + q} {
			if rec := r.h.AsOrg(testOrg).Get(path); rec.Code != http.StatusBadRequest {
				t.Errorf("GET %s: code %d, want 400 (%s)", path, rec.Code, rec.Body.String())
			}
		}
	}
	cases := []struct {
		name string
		body map[string]any
	}{
		{"no number", map[string]any{"instruction": "x", "view": "issue"}},
		{"number on issues", map[string]any{"instruction": "x", "view": "issues", "issueNumber": 7}},
		{"number on main", map[string]any{"instruction": "x", "issueNumber": 7}},
		// The JSON arm is not schema-validated (create-turn also takes
		// multipart), so a zero must not pass for "absent".
		{"zero on main", map[string]any{"instruction": "x", "issueNumber": 0}},
		{"zero on issues", map[string]any{"instruction": "x", "view": "issues", "issueNumber": 0}},
		{"zero on issue", map[string]any{"instruction": "x", "view": "issue", "issueNumber": 0}},
		{"scope", map[string]any{"instruction": "x", "view": "issue", "issueNumber": 7, "scope": map[string]any{"kind": "design-review"}}},
	}
	for _, tc := range cases {
		if rec := postTurnBody(t, r, thread, tc.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: code %d, want 400 (%s)", tc.name, rec.Code, rec.Body.String())
		}
	}
	for _, n := range []string{"", "abc", "0", "-2"} {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		_ = w.WriteField("instruction", "x")
		_ = w.WriteField("view", "issue")
		if n != "" {
			_ = w.WriteField("issueNumber", n)
		}
		_ = w.Close()
		if rec := r.h.AsOrg(testOrg).PostRaw(turnsPath(thread), w.FormDataContentType(), buf.Bytes()); rec.Code != http.StatusBadRequest {
			t.Errorf("multipart issueNumber %q: code %d, want 400 (%s)", n, rec.Code, rec.Body.String())
		}
	}
	if r.fake.turns(t) != 0 {
		t.Error("agents dispatched despite a refused issue turn")
	}
}

// The multipart arm (a message with attachments) carries the issue number too.
func TestIssueView_MultipartTurn(t *testing.T) {
	r := newIssueRig(t, &memConversationRepo{})
	thread := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID
	m := manifestPart(map[string]string{}, nil)
	r.fake.manifest = &m

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("instruction", "see this")
	_ = w.WriteField("view", "issue")
	_ = w.WriteField("issueNumber", "7")
	_ = w.Close()
	turnID := acceptedTurnID(t, r.h.AsOrg(testOrg).PostRaw(turnsPath(thread), w.FormDataContentType(), buf.Bytes()))
	if st := r.waitTerminal(t, turnID); st.UseCase != "issue-7" {
		t.Errorf("multipart issue turn use case = %q, want issue-7", st.UseCase)
	}
	if sent := r.fake.sentTurn(t, 0); sent.req.View != "issue" || sent.req.IssueNumber != 7 {
		t.Errorf("sent view/issueNumber = %q/%d, want issue/7", sent.req.View, sent.req.IssueNumber)
	}
}

// An issue thread rehydrates from its own namespace — the one its turns were
// stored under — though the read names only the thread id.
func TestIssueView_RehydratesItsOwnThread(t *testing.T) {
	r := newIssueRig(t, &memConversationRepo{})
	thread := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID

	if rec := r.h.AsOrg(testOrg).Get(convPath(thread)); rec.Code != http.StatusOK {
		t.Fatalf("rehydrate issue thread: code %d (%s)", rec.Code, rec.Body.String())
	}
	r.fake.mu.Lock()
	got := r.fake.lastConvPath
	r.fake.mu.Unlock()
	if want := "--issue-7--" + thread; !strings.Contains(got, want) {
		t.Errorf("agents conversation path = %q, want it to contain %q", got, want)
	}
}
