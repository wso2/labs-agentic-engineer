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
// AnthropicCredentialService owns the per-org Anthropic API key surface:
//
//   - Connect / Status / Disconnect (POST/GET/DELETE /internal/credentials/orgs/{org}/anthropic)
//   - EffectiveKey (GET .../anthropic/effective-key) — returns the org key
//     (or "none"), used by agents-service per-call. There is no platform
//     fallback: orgs bring their own key.
//   - ApplyWPSecret (POST .../anthropic/apply-wp-secret) — refreshes the
//     per-org K8s Secret in workflows-<ocOrgID> with the freshest value
//     from `org_secrets`. Same model as MintBuildToken's per-dispatch
//     SSA — see build_credentials_service.go.
//
// Secret bytes live in the same `org_secrets` (Postgres + AES-256-GCM)
// table as the GitHub PAT, keyed by `anthropic/key`. The metadata
// (prefix / last4 / status / connected_at / last_validated_at) lives in
// the `org_anthropic_credentials` table.
//
// See docs/design/anthropic-key-dual-token.md.
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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wso2/aep/aep-api/internal/clients/clustergatewayproxy"
	"github.com/wso2/aep/aep-api/internal/clients/k8s"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// AnthropicCredentialService — see package doc.
type AnthropicCredentialService struct {
	repo         OrgAnthropicRepository
	store        secrets.CredentialStore
	wpClient     client.Client
	anthropicAPI string // "https://api.anthropic.com" by default; overridden in tests
	httpClient   *http.Client

	// secretRefWriter mirrors the key into SM-API on Connect. nil-safe.
	secretRefWriter *SecretRefWriter

	// cgwClient + pushNamespace/pushSecretName: when all three are set,
	// Connect() pushes a fresh ExternalSecret to pushNamespace every time an
	// org's key is connected OR rotated — see pushExternalSecret. This closes
	// the gap where the SM-API mirror's vault path changes (a fresh random
	// suffix on every write, by design of the underlying secret-manager
	// contract) but nothing tells a consumer's ExternalSecret to follow it.
	// nil-safe: cgwClient nil or either name empty disables the push
	// entirely — no consumer is assumed by default.
	cgwClient      *clustergatewayproxy.Client
	pushNamespace  string
	pushSecretName string
}

// WithSecretRefWriter injects the SM-API writer; chainable. nil disables
// the mirror — the org_secrets path remains authoritative.
func (s *AnthropicCredentialService) WithSecretRefWriter(w *SecretRefWriter) *AnthropicCredentialService {
	s.secretRefWriter = w
	return s
}

// WithRCAAgentPush configures the post-Connect ExternalSecret push; chainable.
// Pass a nil client or empty names to leave the push disabled (the default).
func (s *AnthropicCredentialService) WithRCAAgentPush(c *clustergatewayproxy.Client, namespace, secretName string) *AnthropicCredentialService {
	s.cgwClient = c
	s.pushNamespace = namespace
	s.pushSecretName = secretName
	return s
}

// pushEnabled reports whether the post-Connect ExternalSecret push is
// configured. All three must be set — a partially configured push (e.g. a
// namespace with no client) would silently do nothing anyway, so treat that
// as "disabled" rather than as an error.
func (s *AnthropicCredentialService) pushEnabled() bool {
	return s.cgwClient != nil && s.pushNamespace != "" && s.pushSecretName != ""
}

// WithAnthropicAPIBase points key validation at base instead of the real
// Anthropic API; chainable. Tests aim it at an httptest server so
// validateAnthropicKey's probe never leaves the process.
func (s *AnthropicCredentialService) WithAnthropicAPIBase(base string) *AnthropicCredentialService {
	s.anthropicAPI = base
	return s
}

// NewAnthropicCredentialService wires the service. db, store must be
// non-nil; wpClient may be nil (off-cluster degraded mode — same shape as
// BuildCredentialsService).
func NewAnthropicCredentialService(
	repo OrgAnthropicRepository,
	store secrets.CredentialStore,
	wpClient client.Client,
) *AnthropicCredentialService {
	return &AnthropicCredentialService{
		repo:         repo,
		store:        store,
		wpClient:     wpClient,
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

// ----------------------------------------------------------------------------
// Projection — what the API + console see
// ----------------------------------------------------------------------------

type AnthropicProjection struct {
	OcOrgID         string     `json:"ocOrgId"`
	KeyPrefix       string     `json:"keyPrefix"`
	KeyLast4        string     `json:"keyLast4"`
	Status          string     `json:"status"`
	ConnectedAt     time.Time  `json:"connectedAt"`
	LastValidatedAt *time.Time `json:"lastValidatedAt,omitempty"`
	ValidationError *string    `json:"validationError,omitempty"`
}

func projectionFromAnthropicRow(r *OrgAnthropicCredential) *AnthropicProjection {
	return &AnthropicProjection{
		OcOrgID:         r.OcOrgID,
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
// `org_secrets` (AES-256-GCM), and upserts the metadata row. Idempotent under
// the org-scoped advisory lock — concurrent Connects produce one consistent
// row. The BFF resolves the effective key per request and forwards it to
// agents-service, so there is no remote cache to invalidate.
//
// Does NOT touch the workflow-plane namespace; the K8s Secret is materialised
// lazily on first dispatch via ApplyWPSecret.
func (s *AnthropicCredentialService) Connect(ctx context.Context, ocOrgID string, req AnthropicConnectRequest) (*AnthropicProjection, error) {
	key := strings.TrimSpace(req.APIKey)
	if err := s.ValidateKey(ctx, key); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	prefix, last4 := anthropicKeyPreview(key)

	row := OrgAnthropicCredential{
		OcOrgID:         ocOrgID,
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

		// Encrypted bytes — same KV store the GitHub PAT uses.
		if err := s.store.Put(ctx, ocOrgID, "anthropic/key", []byte(key)); err != nil {
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
		if _, err := s.secretRefWriter.WriteAnthropic(ctx, ocOrgID, key); err != nil {
			slog.WarnContext(ctx, "anthropic: SM-API mirror failed (legacy store still authoritative)",
				"ocOrgId", ocOrgID, "error", err)
		} else if s.pushEnabled() {
			// The mirror just stamped a FRESH secret_ref_kv_path/property onto the
			// row (every WriteAnthropic call gets a brand-new random-suffixed
			// vault path — never an in-place update of the previous one). Push
			// an ExternalSecret pointing at that fresh path now, synchronously
			// with this Connect() call, rather than waiting for something to
			// re-discover it later — this is what makes a console-side
			// connect/rotate take effect without any manual re-run.
			if err := s.pushExternalSecret(ctx, ocOrgID); err != nil {
				slog.WarnContext(ctx, "anthropic: ExternalSecret push failed (consumer keeps its last-synced key)",
					"ocOrgId", ocOrgID, "error", err)
			}
		}
	}

	slog.InfoContext(ctx, "anthropic.connected", "ocOrgId", ocOrgID, "keyPrefix", prefix)
	return projectionFromAnthropicRow(&row), nil
}

// pushExternalSecret applies an ExternalSecret in s.pushNamespace whose
// remoteRef points at the org's CURRENT SM-API-mirrored vault path (read
// back from the row this call just stamped), via the same
// cluster-gateway-proxy ApplyExternalSecret the coding-agent dispatcher
// already uses for its own per-run ExternalSecrets
// (internal/delivery/codingagent/dispatcher.go). Idempotent: re-applying
// with the same name updates in place.
func (s *AnthropicCredentialService) pushExternalSecret(ctx context.Context, ocOrgID string) error {
	row, err := s.fetchRow(ctx, ocOrgID)
	if err != nil {
		return fmt.Errorf("push external secret: reload row: %w", err)
	}
	kvPath := row.ResolvedSecretRefKVPath()
	prop := row.ResolvedSecretRefProperty()
	if kvPath == nil || prop == nil || *kvPath == "" || *prop == "" {
		return errors.New("push external secret: row has no secret-ref triplet yet")
	}
	manifest := map[string]any{
		"apiVersion": "external-secrets.io/v1",
		"kind":       "ExternalSecret",
		"metadata": map[string]any{
			"name":      s.pushSecretName,
			"namespace": s.pushNamespace,
		},
		"spec": map[string]any{
			"refreshInterval": "5m",
			"secretStoreRef": map[string]any{
				"kind": "ClusterSecretStore",
				"name": "default",
			},
			"target": map[string]any{
				"name": s.pushSecretName,
			},
			"data": []map[string]any{
				{
					"secretKey": "RCA_LLM_API_KEY",
					"remoteRef": map[string]any{
						"key":      *kvPath,
						"property": *prop,
					},
				},
			},
		},
	}
	if err := s.cgwClient.ApplyExternalSecret(ctx, s.pushNamespace, manifest); err != nil {
		return fmt.Errorf("apply external secret: %w", err)
	}
	slog.InfoContext(ctx, "anthropic: pushed ExternalSecret to consumer",
		"ocOrgId", ocOrgID, "namespace", s.pushNamespace, "secretName", s.pushSecretName)
	return nil
}

// ValidateKey runs the connect-time validation for an Anthropic key WITHOUT
// persisting anything: the shape checks plus the live /v1/messages probe.
//
// It is the probe-only seam the /config PATCH orchestrator calls in its
// pre-persist phase, so a bad key in one section fails the whole atomic patch
// before any section is written (docs/design/org-config-consolidation.md §4).
// Connect calls it too, so the validation logic lives in exactly one place and
// the two paths can't drift.
func (s *AnthropicCredentialService) ValidateKey(ctx context.Context, apiKey string) error {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return &ValidationError{Code: "anthropic_key_missing", Message: "apiKey is required"}
	}
	if !looksLikeAnthropicKey(key) {
		return &ValidationError{Code: "anthropic_key_invalid", Message: "API key does not look like an Anthropic key (expected prefix 'sk-ant-')"}
	}
	return s.validateAnthropicKey(ctx, key)
}

// ----------------------------------------------------------------------------
// Status
// ----------------------------------------------------------------------------

// Status returns the projection for ocOrgID. Returns NotFoundError when
// no row exists so the API edge can map to 404.
func (s *AnthropicCredentialService) Status(ctx context.Context, ocOrgID string) (*AnthropicProjection, error) {
	row, err := s.fetchRow(ctx, ocOrgID)
	if err != nil {
		return nil, err
	}
	return projectionFromAnthropicRow(row), nil
}

// ----------------------------------------------------------------------------
// Disconnect
// ----------------------------------------------------------------------------

// Disconnect removes the org's Anthropic key: deletes the encrypted bytes
// from `org_secrets`, drops the metadata row (status flip first, then
// delete via best-effort sweep is overkill for a single per-org credential),
// and best-effort deletes the per-org WP Secret.
//
// Idempotent: missing row is a no-op (200 → 204 at the API edge).
func (s *AnthropicCredentialService) Disconnect(ctx context.Context, ocOrgID string) error {
	err := s.repo.Tx(ctx, func(tx OrgAnthropicTx) error {
		if err := tx.AdvisoryLock("org_anthropic:" + ocOrgID); err != nil {
			return fmt.Errorf("anthropic disconnect: lock: %w", err)
		}

		// Delete the metadata row directly — the existing GitHub PAT flow flips
		// to `disconnected` for audit, but here we have nothing else referencing
		// the row (no installation_id, no webhook routing). Delete is cleaner.
		if err := tx.DeleteByOrg(ocOrgID); err != nil {
			return fmt.Errorf("anthropic disconnect: delete row: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Best-effort GC. Failures are logged, not surfaced.
	if err := s.store.Delete(ctx, ocOrgID, "anthropic/key"); err != nil {
		slog.WarnContext(ctx, "anthropic disconnect: store delete failed",
			"ocOrgId", ocOrgID, "error", err)
	}
	if err := s.DeleteAnthropicSecret(ctx, ocOrgID); err != nil {
		slog.WarnContext(ctx, "anthropic disconnect: wp secret delete failed",
			"ocOrgId", ocOrgID, "error", err)
	}

	slog.InfoContext(ctx, "anthropic.disconnected", "ocOrgId", ocOrgID)
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

// EffectiveKey returns the org key when configured (and active). Returns
// { source: "none" } when the org has no usable key — agents-service maps
// to 503. There is no platform fallback: orgs bring their own key.
func (s *AnthropicCredentialService) EffectiveKey(ctx context.Context, ocOrgID string) (*EffectiveKeyResponse, error) {
	row, err := s.fetchRow(ctx, ocOrgID)
	if err == nil && row.Status == "active" {
		key, getErr := s.store.Get(ctx, ocOrgID, "anthropic/key")
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
// ApplyWPSecret
// ----------------------------------------------------------------------------

// ApplyWPSecretResult is returned to the dispatch caller — the K8s Secret
// name to thread into the WorkflowRun's `parameters.anthropic.secretRef`.
type ApplyWPSecretResult struct {
	SecretRefName string `json:"secretRefName"`
}

// ApplyWPSecret reads the per-org key from `org_secrets`, decrypts it, and
// SSA-applies the per-org K8s Secret in `workflows-<ocOrgID>`. Returns
// ErrAnthropicKeyRequired when no org row exists or it's not active —
// the dispatch path maps to 422. Returns a wrapped error when the
// underlying SSA fails.
//
// Same model as `BuildCredentialsService.MintBuildToken` → `applyBuildSecret`:
// per-dispatch refresh, idempotent SSA with FieldOwner, no long-term K8s
// state ownership.
func (s *AnthropicCredentialService) ApplyWPSecret(ctx context.Context, ocOrgID string) (*ApplyWPSecretResult, error) {
	row, err := s.fetchRow(ctx, ocOrgID)
	if err != nil {
		return nil, ErrAnthropicKeyRequired
	}
	if row.Status != "active" {
		return nil, ErrAnthropicKeyRequired
	}

	key, err := s.store.Get(ctx, ocOrgID, "anthropic/key")
	if err != nil {
		// Row is active but the bytes are missing — refuse rather than
		// silently fall through.
		return nil, fmt.Errorf("anthropic apply-wp-secret: store get: %w", err)
	}
	if len(key) == 0 {
		return nil, ErrAnthropicKeyRequired
	}

	if err := s.applyAnthropicSecret(ctx, ocOrgID, key); err != nil {
		return nil, fmt.Errorf("anthropic apply-wp-secret: ssa: %w", err)
	}

	return &ApplyWPSecretResult{SecretRefName: tenant.AnthropicSecretName}, nil
}

// applyAnthropicSecret SSA-applies the per-org Opaque Secret carrying
// ANTHROPIC_API_KEY into workflows-<ocOrgID>. No-op (with a warn) when
// wpClient is nil — same degraded-mode behaviour as build_credentials_service.
func (s *AnthropicCredentialService) applyAnthropicSecret(ctx context.Context, ocOrgID string, key []byte) error {
	if s.wpClient == nil {
		slog.WarnContext(ctx, "anthropic apply-wp-secret: wp k8s client not configured — Secret write skipped",
			"ocOrgId", ocOrgID)
		return nil
	}

	ns := tenant.WorkflowPlaneNamespace(ocOrgID)
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      tenant.AnthropicSecretName,
			Namespace: ns,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":   "aep-git-service",
				"aep.openchoreo.dev/oc-org-id":   ocOrgID,
				"aep.openchoreo.dev/secret-type": "anthropic-credentials",
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"ANTHROPIC_API_KEY": string(key),
		},
	}

	if err := s.wpClient.Patch(
		ctx, secret,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner(k8s.FieldOwner),
	); err != nil {
		return fmt.Errorf("ssa anthropic secret: %w", err)
	}
	return nil
}

// DeleteAnthropicSecret removes the per-org Anthropic Secret from
// workflows-<ocOrgID>. Idempotent — NotFound + nil wpClient are no-ops.
// Implements the AnthropicSecretCleaner interface.
func (s *AnthropicCredentialService) DeleteAnthropicSecret(ctx context.Context, ocOrgID string) error {
	if s.wpClient == nil {
		return nil
	}
	ns := tenant.WorkflowPlaneNamespace(ocOrgID)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tenant.AnthropicSecretName,
			Namespace: ns,
		},
	}
	if err := s.wpClient.Delete(ctx, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete anthropic secret %s/%s: %w", ns, secret.Name, err)
	}
	slog.InfoContext(ctx, "anthropic.deleted-wp-secret",
		"ocOrgId", ocOrgID, "namespace", ns, "secret", secret.Name)
	return nil
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

// ResyncSecretRef re-pushes the org's Anthropic key through the in-process
// SecretRefWriter (local OpenBao repair). Returns (false, nil) when there is
// nothing to push. ctx must carry an ouId claim (repair injects thunder_org_uuid).
func (s *AnthropicCredentialService) ResyncSecretRef(ctx context.Context, ocOrgID string) (bool, error) {
	if s.secretRefWriter == nil || !s.secretRefWriter.Enabled() {
		return false, nil
	}
	row, err := s.fetchRow(ctx, ocOrgID)
	if err != nil {
		var nf *NotFoundError
		if errors.As(err, &nf) {
			return false, nil
		}
		return false, fmt.Errorf("anthropic resync: load row: %w", err)
	}
	if row.Status != "active" {
		return false, nil
	}
	kvPath := row.ResolvedSecretRefKVPath()
	prop := row.ResolvedSecretRefProperty()
	if kvPath == nil || prop == nil || *kvPath == "" || *prop == "" {
		return false, nil
	}
	key, err := s.store.Get(ctx, ocOrgID, "anthropic/key")
	if err != nil || len(key) == 0 {
		return false, nil
	}
	if _, err := s.secretRefWriter.WriteAnthropic(ctx, ocOrgID, string(key)); err != nil {
		return false, fmt.Errorf("anthropic resync: write: %w", err)
	}
	return true, nil
}

func (s *AnthropicCredentialService) fetchRow(ctx context.Context, ocOrgID string) (*OrgAnthropicCredential, error) {
	row, err := s.repo.GetByOrg(ctx, ocOrgID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, &NotFoundError{What: fmt.Sprintf("org_anthropic_credentials.%s", ocOrgID)}
	}
	return row, nil
}

// validateAnthropicKey probes Anthropic's /v1/messages with a minimal
// payload. 401 → ValidationError{anthropic_key_invalid}. A 5xx is the
// upstream's fault → UpstreamError (mapped to 502 Bad Gateway), NOT a
// client-fault 400. Other unexpected non-5xx statuses (e.g. 429) stay a
// ValidationError.
//
// Anthropic's /v1/messages requires both `x-api-key` and `anthropic-version`
// headers; a malformed request returns 400 (which still proves the key is
// recognized). We send a single 1-token completion request that should
// either 200 OK or 401 Unauthorized.
func (s *AnthropicCredentialService) validateAnthropicKey(ctx context.Context, key string) error {
	body := []byte(`{
	  "model": "claude-haiku-4-5",
	  "max_tokens": 1,
	  "messages": [{"role":"user","content":"ping"}]
	}`)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.anthropicAPI+"/v1/messages", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
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
