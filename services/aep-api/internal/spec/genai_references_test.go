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
	"slices"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The reference-documents channel (#383/#384), sibling to the captured idea:
// the console uploads what the user attached at create, the platform stores it
// OFF GIT (console ADR-0017) and overlays it into each turn's snapshot, and
// `/start` tells the agent the documents are there. These tests assert the
// FACTS the BFF puts on the turn — the wording that renders them belongs to the
// agents service. The paths on the turn are snapshot paths, not repo paths:
// nothing is committed at them.

// References attached at create ride the `/start` turn, sorted so the same
// folder always produces the same turn.
func TestStartCommand_CarriesReferenceDocuments(t *testing.T) {
	got := startTurnSpecWithReferences(t,
		map[string]string{spec.DescriptorPath: descriptorTOML(t, testIdea)},
		[]gitfs.ReferenceDoc{
			{Name: "rfp.pdf", Content: []byte("%PDF-1.4\n")},
			{Name: "glossary.md", Content: []byte("# Terms\n")},
			{Name: "interviews.txt", Content: []byte("notes\n")},
		}, "/start")

	want := []string{
		"specs/requirements/references/glossary.md",
		"specs/requirements/references/interviews.txt",
		"specs/requirements/references/rfp.pdf",
	}
	if !slices.Equal(got.References, want) {
		t.Fatalf("references = %v, want %v (sorted, all three)", got.References, want)
	}
	// The idea channel is untouched by the new one — they ride together.
	if got.Idea != testIdea {
		t.Fatalf("idea = %q, want the captured idea to still ride", got.Idea)
	}
}

// No stored references → nothing is added, so a docless project's turn stays
// exactly what it is today. This is the no-regression guarantee: every existing
// project takes this path.
func TestStartCommand_NoReferencesFolderAddsNothing(t *testing.T) {
	got := startTurnSpec(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
	}, "/start")

	if len(got.References) != 0 {
		t.Fatalf("references = %v, want none for a project with no references folder", got.References)
	}
}

// Only stored references ride. The rest of specs/ is already the agent's to
// read from its snapshot; re-listing it here would drown the real brief. Under
// v1 this test guarded a path-prefix trap (a sibling `references-old/` folder
// leaking in); the store cannot express that at all, so what it now pins is the
// stronger property — committed spec files never enter this channel, whatever
// they are called.
func TestStartCommand_OnlyStoredReferencesRide(t *testing.T) {
	got := startTurnSpecWithReferences(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
		// Committed siblings that must NOT ride — including files under the
		// overlay path itself, which is what a project created under the
		// feature's v1 actually looks like (decision 9: no migration, they
		// simply stop steering). (prd.md is left out deliberately — the shared
		// rig writes it as the turn's own output.)
		"specs/requirements/scope.md":                     "# Scope\n",
		"specs/design/domain-model.md":                    "# Design\n",
		"README.md":                                       "hi\n",
		"specs/requirements/references-old/superseded.md": "# Old\n",
		"specs/requirements/references/legacy-v1.md":      "# Committed under v1\n",
	}, []gitfs.ReferenceDoc{{Name: "brief.md", Content: []byte("# Brief\n")}}, "/start")

	want := []string{"specs/requirements/references/brief.md"}
	if !slices.Equal(got.References, want) {
		t.Fatalf("references = %v, want only the stored document %v", got.References, want)
	}
}

// Flow turns carry the references too (#383 follow-up): the design flow
// generates wireframes.dsl, and a user-drawn sketch attached at create is
// exactly what those wireframes must follow. Same channel, same sorting,
// same best-effort posture as the start turn.
func TestFlowCommand_CarriesReferenceDocuments(t *testing.T) {
	got := startTurnSpecWithReferences(t,
		map[string]string{spec.DescriptorPath: descriptorTOML(t, testIdea)},
		[]gitfs.ReferenceDoc{{Name: "sketch.png", Content: []byte("\x89PNG\r\n")}}, "/design")

	if got.Kind != agentsvc.TurnKindFlow || got.Skill != "design" {
		t.Fatalf("/design was not recognised as a flow turn: %+v", got)
	}
	want := []string{"specs/requirements/references/sketch.png"}
	if !slices.Equal(got.References, want) {
		t.Fatalf("references = %v, want %v on a flow turn", got.References, want)
	}
}

// Ordinary chat prose stays reference-free: the documents are already in the
// conversation history from the kickoff, and a chat turn generates nothing
// that must be grounded in them.
func TestChatProse_CarriesNoReferences(t *testing.T) {
	got := startTurnSpec(t, map[string]string{
		spec.DescriptorPath:                        descriptorTOML(t, testIdea),
		"specs/requirements/references/sketch.png": "\x89PNG\r\n",
	}, "tighten the second requirement")

	if got.Kind != agentsvc.TurnKindChat {
		t.Fatalf("prose was not a chat turn: %+v", got)
	}
	if len(got.References) != 0 {
		t.Fatalf("references = %v, want none on a chat turn", got.References)
	}
}
