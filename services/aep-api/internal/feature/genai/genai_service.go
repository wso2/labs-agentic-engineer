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

// Package genai is the BFF's committed-truth LLM turn surface
// (docs/design/shared-volume-clone-architecture.md §6, D13–D20). A turn is
// started with POST …/turns → 202 {turnId} and runs detached server-side: the
// runner snapshots the project repo + the org's _skills repo onto the shared
// workspace volume, dispatches the agents service with a WorkspaceRef (IDs +
// shas — no file content on the wire), taps the SSE stream into an in-memory
// broker (resumable GET …/turns/{id}/stream), folds the file mutations in Go
// (internal/platform/agentfold), gates the fold on the agents-side manifest,
// and commits the result straight to main. The durable agent_turns row is the
// one-active-turn-per-project guard and the crash-safety anchor.
package genai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/credentials"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/models"
)

// Valid use cases (the FE-supplied "useCase" field).
const (
	useCaseRequirementsGenerate = "requirements-generate"
	useCaseRequirementsChat     = "requirements-chat"
	useCaseDesignGenerate       = "design-generate"
)

var validUseCases = map[string]bool{
	useCaseRequirementsGenerate: true,
	useCaseRequirementsChat:     true,
	useCaseDesignGenerate:       true,
}

// Errors — pre-202, mapped to HTTP status by the huma layer.
var (
	ErrProjectRepoNotFound     = errors.New("project repository not found")
	ErrInvalidUseCase          = errors.New("invalid use case")
	ErrInvalidConversationID   = errors.New("invalid conversation id")
	ErrEmptyInstruction        = errors.New("instruction must not be empty")
	ErrNoAnthropicKey          = errors.New("organization has no Anthropic API key configured")
	ErrRequirementsNotApproved = errors.New("design generation requires an approved (tagged) requirements version")
	ErrConversationNotFound    = errors.New("conversation not found")
	ErrTurnNotFound            = errors.New("turn not found")
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

// requirementsTagPattern matches a requirements version tag `v<N>` (design
// tags are `v<N>-<M>` and are deliberately excluded — approval is the
// requirements version, not a design revision).
var requirementsTagPattern = regexp.MustCompile(`^v(\d+)$`)

// ---- ports -----------------------------------------------------------------

// RepoResolver looks up the project's git repo row (its OrgID/ProjectID are the
// authenticated tenant scope woven into the namespaced conversation id).
type RepoResolver interface {
	GetRepo(ctx context.Context, orgID, projectID string) (*models.GitRepository, error)
}

// GitReader is the workspace-backed git surface the turn flow drives: reads +
// the commit path (Workspace), per-op credentials (Resolver), and the save
// identity helper. gitrepo.GitOpsService satisfies it.
type GitReader interface {
	Workspace() gitrepo.Workspace
	Resolver() credentials.Resolver
	ResolveSaveIdentities(cred credentials.Credential) (*gitrepo.GitIdentity, *gitrepo.GitIdentity)
}

// AnthropicKeyResolver resolves the effective org Anthropic key. An empty key
// with a nil error means "org has none" → the service raises ErrNoAnthropicKey
// pre-202 (no platform fallback). Wired from AnthropicCredentialService.
type AnthropicKeyResolver func(ctx context.Context, orgID string) (string, error)

// SkillsRepoResolver ensures the org's _skills repo is provisioned (seeded
// with the embedded flow skills) and returns its row — the source of the
// turn's SkillsRef snapshot. Wired at the composition root from the skills
// feature (EnsureProvisioned + GetRepo) so genai holds no skills edge.
type SkillsRepoResolver func(ctx context.Context, orgID string) (*models.GitRepository, error)

// ---- input / views ----------------------------------------------------------

// TurnInput is the assembled turn request (conversationId from the path, the
// rest from the body). There is no Files field — the turn input is the
// committed snapshot at HEAD (D13), and filesChangedExternally is
// server-derived (D20).
type TurnInput struct {
	UseCase        string
	ConversationID string
	Instruction    string
	Target         string
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
}

func turnStatusOf(t *models.AgentTurn) *TurnStatus {
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
	}
}

// ---- service ---------------------------------------------------------------

// GenAIService is the typed entry point behind the turn/status/stream/rehydrate
// endpoints.
type GenAIService interface {
	// StartTurn validates + snapshots + guards, detaches the turn runner, and
	// returns the turn id (the 202 body). Pre-202 failures are the typed
	// errors above (incl. *TurnInProgressError).
	StartTurn(ctx context.Context, orgID, projectID string, in TurnInput) (string, error)
	// TurnStatus returns one turn's status; ErrTurnNotFound for unknown or
	// foreign ids (the row fence is (org, project, id)).
	TurnStatus(ctx context.Context, orgID, projectID, turnID string) (*TurnStatus, error)
	// ActiveTurn returns the project's running turn, or nil when none.
	ActiveTurn(ctx context.Context, orgID, projectID string) (*TurnStatus, error)
	// AttachTurn subscribes to a turn's part stream from an absolute event
	// index. ErrTurnNotFound (404) for unknown/foreign/expired turns;
	// ErrTurnBufferTruncated (409) when a mid-run overflow makes replay
	// impossible.
	AttachTurn(ctx context.Context, orgID, projectID, turnID string, from int) (*TurnSubscription, error)
	// Rehydrate returns the chat conversation's messages JSON (chat only).
	Rehydrate(ctx context.Context, orgID, projectID, conversationID string) (json.RawMessage, error)
}

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
	Snapshots  gitrepo.SnapshotProvider
	SkillsRepo SkillsRepoResolver
	MCPTokens  MCPTokenMinter
	MCPBaseURL string
}

type service struct {
	repos      RepoResolver
	git        GitReader
	keys       AnthropicKeyResolver
	client     agentsvc.Client
	turns      TurnRepository
	broker     *TurnBroker
	snapshots  gitrepo.SnapshotProvider
	skillsRepo SkillsRepoResolver
	mcpTokens  MCPTokenMinter
	mcpBaseURL string
}

// NewService wires the genai service.
func NewService(d ServiceDeps) GenAIService {
	return &service{
		repos:      d.Repos,
		git:        d.Git,
		keys:       d.Keys,
		client:     d.Client,
		turns:      d.Turns,
		broker:     d.Broker,
		snapshots:  d.Snapshots,
		skillsRepo: d.SkillsRepo,
		mcpTokens:  d.MCPTokens,
		mcpBaseURL: d.MCPBaseURL,
	}
}

func (s *service) StartTurn(ctx context.Context, orgID, projectID string, in TurnInput) (string, error) {
	if !validUseCases[in.UseCase] {
		return "", ErrInvalidUseCase
	}
	if !validConversationID(in.ConversationID) {
		return "", ErrInvalidConversationID
	}
	// A blank/whitespace-only instruction can never produce a turn — reject it
	// synchronously (pre-202, no row, guard untaken) instead of letting it fail
	// post-dispatch as an opaque "dispatch-failed".
	if strings.TrimSpace(in.Instruction) == "" {
		return "", ErrEmptyInstruction
	}
	repo, err := s.resolveRepo(ctx, orgID, projectID)
	if err != nil {
		return "", err
	}
	ref, err := gitrepo.ResolveWorkspaceRef(ctx, s.git.Resolver(), orgID, repo)
	if err != nil {
		return "", fmt.Errorf("resolve workspace ref: %w", err)
	}

	// D19 design gate: an approved (tagged) requirements version must exist.
	// The turn then reads HEAD (one real sha, no synthetic merge); the latest
	// vN is stamped as the lineage specTag.
	specTag := ""
	if in.UseCase == useCaseDesignGenerate {
		specTag, err = s.latestRequirementsTag(ctx, ref)
		if err != nil {
			return "", err
		}
	}

	key, err := s.resolveKey(ctx, orgID)
	if err != nil {
		return "", err
	}

	// D20: identities are captured NOW — the turn runs detached, with no
	// request context left to consult. Author = the prompting user (JWT
	// subject + a noreply address; Thunder claims carry no email), committer
	// = the org credential's save identity (the AEP bot fallback).
	author, committer := s.captureIdentities(ctx, ref)

	ws := s.git.Workspace()
	baseRef, err := ws.Head(ctx, ref, "")
	if err != nil {
		return "", fmt.Errorf("resolve base ref: %w", err)
	}
	// Skills resolve failures are typed: both arms mean the org's _skills repo
	// is unusable right now (row missing/unprovisionable, or the backing repo
	// gone/unreachable — e.g. deleted externally under a lingering row). The
	// edge maps this to a logged 503 rather than the old opaque 500.
	skillsRow, err := s.skillsRepo(ctx, orgID)
	if err != nil {
		return "", fmt.Errorf("%w: resolve repo row: %w", ErrSkillsRepoUnavailable, err)
	}
	skillsRepoRef := gitrepo.WorkspaceRefFor(orgID, skillsRow, ref.Cred)
	skillsRef, err := ws.Head(ctx, skillsRepoRef, "")
	if err != nil {
		return "", fmt.Errorf("%w: resolve head: %w", ErrSkillsRepoUnavailable, err)
	}
	if err := s.snapshots.Ensure(ctx, ref, baseRef); err != nil {
		return "", fmt.Errorf("ensure repo snapshot: %w", err)
	}
	if err := s.snapshots.Ensure(ctx, skillsRepoRef, skillsRef); err != nil {
		return "", fmt.Errorf("ensure skills snapshot: %w", err)
	}

	// D18 guard: one active turn per project, any use case.
	row, err := s.turns.TryStart(ctx, &models.AgentTurn{
		OrgID:          orgID,
		ProjectID:      projectID,
		ConversationID: in.ConversationID,
		UseCase:        in.UseCase,
		BaseRef:        baseRef,
		SkillsRef:      skillsRef,
		Status:         turnStatusRunning,
		SpecTag:        specTag,
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
		useCase:          in.UseCase,
		conversationID:   in.ConversationID,
		nsConversationID: namespacedID(repo, in.UseCase, in.ConversationID),
		instruction:      in.Instruction + steeringByUseCase[in.UseCase] + targetSuffix(in.Target),
		summary:          in.Instruction,
		repoRef:          ref,
		baseRef:          baseRef,
		skillsRef:        skillsRef,
		anthropicKey:     key,
		author:           author,
		committer:        committer,
	}
	// Detached: the turn runs to completion (or a terminal failure) server-
	// side regardless of the client connection (D16). runTurnSafe is the panic
	// barrier so a fold/parser panic fails just this turn, not the process.
	go s.runTurnSafe(context.WithoutCancel(ctx), job)
	return row.ID, nil
}

func (s *service) TurnStatus(ctx context.Context, orgID, projectID, turnID string) (*TurnStatus, error) {
	t, err := s.turns.Get(ctx, orgID, projectID, turnID)
	if err != nil {
		return nil, fmt.Errorf("get turn: %w", err)
	}
	if t == nil {
		return nil, ErrTurnNotFound
	}
	return turnStatusOf(t), nil
}

func (s *service) ActiveTurn(ctx context.Context, orgID, projectID string) (*TurnStatus, error) {
	t, err := s.turns.GetActive(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get active turn: %w", err)
	}
	if t == nil {
		return nil, nil
	}
	return turnStatusOf(t), nil
}

func (s *service) AttachTurn(ctx context.Context, orgID, projectID, turnID string, from int) (*TurnSubscription, error) {
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
		// FE falls back to the status GET.
		return nil, ErrTurnNotFound
	}
	if err != nil {
		return nil, err
	}
	return sub, nil
}

func (s *service) Rehydrate(ctx context.Context, orgID, projectID, conversationID string) (json.RawMessage, error) {
	if !validConversationID(conversationID) {
		return nil, ErrInvalidConversationID
	}
	repo, err := s.resolveRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	// Rehydrate is chat-only (single-turn generate flows never rehydrate), so
	// the namespaced id is reconstructed under the requirements-chat use case.
	convID := namespacedID(repo, useCaseRequirementsChat, conversationID)
	raw, err := s.client.GetConversation(ctx, convID, orgID)
	if err != nil {
		var ue *agentsvc.UpstreamError
		if errors.As(err, &ue) && ue.StatusCode == 404 {
			return nil, ErrConversationNotFound
		}
		return nil, err
	}
	return raw, nil
}

func (s *service) resolveRepo(ctx context.Context, orgID, projectID string) (*models.GitRepository, error) {
	if s == nil || s.repos == nil {
		return nil, ErrProjectRepoNotFound
	}
	repo, err := s.repos.GetRepo(ctx, orgID, projectID)
	if err != nil {
		if errors.Is(err, gitrepo.ErrRepoNotFound) {
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

func (s *service) resolveKey(ctx context.Context, orgID string) (string, error) {
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

// latestRequirementsTag lists the v* tags and returns the highest `v<N>`
// requirements tag name, or ErrRequirementsNotApproved when none exists — the
// D19 gate.
func (s *service) latestRequirementsTag(ctx context.Context, ref gitrepo.RepoRef) (string, error) {
	tags, err := s.git.Workspace().ListTags(ctx, ref, "v")
	if err != nil {
		return "", fmt.Errorf("list requirement tags: %w", err)
	}
	bestN := -1
	best := ""
	for _, tag := range tags {
		m := requirementsTagPattern.FindStringSubmatch(tag.Name)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if n > bestN {
			bestN, best = n, tag.Name
		}
	}
	if bestN < 0 {
		return "", ErrRequirementsNotApproved
	}
	return best, nil
}

// captureIdentities resolves the D20 commit identities at POST time: author =
// the prompting user from the verified JWT claims (subject + noreply email —
// Thunder claims carry no email/display name), committer = the org
// credential's save identity (falls back to the AEP bot). When no claims are
// present (should not happen on the authed edge), both fall back to the
// committer.
func (s *service) captureIdentities(ctx context.Context, ref gitrepo.RepoRef) (author, committer *gitrepo.GitIdentity) {
	_, committer = s.git.ResolveSaveIdentities(ref.Cred)
	author = committer
	if claims := auth.ClaimsFromContext(ctx); claims != nil && claims.Subject != "" {
		author = &gitrepo.GitIdentity{
			Name:  claims.Subject,
			Email: noreplyEmail(claims.Subject),
		}
	}
	return author, committer
}

// noreplyEmail derives a stable, valid noreply address from a JWT subject.
func noreplyEmail(subject string) string {
	var b strings.Builder
	for _, r := range subject {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	local := b.String()
	if local == "" {
		local = "aep-user"
	}
	return local + "@users.noreply.aep.dev"
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
func namespacedID(repo *models.GitRepository, useCase, uuid string) string {
	return agentsvc.ConversationID(repo.OrgID, repo.ProjectID, useCase, uuid)
}

func targetSuffix(target string) string {
	if strings.TrimSpace(target) == "" {
		return ""
	}
	return "\n\n(target: " + target + ")"
}
