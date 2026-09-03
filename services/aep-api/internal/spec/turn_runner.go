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

package spec

// turn_runner.go — the detached committed-truth turn execution (design §6,
// D13–D15, D20): dispatch the agents service with the WorkspaceRef, tap every
// StreamPart into the broker while folding file mutations in Go, gate the
// fold on the terminal manifest, and land the result as ONE commit on main
// via Workspace.Mutate. The runner heartbeats the agent_turns row; a replica
// crash leaves a stale heartbeat for the sweep (turn_sweep.go).

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/wso2/aep/aep-api/internal/platform/text"
	"io"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/platform/agentfold"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

const (
	// turnRunTimeout bounds one detached turn end to end (generation runs
	// minutes; 30min is the hard stop — plan.go precedent, made explicit).
	turnRunTimeout = 30 * time.Minute
	// turnIdleTimeout aborts the stream if no bytes arrive for this long.
	// The agents service emits keep-alives ~every 15s, so a longer silence
	// means a hung turn.
	turnIdleTimeout = 90 * time.Second
	// turnHeartbeatEvery is the agent_turns heartbeat cadence (the sweep
	// fails rows stale past turnSweepStaleAfter).
	turnHeartbeatEvery = 15 * time.Second
)

// turnJob is everything the detached runner needs, captured at POST time
// (identities, key, refs — D20: nothing is re-read from a request context).
type turnJob struct {
	turnID           string
	orgID            string
	projectID        string
	flow             string            // recognised `/<skill>` token ("start", "design", …); "" for plain chat
	conversationID   string            // FE-chosen uuid (agent_turns key)
	nsConversationID string            // namespaced agents-service id
	turn             agentsvc.TurnSpec // what this turn is FOR (the agents service composes the text)
	target           string            // spec-bundle path this turn should write to, when pinned
	summary          string            // raw user instruction (feed line subject + journal display, #463)
	// attachments are this message's chat attachments (#428), captured at POST
	// time like everything else here (D20). They live ONLY in this struct
	// between the POST and the dispatch — nothing writes them to disk (ADR-0019)
	// — which is why the turn is the only thing that can carry them.
	attachments []agentsvc.TurnAttachment
	// aim is what the user pointed at in a spec document, and what for (#666).
	// The agents service leads the instruction with it AND journals its anchor
	// from the same value, which is why nothing here writes a second copy onto
	// the journal block: two sources for one fact is how a transcript ends up
	// tagging a selection the model was never told about.
	aim *agentsvc.AimBlock
	// author is the acting user for the journal (#463), nil when the bearer
	// carries no human identity — an M2M token journals no author rather than
	// a bare subject claim.
	author       *agentsvc.JournalAuthor
	repoRef      sourcecontrol.RepoRef
	baseRef      string
	skillsRef    string
	anthropicKey string
	// Room-scoped turn (#86 phase 4): non-empty collabRoomID makes the agents
	// service a live peer of this room (joining with collabToken, the
	// prompting user's bearer). The doc is the write surface — the runner
	// relays frames but folds nothing and commits nothing.
	collabRoomID string
	collabToken  string
}

// runTurnSafe is the panic barrier around the detached turn goroutine: a panic
// anywhere on the turn path (the fold, the YAML frontmatter parser, a
// misbehaving dep) must not crash the whole aep-api process. On recover it logs
// the stack, marks the turn FAILED through the normal finish path — releasing
// the D18 one-active-turn guard immediately and emitting the broker terminal so
// attached viewers are unblocked — and returns. The happy path (and every
// handled terminal) is finished inside runTurn itself; this only fires when
// runTurn unwinds via panic before its own finishTurn ran.
func (s *Service) runTurnSafe(ctx context.Context, job turnJob) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("genai: turn runner panicked — recovered, failing the turn",
				"turn", job.turnID, "panic", r, "stack", string(debug.Stack()))
			// The run context may be poisoned by the unwind; finish on a fresh
			// budget so the failed row + terminal event always land.
			finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			s.finishTurn(finishCtx, job, failedTerminal(turnReasonInternal, "turn runner panicked", nil))
		}
	}()
	s.runTurn(ctx, job)
}

// runTurn drives one detached turn to its terminal state. ctx is already
// detached from the request (context.WithoutCancel).
func (s *Service) runTurn(ctx context.Context, job turnJob) {
	runCtx, cancel := context.WithTimeout(ctx, turnRunTimeout)
	defer cancel()
	term := s.executeTurn(runCtx, job)
	// The terminal write must not die with the run deadline (a timed-out turn
	// still needs its failed row + terminal event) — give it its own budget.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer finishCancel()
	s.finishTurn(finishCtx, job, term)
}

// designOrCollabTurn is the shared gate design-flow turns AND collab
// room-scoped turns satisfy: the Spec view authors design.json interactively
// through collab, so gating on the design flow alone would starve the collab
// architect of list_org_endpoints, causing invented cross-project org-service
// names that fail exact-name resolution at build. mcpForTurn (additionally
// gated on the MCP token minter being wired) and the dispatched
// TurnRequest.WebSearch flag (external-dependency-discovery #252 — Anthropic's
// web_search provider tool, which needs no BFF-minted credential) key off this
// SAME condition. A plain chat turn with no room does not qualify.
func designOrCollabTurn(job turnJob) bool {
	return job.flow == "design" || job.collabRoomID != ""
}

// journalFor is the turn's display record (#463): the raw client-sent
// instruction — exactly what the sender's UI rendered as the user bubble —
// plus the acting user. The agents service stores it beside the transcript;
// its get-conversation read serves it for user rows so the composed prompt
// never reaches a browser. A blank instruction (never sent by real clients)
// journals nothing rather than an empty bubble.
func journalFor(job turnJob) *agentsvc.JournalBlock {
	// Trimming detects a blank instruction only — the journal carries the
	// instruction VERBATIM (that is its contract; the sender's UI rendered
	// exactly these bytes).
	if strings.TrimSpace(job.summary) == "" {
		return nil
	}
	return &agentsvc.JournalBlock{
		Text:        job.summary,
		Author:      job.author,
		Attachments: attachmentNames(job.attachments),
	}
}

// attachmentNames lists an attachment set's names for the journal — names only,
// never bytes (ADR-0019). Nil for an empty set, so a message without attachments
// journals exactly the shape it did before this feature.
//
// Local to this package rather than shared with the handler that parses them:
// genaiturns imports spec, so spec cannot import genaiturns back.
func attachmentNames(as []agentsvc.TurnAttachment) []string {
	if len(as) == 0 {
		return nil
	}
	names := make([]string, 0, len(as))
	for _, a := range as {
		names = append(names, a.Name)
	}
	return names
}

// authorIDOf / authorNameOf flatten the journal author onto the turn row's two
// columns. Nil — an M2M or minimal token, which journals no author rather than
// a bare subject — flattens to two empty strings, the row's "unattributable"
// state and the same one every pre-#562 row carries.
func authorIDOf(a *agentsvc.JournalAuthor) string {
	if a == nil {
		return ""
	}
	return a.ID
}

func authorNameOf(a *agentsvc.JournalAuthor) string {
	if a == nil {
		return ""
	}
	return a.DisplayName
}

// journalAuthorFrom projects the request bearer onto the journal's author
// shape, EMAIL-ANCHORED to match the console's live author identity
// ({id: email, displayName}) — that equality is what lets a rehydrated row
// read as "you" vs a teammate. No email means no attributable human (an M2M
// token, or a minimal user token): journal no author, never a bare subject.
func journalAuthorFrom(ctx context.Context) *agentsvc.JournalAuthor {
	token := auth.GetAuthToken(ctx)
	if token == "" {
		return nil
	}
	name, email := parseDisplayIdentity("Bearer " + token)
	if email == "" {
		return nil
	}
	if name == "" {
		name = email
	}
	return &agentsvc.JournalAuthor{ID: email, DisplayName: name}
}

// mcpForTurn mints the per-turn MCP discovery block for design-generation turns
// AND collab room-scoped turns (dependency-management Phase 5): a BFF-signed
// token (aud aep-api-mcp) carrying the org, plus the BFF's internal MCP endpoint
// the agents service calls back into. Returns nil (no MCP block) when the minter
// / base URL are not wired, when the turn is neither a design-generate nor a
// collab room-scoped turn, or when minting fails — a turn without MCP is
// byte-identical to today, so this is best-effort.
func (s *Service) mcpForTurn(ctx context.Context, job turnJob) *agentsvc.MCPBlock {
	if s.mcpTokens == nil || s.mcpBaseURL == "" {
		return nil
	}
	if !designOrCollabTurn(job) {
		return nil
	}
	token, err := s.mcpTokens.IssueMCPToken(job.orgID)
	if err != nil {
		slog.WarnContext(ctx, "genai: MCP token mint failed — dispatching turn without MCP discovery",
			"turn", job.turnID, "error", err)
		return nil
	}
	return &agentsvc.MCPBlock{
		URL:   strings.TrimRight(s.mcpBaseURL, "/") + "/internal/v1/mcp",
		Token: token,
	}
}

// executeTurn produces the turn's terminal state (never panics the goroutine).
func (s *Service) executeTurn(ctx context.Context, job turnJob) TurnTerminal {
	filesChangedExternally := false
	previousTurnFailed := false
	// D20: both flags are server-derived — the last terminal turn of this
	// conversation landed a ref different from the current base (an Apply,
	// another conversation's turn, or an external push moved main), and/or it
	// failed, leaving the conversation history claiming work git never
	// received. The agents service turns each into its note.
	if last, err := s.turns.LastTerminal(ctx, job.orgID, job.projectID, job.conversationID); err != nil {
		slog.WarnContext(ctx, "genai: last-terminal lookup failed — dispatching without the D20 flags",
			"turn", job.turnID, "error", err)
	} else if last != nil {
		landed := last.CommitSHA
		if landed == "" {
			landed = last.BaseRef
		}
		filesChangedExternally = landed != job.baseRef
		previousTurnFailed = last.Status == turnStatusFailed
	}

	// Heartbeat the row for the WHOLE run, starting BEFORE dispatch (D17).
	// The agents service opens its SSE stream lazily on the first model event,
	// so Dispatch blocks until then — and a turn carrying a large PDF or image
	// can spend well past the 60s stale threshold being read before the model
	// emits anything. Heartbeating only after Dispatch returned left that
	// window unguarded, and the sweep failed healthy turns as "replica crashed
	// or hung".
	hbEvery := s.heartbeatEvery
	if hbEvery <= 0 {
		hbEvery = turnHeartbeatEvery
	}
	hbStop := make(chan struct{})
	defer close(hbStop)
	go func() {
		ticker := time.NewTicker(hbEvery)
		defer ticker.Stop()
		for {
			select {
			case <-hbStop:
				return
			case <-ticker.C:
				if err := s.turns.Heartbeat(ctx, job.turnID); err != nil {
					slog.WarnContext(ctx, "genai: turn heartbeat failed", "turn", job.turnID, "error", err)
				}
			}
		}
	}()

	var collab *agentsvc.CollabBlock
	if job.collabRoomID != "" {
		collab = &agentsvc.CollabBlock{RoomID: job.collabRoomID, Token: job.collabToken}
	}
	body, err := s.client.Turn(ctx, job.nsConversationID, job.orgID, job.anthropicKey, agentsvc.TurnRequest{
		Turn: job.turn,
		Workspace: agentsvc.WorkspaceRef{
			ConversationID: job.nsConversationID,
			TurnID:         job.turnID,
			RepoSlug:       job.repoRef.RepoSlug,
			Ref:            job.baseRef,
			SkillsRef:      job.skillsRef,
		},
		Target:                 job.target,
		FilesChangedExternally: filesChangedExternally,
		PreviousTurnFailed:     previousTurnFailed,
		MCP:                    s.mcpForTurn(ctx, job),
		WebSearch:              designOrCollabTurn(job),
		Collab:                 collab,
		Journal:                journalFor(job),
		Surface:                agentsvc.SurfaceConsole,
		// The attachments themselves, not just their names on the journal. Both
		// are needed and they are NOT the same thing: the journal drives the
		// chips a reader sees, this is what the MODEL reads. Omitting it made a
		// turn look entirely successful — 202, chips rendered, journal correct —
		// while the agent truthfully reported seeing no file.
		Attachments: job.attachments,
		Aim:         job.aim,
	})
	if err != nil {
		slog.WarnContext(ctx, "genai: turn dispatch failed", "turn", job.turnID, "error", err)
		return failedTerminal(turnReasonDispatchFailed, "agents dispatch failed: "+err.Error(), nil)
	}
	defer body.Close()

	// Idle watchdog (plan_tap precedent): any read pulses activity; silence
	// past the deadline closes body to unblock the pending read.
	var idleAborted atomic.Bool
	activity := make(chan struct{}, 1)
	watchStop := make(chan struct{})
	defer close(watchStop)
	go func() {
		timer := time.NewTimer(turnIdleTimeout)
		defer timer.Stop()
		for {
			select {
			case <-watchStop:
				return
			case <-activity:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(turnIdleTimeout)
			case <-timer.C:
				idleAborted.Store(true)
				_ = body.Close()
				return
			}
		}
	}()

	// Relay-only turns: collab roomMode (#86 phase 4) — doc is the write
	// surface — and Marketplace register chat, whose project snapshot is a
	// dual materialization of org-skills (folding would commit into skills).
	// Relay frames only; nothing folds, nothing commits, no manifest gate.
	roomMode := job.collabRoomID != "" || isMarketplaceRegisterProject(job.projectID)

	fold := agentfold.New(s.turnBaseReader(job.repoRef, job.baseRef))
	var manifest *agentfold.Manifest
	var foldErr error
	end, readErr := agentfold.ForEachDataFrame(&pulseReader{r: body, activity: activity}, func(raw []byte) error {
		var part agentfold.StreamPart
		if err := json.Unmarshal(raw, &part); err != nil {
			return nil // truncation remnant — skip, matching the parser rule
		}
		if m, ok := agentfold.ManifestOf(part); ok {
			manifest = &m
			return nil // backend integrity plumbing — never forwarded (D14)
		}
		s.broker.Append(job.turnID, raw)
		if roomMode {
			return nil // doc-applied on the agents side — nothing to fold
		}
		if _, err := fold.ApplyToolCall(ctx, part); err != nil {
			foldErr = err
			return err
		}
		return nil
	})

	if roomMode {
		switch {
		case readErr != nil:
			msg := "agents stream read failed: " + readErr.Error()
			if idleAborted.Load() {
				msg = "agents stream idle past deadline"
			}
			return failedTerminal(turnReasonStreamDied, msg, nil)
		case manifest == nil:
			// Same severed/errored semantics as the committed path — the agents
			// service vouched for nothing.
			msg := "stream ended without a manifest"
			if end == agentfold.StreamEOF {
				msg = "stream severed before the manifest"
			}
			return failedTerminal(turnReasonStreamDied, msg, nil)
		}
		// Edits live in the room's doc; git is untouched (persistence is the
		// #86 phase-3 committer). The base sha stays the "content as of" pin.
		// SpecEdited reflects the agent's doc edits (non-empty manifest) so the
		// feed can attribute this agent work — the committer's later flush lands
		// under the user's token and cannot (issue #239). withUsage folds the
		// manifest's token usage onto the same terminal (#249).
		return withUsage(TurnTerminal{
			Status:      turnStatusCompleted,
			CommitSHA:   job.baseRef,
			NoChanges:   true,
			SpecEdited:  !manifest.IsEmpty(),
			EditedPaths: manifest.MutatedPaths(),
		}, manifest)
	}

	switch {
	case foldErr != nil:
		// The fold's add/edit/remove ops are pure byte-deterministic string
		// mutations; the only error they surface is a base-read infrastructure
		// failure. Byte-parity against the agents-side bundle is enforced
		// separately by the D14 Verify gate below.
		slog.WarnContext(ctx, "genai: fold infrastructure failure", "turn", job.turnID, "error", foldErr)
		return failedTerminal(turnReasonInternal, foldErr.Error(), nil)
	case readErr != nil:
		msg := "agents stream read failed: " + readErr.Error()
		if idleAborted.Load() {
			msg = "agents stream idle past deadline"
		}
		return failedTerminal(turnReasonStreamDied, msg, nil)
	case manifest == nil:
		// Severed (EOF) or completed-with-error ([DONE] after an in-band
		// error frame) — either way agents vouched for nothing: no commit.
		msg := "stream ended without a manifest"
		if end == agentfold.StreamEOF {
			msg = "stream severed before the manifest"
		}
		return failedTerminal(turnReasonStreamDied, msg, nil)
	}

	// D14 integrity gate: the Go fold must agree byte-for-byte with the
	// agents-side FileBundle on every mutated path.
	if err := agentfold.Verify(fold, *manifest); err != nil {
		slog.ErrorContext(ctx, "genai: FOLD PARITY FAILURE — turn rejected, main untouched",
			"turn", job.turnID, "error", err)
		// The model DID run — a parity-rejected turn still burnt its tokens.
		return withUsage(failedTerminal(turnReasonFoldParity, err.Error(), nil), manifest)
	}
	if manifest.IsEmpty() {
		// A chat turn with no file ops: valid, completes with no commit. The
		// terminal still carries the base sha as the "content as of" pin.
		return withUsage(TurnTerminal{Status: turnStatusCompleted, CommitSHA: job.baseRef, NoChanges: true}, manifest)
	}
	// Every genai turn is conversational/preview-only (#373 — the old
	// commit-on-turn useCases are gone): file mutations stream to the client
	// for display via the fold, but NOTHING is committed to main here — room
	// turns persist through the collab server's save path instead. A non-empty
	// fold is reported like a no-op completion (base sha pinned, noChanges),
	// so a refetch on the terminal reconciles the live preview back to the
	// unchanged tree.
	return withUsage(TurnTerminal{Status: turnStatusCompleted, CommitSHA: job.baseRef, NoChanges: true}, manifest)
}

// withUsage stamps the manifest's token spend (#249) onto a terminal. A nil
// manifest or a manifest without usage (pre-capture agents) leaves it unset.
func withUsage(term TurnTerminal, m *agentfold.Manifest) TurnTerminal {
	if m == nil || m.Usage == nil {
		return term
	}
	term.Usage = &contracts.TokenUsage{
		InputTokens:         m.Usage.InputTokens,
		OutputTokens:        m.Usage.OutputTokens,
		CacheReadTokens:     m.Usage.CacheReadTokens,
		CacheCreationTokens: m.Usage.CacheCreationTokens,
		Model:               m.Usage.Model,
	}
	return term
}

// finishTurn stamps the terminal row state and emits the ONE terminal stream
// event. A row that is no longer running (swept mid-run) keeps the sweep's
// verdict — the runner does not overwrite it or emit a competing terminal.
func (s *Service) finishTurn(ctx context.Context, job turnJob, term TurnTerminal) {
	ok, err := s.turns.Finish(ctx, job.turnID, term)
	if err != nil {
		slog.ErrorContext(ctx, "genai: finish turn row failed", "turn", job.turnID, "error", err)
		// Fall through: attached clients still deserve the terminal event.
	} else if !ok {
		slog.WarnContext(ctx, "genai: turn was swept before it finished — keeping the sweep verdict",
			"turn", job.turnID, "wouldBe", term.Status)
		return
	}
	s.broker.Terminal(job.turnID, terminalEventJSON(term))
	s.recordTurnActivity(ctx, job, term)
	// Notify any waiting devflow workflow of the terminal outcome (best-effort).
	// The hook does I/O (a DB lookup + a Temporal signal), so it runs detached
	// with its own bounded context — a slow hook must never delay or fail the
	// turn (the documented TurnFinishHook contract).
	if hook := s.finishHook; hook != nil {
		go func() {
			hookCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			hook(hookCtx, job.orgID, job.projectID, job.turnID, useCaseGeneral, term.Status)
		}()
	}
}

// recordTurnActivity appends the spec_updated feed line for a turn that authored
// real spec changes (issue #239). Best-effort and observational: a nil recorder,
// a failed turn, or a turn that edited nothing records nothing. A genai turn is
// the agent working, so the actor is the agent (the console renders "Spec agent
// updated the spec") regardless of the git commit author — the committer flush
// of a room turn lands under the user's token, which is exactly why the user
// cannot be the feed actor here. The turn id keys dedup so a re-finish is a
// no-op. SpecEdited (not NoChanges) is the gate: a room turn is always NoChanges
// yet still authored the doc edits the feed must attribute.
func (s *Service) recordTurnActivity(ctx context.Context, job turnJob, term TurnTerminal) {
	if s.recorder == nil || term.Status != turnStatusCompleted || !term.SpecEdited {
		return
	}
	s.recorder.RecordSpecUpdated(ctx, job.orgID, job.projectID, job.turnID, firstLine(job.summary, 96), term.EditedPaths)
}

// turnBaseReader adapts Workspace.ReadFile at the turn's base ref into the
// agentfold BaseReader, applying the agents-side InTurnSnapshot filter — a
// path the agents snapshot walk dropped (non-.md/.dsl/design.json, dot-led
// segment, binary) must read as exists=false here even though it is in git,
// or NO_SUCH_FILE parity breaks (agentfold snapshot_filter contract).
func (s *Service) turnBaseReader(ref sourcecontrol.RepoRef, baseRef string) agentfold.BaseReader {
	return func(ctx context.Context, path string) ([]byte, bool, error) {
		content, _, err := s.git.Workspace().ReadFile(ctx, ref, baseRef, path)
		if errors.Is(err, sourcecontrol.ErrPathNotFound) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		if !agentfold.InTurnSnapshot(path, content) {
			return nil, false, nil
		}
		return content, true, nil
	}
}

func firstLine(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if cut := text.Truncate(s, max); cut != s {
		// Re-trim: the cut can land just after a space.
		s = strings.TrimSpace(strings.TrimSuffix(cut, "…")) + "…"
	}
	if s == "" {
		s = "agent turn"
	}
	return s
}

// failedTerminal builds a failed TurnTerminal.
func failedTerminal(reason, message string, paths []string) TurnTerminal {
	return TurnTerminal{Status: turnStatusFailed, Reason: reason, Message: message, Paths: paths}
}

// terminalEventJSON renders the ONE terminal stream event appended by aep-api
// (never by agents): turn-committed / turn-failed, exactly as pinned by the
// console contract.
func terminalEventJSON(term TurnTerminal) []byte {
	var payload any
	if term.Status == turnStatusCompleted {
		payload = struct {
			Type      string `json:"type"`
			CommitSHA string `json:"commitSha"`
			NoChanges bool   `json:"noChanges"`
		}{Type: "turn-committed", CommitSHA: term.CommitSHA, NoChanges: term.NoChanges}
	} else {
		payload = struct {
			Type    string   `json:"type"`
			Reason  string   `json:"reason"`
			Message string   `json:"message,omitempty"`
			Paths   []string `json:"paths,omitempty"`
		}{Type: "turn-failed", Reason: term.Reason, Message: term.Message, Paths: term.Paths}
	}
	b, err := json.Marshal(payload)
	if err != nil { // unreachable: static shapes
		return []byte(`{"type":"turn-failed","reason":"internal"}`)
	}
	return b
}

// pulseReader pulses the idle-watchdog activity channel on every successful
// read (keep-alive comment bytes count — they are exactly the liveness signal).
type pulseReader struct {
	r        io.Reader
	activity chan<- struct{}
}

func (p *pulseReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		select {
		case p.activity <- struct{}{}:
		default:
		}
	}
	return n, err
}
