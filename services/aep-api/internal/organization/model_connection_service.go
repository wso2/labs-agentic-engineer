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

// model_connection_service.go — the org's model connection: the one reader
// every consumer outside this domain goes through, and the connection half of
// the AI agents card's save.
//
// ModelConnectionService reads the connection (which format, URL, model and
// auth scheme the org's agents use, modelconn.Connection), the key's bytes or
// where they live, and which credential a coding run mounts. It offers two
// ports:
//
//   - ConnectionReader — Effective (the connection and its key's bytes, for the
//     spec agents, task planning and Agent Manager) and KeyRef (the connection
//     and its key's vault reference, for a consumer that mounts it).
//   - CodingCredentialResolver — which credential a coding run on a runtime
//     mounts: the Claude subscription or the connection's key, stated once
//     here (ADR-0036).
//
// For the card (AgentSettingsService) it probes a draft connection
// (model_probe.go), writes or deletes the row and the key's bytes inside the
// card's transaction, mirrors the key to SM-API after commit (under the card's
// lock, from the row as it stands), and projects the connection for GET
// /config.
//
// The connection lives in org_model_connections, one row per org; the key's
// bytes in org_secrets under modelKeyStoreKey.
package organization

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// ConnectionReader is the connection for consumers that call the model
// themselves or hand its key to something that does.
type ConnectionReader interface {
	// Effective is the connection and its key's bytes; ok is false when the
	// org has no usable connection, which is "not connected yet", not an
	// error. There is no platform fallback: orgs bring their own key.
	Effective(ctx context.Context, ocOrgID string) (conn modelconn.Connection, key string, ok bool, err error)
	// KeyRef is the connection and where its key lives, for a consumer that
	// points a SecretReference at the vault path rather than forwarding the
	// value. A NotFoundError means no connection: "not connected yet".
	KeyRef(ctx context.Context, ocOrgID string) (modelconn.Connection, SecretRefTriplet, error)
}

// CodingCredentialResolver answers which credential a coding run on runtime
// mounts. It is the ONLY place that choice is written; every other reader
// gets the connection's key by construction.
type CodingCredentialResolver interface {
	ResolveCodingCredential(ctx context.Context, ocOrgID string, runtime orgconfig.AgentRuntime) (CodingCredential, error)
}

// CodingCredentialKind is which credential a coding run bills.
type CodingCredentialKind string

const (
	CodingCredentialConnectionKey CodingCredentialKind = "connection_key"
	// CodingCredentialClaudeSubscription: the org's Claude subscription token,
	// only ever on Claude Code.
	CodingCredentialClaudeSubscription CodingCredentialKind = "claude_subscription"
)

// CodingCredential is a coding run's resolved credential: the connection it
// runs on, the secret it mounts and which kind that secret is.
type CodingCredential struct {
	Conn modelconn.Connection
	Ref  SecretRefTriplet
	Kind CodingCredentialKind
}

// SecretRefTriplet is a resolved SM-API secret reference: the name plus the
// vault coordinates an ExternalSecret's remoteRef needs.
type SecretRefTriplet struct {
	Name     string
	KVPath   string
	Property string
}

// RateCard answers whether the platform prices usage on (host, model): the
// connection's `priced`. Satisfied by *modelcost.Stamper, the lookup the
// usage stamps are priced from, so the card and the Usage page cannot
// disagree.
type RateCard interface {
	Priced(host, model string) bool
}

// ModelConnectionService — see file doc.
type ModelConnectionService struct {
	conns   OrgModelConnectionRepository
	subs    OrgAnthropicRepository
	store   secrets.CredentialStore
	rates   RateCard
	probers modelProbers

	// secretRefWriter mirrors a saved key into SM-API. nil-safe.
	secretRefWriter *SecretRefWriter

	// onChange runs after a committed save or delete of the connection.
	onChange func(ocOrgID string)
}

var (
	_ ConnectionReader         = (*ModelConnectionService)(nil)
	_ CodingCredentialResolver = (*ModelConnectionService)(nil)
)

// NewModelConnectionService wires the service over the connection rows, the
// Claude subscription rows (subs), the key's bytes and the rate card. All
// must be non-nil. The probe calls public endpoints only (netguard).
func NewModelConnectionService(conns OrgModelConnectionRepository, subs OrgAnthropicRepository, store secrets.CredentialStore, rates RateCard) *ModelConnectionService {
	return &ModelConnectionService{
		conns:   conns,
		subs:    subs,
		store:   store,
		rates:   rates,
		probers: newModelProbers(defaultModelProbeClient()),
	}
}

// WithSecretRefWriter injects the SM-API writer; chainable. nil disables the
// mirror — org_secrets remains authoritative.
func (s *ModelConnectionService) WithSecretRefWriter(w *SecretRefWriter) *ModelConnectionService {
	s.secretRefWriter = w
	return s
}

// WithProbeClient replaces the probe's HTTP client; chainable. Tests aim it at
// an httptest server the real guard would (rightly) refuse to reach. The
// probers still follow no redirects, whatever client they are given.
func (s *ModelConnectionService) WithProbeClient(c *http.Client) *ModelConnectionService {
	s.probers = newModelProbers(c)
	return s
}

// OnChange registers f to run, with the org, after every committed save or
// delete of the connection: a change can decide whether the SRE agent runs
// on it (its SREAgent capability). One callback: a second call replaces the
// first.
func (s *ModelConnectionService) OnChange(f func(ocOrgID string)) {
	s.onChange = f
}

// changed runs the OnChange callback for a committed change of ocOrgID's
// connection.
func (s *ModelConnectionService) changed(ocOrgID string) {
	if s.onChange != nil {
		s.onChange(ocOrgID)
	}
}

// --- reads --------------------------------------------------------------------

// Effective returns the connection and its key when the org has a connection
// and its bytes are present; ok=false otherwise, which the turn surface maps
// to a pre-202 4xx.
//
// Deliberately the connection's key only: the spec agents are AI SDK calls,
// which cannot present the coding role's subscription token.
func (s *ModelConnectionService) Effective(ctx context.Context, ocOrgID string) (modelconn.Connection, string, bool, error) {
	row, err := s.conns.GetByOrg(ctx, ocOrgID)
	if err != nil {
		return modelconn.Connection{}, "", false, err
	}
	if row == nil {
		return modelconn.Connection{}, "", false, nil
	}
	key, ok := s.storedKey(ctx, ocOrgID)
	if !ok {
		return modelconn.Connection{}, "", false, nil
	}
	return row.Connection(), key, true, nil
}

// storedKey reads the connection key's bytes. A read error or missing bytes
// is "none", not an error: the row says connected, so it is logged loudly.
func (s *ModelConnectionService) storedKey(ctx context.Context, ocOrgID string) (string, bool) {
	key, err := s.store.Get(ctx, ocOrgID, modelKeyStoreKey)
	if err == nil && len(key) > 0 {
		return string(key), true
	}
	slog.WarnContext(ctx, "model connection: row exists but its key bytes are missing",
		"ocOrgId", ocOrgID, "error", err)
	return "", false
}

// KeyRef returns the connection and its key's vault coordinates. It never
// reads the key's bytes, only where they live, for a caller that points an
// OpenChoreo SecretReference at the path rather than forwarding the value
// itself (e.g. wiring an ai-agent component's MODEL_API_KEY).
func (s *ModelConnectionService) KeyRef(ctx context.Context, ocOrgID string) (modelconn.Connection, SecretRefTriplet, error) {
	row, err := s.connectionRow(ctx, ocOrgID)
	if err != nil {
		return modelconn.Connection{}, SecretRefTriplet{}, err
	}
	ref, err := tripletOf(row.SecretRefName, row.SecretRefKVPath, row.SecretRefProperty)
	if err != nil {
		return modelconn.Connection{}, SecretRefTriplet{}, err
	}
	return row.Connection(), ref, nil
}

// stored is the org's connection row, nil when it has none.
func (s *ModelConnectionService) stored(ctx context.Context, ocOrgID string) (*OrgModelConnection, error) {
	return s.conns.GetByOrg(ctx, ocOrgID)
}

// connectionRow loads the org's row, answering NotFoundError when it has none.
func (s *ModelConnectionService) connectionRow(ctx context.Context, ocOrgID string) (*OrgModelConnection, error) {
	row, err := s.conns.GetByOrg(ctx, ocOrgID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, &NotFoundError{What: fmt.Sprintf("org_model_connections.%s", ocOrgID)}
	}
	return row, nil
}

// ResolveCodingCredential returns the credential a coding run on runtime must
// mount: the org's Claude subscription when it has one and the runtime is
// Claude Code, the connection's key otherwise.
//
// Only Claude Code can present a subscription token, so on any other runtime
// the subscription is not consulted at all (the save rule keeps one from being
// stored alongside OpenCode; this keeps a stray row from ever reaching a run).
//
// Fails closed. A subscription that exists but has no usable triplet is an
// error, never a silent fall-through to the connection's key: the org chose to
// bill its plan, and quietly billing API credits instead defeats that choice
// while leaving no trace the org can see.
func (s *ModelConnectionService) ResolveCodingCredential(ctx context.Context, ocOrgID string, runtime orgconfig.AgentRuntime) (CodingCredential, error) {
	row, err := s.conns.GetByOrg(ctx, ocOrgID)
	if err != nil {
		return CodingCredential{}, fmt.Errorf("model connection for org %q: %w", ocOrgID, err)
	}
	if row == nil {
		return CodingCredential{}, fmt.Errorf("model connection missing for org %q: no connection is saved", ocOrgID)
	}
	if runtime == orgconfig.AgentRuntimeClaudeCode {
		ref, ok, err := s.subscriptionRef(ctx, ocOrgID)
		if err != nil {
			return CodingCredential{}, err
		}
		if ok {
			return CodingCredential{Conn: row.Connection(), Ref: ref, Kind: CodingCredentialClaudeSubscription}, nil
		}
	}
	ref, err := tripletOf(row.SecretRefName, row.SecretRefKVPath, row.SecretRefProperty)
	if err != nil {
		return CodingCredential{}, fmt.Errorf("model connection secret reference for org %q: %w", ocOrgID, err)
	}
	return CodingCredential{Conn: row.Connection(), Ref: ref, Kind: CodingCredentialConnectionKey}, nil
}

// subscriptionRef is the Claude subscription's reference, ok=false when the
// org has none; an unusable one is an error (see ResolveCodingCredential).
func (s *ModelConnectionService) subscriptionRef(ctx context.Context, ocOrgID string) (SecretRefTriplet, bool, error) {
	sub, err := s.subs.GetByOrg(ctx, ocOrgID, AnthropicRoleCoding)
	if err != nil {
		return SecretRefTriplet{}, false, fmt.Errorf("resolve coding credential: load subscription row: %w", err)
	}
	if sub == nil {
		return SecretRefTriplet{}, false, nil
	}
	if sub.Status != "active" {
		return SecretRefTriplet{}, false, fmt.Errorf(
			"the Claude subscription for org %q is %s — replace its token in Settings, "+
				"or remove the subscription so coding bills the connection's key", ocOrgID, sub.Status)
	}
	ref, err := tripletOf(sub.SecretRefName, sub.SecretRefKVPath, sub.SecretRefProperty)
	if err != nil {
		return SecretRefTriplet{}, false, fmt.Errorf(
			"the Claude subscription for org %q is configured but %w — save its token again in Settings, "+
				"or remove the subscription so coding bills the connection's key", ocOrgID, err)
	}
	return ref, true, nil
}

// tripletOf reads a row's resolved secret-ref coordinates, naming whichever
// one is missing so a half-mirrored row is diagnosable from the error alone.
func tripletOf(name, kvPath, property *string) (SecretRefTriplet, error) {
	ref := SecretRefTriplet{Name: derefOrEmpty(name), KVPath: derefOrEmpty(kvPath), Property: derefOrEmpty(property)}
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

// Projection is the org's connection as GET /config shows it, nil when it has
// none.
func (s *ModelConnectionService) Projection(ctx context.Context, ocOrgID string) (*orgconfig.LLMProjection, error) {
	row, err := s.conns.GetByOrg(ctx, ocOrgID)
	if err != nil || row == nil {
		return nil, err
	}
	conn := row.Connection()
	return &orgconfig.LLMProjection{
		Kind:         row.Format,
		BaseURL:      row.BaseURL,
		Model:        row.Model,
		KeyPreview:   row.KeyPreview,
		ConnectedAt:  row.ConnectedAt,
		UpdatedAt:    row.UpdatedAt,
		UpdatedBy:    row.UpdatedBy,
		Priced:       s.rates.Priced(row.Host, row.Model),
		Capabilities: orgconfig.LLMCapabilitiesFrom(modelconn.CapabilitiesOf(conn)),
	}, nil
}

// --- the card's connection half -------------------------------------------------

// probe checks d against its endpoint. A draft with no key reuses the stored
// key, which draftConnection only allows on the stored connection's origin.
func (s *ModelConnectionService) probe(ctx context.Context, ocOrgID string, d connectionDraft) (ProbeResult, error) {
	key := d.Key
	if key == "" {
		stored, ok := s.storedKey(ctx, ocOrgID)
		if !ok {
			return ProbeResult{}, &ValidationError{Code: "llm_field_required",
				Message: "the stored key could not be read; send the apiKey again"}
		}
		key = stored
	}
	return s.probers.probe(ctx, probeTarget{
		Org: ocOrgID, Format: d.Format, BaseURL: d.BaseURL, Host: d.Host, Model: d.Model, Key: key,
	})
}

// check is what a probe of d found, as the card reads it.
func (s *ModelConnectionService) check(d connectionDraft, res ProbeResult) orgconfig.LLMCheck {
	conn := d.connection()
	conn.ImageInput = res.ImageInput
	out := orgconfig.LLMCheck{
		Kind:         d.Format,
		BaseURL:      d.BaseURL,
		Model:        d.Model,
		ModelListed:  res.ModelListed,
		Priced:       s.rates.Priced(d.Host, d.Model),
		Capabilities: orgconfig.LLMCapabilitiesFrom(modelconn.CapabilitiesOf(conn)),
	}
	if res.ProviderLimited {
		out.Warning = orgconfig.LLMWarningProviderLimit
	}
	return out
}

// writeTx stores the probed connection inside the card's transaction: the row,
// and the key's bytes when the save sent one. stored is the row it replaces
// (nil on first connect); connected_at survives a save on the same host.
func (s *ModelConnectionService) writeTx(ctx context.Context, tx AgentsCardTx, ocOrgID, actor string,
	d connectionDraft, res ProbeResult, stored *OrgModelConnection, now time.Time) (*OrgModelConnection, error) {
	row := &OrgModelConnection{
		OcOrgID:       ocOrgID,
		Format:        d.Format,
		BaseURL:       d.BaseURL,
		Host:          d.Host,
		Model:         d.Model,
		AuthScheme:    res.AuthScheme,
		ContextWindow: res.ContextWindow,
		OutputLimit:   res.OutputLimit,
		ImageInput:    res.ImageInput,
		ConnectedAt:   now,
		UpdatedAt:     now,
		UpdatedBy:     &actor,
	}
	if stored != nil && stored.Host == d.Host {
		row.ConnectedAt = stored.ConnectedAt
	}
	keyWritten := d.Key != ""
	if keyWritten {
		if err := tx.Secrets().Put(ctx, ocOrgID, modelKeyStoreKey, []byte(d.Key)); err != nil {
			return nil, fmt.Errorf("model connection: store put: %w", err)
		}
		row.KeyPreview = keyPreview(d.Key)
	} else {
		row.KeyPreview = stored.KeyPreview
		row.SecretRefName, row.SecretRefKVPath, row.SecretRefProperty = stored.SecretRefName, stored.SecretRefKVPath, stored.SecretRefProperty
	}
	if err := tx.UpsertConnection(row, keyWritten); err != nil {
		return nil, fmt.Errorf("model connection: upsert: %w", err)
	}
	return row, nil
}

// deleteTx removes the connection — row and bytes — inside the card's
// transaction, returning the SM-API secret-ref name the row carried so its
// copy can be deleted once the transaction commits. The bytes go under both
// names: an org ModelKeyRename has not finished still holds `anthropic/key`,
// which would otherwise outlive the connection. Idempotent.
func (s *ModelConnectionService) deleteTx(ctx context.Context, tx AgentsCardTx, ocOrgID string) (string, error) {
	row, err := tx.GetConnection(ocOrgID)
	if err != nil || row == nil {
		return "", err
	}
	if err := tx.DeleteConnection(ocOrgID); err != nil {
		return "", fmt.Errorf("model connection: delete row: %w", err)
	}
	for _, key := range []string{modelKeyStoreKey, legacyModelKeyStoreKey} {
		if err := tx.Secrets().Delete(ctx, ocOrgID, key); err != nil {
			return "", fmt.Errorf("model connection: store delete %s: %w", key, err)
		}
	}
	return derefOrEmpty(row.SecretRefName), nil
}

// mirrorKey copies the connection key as it stands into SM-API and records
// where on the row, inside the transaction the card's copies run in (under its
// lock), best-effort: org_secrets stays authoritative. It reads the key rather
// than taking the one a save wrote, so a save's copy that runs after a later
// save's leaves the later key in the vault. A save that wrote a key cleared the
// row's triplet, so a failed mirror leaves it NULL until the next key save, and
// dispatch fails closed naming why. No row: a disconnect landed since, and
// there is nothing to copy.
func (s *ModelConnectionService) mirrorKey(ctx context.Context, tx AgentsCardTx, ocOrgID string) {
	if !s.secretRefWriter.Enabled() {
		return
	}
	if err := s.mirrorKeyTx(ctx, tx, ocOrgID); err != nil {
		slog.WarnContext(ctx, "model connection: SM-API mirror failed (org_secrets still authoritative)",
			"ocOrgId", ocOrgID, "error", err)
	}
}

func (s *ModelConnectionService) mirrorKeyTx(ctx context.Context, tx AgentsCardTx, ocOrgID string) error {
	row, err := tx.GetConnection(ocOrgID)
	if err != nil || row == nil {
		return err
	}
	key, err := tx.Secrets().Get(ctx, ocOrgID, modelKeyStoreKey)
	if err != nil {
		return fmt.Errorf("read the key: %w", err)
	}
	ref, err := s.secretRefWriter.UploadModelKey(ctx, ocOrgID, strings.TrimSpace(string(key)))
	if err != nil {
		return err
	}
	if err := tx.StampConnectionSecretRef(ocOrgID, ref); err != nil {
		return fmt.Errorf("stamp the secret reference: %w", err)
	}
	slog.InfoContext(ctx, "model connection: key mirrored to SM-API",
		"ocOrgId", ocOrgID, "secretRefName", ref.Name, "vaultKey", ref.KVPath)
	return nil
}

// forgetKey deletes a removed connection key's SM-API copy, best-effort, inside
// the transaction the card's copies run in. A connection saved since mirrors
// its key to the same path, so the copy is left to it; the Anthropic-era path
// no save writes goes either way.
func (s *ModelConnectionService) forgetKey(ctx context.Context, tx AgentsCardTx, ocOrgID, secretRefName string) {
	if secretRefName == "" || !s.secretRefWriter.Enabled() {
		return
	}
	row, err := tx.GetConnection(ocOrgID)
	if err == nil && row != nil && secretRefName == modelKeyRefName {
		return
	}
	if err == nil {
		err = s.secretRefWriter.DeleteModelKey(ctx, ocOrgID, secretRefName)
	}
	if err != nil {
		slog.WarnContext(ctx, "model connection: SM-API delete failed (orphaned copy until the next save)",
			"ocOrgId", ocOrgID, "error", err)
	}
}

// ResyncSecretRef re-pushes the connection key through the in-process
// SecretRefWriter (local OpenBao repair). (false, nil) when there is nothing
// to repair: no connection, no triplet yet, or no bytes. ctx must carry an
// ouId claim.
func (s *ModelConnectionService) ResyncSecretRef(ctx context.Context, ocOrgID string) (bool, error) {
	if !s.secretRefWriter.Enabled() {
		return false, nil
	}
	row, err := s.conns.GetByOrg(ctx, ocOrgID)
	if err != nil {
		return false, fmt.Errorf("model connection resync: load row: %w", err)
	}
	if row == nil || derefOrEmpty(row.SecretRefKVPath) == "" || derefOrEmpty(row.SecretRefProperty) == "" {
		return false, nil
	}
	key, err := s.store.Get(ctx, ocOrgID, modelKeyStoreKey)
	if err != nil || len(key) == 0 {
		return false, nil
	}
	if _, err := s.secretRefWriter.WriteModelKey(ctx, ocOrgID, string(key)); err != nil {
		return false, fmt.Errorf("model connection resync: write: %w", err)
	}
	return true, nil
}
