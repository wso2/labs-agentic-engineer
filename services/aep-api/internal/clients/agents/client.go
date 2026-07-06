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

// Package agents is the client for the aep-agents-service (AI SDK v6-based,
// ANTHROPIC_API_KEY auth). Streams AI SDK UI Message Stream SSE responses for
// the streaming agents (business-analyst, architect, tech-lead).
package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/httpx"
	"github.com/wso2/aep/aep-api/models"
)

// agentsAudience is the aud claim the BFF stamps on outbound agents tokens and
// agents-service verifies. serviceTokenTTL is short — the token only needs to
// be valid when the request reaches agents; once the stream is accepted its
// expiry is irrelevant, so minutes is ample and bounds replay.
const (
	agentsAudience  = "agents-service"
	serviceTokenTTL = 5 * time.Minute
)

// ServiceTokenSigner mints a short-lived BFF-signed identity JWT (aud:
// agents-service) carrying the acting org in the ocOrgId claim. The agents
// client attaches it as the bearer so org travels in a verified claim, not a
// trusted header. *auth.TaskTokenManager implements this. A nil signer skips
// signing (dev/tests that never reach the network).
type ServiceTokenSigner interface {
	IssueServiceToken(audience, ocOrgID string, ttl time.Duration) (string, error)
}

// agentsBase / internalBase are the agents-service path roots. The service is
// internal-only (S2S; only the BFF calls it, never the gateway), so it lives
// under /internal/v1 — the same fixed version slot the BFF's edge surface uses
// at /api/v1. Keep these in sync with agents/src/shared/config.ts.
const (
	agentsBase   = "/internal/v1/agents"
	internalBase = "/internal/v1"
)

// ErrNoAnthropicKey is returned by a KeyResolver when the org has not
// configured an Anthropic key (there is no platform fallback). The
// streaming methods propagate it; callers surface it as a stream error.
var ErrNoAnthropicKey = errors.New("no Anthropic API key configured for this organization")

// KeyResolver resolves the effective Anthropic API key for an org. The BFF
// wires this from AnthropicCredentialService.EffectiveKey so the key is
// resolved in-process (from the gated, token-derived org) and forwarded to
// agents-service, rather than agents-service calling back to fetch it.
type KeyResolver func(ctx context.Context, orgID string) (string, error)

// MCPTokenIssuer mints a short-lived BFF-signed MCP token (aud aep-api-mcp)
// bound to an org, so agents-service can call back to the BFF's internal MCP
// discovery surface as the acting org. The BFF wires this from
// auth.TaskTokenManager.IssueMCPToken. A nil issuer (or empty MCP URL) disables
// MCP propagation — the additive `mcp` field is simply omitted.
type MCPTokenIssuer func(orgID string) (string, error)

// MCPBinding is the additive `mcp` block the BFF attaches to the architect
// request: the URL of the BFF's internal MCP surface plus a per-run BFF-signed
// token the agent presents on the callback. Legacy agents-service ignores it
// until the E-phase wires the MCP client. Kept in sync with
// agents/src/agents/architect/schema.ts.
type MCPBinding struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// Client calls the aep-agents-service.
//
// Every streaming method carries an `orgID` parameter, bound into a per-call
// BFF-signed identity JWT (the `ocOrgId` claim) so agents-service derives the
// org from a verified claim rather than a trusted header. The BFF also resolves
// the effective Anthropic key for that org via its KeyResolver and forwards it
// as the `X-Anthropic-Key` header — agents-service consumes the key directly
// instead of resolving it itself. See docs/design/internal-s2s-api.md (§4).
type Client interface {
	// StreamDocumentGeneration POSTs to /internal/v1/agents/document-generation/{skillId}
	// to run a registered document-generation skill (e.g. requirements bootstrap,
	// functional-requirements derivation). The skillID selects the skill; sources
	// are the sibling files passed as context (filename → content); prompt is the
	// optional user prompt for bootstrap-style skills. Returns the raw SSE response
	// body. Caller must close.
	StreamDocumentGeneration(ctx context.Context, orgID, skillID string, req DocumentGenerationRequest) (io.ReadCloser, error)

	// StreamArchitect POSTs the requirements bundle (and optional previous design)
	// to /internal/v1/agents/architect and returns the raw SSE response body. The stream
	// emits structured custom events (data-overview, data-requirements,
	// data-component, data-finish) as the architecture is generated.
	// Caller must close.
	StreamArchitect(ctx context.Context, orgID string, req ArchitectRequest) (io.ReadCloser, error)

	// StreamTechLeadPlan POSTs the planner input to /internal/v1/agents/task-planner/plan
	// and returns the raw SSE response body.
	StreamTechLeadPlan(ctx context.Context, orgID string, req TechLeadPlanRequest) (io.ReadCloser, error)

	// StreamTechLeadDetail POSTs N issued tasks to /internal/v1/agents/task-planner/detail.
	StreamTechLeadDetail(ctx context.Context, orgID string, req TechLeadDetailRequest) (io.ReadCloser, error)

	// StreamRequirementsChat POSTs a chat turn to /internal/v1/agents/requirements-chat
	// and returns the raw SSE response body. The stream emits text-delta plus
	// custom data-* frames (tool-started, tool-result, tool-error,
	// validation-failed, finish).
	StreamRequirementsChat(ctx context.Context, orgID string, req RequirementsChatRequest) (io.ReadCloser, error)

	// RenderDsl calls /internal/v1/dsl/render — a cluster-internal helper.
	// kind is "wireframes" or "domain-model"; returns the rendered Excalidraw
	// scene JSON.
	RenderDsl(ctx context.Context, kind, dsl string) (string, error)
}

// RequirementsChatRequest mirrors RequirementsChatInput in
// agents/src/agents/requirements-chat/schema.ts.
type RequirementsChatRequest struct {
	Message string               `json:"message"`
	History []ChatHistoryMessage `json:"history,omitempty"`
	Files   map[string]string    `json:"files,omitempty"`
	Mode    string               `json:"mode"` // "edit" | "ask"
}

type ChatHistoryMessage struct {
	Role    string `json:"role"` // "user" | "assistant"
	Content string `json:"content"`
}

// DocumentGenerationRequest is the body sent to the generic skill endpoint.
type DocumentGenerationRequest struct {
	Sources map[string]string `json:"sources,omitempty"` // sibling files: filename → content
	Prompt  string            `json:"prompt,omitempty"`  // optional user prompt
}

// ArchitectRequest is the body sent to the architect endpoint.
type ArchitectRequest struct {
	ProjectName         string            `json:"projectName"`
	Spec                string            `json:"spec"`
	PreviousDesign      *ArchitectDesign  `json:"previousDesign,omitempty"`
	Wireframes          map[string]string `json:"wireframes,omitempty"`
	AvailableWireframes []string          `json:"availableWireframes,omitempty"`
	// Skills propagation — see docs/design/skills-system.md.
	// BuiltinSkills are inlined as full bodies under "Platform skills —
	// MUST consult"; OrgSkills appear as a manifest with descriptions.
	BuiltinSkills []SkillRecord      `json:"builtinSkills,omitempty"`
	OrgSkills     []SkillDescription `json:"orgSkills,omitempty"`
	SkillsApplied []string           `json:"skillsApplied,omitempty"`
	// HighLevelArchitectureSkill is the `high-level-architecture` skill body
	// pushed to the architect on every design call. It carries the design-
	// authoring judgment (component decomposition + the unified dependency
	// model: kind selection, discovery-tool ordering, exact-catalog-name rule,
	// platform-resource database trigger, external needsSpec/specUrl/config
	// derivation, authorable candidates, per-dep description) so the architect
	// prompt stays wire/output scaffolding. Sourced from the embedded default
	// (never the org catalogue — it is design guidance, not a per-component
	// attachable skill); when nil the agent falls back to its prompt guidance.
	// Mirrors TaskBreakdownSkill on the task-planner. See ADR-0005.
	HighLevelArchitectureSkill *SkillRecord `json:"highLevelArchitectureSkill,omitempty"`
	// MCP, when present, tells agents-service where the BFF's internal MCP
	// discovery surface lives and carries the BFF-signed token to reach it.
	// Injected by the client at send time (StreamArchitect) — callers never
	// set it. Additive; legacy agents-service ignores it until the E-phase.
	MCP *MCPBinding `json:"mcp,omitempty"`
}

// SkillDescription mirrors agents/src/agents/architect/schema.ts >
// SkillDescription (name + description). Used in the architect's org
// skills manifest and the tech-lead plan's attached-skills context.
type SkillDescription struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SkillRecord mirrors agents/src/agents/architect/schema.ts > SkillRecord
// (name + description + full SKILL.md body). Used in the architect's
// built-in inlining and the tech-lead detail's skillsResolved field.
type SkillRecord struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

// ArchitectDesign mirrors the architect output shape for incremental regen.
type ArchitectDesign struct {
	Overview   string                   `json:"overview"`
	Components []models.DesignComponent `json:"components"`
}

// TechLeadPlanRequest is the body sent to /internal/v1/agents/task-planner/plan. Mirrors
// agents/src/agents/tech-lead/schema.ts → TechLeadPlanInput plus the optional
// validator diff context.
type TechLeadPlanRequest struct {
	ProjectName   string                        `json:"projectName"`
	Spec          string                        `json:"spec"`
	SlimDesign    []TechLeadSlimComponent       `json:"slimDesign"`
	SpecDiff      string                        `json:"specDiff,omitempty"`
	DesignDiff    string                        `json:"designDiff,omitempty"`
	ExistingTasks []TechLeadExistingTaskSummary `json:"existingTasks,omitempty"`
	Mode          string                        `json:"mode"` // "fresh" | "incremental"
	Diff          *TechLeadValidatorDiffContext `json:"diff,omitempty"`
	// AttachedSkills are the skills the architect attached to the
	// project's design.md. Name + description only; bodies arrive in
	// each TechLeadDetailItem.SkillsResolved at the detail phase.
	AttachedSkills []SkillDescription `json:"attachedSkills,omitempty"`
	// TaskBreakdownSkill is the `task-breakdown` skill body pushed to the
	// task-planner. It carries the decomposition judgment (topological
	// ordering, dependency-kind gating, issue-brief quality) so the agent's
	// prompt stays pure scaffolding. Sourced from the embedded default; when
	// nil the agent falls back to its built-in guidance.
	TaskBreakdownSkill *SkillRecord `json:"taskBreakdownSkill,omitempty"`
}

type TechLeadSlimComponent struct {
	Name          string   `json:"name"`
	ComponentType string   `json:"componentType"`
	Language      string   `json:"language"`
	DependsOn     []string `json:"dependsOn"`
}

type TechLeadExistingTaskSummary struct {
	IssueNumber   *int   `json:"issueNumber,omitempty"`
	Title         string `json:"title"`
	ComponentName string `json:"componentName"`
	Status        string `json:"status"`
}

type TechLeadValidatorDiffContext struct {
	Added                    []string `json:"added"`
	ContractAffectedModified []string `json:"contractAffectedModified"`
	Removed                  []string `json:"removed"`
}

// TechLeadDetailRequest is the body sent to /internal/v1/agents/task-planner/detail.
type TechLeadDetailRequest struct {
	ProjectName string               `json:"projectName"`
	Spec        string               `json:"spec"`
	Items       []TechLeadDetailItem `json:"items"`
	// TaskBreakdownSkill — the `task-breakdown` skill body, pushed once for the
	// whole detail run; its "issue brief" guidance shapes each task body.
	TaskBreakdownSkill *SkillRecord `json:"taskBreakdownSkill,omitempty"`
}

type TechLeadDetailItem struct {
	TaskID                     string                  `json:"taskId"`
	ComponentName              string                  `json:"componentName"`
	Title                      string                  `json:"title"`
	Rationale                  string                  `json:"rationale"`
	DesignSlice                string                  `json:"designSlice"`
	DepSummaries               []TechLeadSlimComponent `json:"depSummaries"`
	ExistingTitlesForComponent []TechLeadExistingTitle `json:"existingTitlesForComponent"`
	// SkillsResolved are the full bodies of every skill attached to
	// the project's design at tech-lead detail time. The tech-lead
	// inlines them under "Skills active for this project" with "MUST
	// consult" framing.
	SkillsResolved []SkillRecord `json:"skillsResolved,omitempty"`
}

type TechLeadExistingTitle struct {
	Title  string `json:"title"`
	Status string `json:"status"`
}

type client struct {
	baseURL     string
	httpClient  *http.Client
	signer      ServiceTokenSigner
	keyResolver KeyResolver
	// mcpURL + mcpToken, when both set, drive the additive `mcp` block on the
	// architect request. Both nil/empty ⇒ no MCP propagation.
	mcpURL   string
	mcpToken MCPTokenIssuer
}

// NewClient builds an agents-service client. signer mints a short-lived
// BFF-signed identity JWT per call (audience: agents-service) carrying the org
// in the ocOrgId claim, attached as the bearer; pass nil to disable signing in
// tests/dev. keyResolver resolves the effective Anthropic key per call and is
// forwarded as X-Anthropic-Key; pass nil in tests/dev that don't stream (no key
// is forwarded then). mcpURL + mcpToken, when both set, attach the additive
// `mcp` block to the architect request (empty/nil disables it).
func NewClient(baseURL string, signer ServiceTokenSigner, keyResolver KeyResolver, mcpURL string, mcpToken MCPTokenIssuer) Client {
	return &client{
		baseURL: baseURL,
		// No client-side timeout — streaming responses can take minutes.
		// Cancellation flows via ctx. WrapTransport adds correlation-ID
		// propagation; the per-org identity bearer is minted per call by
		// attachIdentity (org-bound, not a cached service token).
		httpClient: &http.Client{
			Transport: httpx.WrapTransport(nil),
		},
		signer:      signer,
		keyResolver: keyResolver,
		mcpURL:      mcpURL,
		mcpToken:    mcpToken,
	}
}

// resolveKeyHeader resolves the effective Anthropic key for orgID and sets it
// on the request. Returns the resolver error (e.g. ErrNoAnthropicKey) so the
// caller can surface it as a stream error. A nil resolver is a no-op (tests).
func (c *client) resolveKeyHeader(ctx context.Context, orgID string, req *http.Request) error {
	if c.keyResolver == nil {
		return nil
	}
	key, err := c.keyResolver(ctx, orgID)
	if err != nil {
		return err
	}
	req.Header.Set("X-Anthropic-Key", key)
	return nil
}

// attachIdentity mints a short-lived BFF-signed identity JWT carrying orgID in
// the ocOrgId claim and sets it as the bearer. This is the outbound half of the
// symmetric S2S identity model: agents-service verifies it against the BFF JWKS
// and reads org from the verified claim — replacing the old trusted
// X-Oc-Org-Id header. orgID may be empty for org-less calls (dsl/render); the
// token still authenticates by signature + audience. A nil signer is a no-op
// (dev/tests).
func (c *client) attachIdentity(orgID string, req *http.Request) error {
	if c.signer == nil {
		return nil
	}
	tok, err := c.signer.IssueServiceToken(agentsAudience, orgID, serviceTokenTTL)
	if err != nil {
		return fmt.Errorf("mint agents identity token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return nil
}

func (c *client) StreamDocumentGeneration(ctx context.Context, orgID, skillID string, req DocumentGenerationRequest) (io.ReadCloser, error) {
	return c.streamSSE(ctx, orgID, agentsBase+"/document-generation/"+skillID, req)
}

func (c *client) StreamArchitect(ctx context.Context, orgID string, req ArchitectRequest) (io.ReadCloser, error) {
	// Attach the additive MCP bundle so agents-service can reach the BFF's
	// internal MCP discovery surface as this org. Best-effort: a mint failure
	// must not fail design generation — the field is simply omitted and the
	// agent falls back to its non-MCP path (legacy today; consumed in E-phase).
	if c.mcpURL != "" && c.mcpToken != nil {
		if tok, terr := c.mcpToken(orgID); terr != nil {
			slog.WarnContext(ctx, "agents: MCP token mint failed; omitting mcp block", "error", terr)
		} else {
			req.MCP = &MCPBinding{URL: c.mcpURL, Token: tok}
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + agentsBase + "/architect"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if err := c.attachIdentity(orgID, httpReq); err != nil {
		return nil, err
	}
	if err := c.resolveKeyHeader(ctx, orgID, httpReq); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("agents service request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agents service error (status %d): %s", resp.StatusCode, string(msg))
	}

	return resp.Body, nil
}

func (c *client) StreamTechLeadPlan(ctx context.Context, orgID string, req TechLeadPlanRequest) (io.ReadCloser, error) {
	return c.streamSSE(ctx, orgID, agentsBase+"/task-planner/plan", req)
}

func (c *client) StreamTechLeadDetail(ctx context.Context, orgID string, req TechLeadDetailRequest) (io.ReadCloser, error) {
	return c.streamSSE(ctx, orgID, agentsBase+"/task-planner/detail", req)
}

func (c *client) StreamRequirementsChat(ctx context.Context, orgID string, req RequirementsChatRequest) (io.ReadCloser, error) {
	return c.streamSSE(ctx, orgID, agentsBase+"/requirements-chat", req)
}

func (c *client) RenderDsl(ctx context.Context, kind, dsl string) (string, error) {
	body, err := json.Marshal(map[string]string{"kind": kind, "dsl": dsl})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+internalBase+"/dsl/render", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// dsl/render is a stateless, org-less transform but now sits under the
	// agents INTERNAL_V1 JWT gate, so it still carries a (org-less) identity
	// token authenticated by signature + audience.
	if err := c.attachIdentity("", httpReq); err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("dsl render request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("dsl render error (status %d): %s", resp.StatusCode, string(msg))
	}
	var out struct {
		Excalidraw string `json:"excalidraw"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode dsl render response: %w", err)
	}
	return out.Excalidraw, nil
}

// streamSSE is the shared POST + SSE wrapper used by every streaming agent
// route. Caller must close the returned body. No client-side timeout —
// streams can run for minutes; cancellation flows via ctx. The orgID is bound
// into the per-call BFF-signed identity token (ocOrgId claim) and also drives
// the Anthropic-key resolver.
func (c *client) streamSSE(ctx context.Context, orgID, path string, body any) (io.ReadCloser, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if err := c.attachIdentity(orgID, httpReq); err != nil {
		return nil, err
	}
	if err := c.resolveKeyHeader(ctx, orgID, httpReq); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("agents service request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agents service error (status %d): %s", resp.StatusCode, string(msg))
	}
	return resp.Body, nil
}
