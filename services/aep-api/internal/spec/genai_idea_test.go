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
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
	"github.com/wso2/aep/aep-api/internal/spec"
)

const testIdea = "An expense claim tracker for a 200-person company"

func descriptorTOML(t *testing.T, idea string) string {
	t.Helper()
	raw, err := spec.MarshalDescriptor(spec.NewDescriptor("proj", idea, "2026-07-29T10:14:00Z"))
	if err != nil {
		t.Fatalf("spec.MarshalDescriptor: %v", err)
	}
	return string(raw)
}

// startTurnSpec seeds a project and returns the TURN SPEC dispatched for `msg`
// — what the BFF decided the turn is for. It carries no prompt text: the
// agents service composes that (services/agents/src/prompts/turn.ts), so these
// tests assert the facts, which is all this side owns.
//
// `/start` turns carry NO useCase — that field is part of the conversation
// identity, and the kickoff must share the conversation with the chat around it
// so its interview answers land in the same history.
func startTurnSpec(t *testing.T, seed map[string]string, msg string) agentsvc.TurnSpec {
	t.Helper()
	return startTurnSpecWithReferences(t, seed, nil, msg)
}

// startTurnSpecWithReferences is startTurnSpec with reference documents in the
// project's STORE. They are seeded there and not into `seed` (the git tree) on
// purpose: references are never committed (console ADR-0017), so the store is
// the only place a turn can learn about them.
func startTurnSpecWithReferences(t *testing.T, seed map[string]string, refs []gitfs.ReferenceDoc, msg string) agentsvc.TurnSpec {
	t.Helper()
	r := newGenaiRig(t, seed)
	if len(refs) > 0 {
		// The store is addressed by the ref the SERVICE resolves from the repo
		// row (testOrg/testProj), not the fixture's own default path key —
		// seeding fx.Ref writes a store the turn never looks at.
		ref := r.fx.Ref
		ref.OrgID, ref.ProjectID = testOrg, testProj
		ref.RepoSlug = workspacetest.DefaultSlug
		if err := r.fx.Engine.PutReferences(t.Context(), ref, refs); err != nil {
			t.Fatalf("seed reference store: %v", err)
		}
	}
	r.fake.parts = []string{addFilePart("specs/requirements/prd.md", "# Reqs\n")}
	m := manifestPart(map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, nil)
	r.fake.manifest = &m

	turnID := r.startTurn(t, convUUID, "", msg)
	st := r.waitTerminal(t, turnID)
	if st.Status != "completed" {
		t.Fatalf("turn status = %q, want completed", st.Status)
	}
	return r.fake.sentTurn(t, 0).req.Turn
}

// The server owns `/start`: it recognises the bare command and attaches the
// idea captured at project creation — which the client never sent, and which
// the agent cannot read for itself (the descriptor's dot-led segment is
// stripped from every turn snapshot).
func TestStartCommand_RecognisedAndCarriesCapturedIdea(t *testing.T) {
	got := startTurnSpec(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
	}, "/start")

	if got.Kind != agentsvc.TurnKindStart {
		t.Fatalf("/start was not recognised as a start turn: %+v", got)
	}
	if got.Idea != testIdea {
		t.Fatalf("turn missing the captured idea: %+v", got)
	}
	if strings.Contains(got.Text, "/start") {
		t.Fatalf("the raw command must not survive onto the turn: %+v", got)
	}
}

// An idea typed inline wins over the descriptor — the user is restating what
// they want right now.
func TestStartCommand_InlineIdeaOverridesDescriptor(t *testing.T) {
	got := startTurnSpec(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
	}, "/start a rota planner for nurses")

	if got.Idea != "a rota planner for nurses" {
		t.Fatalf("inline idea missing: %+v", got)
	}
	if strings.Contains(got.Idea, testIdea) {
		t.Fatalf("descriptor idea must not also ride when one was typed inline: %+v", got)
	}
}

// A `/start` the user TYPED is journalled verbatim: they saw those bytes go
// into the composer, and the transcript has to show what they sent. Only the
// bare token — which nobody types any more, since the platform fires the
// kickoff itself (#562) — gets the resolved idea appended.
func TestStartCommand_TypedInlineIdeaJournalsVerbatim(t *testing.T) {
	r := newGenaiRig(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
	})
	r.fake.parts = []string{addFilePart("specs/requirements/prd.md", "# Reqs\n")}
	m := manifestPart(map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, nil)
	r.fake.manifest = &m

	const typed = "/start a rota planner for nurses"
	r.waitTerminal(t, r.startTurn(t, convUUID, "", typed))

	journal := r.fake.sentTurn(t, 0).req.Journal
	if journal == nil || journal.Text != typed {
		t.Fatalf("journal = %+v, want the typed command verbatim", journal)
	}
}

// The idea is trimmed on the way onto the turn: the agents service renders it
// into a prompt paragraph, and stray whitespace there is the platform's to fix,
// not the model's to cope with.
func TestStartCommand_InlineIdeaIsTrimmed(t *testing.T) {
	got := startTurnSpec(t, map[string]string{"README.md": "hi\n"}, "/start   a rota planner for nurses  ")

	if got.Idea != "a rota planner for nurses" {
		t.Fatalf("idea = %q, want it trimmed", got.Idea)
	}
}

// No descriptor → the turn still dispatches, just with no idea. An older
// project (or a best-effort descriptor write that failed) still starts; the
// skill asks the user for the idea instead.
func TestStartCommand_NoDescriptorStillDispatches(t *testing.T) {
	got := startTurnSpec(t, map[string]string{"README.md": "hi\n"}, "/start")

	if got.Kind != agentsvc.TurnKindStart {
		t.Fatalf("/start must still be recognised without a descriptor: %+v", got)
	}
	if got.Idea != "" {
		t.Fatalf("no descriptor must carry no idea: %+v", got)
	}
}

// A corrupt descriptor is best-effort: losing the idea costs one question,
// failing the turn costs the user their kickoff.
func TestStartCommand_CorruptDescriptorDoesNotFailTheTurn(t *testing.T) {
	got := startTurnSpec(t, map[string]string{
		spec.DescriptorPath: "this is not = = toml [[[",
	}, "/start")

	if got.Kind != agentsvc.TurnKindStart {
		t.Fatalf("/start must still be recognised: %+v", got)
	}
	if got.Idea != "" {
		t.Fatalf("corrupt descriptor must carry no idea: %+v", got)
	}
}

// The command grammar is narrow: ordinary prose that merely mentions the word
// is a normal turn, sent through untouched.
func TestStartCommand_OrdinaryProseIsUntouched(t *testing.T) {
	got := startTurnSpec(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
	}, "where do I /start with the design?")

	if got.Kind != agentsvc.TurnKindChat || got.Text != "where do I /start with the design?" {
		t.Fatalf("ordinary prose must ride verbatim as chat: %+v", got)
	}
	if got.Idea != "" {
		t.Fatalf("a non-command turn must not carry the idea: %+v", got)
	}
}
