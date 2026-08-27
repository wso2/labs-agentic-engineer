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

// Component coverage for the synthetic Marketplace register chat project:
// GetRepo 404s for __marketplace_register__ (and the usual testProj still
// resolves when a fixture row is present), while list / rotate / StartTurn /
// rehydrate succeed without minting a git_repositories row.

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

func marketplaceConversationsPath() string {
	return "/api/v1/projects/" + spec.MarketplaceRegisterProjectID + "/agents/conversations"
}

func marketplaceTurnsPath(uuid string) string {
	return "/api/v1/projects/" + spec.MarketplaceRegisterProjectID + "/agents/" + uuid + "/messages"
}

func marketplaceConvPath(uuid string) string {
	return "/api/v1/projects/" + spec.MarketplaceRegisterProjectID + "/agents/" + uuid + "/messages"
}

func marketplaceTurnPath(turnID string) string {
	return "/api/v1/projects/" + spec.MarketplaceRegisterProjectID + "/turns/" + turnID
}

// countingRepoResolver records GetRepo project ids while delegating to an
// inner stub (404 for the synthetic id; optional fixture for testProj).
type countingRepoResolver struct {
	inner stubRepoResolver
	mu    sync.Mutex
	calls []string
}

func (c *countingRepoResolver) GetRepo(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error) {
	c.mu.Lock()
	c.calls = append(c.calls, projectID)
	c.mu.Unlock()
	return c.inner.GetRepo(ctx, orgID, projectID)
}

func (c *countingRepoResolver) calledFor(projectID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, id := range c.calls {
		if id == projectID {
			n++
		}
	}
	return n
}

func listMarketplaceConversations(t *testing.T, r *genaiRig) []conversationViewBody {
	t.Helper()
	rec := r.h.AsOrg(testOrg).Get(marketplaceConversationsPath())
	if rec.Code != http.StatusOK {
		t.Fatalf("GET marketplace conversations: code %d (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Conversations []conversationViewBody `json:"conversations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("list body: %v (%s)", err, rec.Body.String())
	}
	return out.Conversations
}

func TestMarketplaceRegister_ConversationsListAndRotateWithoutRepo(t *testing.T) {
	// inner.rec is a testProj fixture only — GetRepo for the synthetic id 404s.
	counter := &countingRepoResolver{inner: stubRepoResolver{rec: &sourcecontrol.GitRepository{
		OrgID: testOrg, ProjectID: testProj, Status: "ready",
	}}}
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}),
		withRepos(counter),
	)

	first := listMarketplaceConversations(t, r)
	if len(first) != 1 || first[0].ConversationID == "" || !first[0].Current {
		t.Fatalf("first list = %+v, want one current thread", first)
	}
	again := listMarketplaceConversations(t, r)
	if again[0].ConversationID != first[0].ConversationID {
		t.Fatalf("list minted a second thread: %q then %q", first[0].ConversationID, again[0].ConversationID)
	}
	if n := counter.calledFor(spec.MarketplaceRegisterProjectID); n != 0 {
		t.Fatalf("GetRepo called %d time(s) for synthetic id — must not be a hard dependency", n)
	}

	rrec := r.h.AsOrg(testOrg).Post(marketplaceConversationsPath(), "")
	if rrec.Code != http.StatusCreated {
		t.Fatalf("POST rotate: code %d (%s)", rrec.Code, rrec.Body.String())
	}
	var rotated conversationViewBody
	if err := json.Unmarshal(rrec.Body.Bytes(), &rotated); err != nil || rotated.ConversationID == "" {
		t.Fatalf("rotate body = %s (err %v)", rrec.Body.String(), err)
	}
	if rotated.ConversationID == first[0].ConversationID {
		t.Fatal("rotate returned the demoted thread")
	}
	if got := listMarketplaceConversations(t, r)[0].ConversationID; got != rotated.ConversationID {
		t.Fatalf("list after rotate = %q, want %q", got, rotated.ConversationID)
	}
	if n := counter.calledFor(spec.MarketplaceRegisterProjectID); n != 0 {
		t.Fatalf("GetRepo called for synthetic id during rotate/list (%d)", n)
	}
}

func TestMarketplaceRegister_StartTurnWithoutProjectRepo(t *testing.T) {
	counter := &countingRepoResolver{inner: stubRepoResolver{rec: &sourcecontrol.GitRepository{
		OrgID: testOrg, ProjectID: testProj, Status: "ready",
	}}}
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}),
		withRepos(counter),
	)

	current := listMarketplaceConversations(t, r)[0].ConversationID
	skillsSHA := r.skillsOrigin.HeadSHA(t)

	// Non-empty edit frames: on the fold path these would mutate (or fail
	// against) the dual skills snapshot. roomMode must relay-only — same pin
	// as TestCollabTurn_RoomScopedDispatchNoCommit.
	r.fake.parts = []string{
		textPart("drafting the external resource"),
		editFilePart("requirements/requirements.md", "# Reqs\n", "# Requirements\n"),
	}
	m := manifestPart(map[string]string{"requirements/requirements.md": "# Requirements\n"}, nil)
	r.fake.manifest = &m

	payload, _ := json.Marshal(map[string]any{
		"instruction": "/register-external-resource Register a payment gateway",
	})
	rec := r.h.AsOrg(testOrg).Post(marketplaceTurnsPath(current), string(payload))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("StartTurn: code %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		TurnID string `json:"turnId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.TurnID == "" {
		t.Fatalf("202 body = %s (err %v)", rec.Body.String(), err)
	}

	var st spec.TurnStatus
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		grec := r.h.AsOrg(testOrg).Get(marketplaceTurnPath(out.TurnID))
		if grec.Code != http.StatusOK {
			t.Fatalf("GET turn: code %d (%s)", grec.Code, grec.Body.String())
		}
		if err := json.Unmarshal(grec.Body.Bytes(), &st); err != nil {
			t.Fatalf("status body: %v (%s)", err, grec.Body.String())
		}
		if st.Status != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Status != "completed" || !st.NoChanges {
		t.Fatalf("terminal = %+v, want completed no-changes (relay-only)", st)
	}
	if st.CommitSHA != skillsSHA {
		t.Errorf("commitSha = %s, want skills pin %s", st.CommitSHA, skillsSHA)
	}
	if r.skillsOrigin.HeadSHA(t) != skillsSHA {
		t.Error("synthetic turn must not Mutate — skills origin tip moved")
	}
	if n := counter.calledFor(spec.MarketplaceRegisterProjectID); n != 0 {
		t.Fatalf("GetRepo called %d time(s) for synthetic id during StartTurn", n)
	}

	sent := r.fake.sentTurn(t, 0)
	wantConv := agentsvc.ConversationID(testOrg, spec.MarketplaceRegisterProjectID, "general", current)
	if sent.req.Workspace.ConversationID != wantConv {
		t.Errorf("namespaced conversation = %q, want %q", sent.req.Workspace.ConversationID, wantConv)
	}
	if sent.req.Workspace.RepoSlug != "marketplace-register" {
		t.Errorf("repoSlug = %q, want marketplace-register", sent.req.Workspace.RepoSlug)
	}
	if sent.req.Workspace.Ref == "" || sent.req.Workspace.Ref != sent.req.Workspace.SkillsRef {
		t.Errorf("ref=%q skillsRef=%q, want both equal to the skills SHA",
			sent.req.Workspace.Ref, sent.req.Workspace.SkillsRef)
	}
	if sent.req.Workspace.SkillsRef != skillsSHA {
		t.Errorf("skillsRef = %s, want skills head %s", sent.req.Workspace.SkillsRef, skillsSHA)
	}

	// Empty-thread rehydrate on the synthetic id (agents store 404).
	r.fake.mu.Lock()
	r.fake.convStatus = http.StatusNotFound
	r.fake.mu.Unlock()
	hrec := r.h.AsOrg(testOrg).Get(marketplaceConvPath(current))
	if hrec.Code != http.StatusOK {
		t.Fatalf("rehydrate: code %d, want 200 (%s)", hrec.Code, hrec.Body.String())
	}
}

// Console AgentChatPanel always POSTs collab:true. The synthetic id has no
// spec room (no repo; room name fails DNS-label). StartTurn must drop Collab
// so agents never join spec-<org>-__marketplace_register__.
func TestMarketplaceRegister_StartTurnIgnoresCollab(t *testing.T) {
	counter := &countingRepoResolver{inner: stubRepoResolver{rec: &sourcecontrol.GitRepository{
		OrgID: testOrg, ProjectID: testProj, Status: "ready",
	}}}
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}),
		withRepos(counter),
	)
	current := listMarketplaceConversations(t, r)[0].ConversationID
	r.fake.parts = []string{textPart("drafting the external resource")}
	m := manifestPart(nil, nil)
	r.fake.manifest = &m

	payload, _ := json.Marshal(map[string]any{
		"instruction": "/register-external-resource i want to connect to github",
		"collab":      true,
	})
	rec := r.h.AsOrg(testOrg).Post(marketplaceTurnsPath(current), string(payload))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("StartTurn: code %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		TurnID string `json:"turnId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.TurnID == "" {
		t.Fatalf("202 body = %s (err %v)", rec.Body.String(), err)
	}

	var st spec.TurnStatus
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		grec := r.h.AsOrg(testOrg).Get(marketplaceTurnPath(out.TurnID))
		if grec.Code != http.StatusOK {
			t.Fatalf("GET turn: code %d (%s)", grec.Code, grec.Body.String())
		}
		if err := json.Unmarshal(grec.Body.Bytes(), &st); err != nil {
			t.Fatalf("status body: %v (%s)", err, grec.Body.String())
		}
		if st.Status != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Status != "completed" || !st.NoChanges {
		t.Fatalf("terminal = %+v, want completed no-changes (relay-only)", st)
	}
	if n := counter.calledFor(spec.MarketplaceRegisterProjectID); n != 0 {
		t.Fatalf("GetRepo called %d time(s) for synthetic id during collab StartTurn", n)
	}

	sent := r.fake.sentTurn(t, 0)
	if sent.req.Collab != nil {
		t.Fatalf("synthetic register dispatch carried collab room %q — agents must not join a spec room", sent.req.Collab.RoomID)
	}
}

func TestMarketplaceRegister_RealProjectStill404WhenRepoMissing(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}),
		withRepos(stubRepoResolver{}), // every GetRepo misses
	)
	rec := r.h.AsOrg(testOrg).Get(conversationsPath())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("testProj with no repo: code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}
