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
//   - writeKeyTx / deleteKeyTx — the subscription half of that unit of work,
//     inside its transaction; mirrorKey / forgetKey — the SM-API copy, and
//     syncModelProvider — the Agent Manager provider's copy of the
//     connection, both after it commits, under the card's lock.
//   - Status — the masked projection; Holds — whether the row exists.
//
// The connection itself — its row, key and probe — is ModelConnectionService's
// (model_connection_service.go).
//
// Secret bytes live in the same `org_secrets` (Postgres + AES-256-GCM) table
// as the GitHub PAT, keyed by the role's SecretStoreKey(). The metadata
// (prefix / last4 / status / connected_at / last_validated_at) lives in the
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
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// AnthropicCredentialService — see package doc.
type AnthropicCredentialService struct {
	repo         OrgAnthropicRepository
	store        secrets.CredentialStore
	anthropicAPI string // "https://api.anthropic.com" by default; overridden in tests
	httpClient   *http.Client

	// secretRefWriter mirrors a saved credential into SM-API. nil-safe.
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

// WithSecretRefWriter injects the SM-API writer; chainable. nil disables
// the mirror — the org_secrets path remains authoritative.
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

// NewAnthropicCredentialService wires the service. repo and store must be
// non-nil; store serves the resync reads, while the card's reads and writes
// go through the store bound to its transaction.
func NewAnthropicCredentialService(
	repo OrgAnthropicRepository,
	store secrets.CredentialStore,
) *AnthropicCredentialService {
	return &AnthropicCredentialService{
		repo:         repo,
		store:        store,
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
	KeyPrefix       string                  `json:"keyPrefix"`
	KeyLast4        string                  `json:"keyLast4"`
	Status          string                  `json:"status"`
	ConnectedAt     time.Time               `json:"connectedAt"`
	LastValidatedAt *time.Time              `json:"lastValidatedAt,omitempty"`
	ValidationError *string                 `json:"validationError,omitempty"`
}

func projectionFromAnthropicRow(r *OrgAnthropicCredential) *AnthropicProjection {
	return &AnthropicProjection{
		OcOrgID:         r.OcOrgID,
		CredentialKind:  r.CredentialKind,
		KeyPrefix:       r.KeyPrefix,
		KeyLast4:        r.KeyLast4,
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

// writeKeyTx stores key as role's credential inside the card's transaction:
// the encrypted bytes and the metadata row commit or roll back together. key
// must already have passed ValidateKey — the probe runs before the
// transaction opens, so no lock is held across a network call.
func (s *AnthropicCredentialService) writeKeyTx(ctx context.Context, tx AgentsCardTx, ocOrgID string, role AnthropicRole, key string) error {
	key = strings.TrimSpace(key)
	now := time.Now().UTC()
	prefix, last4 := anthropicKeyPreview(key)
	if err := tx.Secrets().Put(ctx, ocOrgID, role.SecretStoreKey(), []byte(key)); err != nil {
		return fmt.Errorf("anthropic %s: store put: %w", role, err)
	}
	row := OrgAnthropicCredential{
		OcOrgID:         ocOrgID,
		Role:            role,
		CredentialKind:  AnthropicCredentialKindOf(key),
		KeyPrefix:       prefix,
		KeyLast4:        last4,
		Status:          "active",
		ConnectedAt:     now,
		LastValidatedAt: &now,
	}
	if err := tx.UpsertCredential(&row); err != nil {
		return fmt.Errorf("anthropic %s: upsert: %w", role, err)
	}
	return nil
}

// deleteKeyTx removes role's credential — row and bytes — inside the card's
// transaction, returning the SM-API secret-ref name the row carried so the
// caller can delete that copy once the transaction commits (the row, and the
// name with it, is gone by then). Idempotent: a missing row returns "", false.
func (s *AnthropicCredentialService) deleteKeyTx(ctx context.Context, tx AgentsCardTx, ocOrgID string, role AnthropicRole) (string, bool, error) {
	row, err := tx.GetCredential(ocOrgID, role)
	if err != nil {
		return "", false, fmt.Errorf("anthropic %s: load row: %w", role, err)
	}
	if row == nil {
		return "", false, nil
	}
	if err := tx.DeleteCredential(ocOrgID, role); err != nil {
		return "", false, fmt.Errorf("anthropic %s: delete row: %w", role, err)
	}
	if err := tx.Secrets().Delete(ctx, ocOrgID, role.SecretStoreKey()); err != nil {
		return "", false, fmt.Errorf("anthropic %s: store delete: %w", role, err)
	}
	return derefOrEmpty(row.SecretRefName), true, nil
}

// mirrorKey copies role's credential as it stands into SM-API and records
// where on its row, inside the transaction the card's copies run in (under its
// lock), best-effort: org_secrets stays authoritative. It reads the credential
// rather than taking the one a save wrote, so an earlier save's copy never
// lands over a later one's. The save cleared the row's triplet
// (UpsertCredential), so a failed mirror leaves it NULL until the next save,
// and dispatch fails closed with a reason naming it rather than mounting the
// previous credential. No row: removed since, nothing to copy.
func (s *AnthropicCredentialService) mirrorKey(ctx context.Context, tx AgentsCardTx, ocOrgID string, role AnthropicRole) {
	if !s.secretRefWriter.Enabled() {
		return
	}
	if err := s.mirrorKeyTx(ctx, tx, ocOrgID, role); err != nil {
		slog.WarnContext(ctx, "anthropic: SM-API mirror failed (org_secrets still authoritative)",
			"ocOrgId", ocOrgID, "role", role, "error", err)
	}
}

func (s *AnthropicCredentialService) mirrorKeyTx(ctx context.Context, tx AgentsCardTx, ocOrgID string, role AnthropicRole) error {
	row, err := tx.GetCredential(ocOrgID, role)
	if err != nil || row == nil {
		return err
	}
	key, err := tx.Secrets().Get(ctx, ocOrgID, role.SecretStoreKey())
	if err != nil {
		return fmt.Errorf("read the credential: %w", err)
	}
	ref, err := s.secretRefWriter.UploadAnthropic(ctx, ocOrgID, role, strings.TrimSpace(string(key)))
	if err != nil {
		return err
	}
	if err := tx.StampCredentialSecretRef(ocOrgID, role, ref); err != nil {
		return fmt.Errorf("stamp the secret reference: %w", err)
	}
	slog.InfoContext(ctx, "anthropic: credential mirrored to SM-API",
		"ocOrgId", ocOrgID, "role", role, "secretRefName", ref.Name, "vaultKey", ref.KVPath)
	return nil
}

// forgetKey deletes a removed credential's SM-API copy, best-effort, by the
// secret-ref name deleteKeyTx captured before the row went, inside the
// transaction the card's copies run in. A credential saved since mirrors to the
// same path, so the copy is left to it. A failure leaves an orphaned vault
// entry nothing reads; the next save of that role overwrites it.
func (s *AnthropicCredentialService) forgetKey(ctx context.Context, tx AgentsCardTx, ocOrgID string, role AnthropicRole, secretRefName string) {
	if secretRefName == "" || !s.secretRefWriter.Enabled() {
		return
	}
	row, err := tx.GetCredential(ocOrgID, role)
	if err == nil && row != nil {
		return
	}
	if err == nil {
		err = s.secretRefWriter.DeleteAnthropic(ctx, ocOrgID, role, secretRefName)
	}
	if err != nil {
		slog.WarnContext(ctx, "anthropic: SM-API delete failed (orphaned copy until the next save)",
			"ocOrgId", ocOrgID, "role", role, "error", err)
	}
}

// syncModelProvider brings the org's Agent Manager provider, which holds a
// COPY of the connection and its key on behalf of every governed agent, in
// line with a committed save: before and after are the org's connection on
// either side of it (nil where it had none), keyWritten whether it stored a
// key. It runs inside the transaction the card's copies run in (under its
// lock), and what it publishes is the connection and key as they stand, not
// as the save left them: a save's publish that runs after a later save's must
// not put the earlier host or key back. A publish finding no connection, or a
// clear finding one, defers to the later save that changed it.
//
// WHY THIS MATTERS MORE THAN IT LOOKS: without it, a rotated key leaves the
// provider calling the upstream with a revoked one, and a switch to another
// host leaves it calling the old one — and EVERY governed agent in the org
// fails at once, at the upstream, far from Settings, with nothing in AEP
// saying why. And a disconnected key is not one the provider may keep, so a
// disconnect clears it once rather than leaving it live in a second system.
//
// Best-effort, and deliberately so: the connection IS stored, and failing the
// user's Settings action because a downstream copy lagged would be the worse
// outcome.
//
// Only the connection's key is ever published: that is the key agents run on.
// The subscription token belongs to the coding agent, which does not go
// through the gateway.
func (s *AnthropicCredentialService) syncModelProvider(ctx context.Context, tx AgentsCardTx, ocOrgID string, before, after *modelconn.Connection, keyWritten bool) {
	if s.modelProvider == nil {
		return
	}
	switch modelProviderStepFor(before, after, keyWritten) {
	case modelProviderPublish:
		conn, key, err := currentConnection(ctx, tx, ocOrgID)
		if err == nil && conn == nil {
			return
		}
		if err == nil {
			err = s.modelProvider.PublishOrgModelConnection(ctx, ocOrgID, *conn, key)
		}
		if err != nil {
			slog.WarnContext(ctx, "model connection: could not publish the saved connection to the Agent Manager provider; governed agents keep the previous one until the next deploy",
				"ocOrgId", ocOrgID, "host", after.Host, "error", err)
		}
	case modelProviderClear:
		row, err := tx.GetConnection(ocOrgID)
		if err == nil && row != nil {
			return
		}
		if err == nil {
			err = s.modelProvider.ClearOrgModelKey(ctx, ocOrgID, *before)
		}
		if err != nil {
			slog.WarnContext(ctx, "model connection: could not clear the Agent Manager provider's copy of the disconnected key; it stays live there until cleared by hand",
				"ocOrgId", ocOrgID, "previousHost", before.Host, "error", err)
		}
	case modelProviderLeave:
	}
}

// currentConnection is the org's connection and its key as they stand in tx;
// a nil connection when it has none.
func currentConnection(ctx context.Context, tx AgentsCardTx, ocOrgID string) (*modelconn.Connection, string, error) {
	row, err := tx.GetConnection(ocOrgID)
	if err != nil || row == nil {
		return nil, "", err
	}
	key, err := tx.Secrets().Get(ctx, ocOrgID, modelKeyStoreKey)
	if err != nil {
		return nil, "", fmt.Errorf("read the stored connection key: %w", err)
	}
	if len(bytes.TrimSpace(key)) == 0 {
		return nil, "", errors.New("the stored connection key is empty")
	}
	conn := row.Connection()
	return &conn, strings.TrimSpace(string(key)), nil
}

// modelProviderStep is what a save does to the Agent Manager provider's copy
// of the org's connection.
type modelProviderStep int

const (
	modelProviderLeave modelProviderStep = iota
	modelProviderPublish
	modelProviderClear
)

// modelProviderStepFor decides it from the connection before and after a save.
// Pure, so the rule is a table test. A model-only change writes nothing: it
// would redeploy every bound proxy.
func modelProviderStepFor(before, after *modelconn.Connection, keyWritten bool) modelProviderStep {
	switch {
	case after == nil && before != nil:
		return modelProviderClear
	case after == nil:
		return modelProviderLeave
	case before == nil || keyWritten || providerFieldsChanged(*before, *after):
		return modelProviderPublish
	default:
		return modelProviderLeave
	}
}

// providerFieldsChanged reports whether a save moved anything the provider is
// built from besides the key: the URL (host and path), the format or the auth
// scheme.
func providerFieldsChanged(before, after modelconn.Connection) bool {
	return before.BaseURL != after.BaseURL || before.Format != after.Format || before.AuthScheme != after.AuthScheme
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

func derefOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

// ResyncSecretRef re-pushes the org's Claude subscription through the
// in-process SecretRefWriter (local OpenBao repair); the connection key's
// repair is ModelConnectionService.ResyncSecretRef, and the repair route runs
// both, so a subscription org never dispatches against a vault path that no
// longer resolves. Returns (true, nil) when the token was pushed, (false, nil)
// when there was nothing to push. ctx must carry an ouId claim (repair
// injects thunder_org_uuid).
func (s *AnthropicCredentialService) ResyncSecretRef(ctx context.Context, ocOrgID string) (bool, error) {
	if s.secretRefWriter == nil || !s.secretRefWriter.Enabled() {
		return false, nil
	}
	return s.resyncRole(ctx, ocOrgID, AnthropicRoleCoding)
}

// resyncRole re-pushes one role's key. A role with no row, an inactive row, no
// triplet, or missing bytes is simply nothing to repair — (false, nil), not an
// error, because the common case is an org with no subscription.
func (s *AnthropicCredentialService) resyncRole(ctx context.Context, ocOrgID string, role AnthropicRole) (bool, error) {
	row, err := fetchAnthropicRow(ctx, s.repo, ocOrgID, role)
	if err != nil {
		var nf *NotFoundError
		if errors.As(err, &nf) {
			return false, nil
		}
		return false, fmt.Errorf("anthropic resync %s: load row: %w", role, err)
	}
	if row.Status != "active" {
		return false, nil
	}
	kvPath := row.SecretRefKVPath
	prop := row.SecretRefProperty
	if kvPath == nil || prop == nil || *kvPath == "" || *prop == "" {
		return false, nil
	}
	key, err := s.store.Get(ctx, ocOrgID, role.SecretStoreKey())
	if err != nil || len(key) == 0 {
		return false, nil
	}
	if _, err := s.secretRefWriter.WriteAnthropic(ctx, ocOrgID, role, string(key)); err != nil {
		return false, fmt.Errorf("anthropic resync %s: write: %w", role, err)
	}
	return true, nil
}

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

// anthropicKeyPreview returns the standard prefix + last-4 display
// shape used everywhere (`sk-ant-ap03-A1B2…XyZw`).
func anthropicKeyPreview(k string) (prefix, last4 string) {
	if len(k) < 20 {
		return k, ""
	}
	// `sk-ant-` + next 8 chars = stable prefix.
	prefix = k[:15]
	last4 = k[len(k)-4:]
	return
}
