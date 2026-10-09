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

// anthropic_credential_service.go — Anthropic credential service.
//
// AnthropicCredentialService owns the org's Claude subscription: the
// optional `coding` token (`claude setup-token`) the coding agent bills instead
// of the connection's key, only while it runs on Claude Code against
// Anthropic's own API. It cannot exist without the connection. It also keeps
// the Agent Manager provider's copy of the connection in line with a save
// (syncModelProvider).
//
// Surface — all in-process; this service has no HTTP routes of its own:
//
//   - ValidateKey — the shape checks plus the live probe. The AI agents card
//     (AgentSettingsService) calls it before its unit of work.
//   - writeKey — the token's coding-agent-key reference in vault, written from
//     the save's request before its transaction commits; writeKeyTx /
//     deleteKeyTx — the subscription row, inside that transaction; forgetKey —
//     a deleted token's reference, and syncModelProvider — the Agent Manager
//     provider's copy of the connection, both after it commits, under the
//     card's lock.
//   - Status — the projection (no character of the token); Holds — whether
//     the row exists.
//
// The connection itself — its row, key and probe — is ModelConnectionService's
// (model_connection_service.go).
//
// The token lives only in vault, under the org's coding-agent-key reference.
// The metadata (status / connected_at / last_validated_at) lives in the
// `org_anthropic_credentials` table.
//
// See docs/decisions/ADR-0036-the-coding-credential-is-a-subscription.md.
package organization

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// AnthropicCredentialService — see package doc.
type AnthropicCredentialService struct {
	repo         OrgAnthropicRepository
	anthropicAPI string // "https://api.anthropic.com" by default; overridden in tests
	httpClient   *http.Client

	// secretRefWriter writes and removes the token's coding-agent-key
	// reference. nil, or not enabled: no secret store, no token can be saved.
	secretRefWriter *SecretRefWriter

	// modelProvider is told when the org's connection changes, so an Agent
	// Manager provider holding a COPY of it stops calling a revoked key or a
	// host the org moved off. nil-safe.
	modelProvider ModelProviderPublisher
}

// ModelProviderPublisher keeps a governed model provider's copy of the org's
// connection truthful: the current connection and key while the org has one,
// no usable key once it disconnects.
//
// Declared here, implemented at the composition root: this domain must not
// reach into Agent Manager, and the only things it has to say are "the
// connection changed" and "there is no connection to hold". An org with no
// governed environment has no implementation wired and nothing happens.
type ModelProviderPublisher interface {
	// PublishOrgModelConnection writes conn and its key onto the provider —
	// the format, URL and auth as well as the key, in one write.
	PublishOrgModelConnection(ctx context.Context, ocOrgID string, conn modelconn.Connection, apiKey string) error
	// ClearOrgModelKey replaces the provider's copy of the org's key with no
	// usable credential, so a disconnected key is not left live in a second
	// system. last is the connection the copy belonged to. A no-op for an org
	// with no provider.
	ClearOrgModelKey(ctx context.Context, ocOrgID string, last modelconn.Connection) error
}

// WithModelProvider injects the publisher; chainable, nil disables the push.
func (s *AnthropicCredentialService) WithModelProvider(p ModelProviderPublisher) *AnthropicCredentialService {
	s.modelProvider = p
	return s
}

// WithSecretRefWriter injects the writer of the token's coding-agent-key
// reference; chainable. nil leaves the installation without a secret store:
// a token save is refused.
func (s *AnthropicCredentialService) WithSecretRefWriter(w *SecretRefWriter) *AnthropicCredentialService {
	s.secretRefWriter = w
	return s
}

// WithAnthropicAPIBase points key validation at base instead of the real
// Anthropic API; chainable. Tests aim it at an httptest server so
// validateAnthropicKey's probe never leaves the process.
func (s *AnthropicCredentialService) WithAnthropicAPIBase(base string) *AnthropicCredentialService {
	s.anthropicAPI = base
	return s
}

// NewAnthropicCredentialService wires the service over the subscription rows.
func NewAnthropicCredentialService(repo OrgAnthropicRepository) *AnthropicCredentialService {
	return &AnthropicCredentialService{
		repo:         repo,
		anthropicAPI: "https://api.anthropic.com",
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

// ----------------------------------------------------------------------------
// Projection — what the API + console see
// ----------------------------------------------------------------------------

type AnthropicProjection struct {
	OcOrgID         string                  `json:"ocOrgId"`
	CredentialKind  AnthropicCredentialKind `json:"credentialKind"`
	Status          string                  `json:"status"`
	ConnectedAt     time.Time               `json:"connectedAt"`
	LastValidatedAt *time.Time              `json:"lastValidatedAt,omitempty"`
	ValidationError *string                 `json:"validationError,omitempty"`
}

func projectionFromAnthropicRow(r *OrgAnthropicCredential) *AnthropicProjection {
	return &AnthropicProjection{
		OcOrgID:         r.OcOrgID,
		CredentialKind:  r.CredentialKind,
		Status:          r.Status,
		ConnectedAt:     r.ConnectedAt,
		LastValidatedAt: r.LastValidatedAt,
		ValidationError: r.ValidationError,
	}
}

// ----------------------------------------------------------------------------
// Validation + the card's credential writes
// ----------------------------------------------------------------------------

// ValidateKey runs the save-time validation for a Claude subscription token
// WITHOUT persisting anything: the shape checks plus the live /v1/messages
// probe against Anthropic's own API, authenticated as Bearer.
//
// Only a subscription token is accepted (a separate coding API key is not a
// thing the platform offers); an API key is refused here, before a probe is
// spent on it, rather than discovered later by a run that cannot use it.
func (s *AnthropicCredentialService) ValidateKey(ctx context.Context, apiKey string) error {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return &ValidationError{Code: "anthropic_key_missing", Message: "a credential is required"}
	}
	if !looksLikeAnthropicKey(key) {
		return &ValidationError{Code: "anthropic_key_invalid", Message: "value does not look like an Anthropic credential (expected prefix 'sk-ant-')"}
	}
	kind := AnthropicCredentialKindOf(key)
	if kind != AnthropicCredentialOAuth {
		return &ValidationError{
			Code: "agents_subscription_token_required",
			Message: "a Claude subscription takes a token from `claude setup-token` (sk-ant-oat…); " +
				"an Anthropic API key belongs in the model connection's key field",
		}
	}
	return s.validateAnthropicKey(ctx, kind, key)
}

// canWriteKey reports whether a token can be saved: only to vault, so only
// with a secret store.
func (s *AnthropicCredentialService) canWriteKey() bool {
	return s.secretRefWriter.Enabled()
}

// writeKey writes token as role's new coding-agent-key reference
// (OrgSecretWriter.Write); commit runs while the write's lock is held, after
// the row names the new reference: when it fails the write is undone and the
// previous reference stays. token must already have passed ValidateKey.
func (s *AnthropicCredentialService) writeKey(ctx context.Context, ocOrgID string, role AnthropicRole, token string, commit func() error) (OrgSecretWrite, error) {
	written, err := s.secretRefWriter.WriteAnthropic(ctx, ocOrgID, role, strings.TrimSpace(token), func(SecretRefTriplet) error {
		return commit()
	})
	if err != nil {
		return OrgSecretWrite{}, err
	}
	slog.InfoContext(ctx, "anthropic: credential reference written", "ocOrgId", ocOrgID, "role", role, "secretRefName", written.Name)
	return written, nil
}

// writeKeyTx records role's credential row for token inside the card's
// transaction; the token itself is in vault (writeKey). token must already
// have passed ValidateKey — the probe runs before the transaction opens, so
// no lock is held across a network call.
func (s *AnthropicCredentialService) writeKeyTx(tx AgentsCardTx, ocOrgID string, role AnthropicRole, token string) error {
	now := time.Now().UTC()
	row := OrgAnthropicCredential{
		OcOrgID:         ocOrgID,
		Role:            role,
		CredentialKind:  AnthropicCredentialKindOf(strings.TrimSpace(token)),
		Status:          "active",
		ConnectedAt:     now,
		LastValidatedAt: &now,
	}
	if err := tx.UpsertCredential(&row); err != nil {
		return fmt.Errorf("anthropic %s: upsert: %w", role, err)
	}
	return nil
}

// deleteKeyTx removes role's credential row inside the card's transaction
// and reports whether there was one; its reference goes after commit
// (forgetKey). Idempotent.
func (s *AnthropicCredentialService) deleteKeyTx(tx AgentsCardTx, ocOrgID string, role AnthropicRole) (bool, error) {
	row, err := tx.GetCredential(ocOrgID, role)
	if err != nil {
		return false, fmt.Errorf("anthropic %s: load row: %w", role, err)
	}
	if row == nil {
		return false, nil
	}
	if err := tx.DeleteCredential(ocOrgID, role); err != nil {
		return false, fmt.Errorf("anthropic %s: delete row: %w", role, err)
	}
	return true, nil
}

// forgetKey removes a deleted credential's coding-agent-key row and then its
// reference after the save committed, best-effort, under the secret's lock.
func (s *AnthropicCredentialService) forgetKey(ctx context.Context, ocOrgID string, role AnthropicRole) {
	if !s.secretRefWriter.Enabled() {
		return
	}
	if err := s.secretRefWriter.ForgetAnthropic(ctx, ocOrgID, role); err != nil {
		slog.WarnContext(ctx, "anthropic: reference removal failed (orphaned copy until the next save)",
			"ocOrgId", ocOrgID, "role", role, "error", err)
	}
}

// AgentManagerNotUpdatedError is a committed key save whose push to Agent
// Manager's provider failed. The key IS saved (vault and its reference row);
// only the provider's copy lags. It is a failure the user must act on, not a
// warning: a governed deploy fails closed without the provider
// (agentgovernance.ErrProviderMissing), and only a key save writes it. /config
// answers it 502 agent_manager_not_updated.
type AgentManagerNotUpdatedError struct{ Err error }

func (e *AgentManagerNotUpdatedError) Error() string {
	return "agent manager not updated: " + e.Err.Error()
}

func (e *AgentManagerNotUpdatedError) Unwrap() error { return e.Err }

// AgentManagerNotUpdatedCode and agentManagerNotUpdatedMessage are the 502 a
// key save answers when its Agent Manager push failed.
const (
	AgentManagerNotUpdatedCode    = "agent_manager_not_updated"
	agentManagerNotUpdatedMessage = "Key saved; Agent Manager was not updated. Save the key again."
)

// syncModelProvider brings the org's Agent Manager provider, which holds a
// COPY of the connection and its key on behalf of every governed agent, in
// line with a committed save: before and after are the org's connection on
// either side of it (nil where it had none), key the key the save's request
// carried ("" when it carried none). It runs after the save committed, under
// the card's lock, so the next save's push always follows this one.
//
// WHY THIS MATTERS MORE THAN IT LOOKS: the save is the provider's ONLY writer.
// The deploy path never writes it (it holds no key), so without this a rotated
// key leaves the provider calling the upstream with a revoked one, a switch to
// another host leaves it calling the old one, and an org that never got a
// provider fails every governed deploy. And a disconnected key is not one the
// provider may keep, so a disconnect clears it once rather than leaving it
// live in a second system.
//
// The key published is the request's: no stored key is read back. A failed
// publish is RETURNED (the caller answers 502 agent_manager_not_updated, the
// key staying saved); a failed clear is logged: the connection is gone either
// way and nothing fails closed on the copy.
//
// Only the connection's key is ever published: that is the key agents run on.
// The subscription token belongs to the coding agent, which does not go
// through the gateway.
func (s *AnthropicCredentialService) syncModelProvider(ctx context.Context, ocOrgID string, before, after *modelconn.Connection, key string) error {
	if s.modelProvider == nil {
		return nil
	}
	switch modelProviderStepFor(before, after, key != "") {
	case modelProviderPublish:
		if err := s.modelProvider.PublishOrgModelConnection(ctx, ocOrgID, *after, key); err != nil {
			slog.WarnContext(ctx, "model connection: could not publish the saved connection to the Agent Manager provider",
				append([]any{"ocOrgId", ocOrgID, "host", after.Host}, agentManagerFailureAttrs(err)...)...)
			return fmt.Errorf("publish the saved connection to the Agent Manager provider: %w", err)
		}
	case modelProviderClear:
		if err := s.modelProvider.ClearOrgModelKey(ctx, ocOrgID, *before); err != nil {
			slog.WarnContext(ctx, "model connection: could not clear the Agent Manager provider's copy of the disconnected key; it stays live there until cleared by hand",
				append([]any{"ocOrgId", ocOrgID, "previousHost", before.Host}, agentManagerFailureAttrs(err)...)...)
		}
	case modelProviderLeave:
	}
	return nil
}

// agentManagerFailureAttrs classifies a failed Agent Manager push for the
// log: a reason class, plus AMP's status when it answered, never the error's
// text (a transport error names AMP's URL; an AMP body can echo what it was
// sent).
func agentManagerFailureAttrs(err error) []any {
	var perm *agentmanager.PermanentError
	var server *agentmanager.ServerError
	var urlErr *url.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return []any{"reason", "timeout"}
	case errors.Is(err, context.Canceled):
		return []any{"reason", "canceled"}
	case errors.As(err, &perm):
		return []any{"reason", "rejected", "status", perm.Status}
	case errors.As(err, &server):
		return []any{"reason", "upstream_error", "status", server.Status}
	case errors.As(err, &urlErr):
		return []any{"reason", "unreachable"}
	default:
		return []any{"reason", "other"}
	}
}

// modelProviderStep is what a save does to the Agent Manager provider's copy
// of the org's connection.
type modelProviderStep int

const (
	modelProviderLeave modelProviderStep = iota
	modelProviderPublish
	modelProviderClear
)

// modelProviderStepFor decides it from the connection before and after a save
// and whether the save carried a key. Pure, so the rule is a table test. Only
// a save with a key publishes: a first connect and every edit of what the
// provider is built from (format, URL, auth) carry one (draftConnection), and
// the provider is never written without the key. A model-only change writes
// nothing: it would redeploy every bound proxy.
func modelProviderStepFor(before, after *modelconn.Connection, keyWritten bool) modelProviderStep {
	switch {
	case after == nil && before != nil:
		return modelProviderClear
	case after != nil && keyWritten:
		return modelProviderPublish
	default:
		return modelProviderLeave
	}
}

// ----------------------------------------------------------------------------
// Status
// ----------------------------------------------------------------------------

// Status returns the projection for (ocOrgID, role). Returns NotFoundError
// when no row exists, which the config projection maps to null (no
// subscription).
func (s *AnthropicCredentialService) Status(ctx context.Context, ocOrgID string, role AnthropicRole) (*AnthropicProjection, error) {
	row, err := fetchAnthropicRow(ctx, s.repo, ocOrgID, role)
	if err != nil {
		return nil, err
	}
	return projectionFromAnthropicRow(row), nil
}

// Holds reports whether the org has a row for role, whatever its status: the
// AI agents card judges a save by which credentials exist, not whether they
// currently validate.
func (s *AnthropicCredentialService) Holds(ctx context.Context, ocOrgID string, role AnthropicRole) (bool, error) {
	row, err := s.repo.GetByOrg(ctx, ocOrgID, role)
	return row != nil, err
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

// fetchAnthropicRow loads (ocOrgID, role)'s row, answering NotFoundError when
// there is none.
func fetchAnthropicRow(ctx context.Context, repo OrgAnthropicRepository, ocOrgID string, role AnthropicRole) (*OrgAnthropicCredential, error) {
	row, err := repo.GetByOrg(ctx, ocOrgID, role)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, &NotFoundError{What: fmt.Sprintf("org_anthropic_credentials.%s.%s", ocOrgID, role)}
	}
	return row, nil
}

// validateAnthropicKey probes Anthropic's /v1/messages with a minimal
// payload. 401 → ValidationError{anthropic_key_invalid}. A 5xx is the
// upstream's fault → UpstreamError (mapped to 502 Bad Gateway), NOT a
// client-fault 400. Other unexpected non-5xx statuses (e.g. 429) stay a
// ValidationError.
//
// Anthropic's /v1/messages requires `anthropic-version` plus a credential
// header; a malformed request returns 400 (which still proves the credential
// is recognized). We send a single 1-token completion request that should
// either 200 OK or 401 Unauthorized.
//
// The credential header depends on the kind, because the two authenticate
// differently: a Console API key goes in `x-api-key`, a Claude Code OAuth token
// in `Authorization: Bearer`. Verified against the live API — a valid OAuth
// token probed with `x-api-key` comes back 401 `invalid x-api-key`, so without
// this branch every good token would be rejected at Connect.
//
// Bearer alone is enough; Claude Code additionally sends
// `anthropic-beta: oauth-2025-04-20`, but the probe deliberately does not. It
// only needs to prove the credential authenticates, and pinning a beta flag we
// neither own nor version would make validation start failing the day that flag
// is retired.
func (s *AnthropicCredentialService) validateAnthropicKey(ctx context.Context, kind AnthropicCredentialKind, key string) error {
	body := []byte(`{
	  "model": "claude-haiku-4-5",
	  "max_tokens": 1,
	  "messages": [{"role":"user","content":"ping"}]
	}`)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.anthropicAPI+"/v1/messages", bytes.NewReader(body))
	if kind == AnthropicCredentialOAuth {
		req.Header.Set("authorization", "Bearer "+key)
	} else {
		req.Header.Set("x-api-key", key)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return &ValidationError{Code: "anthropic_unreachable", Message: fmt.Sprintf("Anthropic API unreachable: %v", err)}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return &ValidationError{Code: "anthropic_key_invalid", Message: "Anthropic rejected the key (401 Unauthorized)"}
	case http.StatusForbidden:
		return &ValidationError{Code: "anthropic_key_forbidden", Message: "Anthropic key lacks the required permissions"}
	case http.StatusOK, http.StatusBadRequest:
		// 200 = key valid; 400 = key recognized but request payload arguable
		// (e.g. unknown model). Either way the key is authenticated.
		return nil
	}
	if resp.StatusCode >= 500 {
		// Upstream is broken, not the caller's key/request — surface a 502 at
		// the edge so we don't blame the client for Anthropic's outage.
		return &UpstreamError{
			Code:    "anthropic_unavailable",
			Message: fmt.Sprintf("Anthropic API returned %d: %s", resp.StatusCode, truncateForError(respBody)),
		}
	}
	return &ValidationError{
		Code:    "anthropic_unexpected_status",
		Message: fmt.Sprintf("Anthropic API returned %d: %s", resp.StatusCode, truncateForError(respBody)),
	}
}

func looksLikeAnthropicKey(k string) bool {
	return strings.HasPrefix(k, "sk-ant-") && len(k) >= 20
}
