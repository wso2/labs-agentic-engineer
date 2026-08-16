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

// Package agentsvc is the BFF client for the file-mutation agents service
// (services/agents) — the sole agents backend for all generation and task
// planning flows.
//
// Contract:
//   - POST /conversations/{id}/turns  body {instruction, workspace,
//     filesChangedExternally?, toolset?}  → raw StreamPart SSE frames +
//     [DONE] with `: keep-alive` comments; pre-stream statuses 400/403/409.
//     The body carries IDs + shas only — the agents service reads the turn
//     input from the shared-volume snapshot the workspace ref names; no file
//     content or skills ever cross this boundary.
//   - GET  /conversations/{id}         → {messages: [...]} (no files).
//
// Auth is a plain M2M bearer JWT (aud: agents-service) minted per call. The
// acting org rides the X-Org-Id header — for workspace turns it is
// LOAD-BEARING: the agents service 403s when it does not match the org
// segment woven into the conversation id (the tenancy fence). The per-org
// Anthropic key travels in X-Anthropic-Key (resolved by the caller — there is
// no platform fallback). The client performs NO stream parsing: Turn hands
// back the raw response body, and non-2xx pre-stream responses come back as a
// typed *UpstreamError the caller maps to a BFF status.
package agentsvc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/clients/httpx"
)

// WorkspaceRef names the immutable snapshot dirs a turn reads (design D9,
// §12). IDs + shas only — the agents service derives
// $WORKSPACE_MOUNT_ROOT/repos/<org>/<proj>/<repoSlug>/snapshots/<ref>/ (and
// the _skills analog) itself, so no filesystem path crosses the boundary.
// JSON field names are pinned by @aep/agent-stream's WorkspaceRef
// (packages/agent-stream/src/contracts/sse-events.ts).
type WorkspaceRef struct {
	// ConversationID is the namespaced service id
	// (org_<o>--proj_<p>--<useCase>--<uuid>) — it supplies the org/proj path
	// segments and the org claim for the agents-side fence.
	ConversationID string `json:"conversationId"`
	// TurnID is the per-dispatch turn uuid.
	TurnID string `json:"turnId"`
	// RepoSlug is the repo path segment (git_repositories.repo_slug).
	RepoSlug string `json:"repoSlug"`
	// Ref is the committed base sha (40-hex lowercase) → snapshots/<ref>/.
	Ref string `json:"ref"`
	// SkillsRef is the _skills head sha → _skills/org-skills/snapshots/<skillsRef>/.
	SkillsRef string `json:"skillsRef"`
}

// TurnSpec states what a turn is FOR. The BFF sends facts; the agents service
// composes the instruction text from them (services/agents/src/prompts/turn.ts).
// No prompt wording lives on this side of the wire — see that file, and
// services/agents/design/ADR-0003, for why.
//
// Go has no discriminated unions, so this is one flat struct with a Kind tag
// and per-kind fields; the TS side (@aep/agent-stream `TurnSpec`) is a proper
// union and rejects a mismatched combination with a pre-stream 400.
type TurnSpec struct {
	// Kind is one of: chat | flow | start | plan.
	Kind string `json:"kind"`
	// Text is the user's message (chat), or the free text riding after a flow
	// command (flow).
	Text string `json:"text,omitempty"`
	// Skill is the `/<skill>` token a flow turn names.
	Skill string `json:"skill,omitempty"`
	// Idea is the project idea for a start turn — typed inline, else read from
	// specs/.agentic-engineer.toml. A dot-led path is stripped from every turn
	// snapshot, so ONLY the BFF can supply it.
	Idea string `json:"idea,omitempty"`
	// Scope is the milestone a plan turn covers, and which of its stories
	// already have Tasks.
	Scope *PlanScope `json:"scope,omitempty"`
	// TaskContext carries the existing-Task renders: platform state, not
	// repository files, so it cannot ride the workspace snapshot.
	TaskContext []PlanContextFile `json:"taskContext,omitempty"`
}

// Turn kinds (the `TurnSpec.Kind` discriminant).
const (
	TurnKindChat  = "chat"
	TurnKindFlow  = "flow"
	TurnKindStart = "start"
	TurnKindPlan  = "plan"
)

// PlanScope is a plan turn's milestone and its story coverage.
type PlanScope struct {
	Tag     string      `json:"tag"`
	Stories []PlanStory `json:"stories"`
}

// PlanStory is one in-scope story; Covered means it already has Tasks and the
// planner must leave it alone.
type PlanStory struct {
	Number  int    `json:"number"`
	Title   string `json:"title,omitempty"`
	Covered bool   `json:"covered"`
}

// PlanContextFile is one existing-Task render, keeping its historical
// tasks/<n>.md name so the model's mental layout is unchanged.
type PlanContextFile struct {
	Path string `json:"path"`
	Body string `json:"body"`
}

// TurnRequest is the body POSTed to the turn endpoint (design D9): what the
// turn is for plus the WorkspaceRef naming the snapshot to read.
// FilesChangedExternally is SERVER-derived (D20 — the previous turn's landed
// ref differs from the current base). The tool set and the flow's eager skills
// are NOT sent: the agents service derives both from Turn.
type TurnRequest struct {
	Turn                   TurnSpec     `json:"turn"`
	Workspace              WorkspaceRef `json:"workspace"`
	FilesChangedExternally bool         `json:"filesChangedExternally,omitempty"`
	// Target is the spec-bundle path this turn should write to, when the caller
	// pins one. The agents service renders it; the BFF never formats it.
	Target string `json:"target,omitempty"`
	// PreviousTurnFailed (D20) says the last terminal turn of this conversation
	// failed: the conversation history claims work git never received, and the
	// agents service leads the instruction with the note that reconciles them.
	PreviousTurnFailed bool `json:"previousTurnFailed,omitempty"`
	// MCP, when set, is the BFF-minted discovery endpoint + short-lived bearer
	// for this turn (dependency-management migration Phase 5). Omitted → the
	// agents service registers no MCP discovery tools (byte-identical to a turn
	// without it). The wire shape is pinned by @aep/agent-stream's McpConfig
	// (packages/agent-stream/src/contracts/sse-events.ts): `mcp: {url, token}`.
	MCP *MCPBlock `json:"mcp,omitempty"`
	// Collab, when set, makes this a room-scoped turn (#86 phase 4): the agents
	// service joins the collab room as a live Yjs peer, reads its file bundle
	// FROM the doc, and applies ops to the doc — nothing is committed to git.
	// Wire shape pinned by @aep/agent-stream's CollabConfig.
	Collab *CollabBlock `json:"collab,omitempty"`
	// Journal, when set, is the turn's display record (#463): the raw
	// client-sent instruction (exactly what the sender's UI rendered as the
	// user bubble) plus the acting user's best-effort display identity. The
	// agents service stores it beside the transcript and serves it for user
	// rows on the get-conversation read — the composed model prompt never
	// reaches a browser. Wire shape pinned by @aep/agent-stream's TurnRequest.
	Journal *JournalBlock `json:"journal,omitempty"`
	// WebSearch, when true, has the agents service register Anthropic's
	// provider-executed web_search tool for this turn (external-dependency-
	// discovery #252) — it lets the model verify a candidate external API/SDK
	// actually exists before proposing a dependency for it. Unlike MCP, no
	// BFF-minted credential is needed, so the caller sets this under the SAME
	// gate as MCP (design-generate or any collab room-scoped turn) without
	// depending on the MCP minter being wired. Anthropic-only on the agents
	// side; false/omitted is byte-identical to a turn without it.
	WebSearch bool `json:"webSearch,omitempty"`
}

// CollabBlock names the room and carries the prompting user's bearer,
// forwarded request-scoped (#86 decision 7) — the collab server's oracle
// validates it exactly like a browser join. JSON field names match
// @aep/agent-stream's CollabConfig exactly.
type CollabBlock struct {
	RoomID string `json:"roomId"`
	Token  string `json:"token"`
}

// MCPBlock is the caller-supplied MCP discovery config for a turn. URL is the
// BFF's org-bound MCP JSON-RPC endpoint (aep-api's /internal/v1/mcp); Token is
// the short-lived BFF-signed bearer (aud aep-api-mcp) the agents service
// presents when it calls back. JSON field names match @aep/agent-stream's
// McpConfig exactly.
type MCPBlock struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// JournalBlock is a turn's display record (#463): what the client sent,
// verbatim, and who sent it. Author mirrors the console's live author shape
// ({id: email, displayName}) so a rehydrated row is attributable — and
// self-vs-teammate distinguishable — exactly like a live one; nil for callers
// with no human identity (M2M tokens journal no author, never a bare subject).
type JournalBlock struct {
	Text   string         `json:"text"`
	Author *JournalAuthor `json:"author,omitempty"`
}

// JournalAuthor is the acting user in the console's author shape.
type JournalAuthor struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// UpstreamError is a non-2xx pre-stream response from the agents service. The
// caller maps StatusCode to a BFF status (409 → turn_in_progress, 400/500 →
// 502-style). Once the SSE body has started, failures arrive in-band
// as error frames instead — this type only ever carries a pre-stream status.
type UpstreamError struct {
	StatusCode int
	Body       string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("agents service pre-stream status %d: %s", e.StatusCode, e.Body)
}

// Client is the agents-service turn/rehydrate surface.
type Client interface {
	// Turn POSTs a turn and, on 200, returns the raw SSE body for verbatim
	// passthrough (caller must Close). conversationID is the already-namespaced
	// service id; orgID rides X-Org-Id, which carries the org tenancy claim the
	// agents service enforces (a mismatch vs the conversation-id org segment
	// 403s — it is not merely logged); anthropicKey is forwarded as
	// X-Anthropic-Key (must be non-empty — resolve + 4xx before calling).
	// A non-200 pre-stream response is returned as *UpstreamError.
	Turn(ctx context.Context, conversationID, orgID, anthropicKey string, req TurnRequest) (io.ReadCloser, error)

	// GetConversation returns the raw {messages: [...]} JSON for chat rehydrate.
	// A non-200 (e.g. 404 unknown id) is returned as *UpstreamError.
	GetConversation(ctx context.Context, conversationID, orgID string) (json.RawMessage, error)
}

// Config wires the client. Secret + Audience (+ optional Issuer) drive the M2M
// token; a nil/empty Secret disables signing (dev/tests that never reach the
// network — the service's gate would reject an unsigned request in production).
type Config struct {
	BaseURL  string
	Secret   string // HS256 shared secret (AGENT_JWT_SECRET on the service)
	Audience string // aud claim; defaults to "agents-service"
	Issuer   string // optional iss claim
}

type client struct {
	baseURL    string
	httpClient *http.Client
	signer     tokenSigner
}

// New builds an agents-service client. It uses an HS256 signer for local M2M;
// the tokenSigner seam lets an RS256/JWKS signer slot in without touching the
// call sites. No client-side timeout — turns stream for minutes; cancellation
// flows via ctx.
func New(cfg Config) Client {
	audience := cfg.Audience
	if audience == "" {
		audience = defaultAudience
	}
	var signer tokenSigner
	if cfg.Secret != "" {
		signer = newHS256Signer(cfg.Secret, audience, cfg.Issuer)
	}
	return &client{
		baseURL:    cfg.BaseURL,
		httpClient: &http.Client{Transport: httpx.WrapTransport(nil)},
		signer:     signer,
	}
}

func (c *client) Turn(ctx context.Context, conversationID, orgID, anthropicKey string, req TurnRequest) (io.ReadCloser, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal turn request: %w", err)
	}
	url := c.baseURL + "/conversations/" + conversationID + "/turns"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create turn request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if anthropicKey != "" {
		httpReq.Header.Set("X-Anthropic-Key", anthropicKey)
	}
	if err := c.attachAuth(orgID, httpReq); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("agents service turn request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return resp.Body, nil
}

func (c *client) GetConversation(ctx context.Context, conversationID, orgID string) (json.RawMessage, error) {
	url := c.baseURL + "/conversations/" + conversationID
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create get-conversation request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	if err := c.attachAuth(orgID, httpReq); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("agents service get-conversation request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return json.RawMessage(body), nil
}

// attachAuth mints the per-call M2M bearer and sets the X-Org-Id header, which
// carries the org tenancy claim the agents service enforces (a mismatch vs the
// conversation-id org segment 403s — it is not merely logged). A nil signer
// (dev/tests) skips the bearer.
func (c *client) attachAuth(orgID string, req *http.Request) error {
	if orgID != "" {
		req.Header.Set("X-Org-Id", orgID)
	}
	if c.signer == nil {
		return nil
	}
	tok, err := c.signer.sign()
	if err != nil {
		return fmt.Errorf("mint agents M2M token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return nil
}
