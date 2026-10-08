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

// Component tier for removing a closed issue's chat thread (round three §4):
// the issue-<n> rows go, the agents-service conversations behind them are
// deleted, every other thread stays, and a thread whose turn is still running
// is removed only once that turn has finished.

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// issueThreadID is the agents-service id of an issue thread in the test project.
func issueThreadID(n, uuid string) string {
	return agentsvc.ConversationID(testOrg, testProj, "issue-"+n, uuid)
}

// waitDeleted polls until the fake agents service has seen want deletes.
func waitDeleted(t *testing.T, r *genaiRig, want int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := r.fake.deleted()
		if len(got) >= want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRemoveIssueThread_RemovesOnlyThatIssuesThreads(t *testing.T) {
	convs := &memConversationRepo{}
	r := newIssueRig(t, convs)
	ctx := context.Background()

	main := listConversations(t, r)[0].ConversationID
	issues := listConversationsAt(t, r, conversationsPath()+"?view=issues")[0].ConversationID
	demoted := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID
	current, err := convs.Rotate(ctx, testOrg, testProj, "issue-7", "ada")
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	eight := listConversationsAt(t, r, conversationsPath()+issueQuery(8))[0].ConversationID

	if err := r.svc.RemoveIssueThread(ctx, testOrg, testProj, 7); err != nil {
		t.Fatalf("RemoveIssueThread: %v", err)
	}

	got := r.fake.deleted()
	slices.Sort(got)
	want := []string{issueThreadID("7", demoted), issueThreadID("7", current.ID)}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("agents deletes = %v, want %v", got, want)
	}
	for _, id := range []string{demoted, current.ID} {
		if ok, _ := convs.Exists(ctx, testOrg, testProj, "issue-7", id); ok {
			t.Errorf("issue 7 thread %s survived", id)
		}
	}
	for useCase, id := range map[string]string{"general": main, "issues": issues, "issue-8": eight} {
		if ok, _ := convs.IsCurrent(ctx, testOrg, testProj, useCase, id); !ok {
			t.Errorf("%s thread %s was removed with issue 7's", useCase, id)
		}
	}

	// Idempotent: a second removal (the webhook after the agent's own close)
	// finds nothing and deletes nothing.
	if err := r.svc.RemoveIssueThread(ctx, testOrg, testProj, 7); err != nil {
		t.Fatalf("second RemoveIssueThread: %v", err)
	}
	if n := len(r.fake.deleted()); n != 2 {
		t.Errorf("second removal sent %d more deletes", n-2)
	}
}

// An agents-service failure does not keep the thread: aep-api's rows go
// anyway (the store's TTL sweep reaps the orphaned history).
func TestRemoveIssueThread_AgentsDeleteFailureStillDropsRows(t *testing.T) {
	convs := &memConversationRepo{}
	r := newIssueRig(t, convs)
	r.fake.deleteStatus = http.StatusInternalServerError
	ctx := context.Background()
	thread := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID

	if err := r.svc.RemoveIssueThread(ctx, testOrg, testProj, 7); err != nil {
		t.Fatalf("RemoveIssueThread: %v", err)
	}
	if got := r.fake.deleted(); len(got) != 1 {
		t.Fatalf("agents deletes = %v, want one attempt", got)
	}
	if ok, _ := convs.Exists(ctx, testOrg, testProj, "issue-7", thread); ok {
		t.Errorf("issue 7 thread survived an agents-service failure")
	}
}

// The issue agent closes its own issue from inside its turn: the removal waits
// for that turn to finish, then runs.
func TestRemoveIssueThread_WaitsForTheRunningTurn(t *testing.T) {
	convs := &memConversationRepo{}
	r := newIssueRig(t, convs)
	ctx := context.Background()
	thread := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID
	m := manifestPart(map[string]string{}, nil)
	r.fake.manifest = &m
	r.fake.gated = true

	turnID := acceptedTurnID(t, postTurnBody(t, r, thread, map[string]any{
		"instruction": "close it", "view": "issue", "issueNumber": 7,
	}))
	<-r.fake.entered

	if err := r.svc.RemoveIssueThread(ctx, testOrg, testProj, 7); err != nil {
		t.Fatalf("RemoveIssueThread: %v", err)
	}
	if got := r.fake.deleted(); len(got) != 0 {
		t.Fatalf("deleted %v while the turn was running", got)
	}
	if ok, _ := convs.Exists(ctx, testOrg, testProj, "issue-7", thread); !ok {
		t.Fatalf("issue 7 thread removed while its turn was running")
	}

	close(r.fake.release)
	r.waitTerminal(t, turnID)
	if got := waitDeleted(t, r, 1); !slices.Equal(got, []string{issueThreadID("7", thread)}) {
		t.Fatalf("agents deletes after the turn = %v, want issue 7's thread", got)
	}
	if ok, _ := convs.Exists(ctx, testOrg, testProj, "issue-7", thread); ok {
		t.Errorf("issue 7 thread survived its turn's end")
	}
}

// crashedTurn is a running issue-7 row with no runner behind it — a turn on a
// replica that died — whose last heartbeat was age ago.
func crashedTurn(t *testing.T, r *genaiRig, thread string, age time.Duration) *spec.AgentTurn {
	t.Helper()
	turn, err := r.turns.TryStart(context.Background(), &spec.AgentTurn{
		OrgID: testOrg, ProjectID: testProj, ConversationID: thread, UseCase: "issue-7", BaseRef: "base",
	})
	if err != nil {
		t.Fatalf("TryStart: %v", err)
	}
	r.turns.mu.Lock()
	defer r.turns.mu.Unlock()
	for _, row := range r.turns.rows {
		if row.ID == turn.ID {
			row.HeartbeatAt = time.Now().Add(-age)
		}
	}
	return turn
}

// ageTurn makes a running row's heartbeat stale enough for the sweep.
func ageTurn(r *genaiRig, id string) {
	r.turns.mu.Lock()
	defer r.turns.mu.Unlock()
	for _, row := range r.turns.rows {
		if row.ID == id {
			row.HeartbeatAt = time.Now().Add(-2 * time.Minute)
		}
	}
}

// A close that lands while a dead replica's turn still reads as running
// waits for that turn; the sweep that fails it is the turn's end, so the
// removal runs then. A reopen afterwards starts a fresh thread.
func TestRemoveIssueThread_SweptTurnTakesItsRemoval(t *testing.T) {
	convs := &memConversationRepo{}
	r := newIssueRig(t, convs)
	ctx := context.Background()
	thread := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID
	dead := crashedTurn(t, r, thread, 0)

	if err := r.svc.RemoveIssueThread(ctx, testOrg, testProj, 7); err != nil {
		t.Fatalf("RemoveIssueThread: %v", err)
	}
	if ok, _ := convs.Exists(ctx, testOrg, testProj, "issue-7", thread); !ok {
		t.Fatalf("issue 7 thread removed while its turn read as running")
	}

	ageTurn(r, dead.ID)
	if err := spec.NewTurnSweeper(r.turns, r.broker, r.svc, 0, 0).Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := r.fake.deleted(); !slices.Equal(got, []string{issueThreadID("7", thread)}) {
		t.Fatalf("agents deletes after the sweep = %v, want issue 7's thread", got)
	}

	// The reopen webhook finds nothing left; the issue's next read mints a
	// fresh thread.
	if err := r.svc.RemoveIssueThread(ctx, testOrg, testProj, 7); err != nil {
		t.Fatalf("reopen RemoveIssueThread: %v", err)
	}
	if fresh := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID; fresh == thread {
		t.Errorf("reopened issue 7 resumed its closed thread %q", thread)
	}
}

// A removal lost to a restart (the close was marked in the process that
// died) leaves the rows behind; the reopen removes them, so the reopened
// issue starts fresh rather than resuming the closed thread.
func TestRemoveIssueThread_ReopenRemovesALeftoverThread(t *testing.T) {
	convs := &memConversationRepo{}
	r := newIssueRig(t, convs)
	ctx := context.Background()
	thread := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID
	dead := crashedTurn(t, r, thread, 2*time.Minute)
	if err := spec.NewTurnSweeper(r.turns, r.broker, r.svc, 0, 0).Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if r.turns.row(t, dead.ID).Status != "failed" {
		t.Fatalf("dead turn not swept")
	}
	if ok, _ := convs.Exists(ctx, testOrg, testProj, "issue-7", thread); !ok {
		t.Fatalf("an unmarked sweep removed the thread")
	}

	if err := r.svc.RemoveIssueThread(ctx, testOrg, testProj, 7); err != nil {
		t.Fatalf("reopen RemoveIssueThread: %v", err)
	}
	if fresh := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID; fresh == thread {
		t.Errorf("reopened issue 7 resumed its closed thread %q", thread)
	}
}

// A running row whose heartbeat is older than the sweep's threshold is a dead
// turn, not a running one: the close removes the thread now instead of
// waiting on a turn that will never finish.
func TestRemoveIssueThread_StaleHeartbeatRemovesNow(t *testing.T) {
	convs := &memConversationRepo{}
	r := newIssueRig(t, convs)
	ctx := context.Background()
	thread := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID
	crashedTurn(t, r, thread, 2*time.Minute)

	if err := r.svc.RemoveIssueThread(ctx, testOrg, testProj, 7); err != nil {
		t.Fatalf("RemoveIssueThread: %v", err)
	}
	if got := r.fake.deleted(); !slices.Equal(got, []string{issueThreadID("7", thread)}) {
		t.Fatalf("agents deletes = %v, want issue 7's thread now", got)
	}
	if ok, _ := convs.Exists(ctx, testOrg, testProj, "issue-7", thread); ok {
		t.Errorf("issue 7 thread kept for a dead turn")
	}
}

// A removal is bound to the turn it waited for: if that turn ends without
// taking it, the next turn's finish drops the mark rather than deleting the
// thread that turn is using.
func TestRemoveIssueThread_AMarkNeverFiresOnAnotherTurn(t *testing.T) {
	convs := &memConversationRepo{}
	r := newIssueRig(t, convs)
	ctx := context.Background()
	thread := listConversationsAt(t, r, conversationsPath()+issueQuery(7))[0].ConversationID
	first := crashedTurn(t, r, thread, 0)
	if err := r.svc.RemoveIssueThread(ctx, testOrg, testProj, 7); err != nil {
		t.Fatalf("RemoveIssueThread: %v", err)
	}
	// The first turn ends without its finish taking the mark.
	if _, err := r.turns.Finish(ctx, first.ID, spec.TurnTerminal{Status: "failed", Reason: "stream-died"}); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	m := manifestPart(map[string]string{}, nil)
	r.fake.manifest = &m
	second := acceptedTurnID(t, postTurnBody(t, r, thread, map[string]any{
		"instruction": "carry on", "view": "issue", "issueNumber": 7,
	}))
	r.waitTerminal(t, second)

	if got := r.fake.deleted(); len(got) != 0 {
		t.Fatalf("the first turn's removal fired on the second: deletes = %v", got)
	}
	if ok, _ := convs.Exists(ctx, testOrg, testProj, "issue-7", thread); !ok {
		t.Errorf("issue 7 thread removed by a mark for another turn")
	}
}

func TestRemoveIssueThread_RefusesANonPositiveNumber(t *testing.T) {
	r := newIssueRig(t, &memConversationRepo{})
	if err := r.svc.RemoveIssueThread(context.Background(), testOrg, testProj, 0); !errors.Is(err, spec.ErrIssueNumber) {
		t.Fatalf("RemoveIssueThread(0) = %v, want ErrIssueNumber", err)
	}
}
