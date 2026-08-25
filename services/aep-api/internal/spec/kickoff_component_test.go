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

import (
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The journey starts itself (#562): the platform fires `/start` with no client
// trigger, on the project's own thread, and the turn that reaches the agents
// service is the one a typed `/start` would have produced.
func TestKickoff_FiresStartOnTheProjectsThread(t *testing.T) {
	convs := &memConversationRepo{}
	r := newGenaiRig(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
	}, withConversations(convs))
	r.fake.parts = []string{addFilePart("specs/requirements/prd.md", "# Reqs\n")}
	m := manifestPart(map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, nil)
	r.fake.manifest = &m

	ctx := auth.WithAuthToken(t.Context(), "bearer-from-the-create-request")
	turnID, err := r.svc.StartKickoff(ctx, testOrg, testProj)
	if err != nil {
		t.Fatalf("StartKickoff: %v", err)
	}
	if st := r.waitTerminal(t, turnID); st.Status != "completed" {
		t.Fatalf("kickoff turn status = %q, want completed", st.Status)
	}

	sent := r.fake.sentTurn(t, 0)
	if sent.req.Turn.Kind != agentsvc.TurnKindStart {
		t.Fatalf("kickoff turn kind = %q, want %q", sent.req.Turn.Kind, agentsvc.TurnKindStart)
	}
	// The idea rides the descriptor, which only this side can read — the
	// kickoff sends the bare token and the server attaches it, exactly as it
	// does for a typed command.
	if sent.req.Turn.Idea != testIdea {
		t.Fatalf("kickoff turn idea = %q, want the captured idea", sent.req.Turn.Idea)
	}
	// Room-scoped like every console turn: the agent edits the shared doc, so
	// the spec view shows the work landing rather than nothing until it commits.
	if sent.req.Collab == nil {
		t.Fatalf("kickoff turn is not room-scoped: %+v", sent.req)
	}
	// It runs on the project's CURRENT thread, so the interview's answers land
	// in the history the chat panel rehydrates.
	current, err := convs.ResolveCurrent(ctx, testOrg, testProj, "general", "")
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	if !strings.HasSuffix(sent.req.Workspace.ConversationID, "--"+current.ID) {
		t.Fatalf("kickoff ran on %q, want the project's current thread %q",
			sent.req.Workspace.ConversationID, current.ID)
	}
}

// The transcript's first line is the user's own words, not a bare command
// (#528): the server appends the idea it resolved for the same turn, so the
// user can see the agent working from what they wrote.
func TestKickoff_TranscriptLineCarriesTheIdea(t *testing.T) {
	r := newGenaiRig(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
	}, withConversations(&memConversationRepo{}))
	r.fake.parts = []string{addFilePart("specs/requirements/prd.md", "# Reqs\n")}
	m := manifestPart(map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, nil)
	r.fake.manifest = &m

	ctx := auth.WithAuthToken(t.Context(), "bearer-from-the-create-request")
	turnID, err := r.svc.StartKickoff(ctx, testOrg, testProj)
	if err != nil {
		t.Fatalf("StartKickoff: %v", err)
	}
	r.waitTerminal(t, turnID)

	journal := r.fake.sentTurn(t, 0).req.Journal
	if journal == nil {
		t.Fatal("kickoff journalled nothing — the transcript would open on an empty bubble")
	}
	if journal.Text != "/start "+testIdea {
		t.Fatalf("transcript line = %q, want %q", journal.Text, "/start "+testIdea)
	}
}

// The turn row carries its own DISPLAY record (#562), because nothing else can
// supply it while the turn runs: the agents service persists a turn's
// transcript only when the turn ENDS, and the browser that lands on a freshly
// created project never sent the turn, so it has no local copy either. Without
// this the panel renders the agent narrating under a blank space for the whole
// kickoff — the user's first impression of the product.
func TestKickoff_TurnCarriesItsDisplayRecord(t *testing.T) {
	r := newGenaiRig(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
	}, withConversations(&memConversationRepo{}))
	r.fake.parts = []string{addFilePart("specs/requirements/prd.md", "# Reqs\n")}
	m := manifestPart(map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, nil)
	r.fake.manifest = &m

	ctx := auth.WithAuthToken(t.Context(), "bearer-from-the-create-request")
	turnID, err := r.svc.StartKickoff(ctx, testOrg, testProj)
	if err != nil {
		t.Fatalf("StartKickoff: %v", err)
	}
	st := r.waitTerminal(t, turnID)

	if st.Instruction != "/start "+testIdea {
		t.Fatalf("turn instruction = %q, want %q", st.Instruction, "/start "+testIdea)
	}
	// The harness's token carries no email, so there is no attributable human
	// behind this turn — the row says so rather than inventing a subject.
	if st.AuthorID != "" || st.AuthorDisplayName != "" {
		t.Fatalf("author = (%q,%q), want empty for an unattributable token",
			st.AuthorID, st.AuthorDisplayName)
	}
}

// Kickoff runs INLINE: the create answers only once the turn row exists.
//
// It used to detach, which returned the response ~2s before the row appeared —
// and in that window `spec.agent` reads "" for a project that is starting AND
// for one that was never started, so the console could not tell them apart. It
// showed a card offering to start work already starting, then rewrote it a
// second later. This is the assertion that keeps those two states distinct.
func TestKickoff_ReturnsOnlyOnceTheTurnExists(t *testing.T) {
	r := newGenaiRig(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
	}, withConversations(&memConversationRepo{}))
	r.fake.parts = []string{addFilePart("specs/requirements/prd.md", "# Reqs\n")}
	m := manifestPart(map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, nil)
	r.fake.manifest = &m

	ctx := auth.WithAuthToken(t.Context(), "bearer-from-the-create-request")
	r.svc.Kickoff(ctx, testOrg, testProj)

	// No polling, no waiting: the row is there the instant Kickoff returns.
	newest, err := r.turns.Newest(t.Context(), testOrg, testProj)
	if err != nil {
		t.Fatalf("newest turn: %v", err)
	}
	if newest == nil {
		t.Fatal("Kickoff returned before the turn row existed — the create response would race it")
	}
}

// Kickoff never surfaces a failure: a creation the user already committed to
// must not fail on it. The project is simply left un-started, which the spec
// view's empty state offers to fix.
func TestKickoff_SwallowsItsOwnFailure(t *testing.T) {
	r := newGenaiRig(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
	}, withConversations(&memConversationRepo{}))

	// No bearer — the agent cannot join the spec room, so the dispatch refuses.
	r.svc.Kickoff(t.Context(), testOrg, testProj)

	newest, err := r.turns.Newest(t.Context(), testOrg, testProj)
	if err != nil {
		t.Fatalf("newest turn: %v", err)
	}
	if newest != nil {
		t.Fatalf("a refused kickoff left a turn row: %+v", newest)
	}
}

// Idempotent on "has this project ever run a turn": both triggers may fire
// (a create, then the references upload it held for), and neither may start a
// second interview over the first.
func TestKickoff_SecondCallIsRefused(t *testing.T) {
	r := newGenaiRig(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
	}, withConversations(&memConversationRepo{}))
	r.fake.parts = []string{addFilePart("specs/requirements/prd.md", "# Reqs\n")}
	m := manifestPart(map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, nil)
	r.fake.manifest = &m

	ctx := auth.WithAuthToken(t.Context(), "bearer-from-the-create-request")
	turnID, err := r.svc.StartKickoff(ctx, testOrg, testProj)
	if err != nil {
		t.Fatalf("first StartKickoff: %v", err)
	}
	r.waitTerminal(t, turnID)

	if _, err := r.svc.StartKickoff(ctx, testOrg, testProj); !errors.Is(err, spec.ErrKickoffAlreadyRan) {
		t.Fatalf("second StartKickoff error = %v, want ErrKickoffAlreadyRan", err)
	}
	if n := r.fake.turns(t); n != 1 {
		t.Fatalf("dispatched turns = %d, want 1 — the kickoff must not re-interview", n)
	}
}

// Without a bearer the agent cannot join the spec room, so the kickoff refuses
// where the cause is legible rather than failing later inside the turn.
func TestKickoff_RefusesWithoutABearer(t *testing.T) {
	r := newGenaiRig(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
	}, withConversations(&memConversationRepo{}))

	if _, err := r.svc.StartKickoff(t.Context(), testOrg, testProj); err == nil {
		t.Fatal("StartKickoff with no bearer succeeded, want a refusal")
	}
	if n := r.fake.turns(t); n != 0 {
		t.Fatalf("dispatched turns = %d, want 0", n)
	}
}
