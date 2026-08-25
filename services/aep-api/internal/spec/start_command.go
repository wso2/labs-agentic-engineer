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

// `/<command>` flow commands — RECOGNISED by the server, for every token (#373).
//
// Clients send commands VERBATIM, and the server turns one into a TurnSpec: a
// statement of what the turn is for. It does not compose the instruction text —
// the agents service does that (services/agents/src/prompts/turn.ts), so the
// wording exists once, in the service that talks to the model. See
// services/agents/design/ADR-0003.
//
// Recognising the command HERE is still load-bearing:
//
//   - `/start` carries state no client sees and the agent cannot read — the
//     idea captured in specs/.agentic-engineer.toml, whose dot-led segment is
//     stripped from every turn snapshot. Only this side can read it.
//   - The flow token gates web search + MCP minting for design turns
//     (designOrCollabTurn), and MCP needs a BFF-signed bearer.
//
// Flows are deliberately NOT a conversation-identity dimension: a flow runs an
// interview whose answers are ordinary chat turns, so every turn of a project
// conversation must share one namespace (see useCaseGeneral).
//
// The idea only ever rides the FIRST `/start` turn: after that it is in the
// conversation history, so nothing needs re-attaching.

package spec

import (
	"context"
	"log/slog"
	"regexp"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// startCommand is the one token carrying server-read state beyond the token.
const startCommand = "/start"

// slashCommandPattern is deliberately narrow so real chat is never eaten: a
// single leading `/`, a skill-name token ending at whitespace or message end,
// optional free text after it. A bare `/`, a mid-message slash, `//x`, or
// trailing punctuation on the token all fail the match and pass through as
// ordinary chat.
//
// The playground keeps its own copy of this grammar (it plays the server role
// when running without the platform). Duplication is deliberate: a regex cannot
// be shared across languages without a generator, and the two never run on the
// same input — a mismatch shows up as the playground routing a line differently
// from production, not as a wrong prompt.
var slashCommandPattern = regexp.MustCompile(`^/([a-z0-9-]+)(?:\s+([\s\S]+))?$`)

// turnSpecFor classifies a raw instruction. Non-command text is an ordinary
// chat turn with an empty flow. A `/<command>` rides on as its token — most
// tokens ARE a skill name, and the few that name a branch of one (`/feature`)
// resolve in the agents service, where wording lives. `/start` additionally
// carries the project idea (typed inline wins, else read from the descriptor at
// `at` — best-effort: no descriptor, no idea, and the start skill asks the user
// instead).
func (s *Service) turnSpecFor(ctx context.Context, ref sourcecontrol.RepoRef, at, raw string) (agentsvc.TurnSpec, string) {
	m := slashCommandPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return agentsvc.TurnSpec{Kind: agentsvc.TurnKindChat, Text: raw}, ""
	}
	token, rest := m[1], strings.TrimSpace(m[2])

	if "/"+token == startCommand {
		idea := rest
		if idea == "" {
			idea = s.readProjectIdea(ctx, ref, at)
		}
		return agentsvc.TurnSpec{
			Kind:       agentsvc.TurnKindStart,
			Idea:       strings.TrimSpace(idea),
			References: s.listReferenceDocs(ctx, ref),
		}, token
	}

	// Flow turns carry the reference paths too: a flow generates artifacts
	// (wireframes.dsl from /design most of all) that must be grounded in what
	// the user attached — a drawn sketch is the wireframe brief. Chat prose
	// stays reference-free; the documents already sit in the conversation
	// history from the kickoff.
	return agentsvc.TurnSpec{
		Kind:       agentsvc.TurnKindFlow,
		Skill:      token,
		Text:       rest,
		References: s.listReferenceDocs(ctx, ref),
	}, token
}

// startTurnSummary is what a turn's DISPLAY record says — the transcript line
// and the activity feed's subject. It is the instruction verbatim for every
// turn but one.
//
// The exception is `/start` fired without an inline idea, which is now the
// ordinary case: the platform fires the kickoff itself at project creation
// (#562), so nobody types the idea and the bare token is all the wire carries.
// A transcript opening on `/start` alone would show the user a command they
// never issued, about a project they described in their own words a moment
// earlier — so the resolved idea is appended and the line reads as their own
// brief going in (#528). It is a transparency device, not a store: the idea's
// durable home is the descriptor, and this copy is never read back.
//
// Only the idea the SAME turn resolved is appended, so the line never claims
// something the agent did not receive. An inline idea needs nothing — it is
// already in the instruction the caller sent.
func startTurnSummary(instruction string, spec agentsvc.TurnSpec) string {
	if spec.Kind != agentsvc.TurnKindStart || spec.Idea == "" {
		return instruction
	}
	if trimmed := strings.TrimSpace(instruction); trimmed == startCommand {
		return startCommand + " " + spec.Idea
	}
	return instruction
}

// ReferencesDir is where a reference document sits INSIDE a turn's snapshot.
// It is not a repo path: nothing commits there (console ADR-0017). The engine
// stores the documents beside the mirror and overlays them into each extracted
// snapshot at this prefix, so the turn can keep carrying only the PATHS and the
// agent reads them from its own workspace exactly as before.
const ReferencesDir = gitfs.ReferenceOverlayDir + "/"

// listReferenceDocs lists the project's stored reference documents, sorted, as
// the paths they will occupy in the turn's snapshot. Nothing stored (the
// ordinary case — most projects attach none) returns nil, so the field drops
// out of the turn JSON entirely and the turn is byte-identical to one from
// before this channel existed.
//
// Sourced from the STORE, not the git tree: that is what makes this independent
// of `at`, and it is why a project created under the feature's v1 — whose
// documents really are committed — stops steering (decision 9: no migration).
//
// Best-effort, exactly like the captured idea: a store we cannot list is not a
// reason to fail someone's kickoff. Worst case the agent interviews without
// knowing the documents are there.
func (s *Service) listReferenceDocs(ctx context.Context, ref sourcecontrol.RepoRef) []string {
	names, err := s.git.Workspace().ListReferences(ctx, ref)
	if err != nil {
		slog.WarnContext(ctx, "references unlistable; turn continues without them",
			"dir", ReferencesDir, "error", err)
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, ReferencesDir+name)
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return out
}
