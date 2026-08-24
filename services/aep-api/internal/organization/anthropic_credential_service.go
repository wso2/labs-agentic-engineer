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
// AnthropicCredentialService owns the per-org Anthropic API key surface. An org
// holds one row per AnthropicRole:
//
//   - `default` — the org's key. EVERY reader uses it unless overridden.
//   - `coding`  — an optional OVERRIDE read only by coding-agent dispatch. Its
//     absence is what "reuse the default key" means; nothing stores a mode.
//
// The two are not peers: a coding row may only exist while an active default
// row does, and disconnecting the default cascades the coding one away with it
// (ADR-0016). That invariant is what keeps every reader below from needing a
// "which key, and is it there" branch of its own.
//
// Surface — all in-process; this service has no HTTP routes of its own, and is
// reached through the /config orchestrator (Service.Get / Service.Patch) or
// directly from the composition root:
//
//   - Connect / Status / Disconnect — one role at a time. The llm and codingLlm
//     sections of PATCH /config are exactly these, bound to a role.
//   - EffectiveKey — returns the DEFAULT key (or "none") for the genai turn
//     surface, which forwards it to agents-service per call. There is no
//     platform fallback: orgs bring their own key.
//   - ResolveCodingSecretRef — the coding→default fallback, stated once here so
//     no other reader inherits it by accident. Its SecretRefTriplet.EnvVar is
//     what the coding-agent OC Job Component mounts the credential under
//     (ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN) — there is no
//     workflow-plane Secret on this path.
//
// Secret bytes live in the same `org_secrets` (Postgres + AES-256-GCM)
// table as the GitHub PAT, keyed by the role's SecretStoreKey(). The metadata
// (prefix / last4 / status / connected_at / last_validated_at) lives in
// the `org_anthropic_credentials` table.
//
// See docs/decisions/ADR-0016-coding-agent-key-is-an-override-not-a-peer.md.
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

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// AnthropicCredentialService — see package doc.
type AnthropicCredentialService struct {
	repo         OrgAnthropicRepository
	store        secrets.CredentialStore
	anthropicAPI string // "https://api.anthropic.com" by default; overridden in tests
	httpClient   *http.Client

	// secretRefWriter mirrors the key into SM-API on Connect. nil-safe.
	secretRefWriter *SecretRefWriter
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

// NewAnthropicCredentialService wires the service. repo and store must be non-nil.
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
// Errors
// ----------------------------------------------------------------------------

// ErrAnthropicKeyRequired signals that no per-org key is configured and
// the caller specifically required one (dispatch path). Distinct from
// returning the platform fallback. Wrap with status 422 at the API edge.
var ErrAnthropicKeyRequired = errors.New("anthropic: org key required")

// ErrAnthropicDefaultKeyRequired signals an attempt to set the coding-agent
// key on an org with no active default key. The coding key is an override, not
// a peer — there is nothing for it to override yet. sectionErrorFrom turns it
// into a section-scoped client fault, which patchconfig then renders like every
// other probe rejection (400 validation_failed + body.codingLlm). It is what
// stops a client reaching the llm=null + codingLlm=set state the projection
// cannot describe.
var ErrAnthropicDefaultKeyRequired = errors.New("anthropic: connect the organization's Anthropic key before setting a coding-agent key")

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
// Connect / Replace
// ----------------------------------------------------------------------------

// AnthropicConnectRequest is the body for POST /internal/credentials/orgs/{org}/anthropic.
type AnthropicConnectRequest struct {
	APIKey string `json:"apiKey"`
}

// Connect validates the supplied key against Anthropic, persists it in
// `org_secrets` (AES-256-GCM), and upserts the metadata row for role.
// Idempotent under the org-scoped advisory lock — concurrent Connects produce
// one consistent row. The BFF resolves the effective key per request and
// forwards it to agents-service, so there is no remote cache to invalidate.
//
// Connecting the CODING role requires an active default row: the coding key
// overrides the default rather than standing in for it, so without one there
// is nothing to override (ErrAnthropicDefaultKeyRequired → 422). The check runs
// inside the advisory lock so it cannot race a concurrent default disconnect
// and leave an orphan behind.
//
// Does NOT touch any cluster: the coding runner reads the key through
// ResolveCodingSecretRef's SecretRefTriplet, mounted onto the OC Job
// Component's SecretEnv at dispatch time.
func (s *AnthropicCredentialService) Connect(ctx context.Context, ocOrgID string, role AnthropicRole, req AnthropicConnectRequest) (*AnthropicProjection, error) {
	key := strings.TrimSpace(req.APIKey)
	if err := s.ValidateKey(ctx, role, key); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	prefix, last4 := anthropicKeyPreview(key)

	row := OrgAnthropicCredential{
		OcOrgID:         ocOrgID,
		Role:            role,
		CredentialKind:  AnthropicCredentialKindOf(key),
		KeyPrefix:       prefix,
		KeyLast4:        last4,
		Status:          "active",
		ConnectedAt:     now,
		LastValidatedAt: &now,
		ValidationError: nil,
	}
	err := s.repo.Tx(ctx, func(tx OrgAnthropicTx) error {
		if err := tx.AdvisoryLock("org_anthropic:" + ocOrgID); err != nil {
			return fmt.Errorf("anthropic connect: lock: %w", err)
		}

		if role == AnthropicRoleCoding {
			// Read through the TX, not the pool: this precondition and the
			// write that depends on it must see one snapshot, so "a default
			// exists" holds by construction rather than by an argument about
			// the advisory lock.
			base, err := tx.GetByOrg(ocOrgID, AnthropicRoleDefault)
			if err != nil {
				return fmt.Errorf("anthropic connect: load default row: %w", err)
			}
			if base == nil || base.Status != "active" {
				return ErrAnthropicDefaultKeyRequired
			}
		}

		// Encrypted bytes — same KV store the GitHub PAT uses, keyed per role
		// so the coding key can never overwrite the default one's bytes.
		if err := s.store.Put(ctx, ocOrgID, role.SecretStoreKey(), []byte(key)); err != nil {
			return fmt.Errorf("anthropic connect: store put: %w", err)
		}

		// Upsert via ON CONFLICT DO UPDATE so Replace is idempotent. The UPDATE
		// deliberately omits connected_at so a replace preserves the ORIGINAL
		// connection time; RETURNING that column reads the persisted value back so
		// the projection we return matches the stored row (on a replace it's the
		// original, not the in-memory `now`) — Upsert scans it back into
		// row.ConnectedAt.
		if err := tx.Upsert(&row); err != nil {
			return fmt.Errorf("anthropic connect: upsert: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Best-effort SM-API mirror. Same posture as
	// CredentialService.mirrorPATToSMAPI: org_secrets stays authoritative
	// when SM-API is unavailable; the row's SM-API triplet stays NULL
	// until the next successful Connect.
	if s.secretRefWriter != nil && s.secretRefWriter.Enabled() {
		if _, err := s.secretRefWriter.WriteAnthropic(ctx, ocOrgID, role, key); err != nil {
			slog.WarnContext(ctx, "anthropic: SM-API mirror failed (legacy store still authoritative)",
				"ocOrgId", ocOrgID, "role", role, "error", err)
		}
	}

	slog.InfoContext(ctx, "anthropic.connected", "ocOrgId", ocOrgID, "role", role, "keyPrefix", prefix)
	return projectionFromAnthropicRow(&row), nil
}

// ValidateKey runs the connect-time validation for a credential WITHOUT
// persisting anything: the shape checks plus the live /v1/messages probe,
// authenticated the way that KIND of credential authenticates.
//
// role bounds which kinds are acceptable: a Claude Code OAuth token is only
// meaningful for the coding agent, so offering one as the org's default key is
// rejected here rather than discovered later by a design agent that cannot
// authenticate with it.
//
// It is the probe-only seam the /config PATCH orchestrator calls in its
// pre-persist phase, so a bad key in one section fails the whole atomic patch
// before any section is written (docs/design/org-config-consolidation.md §4).
// Connect calls it too, so the validation logic lives in exactly one place and
// the two paths can't drift.
func (s *AnthropicCredentialService) ValidateKey(ctx context.Context, role AnthropicRole, apiKey string) error {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return &ValidationError{Code: "anthropic_key_missing", Message: "apiKey is required"}
	}
	if !looksLikeAnthropicKey(key) {
		return &ValidationError{Code: "anthropic_key_invalid", Message: "value does not look like an Anthropic credential (expected prefix 'sk-ant-')"}
	}
	kind := AnthropicCredentialKindOf(key)
	if kind == AnthropicCredentialOAuth && role != AnthropicRoleCoding {
		return &ValidationError{
			Code: "anthropic_oauth_token_coding_only",
			Message: "a Claude Code OAuth token can only be used as the coding agent's credential; " +
				"the organization's Anthropic key must be a Console API key (sk-ant-api…)",
		}
	}
	return s.validateAnthropicKey(ctx, kind, key)
}

// ----------------------------------------------------------------------------
// Status
// ----------------------------------------------------------------------------

// Status returns the projection for (ocOrgID, role). Returns NotFoundError
// when no row exists so the API edge can map to 404 — and, for the coding
// role, so the config projection can map it to null ("reuse").
func (s *AnthropicCredentialService) Status(ctx context.Context, ocOrgID string, role AnthropicRole) (*AnthropicProjection, error) {
	row, err := s.fetchRow(ctx, ocOrgID, role)
	if err != nil {
		return nil, err
	}
	return projectionFromAnthropicRow(row), nil
}

// ----------------------------------------------------------------------------
// Disconnect
// ----------------------------------------------------------------------------

// Disconnect removes an org's Anthropic key for role: deletes the encrypted
// bytes from `org_secrets` and drops the metadata row (status flip first, then
// delete via best-effort sweep is overkill for a single per-org credential).
//
// Disconnecting the DEFAULT role CASCADES: the coding key is an override on it
// and cannot outlive it, so every role's row and bytes go in the same
// transaction. Without the cascade an org could reach llm=null + codingLlm=set
// — a state the projection has no way to describe and dispatch has no way to
// act on (ADR-0016). Disconnecting the coding role touches only itself, and is
// how the console's "reuse the key above" flip is spelled.
//
// Idempotent: missing row is a no-op (200 → 204 at the API edge).
func (s *AnthropicCredentialService) Disconnect(ctx context.Context, ocOrgID string, role AnthropicRole) error {
	cascade := role == AnthropicRoleDefault

	err := s.repo.Tx(ctx, func(tx OrgAnthropicTx) error {
		if err := tx.AdvisoryLock("org_anthropic:" + ocOrgID); err != nil {
			return fmt.Errorf("anthropic disconnect: lock: %w", err)
		}

		// Delete the metadata row directly — the existing GitHub PAT flow flips
		// to `disconnected` for audit, but here we have nothing else referencing
		// the row (no installation_id, no webhook routing). Delete is cleaner.
		if cascade {
			if err := tx.DeleteAllRoles(ocOrgID); err != nil {
				return fmt.Errorf("anthropic disconnect: delete rows: %w", err)
			}
			return nil
		}
		if err := tx.DeleteByOrg(ocOrgID, role); err != nil {
			return fmt.Errorf("anthropic disconnect: delete row: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Best-effort GC. Failures are logged, not surfaced. The cascade sweeps the
	// coding role's bytes too — the rows are already gone, so anything left
	// here would be unreachable material nothing can ever clean up.
	gcRoles := []AnthropicRole{role}
	if cascade {
		gcRoles = []AnthropicRole{AnthropicRoleDefault, AnthropicRoleCoding}
	}
	for _, r := range gcRoles {
		if err := s.store.Delete(ctx, ocOrgID, r.SecretStoreKey()); err != nil {
			slog.WarnContext(ctx, "anthropic disconnect: store delete failed",
				"ocOrgId", ocOrgID, "role", r, "error", err)
		}
	}

	slog.InfoContext(ctx, "anthropic.disconnected", "ocOrgId", ocOrgID, "role", role, "cascade", cascade)
	return nil
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
// Deliberately default-only. Its caller is agents-service (the design agent),
// which the coding override does not reach: an org that scoped a key to the
// coding agent has said which reader it is for, and this is not that reader.
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
// ResolveCodingSecretRef — the reuse fallback, stated once
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
	// Claude Code OAuth token. Carried here rather than re-derived at the
	// mount site because the secret bytes are never read on that path, so
	// nothing downstream can tell the two apart on its own.
	EnvVar string
}

// ResolveCodingSecretRef returns the secret reference a coding run must mount:
// the coding row's when the org configured one, the default row's otherwise.
// This is the ONLY place the reuse fallback is written; every other reader is
// default-only by construction, so the rule cannot leak into one by omission.
//
// Fails closed. A coding row that exists but has no usable triplet is an
// error, never a silent fall-through to the default key: the org asked for its
// coding agent to bill a specific key, and quietly billing a different one
// defeats the whole point while leaving no trace anywhere the org can see.
func (s *AnthropicCredentialService) ResolveCodingSecretRef(ctx context.Context, ocOrgID string) (SecretRefTriplet, error) {
	coding, err := s.repo.GetByOrg(ctx, ocOrgID, AnthropicRoleCoding)
	if err != nil {
		return SecretRefTriplet{}, fmt.Errorf("anthropic resolve coding ref: load coding row: %w", err)
	}
	if coding != nil {
		if coding.Status != "active" {
			return SecretRefTriplet{}, fmt.Errorf(
				"coding-agent Anthropic key for org %q is %s — reconnect it in Settings, "+
					"or switch the organization back to reusing its default key", ocOrgID, coding.Status)
		}
		ref, refErr := tripletFrom(coding)
		if refErr != nil {
			return SecretRefTriplet{}, fmt.Errorf(
				"coding-agent Anthropic key for org %q is configured but %w — reconnect it in Settings, "+
					"or switch the organization back to reusing its default key", ocOrgID, refErr)
		}
		return ref, nil
	}

	// Reuse: no coding row, so the run bills the org's default key.
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

// ResyncSecretRef re-pushes the org's Anthropic keys through the in-process
// SecretRefWriter (local OpenBao repair). EVERY role is resynced: a repair that
// only restored the default key would leave a separate-key org dispatching
// against a vault path that no longer resolves, which fails closed — a repair
// that visibly does not repair. Returns (true, nil) when at least one role was
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
// error, because the common case is an org that never set a coding key.
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
