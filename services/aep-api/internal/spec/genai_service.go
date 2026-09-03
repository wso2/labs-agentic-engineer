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

// Package genai is the BFF's committed-truth LLM turn surface. A turn is
// started with POST …/turns → 202 {turnId} and runs detached server-side: the
// runner snapshots the project repo + the org's _skills repo onto the shared
// workspace volume, dispatches the agents service with a WorkspaceRef (IDs +
// shas — no file content on the wire), taps the SSE stream into an in-memory
// broker (resumable GET …/turns/{id}/stream), folds the file mutations in Go
// (internal/platform/agentfold), gates the fold on the agents-side manifest,
// and commits the result straight to main. The durable agent_turns row is the
// one-active-turn-per-project guard and the crash-safety anchor.
package spec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// useCaseGeneral is the single flow identity every genai turn runs under. The
// old FE-supplied "useCase" field is gone (#373): flows ride `/<skill>`
// instructions, and this constant survives only because it is baked into
// conversation identities (namespacedID) and turn rows — changing it would
// strand every existing conversation.
const useCaseGeneral = "general"

// Errors — pre-202, mapped to HTTP status by the huma layer.
var (
	ErrProjectRepoNotFound   = errors.New("project repository not found")
	ErrInvalidConversationID = errors.New("invalid conversation id")
	ErrEmptyInstruction      = errors.New("instruction must not be empty")
	// ErrCollabNoToken rejects a room-scoped turn whose request carried no
	// bearer — the agent joins the room with the caller's token (#86 d7).
	ErrCollabNoToken        = errors.New("collab turn requires a bearer token")
	ErrNoAnthropicKey       = errors.New("organization has no Anthropic API key configured")
	ErrConversationNotFound = errors.New("conversation not found")
	ErrTurnNotFound         = errors.New("turn not found")
	// ErrConversationRotated refuses a turn addressed to a thread that is no
	// longer the project's current one (#430) — a teammate rotated while this
	// client held a resolved id. Mapped to the pinned 409 TurnConflict body
	// (code conversation_rotated); the console re-resolves and retries. The
	// SINGLE-ERA rule: when multiple live conversations land, this check
	// relaxes to "id exists and belongs to this project", because posting to
	// an older thread stops being an error and becomes a feature.
	ErrConversationRotated = errors.New("conversation is not the project's current thread")
	// ErrSkillsRepoUnavailable means the org's _skills repo (the turn's
	// SkillsRef source) could not be resolved — its row is missing or
	// unprovisionable, or the backing repo is gone/unreachable (live incident:
	// the GitHub repo was deleted externally while its git_repositories row
	// lingered). Mapped to a LOGGED 503 with a clear message instead of the
	// generic 500 that previously swallowed the cause. Wraps the underlying
	// error for the logs. Recovery is a manual operator action today: delete
	// the stale `_skills` git_repositories row — the next resolve re-provisions
	// and re-seeds the repo.
	ErrSkillsRepoUnavailable = errors.New("org skills repository unavailable")
)

// TurnInProgressError is the D18 guard rejection: another turn holds the
// project's one-active slot. The huma layer renders it as
// 409 {"code":"turn_in_progress","activeTurnId":…} so the FE can attach to
// the running stream as a viewer.
type TurnInProgressError struct {
	ActiveTurnID string
}

func (e *TurnInProgressError) Error() string {
	return "a turn is already running for this project (active turn " + e.ActiveTurnID + ")"
}

// conversationIDPattern bounds the FE-chosen id to a safe shape. A "--"
// substring is additionally rejected (see validConversationID) so the FE can
// never inject the namespace separator and escape its tenant scope.
var conversationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,200}$`)

// ---- ports -----------------------------------------------------------------

// RepoResolver looks up the project's git repo row (its OrgID/ProjectID are the
// authenticated tenant scope woven into the namespaced conversation id).
type RepoResolver interface {
	GetRepo(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error)
}

// GitReader is the workspace-backed git surface the turn flow drives: reads +
// the commit path (Workspace), per-op credentials (Resolver), and the save
// identity helper. sourcecontrol.GitOpsService satisfies it.
type GitReader interface {
	Workspace() sourcecontrol.Workspace
	Resolver() secrets.Resolver
	ResolveSaveIdentities(cred secrets.Credential) (*sourcecontrol.GitIdentity, *sourcecontrol.GitIdentity)
}

// AnthropicKeyResolver resolves the effective org Anthropic key. An empty key
// with a nil error means "org has none" → the service raises ErrNoAnthropicKey
// pre-202 (no platform fallback). Wired from AnthropicCredentialService.
type AnthropicKeyResolver func(ctx context.Context, orgID string) (string, error)

// SkillsRepoResolver returns the org _skills git row used as a turn's
// SkillsRef snapshot source. Production wires SkillsRepoForTurns so the
// library is reconciled (not only first-touch provisioned). Genai holds no
// skills edge.
type SkillsRepoResolver func(ctx context.Context, orgID string) (*sourcecontrol.GitRepository, error)

// SkillsRepoForTurns is the production SkillsRepoResolver: reconcile the org
// _skills library so platform skills shipped after first provision land,
// then return the row. EnsureProvisioned alone is first-touch seed.
func SkillsRepoForTurns(skills *SkillService, repos RepoResolver) SkillsRepoResolver {
	return func(ctx context.Context, orgID string) (*sourcecontrol.GitRepository, error) {
		if _, err := skills.Reconcile(ctx, orgID); err != nil {
			return nil, fmt.Errorf("%w: reconcile: %w", ErrSkillsRepoUnavailable, err)
		}
		return repos.GetRepo(ctx, orgID, SkillsRepoSentinelProjectID)
	}
}

// ---- input / views ----------------------------------------------------------

// TurnInput is the assembled turn request (conversationId from the path, the
// rest from the body). There is no Files field — the turn input is the
// committed snapshot at HEAD (D13), and filesChangedExternally is
// server-derived (D20).
type TurnInput struct {
	ConversationID string
	Instruction    string
	Target         string
	// Collab makes this a room-scoped turn (#86 phase 4): the agents service
	// joins the project's spec room as a live Yjs peer with the prompting
	// user's bearer, edits the shared doc, and nothing is committed to git.
	Collab bool
	// Attachments are files the user attached to THIS message (#428). Never
	// stored: they ride the turn request and are durable only as parts of the
	// conversation's history (ADR-0019).
	Attachments []agentsvc.TurnAttachment
	// Aim is what the user pointed at in a spec document, and what for (#666).
	// Nil for an ordinary chat turn, which then reaches the agents service
	// byte-identical to one sent before this channel existed.
	Aim *agentsvc.AimBlock
}

// TurnStatus is the read view of one turn (the status GET body).
type TurnStatus struct {
	TurnID         string    `json:"turnId"`
	ConversationID string    `json:"conversationId"`
	UseCase        string    `json:"useCase"`
	Status         string    `json:"status"`
	CommitSHA      string    `json:"commitSha,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	Paths          []string  `json:"paths,omitempty"`
	NoChanges      bool      `json:"noChanges,omitempty"`
	Message        string    `json:"message,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	// The turn's DISPLAY record (#562) — the transcript line for the message
	// that started it, and who sent it. A client attaching to a turn it did not
	// send has no other source for these until the turn lands: the conversation
	// store persists a turn's transcript only at the end. Empty on rows written
	// before this existed, and on turns with no attributable human behind them.
	Instruction       string `json:"instruction,omitempty"`
	AuthorID          string `json:"authorId,omitempty"`
	AuthorDisplayName string `json:"authorDisplayName,omitempty"`
}

func turnStatusOf(t *AgentTurn) *TurnStatus {
	return &TurnStatus{
		TurnID:         t.ID,
		ConversationID: t.ConversationID,
		UseCase:        t.UseCase,
		Status:         t.Status,
		CommitSHA:      t.CommitSHA,
		Reason:         t.Reason,
		Paths:          decodePaths(t.Paths),
		NoChanges:      t.NoChanges,
		Message:        t.Message,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,

		Instruction:       t.Summary,
		AuthorID:          t.AuthorID,
		AuthorDisplayName: t.AuthorDisplayName,
	}
}

// ---- service ---------------------------------------------------------------

// MCPTokenMinter mints a short-lived BFF-signed identity token (aud
// aep-api-mcp) carrying orgID, for the agents service to call back into the
// BFF's internal MCP discovery surface on a generation turn.
// *auth.TaskTokenManager satisfies it via IssueMCPToken; defined here (consumer
// side) so genai needn't import the platform/auth package.
type MCPTokenMinter interface {
	IssueMCPToken(orgID string) (string, error)
}

// ServiceDeps wires the genai service. Repos..SkillsRepo are required; MCPTokens
// + MCPBaseURL are optional (dependency-management Phase 5) — when both are set,
// design-generation turns carry a BFF-minted `mcp: {url, token}` block so the
// architect can discover org endpoints / external resources / resource types.
type ServiceDeps struct {
	Repos      RepoResolver
	Git        GitReader
	Keys       AnthropicKeyResolver
	Client     agentsvc.Client
	Turns      TurnRepository
	Broker     *TurnBroker
	Snapshots  sourcecontrol.SnapshotProvider
	SkillsRepo SkillsRepoResolver
	// Conversations is the project-scoped thread store (#430): resolve/rotate
	// the current thread, and the single-era admission fence on StartTurn.
	// Optional (nil skips the fence) — a test seam; production always wires it.
	Conversations ConversationRepository
	MCPTokens     MCPTokenMinter
	MCPBaseURL    string
	// TurnFinishHook, when set, is invoked once with the terminal outcome of
	// every turn (outcome is "completed" | "failed"). The devflow feature uses
	// it to signal a waiting design-generate workflow. Best-effort — a nil hook
	// or a slow hook must never delay or fail the turn. Kept as a plain func so
	// the genai package does not import devflow.
	TurnFinishHook func(ctx context.Context, orgID, projectID, turnID, useCase, outcome string)
	// Recorder, when set, gets the spec_updated feed line for every turn that
	// lands a real commit (issue #239). Optional; nil disables recording.
	Recorder TurnActivityRecorder
}

// TurnActivityRecorder appends the spec_updated activity line (issue #239) for
// a turn that authored spec changes. Primitives only, so spec need not import
// projects (the app-root adapter maps these onto the activity service's input).
// Best-effort: implementations never return an error — recording must not fail
// a turn. The actor is always the agent (a turn is the agent working), so no
// user identity is passed.
type TurnActivityRecorder interface {
	// RecordSpecUpdated appends the agent-authored line for turnID. title is the
	// turn's instruction subject. editedPaths are the collab-doc paths a room
	// turn edited — they let the implementation suppress the committer's later
	// flush of the same edits; a committed turn passes nil (its commit is its
	// own, nothing flushes later).
	RecordSpecUpdated(ctx context.Context, orgID, projectID, turnID, title string, editedPaths []string)
}

// Service is the typed entry point behind the turn/status/stream/rehydrate
// endpoints. edge.Deps holds it as a concrete *genai.Service — there is one
// implementation and no test fake (the old GenAIService interface had no
// substitution; the component tier exercises the real service).
type Service struct {
	repos  RepoResolver
	git    GitReader
	keys   AnthropicKeyResolver
	client agentsvc.Client
	turns  TurnRepository
	broker *TurnBroker
	// heartbeatEvery is the agent_turns heartbeat cadence. A field rather than
	// the constant so a component test can drive it faster than 15s.
	heartbeatEvery time.Duration
	snapshots      sourcecontrol.SnapshotProvider
	skillsRepo     SkillsRepoResolver
	conversations  ConversationRepository
	mcpTokens      MCPTokenMinter
	mcpBaseURL     string
	finishHook     func(ctx context.Context, orgID, projectID, turnID, useCase, outcome string)
	recorder       TurnActivityRecorder
}

// NewService wires the genai service.
func NewService(d ServiceDeps) *Service {
	return &Service{
		repos:          d.Repos,
		git:            d.Git,
		keys:           d.Keys,
		client:         d.Client,
		turns:          d.Turns,
		broker:         d.Broker,
		snapshots:      d.Snapshots,
		heartbeatEvery: turnHeartbeatEvery,
		skillsRepo:     d.SkillsRepo,
		conversations:  d.Conversations,
		mcpTokens:      d.MCPTokens,
		mcpBaseURL:     d.MCPBaseURL,
		finishHook:     d.TurnFinishHook,
		recorder:       d.Recorder,
	}
}

func (s *Service) StartTurn(ctx context.Context, orgID, projectID string, in TurnInput) (string, error) {
	if !validConversationID(in.ConversationID) {
		return "", ErrInvalidConversationID
	}
	// #430 admission fence, cheap and first: the addressed thread must be the
	// project's current one, so a send racing a teammate's rotation fails
	// fast (409 conversation_rotated → the console re-resolves) instead of
	// landing a turn in a thread nobody is looking at. Nil repo skips — a
	// test seam; production always wires it.
	if s.conversations != nil {
		ok, err := s.conversations.IsCurrent(ctx, orgID, projectID, useCaseGeneral, in.ConversationID)
		if err != nil {
			return "", fmt.Errorf("resolve current conversation: %w", err)
		}
		if !ok {
			return "", ErrConversationRotated
		}
	}
	// A blank/whitespace-only instruction can never produce a turn — reject it
	// synchronously (pre-202, no row, guard untaken) instead of letting it fail
	// post-dispatch as an opaque "dispatch-failed".
	if strings.TrimSpace(in.Instruction) == "" {
		return "", ErrEmptyInstruction
	}
	key, err := s.resolveKey(ctx, orgID)
	if err != nil {
		return "", err
	}

	// Room-scoped turn (#86 phase 4): capture the room + the prompting user's
	// bearer NOW (D20 — the runner has no request context). Access is
	// request-scoped: the collab server's oracle validates this token exactly
	// like a browser join; no token → the turn cannot join, fail pre-202.
	// The synthetic Marketplace register project has no spec room (no git
	// repo; the id is not a DNS label) — ignore collab:true from the panel.
	collabRoomID, collabToken := "", ""
	if in.Collab && !isMarketplaceRegisterProject(projectID) {
		collabToken = auth.GetAuthToken(ctx)
		if collabToken == "" {
			return "", ErrCollabNoToken
		}
		collabRoomID = "spec-" + orgID + "-" + projectID
	}

	ws := s.git.Workspace()

	// Workspace + skills snapshots. Real projects resolve their git row; the
	// synthetic Marketplace register project has none — we rewrite a skills
	// RepoRef onto the agents project snapshot path instead (controller
	// ruling: no GitHub / git_repositories row for that id). Skills resolve +
	// dual Ensure are shared below so error/Ensure changes cannot drift.
	var (
		ref              sourcecontrol.RepoRef
		baseRef          string
		nsConversationID string
		skillsCred       secrets.Credential
	)
	if isMarketplaceRegisterProject(projectID) {
		cred, err := s.git.Resolver().Resolve(ctx, orgID)
		if err != nil {
			return "", fmt.Errorf("resolve credential: %w", err)
		}
		skillsCred = cred
		nsConversationID = agentsvc.ConversationID(orgID, projectID, useCaseGeneral, in.ConversationID)
	} else {
		repo, err := s.resolveRepo(ctx, orgID, projectID)
		if err != nil {
			return "", err
		}
		ref, err = sourcecontrol.ResolveWorkspaceRef(ctx, s.git.Resolver(), orgID, repo)
		if err != nil {
			return "", fmt.Errorf("resolve workspace ref: %w", err)
		}
		baseRef, err = ws.Head(ctx, ref, "")
		if err != nil {
			return "", fmt.Errorf("resolve base ref: %w", err)
		}
		skillsCred = ref.Cred
		nsConversationID = namespacedID(repo, useCaseGeneral, in.ConversationID)
	}

	skillsRow, err := s.skillsRepo(ctx, orgID)
	if err != nil {
		return "", fmt.Errorf("%w: resolve repo row: %w", ErrSkillsRepoUnavailable, err)
	}
	skillsRepoRef := sourcecontrol.WorkspaceRefFor(orgID, skillsRow, skillsCred)
	skillsRef, err := ws.Head(ctx, skillsRepoRef, "")
	if err != nil {
		return "", fmt.Errorf("%w: resolve head: %w", ErrSkillsRepoUnavailable, err)
	}
	if isMarketplaceRegisterProject(projectID) {
		ref = skillsRepoRef
		ref.ProjectID = MarketplaceRegisterProjectID
		ref.RepoSlug = marketplaceRegisterRepoSlug
		baseRef = skillsRef
	}
	if err := s.snapshots.Ensure(ctx, ref, baseRef); err != nil {
		return "", fmt.Errorf("ensure repo snapshot: %w", err)
	}
	if err := s.snapshots.Ensure(ctx, skillsRepoRef, skillsRef); err != nil {
		return "", fmt.Errorf("ensure skills snapshot: %w", err)
	}

	// Flow recognition (#373): `/<skill>` commands arrive VERBATIM and the
	// server classifies them into a TurnSpec — WHAT the turn is for, never its
	// wording (the agents service composes that). `/start` additionally carries
	// the captured idea from specs/.agentic-engineer.toml, which no client
	// parses and the agent cannot read (dot-led segments are stripped from
	// every turn snapshot; an idea typed inline wins). Best-effort — no
	// descriptor, no idea, and the start skill asks the user instead.
	turnSpec, flow := s.turnSpecFor(ctx, ref, baseRef, in.Instruction)
	// What the transcript will SHOW for this turn. Ordinarily the instruction
	// verbatim — but a bare `/start` says nothing about what it is starting,
	// and the idea it carries is exactly the reassurance the user needs on the
	// journey's first screen: the agent is working from THEIR words (#528).
	// Only `/start` is rewritten, and only to append an idea the server just
	// resolved for the same turn, so the line still describes what was sent.
	summary := startTurnSummary(in.Instruction, turnSpec)

	// D18 guard: one active turn per project, any use case.
	// The display record rides the row itself: a client attaching to this turn
	// reads it off the active-turn response and paints the sender's message,
	// which no other source can give it until the turn lands (see AgentTurn).
	author := journalAuthorFrom(ctx)
	row, err := s.turns.TryStart(ctx, &AgentTurn{
		OrgID:             orgID,
		ProjectID:         projectID,
		ConversationID:    in.ConversationID,
		UseCase:           useCaseGeneral,
		Flow:              flow,
		BaseRef:           baseRef,
		SkillsRef:         skillsRef,
		Status:            turnStatusRunning,
		Summary:           summary,
		AuthorID:          authorIDOf(author),
		AuthorDisplayName: authorNameOf(author),
	})
	if errors.Is(err, ErrTurnActive) {
		return "", &TurnInProgressError{ActiveTurnID: row.ID}
	}
	if err != nil {
		return "", fmt.Errorf("start turn: %w", err)
	}
	s.broker.Open(row.ID)

	job := turnJob{
		turnID:           row.ID,
		orgID:            orgID,
		projectID:        projectID,
		flow:             flow,
		conversationID:   in.ConversationID,
		nsConversationID: nsConversationID,
		turn:             turnSpec,
		target:           in.Target,
		summary:          summary,
		attachments:      in.Attachments,
		aim:              in.Aim,
		// Captured before the detached goroutine: the identity reads the
		// request's bearer, and the journal (#463) attributes the turn.
		author:       author,
		repoRef:      ref,
		baseRef:      baseRef,
		skillsRef:    skillsRef,
		anthropicKey: key,
		collabRoomID: collabRoomID,
		collabToken:  collabToken,
	}
	// Detached: the turn runs to completion (or a terminal failure) server-
	// side regardless of the client connection (D16). runTurnSafe is the panic
	// barrier so a fold/parser panic fails just this turn, not the process.
	go s.runTurnSafe(context.WithoutCancel(ctx), job)
	return row.ID, nil
}

func (s *Service) TurnStatus(ctx context.Context, orgID, projectID, turnID string) (*TurnStatus, error) {
	t, err := s.turns.Get(ctx, orgID, projectID, turnID)
	if err != nil {
		return nil, fmt.Errorf("get turn: %w", err)
	}
	if t == nil {
		return nil, ErrTurnNotFound
	}
	return turnStatusOf(t), nil
}

func (s *Service) ActiveTurn(ctx context.Context, orgID, projectID string) (*TurnStatus, error) {
	t, err := s.turns.GetActive(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get active turn: %w", err)
	}
	if t == nil {
		return nil, nil
	}
	return turnStatusOf(t), nil
}

func (s *Service) AttachTurn(ctx context.Context, orgID, projectID, turnID string, from int) (*TurnSubscription, error) {
	// Tenant fence first: the broker is keyed by turn id alone, so ownership
	// is checked against the durable row before any buffer is exposed.
	t, err := s.turns.Get(ctx, orgID, projectID, turnID)
	if err != nil {
		return nil, fmt.Errorf("get turn: %w", err)
	}
	if t == nil {
		return nil, ErrTurnNotFound
	}
	sub, err := s.broker.Subscribe(turnID, from)
	if errors.Is(err, ErrTurnNotBuffered) {
		// Known turn, expired (or other-replica) buffer → 404 pre-stream; the
		// console retries attach with backoff and settles via the status GET when
		// the turn is already terminal.
		return nil, ErrTurnNotFound
	}
	if err != nil {
		return nil, err
	}
	return sub, nil
}

func (s *Service) Rehydrate(ctx context.Context, orgID, projectID, conversationID string) (json.RawMessage, error) {
	if !validConversationID(conversationID) {
		return nil, ErrInvalidConversationID
	}
	var convID string
	if isMarketplaceRegisterProject(projectID) {
		// Synthetic register chat has no git_repositories row — build the
		// agents tenancy id from the JWT org + path project id directly.
		convID = agentsvc.ConversationID(orgID, projectID, useCaseGeneral, conversationID)
	} else {
		repo, err := s.resolveRepo(ctx, orgID, projectID)
		if err != nil {
			return nil, err
		}
		// Rehydrate is chat-only (single-turn generate flows never rehydrate). The
		// console omits "useCase", so its turns are namespaced under useCaseGeneral;
		// reconstruct the id under the same use case the write path stored it with.
		convID = namespacedID(repo, useCaseGeneral, conversationID)
	}
	raw, err := s.client.GetConversation(ctx, convID, orgID)
	if err != nil {
		var ue *agentsvc.UpstreamError
		if errors.As(err, &ue) && ue.StatusCode == 404 {
			// A thread the BFF minted (#430) has no agents-store row until its
			// first turn — and the store's TTL sweep can also reap an idle one.
			// Either way the thread EXISTS and its history is EMPTY: answer
			// that, and reserve 404 for genuinely unknown ids, because the
			// console treats 404-class failures as "keep painting the local
			// cache" and an empty-thread 404 would leave stale logs immortal.
			if s.conversations != nil {
				known, kerr := s.conversations.Exists(ctx, orgID, projectID, useCaseGeneral, conversationID)
				if kerr != nil {
					return nil, fmt.Errorf("resolve thread existence: %w", kerr)
				}
				if known {
					return json.RawMessage(`{"messages":[]}`), nil
				}
			}
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	return raw, nil
}

func (s *Service) resolveRepo(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error) {
	if s == nil || s.repos == nil {
		return nil, ErrProjectRepoNotFound
	}
	repo, err := s.repos.GetRepo(ctx, orgID, projectID)
	if err != nil {
		if errors.Is(err, sourcecontrol.ErrRepoNotFound) {
			return nil, ErrProjectRepoNotFound
		}
		return nil, fmt.Errorf("resolve project repo: %w", err)
	}
	if repo == nil {
		return nil, ErrProjectRepoNotFound
	}
	// A repo that is not ready yet (still provisioning / errored) cannot back
	// a snapshot — surface it as not-found rather than dispatching a turn
	// that would fail at the mirror.
	if repo.Status != "" && repo.Status != "ready" {
		return nil, ErrProjectRepoNotFound
	}
	return repo, nil
}

func (s *Service) resolveKey(ctx context.Context, orgID string) (string, error) {
	if s.keys == nil {
		return "", ErrNoAnthropicKey
	}
	key, err := s.keys(ctx, orgID)
	if err != nil {
		return "", fmt.Errorf("resolve anthropic key: %w", err)
	}
	if key == "" {
		return "", ErrNoAnthropicKey
	}
	return key, nil
}

// ---- pure helpers ----------------------------------------------------------

// validConversationID enforces the safe shape and rejects the namespace
// separator so a crafted id cannot escape the tenant scope.
func validConversationID(id string) bool {
	return conversationIDPattern.MatchString(id) && !strings.Contains(id, "--")
}

// namespacedID scopes the FE-supplied conversation uuid to the authenticated
// tenant + use case via the shared agentsvc encoding. The FE never sees the
// namespaced id; validConversationID rejects "--" so the uuid cannot forge
// extra segments.
func namespacedID(repo *sourcecontrol.GitRepository, useCase, uuid string) string {
	return agentsvc.ConversationID(repo.OrgID, repo.ProjectID, useCase, uuid)
}
