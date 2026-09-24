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
// AnthropicCredentialService owns the per-org Anthropic credentials. An org
// holds one row per AnthropicRole:
//
//   - `default` — the org's API key. EVERY reader uses it.
//   - `coding`  — an optional Claude subscription token (`claude setup-token`)
//     the coding agent bills instead of the API key, only while it runs on
//     Claude Code. It cannot exist without the default row.
//
// Surface — all in-process; this service has no HTTP routes of its own:
//
//   - ValidateKey — the shape checks plus the live probe, per role. The AI
//     agents card (AgentSettingsService) calls it before its unit of work.
//   - writeKeyTx / deleteKeyTx — the credential half of that unit of work,
//     inside its transaction; mirrorKey / forgetKey — the SM-API copy, after
//     it commits; publishModelKey — the Agent Manager provider's copy of the
//     default key, after it commits.
//   - Status — one role's masked projection; Holds — whether a role's row exists.
//   - EffectiveKey — the DEFAULT key (or "none") for the spec agents, which
//     the BFF forwards to agents-service per turn. There is no platform
//     fallback: orgs bring their own key.
//   - ResolveCodingSecretRef — which credential a coding run mounts, stated
//     once here so no other reader inherits the rule by accident. Its
//     SecretRefTriplet.EnvVar is what the coding-agent OC Job Component mounts
//     the credential under (ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN).
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

	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
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

	// modelProvider is told when the org's key changes, so an Agent Manager
	// provider holding a COPY of it stops holding a revoked one. nil-safe.
	modelProvider ModelProviderPublisher
}

// ModelProviderPublisher receives the org's current Anthropic key so a governed
// model provider can be kept truthful.
//
// Declared here, implemented at the composition root: this domain must not
// reach into Agent Manager, and the only thing it has to say is "the key
// changed". An org with no governed environment has no implementation wired and
// nothing happens.
type ModelProviderPublisher interface {
	PublishOrgModelKey(ctx context.Context, ocOrgID, apiKey string) error
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
// non-nil; store serves the reads (EffectiveKey, resync), while the card's
// writes go through the store bound to its transaction.
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

// ValidateKey runs the save-time validation for a credential WITHOUT
// persisting anything: the shape checks plus the live /v1/messages probe,
// authenticated the way that KIND of credential authenticates.
//
// role fixes the one kind it accepts: the default role holds a Console API key
// (the spec agents are AI SDK calls that cannot present a bearer token), the
// coding role a Claude subscription token (a separate coding API key is not a
// thing the platform offers). The wrong kind is refused here, before a probe is
// spent on it, rather than discovered later by an agent that cannot use it.
func (s *AnthropicCredentialService) ValidateKey(ctx context.Context, role AnthropicRole, apiKey string) error {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return &ValidationError{Code: "anthropic_key_missing", Message: "a credential is required"}
	}
	if !looksLikeAnthropicKey(key) {
		return &ValidationError{Code: "anthropic_key_invalid", Message: "value does not look like an Anthropic credential (expected prefix 'sk-ant-')"}
	}
	kind := AnthropicCredentialKindOf(key)
	switch {
	case role == AnthropicRoleCoding && kind != AnthropicCredentialOAuth:
		return &ValidationError{
			Code: "agents_subscription_token_required",
			Message: "a Claude subscription takes a token from `claude setup-token` (sk-ant-oat…); " +
				"an Anthropic API key belongs in the organization's API key field",
		}
	case role != AnthropicRoleCoding && kind == AnthropicCredentialOAuth:
		return &ValidationError{
			Code: "anthropic_oauth_token_coding_only",
			Message: "a Claude subscription token can only bill the coding agent; " +
				"the organization's Anthropic key must be a Console API key (sk-ant-api…)",
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

// mirrorKey copies a committed credential into SM-API, best-effort: org_secrets
// stays authoritative. The save cleared the row's triplet (UpsertCredential), so
// a failed mirror leaves it NULL until the next save, and dispatch fails closed
// with a reason naming it rather than mounting the previous credential.
func (s *AnthropicCredentialService) mirrorKey(ctx context.Context, ocOrgID string, role AnthropicRole, key string) {
	if s.secretRefWriter == nil || !s.secretRefWriter.Enabled() {
		return
	}
	if _, err := s.secretRefWriter.WriteAnthropic(ctx, ocOrgID, role, strings.TrimSpace(key)); err != nil {
		slog.WarnContext(ctx, "anthropic: SM-API mirror failed (org_secrets still authoritative)",
			"ocOrgId", ocOrgID, "role", role, "error", err)
	}
}

// forgetKey deletes a removed credential's SM-API copy, best-effort, by the
// secret-ref name deleteKeyTx captured before the row went. A failure leaves an
// orphaned vault entry nothing reads; the next save of that role overwrites it.
func (s *AnthropicCredentialService) forgetKey(ctx context.Context, ocOrgID string, role AnthropicRole, secretRefName string) {
	if s.secretRefWriter == nil || !s.secretRefWriter.Enabled() {
		return
	}
	if err := s.secretRefWriter.DeleteAnthropic(ctx, ocOrgID, role, secretRefName); err != nil {
		slog.WarnContext(ctx, "anthropic: SM-API delete failed (orphaned copy until the next save)",
			"ocOrgId", ocOrgID, "role", role, "error", err)
	}
}

// publishModelKey pushes a committed DEFAULT key to the org's Agent Manager
// provider, which holds a COPY of it on behalf of every governed agent.
//
// WHY THIS MATTERS MORE THAN IT LOOKS: without it, a rotated key leaves the
// provider calling Anthropic with a revoked one, and EVERY governed agent in
// the org fails at once — at the upstream, far from Settings, with nothing in
// AEP saying why.
//
// Best-effort, and deliberately so: the key IS stored, and failing the user's
// Settings action because a downstream copy lagged would be the worse outcome.
// The next governed deploy re-asserts it anyway (EnsureProvider writes the
// current key every time), so this is how fast it converges, not whether it
// does.
//
// Only the DEFAULT role is ever published: that is the key agents run on. The
// coding role's subscription token belongs to the coding agent, which does not
// go through the gateway.
func (s *AnthropicCredentialService) publishModelKey(ctx context.Context, ocOrgID, key string) {
	if s.modelProvider == nil {
		return
	}
	if err := s.modelProvider.PublishOrgModelKey(ctx, ocOrgID, strings.TrimSpace(key)); err != nil {
		slog.WarnContext(ctx, "anthropic: could not publish the rotated key to the Agent Manager provider; governed agents keep the previous key until the next deploy",
			"ocOrgId", ocOrgID, "error", err)
	}
}

// ----------------------------------------------------------------------------
// Status
// ----------------------------------------------------------------------------

// Status returns the projection for (ocOrgID, role). Returns NotFoundError
// when no row exists, which the config projection maps to null (no key, or no
// subscription).
func (s *AnthropicCredentialService) Status(ctx context.Context, ocOrgID string, role AnthropicRole) (*AnthropicProjection, error) {
	row, err := s.fetchRow(ctx, ocOrgID, role)
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
// EffectiveKey
// ----------------------------------------------------------------------------

// EffectiveKeyResponse is the shape returned to agents-service.
type EffectiveKeyResponse struct {
	Source string `json:"source"` // "org" | "none"
	Key    string `json:"key,omitempty"`
}

// EffectiveKey returns the org's DEFAULT key when configured (and active).
// Returns { source: "none" } when the org has no usable key — agents-service
// maps to 503. There is no platform fallback: orgs bring their own key.
//
// Deliberately default-only: the spec agents are AI SDK calls, which cannot
// present the coding role's subscription token.
func (s *AnthropicCredentialService) EffectiveKey(ctx context.Context, ocOrgID string) (*EffectiveKeyResponse, error) {
	row, err := s.fetchRow(ctx, ocOrgID, AnthropicRoleDefault)
	if err == nil && row.Status == "active" {
		key, getErr := s.store.Get(ctx, ocOrgID, AnthropicRoleDefault.SecretStoreKey())
		if getErr == nil && len(key) > 0 {
			return &EffectiveKeyResponse{Source: "org", Key: string(key)}, nil
		}
		// Row says active but bytes are gone — log loudly and return "none".
		slog.WarnContext(ctx, "anthropic effective-key: row=active but org_secrets missing",
			"ocOrgId", ocOrgID, "error", getErr)
	}
	// Row absent (NotFoundError) or not active, or bytes missing.
	return &EffectiveKeyResponse{Source: "none"}, nil
}

// ----------------------------------------------------------------------------
// ResolveCodingSecretRef — which credential a coding run mounts, stated once
// ----------------------------------------------------------------------------

// SecretRefTriplet is a resolved SM-API secret reference: the name plus the
// vault coordinates an ExternalSecret's remoteRef needs, and the env var the
// materialised value must land under.
type SecretRefTriplet struct {
	Name     string
	KVPath   string
	Property string

	// EnvVar is the name a coding run must receive this credential as —
	// ANTHROPIC_API_KEY for a Console API key, CLAUDE_CODE_OAUTH_TOKEN for a
	// Claude subscription token. Carried here rather than re-derived at the
	// mount site because the secret bytes are never read on that path, so
	// nothing downstream can tell the two apart on its own.
	EnvVar string
}

// ResolveCodingSecretRef returns the secret reference a coding run on runtime
// must mount: the org's Claude subscription when it has one and the runtime is
// Claude Code, its API key otherwise. This is the ONLY place that choice is
// written; every other reader is default-only by construction.
//
// Only Claude Code can present a subscription token, so on any other runtime
// the subscription is not consulted at all (the save rule keeps one from being
// stored alongside OpenCode; this keeps a stray row from ever reaching a run).
//
// Fails closed. A subscription that exists but has no usable triplet is an
// error, never a silent fall-through to the API key: the org chose to bill its
// plan, and quietly billing API credits instead defeats that choice while
// leaving no trace the org can see.
func (s *AnthropicCredentialService) ResolveCodingSecretRef(ctx context.Context, ocOrgID string, runtime orgconfig.AgentRuntime) (SecretRefTriplet, error) {
	if runtime == orgconfig.AgentRuntimeClaudeCode {
		sub, err := s.repo.GetByOrg(ctx, ocOrgID, AnthropicRoleCoding)
		if err != nil {
			return SecretRefTriplet{}, fmt.Errorf("anthropic resolve coding ref: load subscription row: %w", err)
		}
		if sub != nil {
			if sub.Status != "active" {
				return SecretRefTriplet{}, fmt.Errorf(
					"the Claude subscription for org %q is %s — replace its token in Settings, "+
						"or remove the subscription so coding bills the organization's API key", ocOrgID, sub.Status)
			}
			ref, refErr := tripletFrom(sub)
			if refErr != nil {
				return SecretRefTriplet{}, fmt.Errorf(
					"the Claude subscription for org %q is configured but %w — save its token again in Settings, "+
						"or remove the subscription so coding bills the organization's API key", ocOrgID, refErr)
			}
			return ref, nil
		}
	}

	def, err := s.repo.GetByOrg(ctx, ocOrgID, AnthropicRoleDefault)
	if err != nil {
		return SecretRefTriplet{}, fmt.Errorf("anthropic resolve coding ref: load default row: %w", err)
	}
	if def == nil {
		return SecretRefTriplet{}, fmt.Errorf(
			"anthropic secret reference missing for org %q: org_anthropic_credentials row not found", ocOrgID)
	}
	ref, err := tripletFrom(def)
	if err != nil {
		return SecretRefTriplet{}, fmt.Errorf("anthropic secret reference for org %q: %w", ocOrgID, err)
	}
	return ref, nil
}

// ----------------------------------------------------------------------------
// DefaultKeyRef — the default-role vault triplet, for consumers that mount
// a SecretReference rather than reading the key's bytes
// ----------------------------------------------------------------------------

// DefaultKeyRef returns the org's DEFAULT-role Anthropic key's vault
// coordinates — the same {kvPath, property} pushExternalSecret resolves to
// deliver the RCA agent's ExternalSecret (see that method's doc comment).
// Distinct from EffectiveKey: this never reads the key's bytes, only where
// they live, for a caller that points an OpenChoreo SecretReference at the
// path rather than forwarding the value itself (e.g. wiring an ai-agent
// component's MODEL_API_KEY — docs/glossary.md's SecretReference entry:
// "authored in the org NS, ESO materializes it into the consuming-plane
// NS").
//
// Returns NotFoundError when the org has no active default key. Every
// caller must treat that as "not connected yet", not a hard failure — same
// discipline EffectiveKey's Source:"none" gives genai callers.
func (s *AnthropicCredentialService) DefaultKeyRef(ctx context.Context, ocOrgID string) (SecretRefTriplet, error) {
	row, err := s.fetchRow(ctx, ocOrgID, AnthropicRoleDefault)
	if err != nil {
		return SecretRefTriplet{}, err
	}
	if row.Status != "active" {
		return SecretRefTriplet{}, &NotFoundError{What: fmt.Sprintf("org_anthropic_credentials.%s.default (status=%s)", ocOrgID, row.Status)}
	}
	return tripletFrom(row)
}

// tripletFrom reads a row's resolved secret-ref coordinates, naming whichever
// one is missing so a half-mirrored row is diagnosable from the error alone.
func tripletFrom(row *OrgAnthropicCredential) (SecretRefTriplet, error) {
	ref := SecretRefTriplet{
		Name:     derefOrEmpty(row.SecretRefName),
		KVPath:   derefOrEmpty(row.SecretRefKVPath),
		Property: derefOrEmpty(row.SecretRefProperty),
		EnvVar:   row.CredentialKind.RunnerEnvVar(),
	}
	switch {
	case ref.Name == "":
		return SecretRefTriplet{}, errors.New("secret_ref_name is not populated")
	case ref.KVPath == "":
		return SecretRefTriplet{}, errors.New("secret_ref_kv_path is not populated")
	case ref.Property == "":
		return SecretRefTriplet{}, errors.New("secret_ref_property is not populated")
	}
	return ref, nil
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

// ResyncSecretRef re-pushes the org's Anthropic credentials through the
// in-process SecretRefWriter (local OpenBao repair). EVERY role is resynced: a
// repair that only restored the API key would leave a subscription org
// dispatching against a vault path that no longer resolves, which fails
// closed — a repair that visibly does not repair. Returns (true, nil) when at least one role was
// pushed, (false, nil) when there was nothing to push. ctx must carry an ouId
// claim (repair injects thunder_org_uuid).
func (s *AnthropicCredentialService) ResyncSecretRef(ctx context.Context, ocOrgID string) (bool, error) {
	if s.secretRefWriter == nil || !s.secretRefWriter.Enabled() {
		return false, nil
	}
	wroteAny := false
	for _, role := range []AnthropicRole{AnthropicRoleDefault, AnthropicRoleCoding} {
		wrote, err := s.resyncRole(ctx, ocOrgID, role)
		if err != nil {
			return wroteAny, err
		}
		wroteAny = wroteAny || wrote
	}
	return wroteAny, nil
}

// resyncRole re-pushes one role's key. A role with no row, an inactive row, no
// triplet, or missing bytes is simply nothing to repair — (false, nil), not an
// error, because the common case is an org with no subscription.
func (s *AnthropicCredentialService) resyncRole(ctx context.Context, ocOrgID string, role AnthropicRole) (bool, error) {
	row, err := s.fetchRow(ctx, ocOrgID, role)
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

func (s *AnthropicCredentialService) fetchRow(ctx context.Context, ocOrgID string, role AnthropicRole) (*OrgAnthropicCredential, error) {
	row, err := s.repo.GetByOrg(ctx, ocOrgID, role)
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
