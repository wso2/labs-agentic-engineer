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
// auth scheme the org's agents use, modelconn.Connection), where its key
// lives, and which credential a coding run mounts. It never reads a key: the
// key lives only in vault, under the org's default-key reference, written by
// the card's save from the request (agent_settings_service.go). It offers two
// ports:
//
//   - ConnectionReader — Connection (the connection, no key), KeyRef (the
//     connection and its key's reference as a mount needs it: the default-key
//     row's name and key) and KeyPathRef (the same reference with its vault
//     path, read off the SecretReference, for a consumer that points its own
//     SecretReference at the key's vault entry).
//   - CodingCredentialResolver — which credential a coding run on a runtime
//     mounts: the Claude subscription or the connection's key, stated once
//     here (ADR-0036).
//
// For the card (AgentSettingsService) it probes a draft connection carrying
// its key (model_probe.go), writes or deletes the row inside the card's
// transaction, removes a deleted key's reference, and projects the connection
// for GET /config.
//
// The connection lives in org_model_connections, one row per org; the key's
// reference in org_secrets under default-key. The connection reads as
// configured only while that reference row exists.
package organization

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// ConnectionReader is the connection for consumers that mount its key or
// point at it; none of them reads the key itself.
type ConnectionReader interface {
	// Connection is the connection without its key; ok is false when the org
	// has none, which is "not connected yet", not an error.
	Connection(ctx context.Context, ocOrgID string) (conn modelconn.Connection, ok bool, err error)
	// KeyRef is the connection and its key's reference (name + key), for a
	// consumer that mounts the reference rather than forwarding the value. A
	// NotFoundError means no connection: "not connected yet".
	KeyRef(ctx context.Context, ocOrgID string) (modelconn.Connection, SecretRefTriplet, error)
	// KeyPathRef is the connection and its key's reference with its vault
	// path, for a consumer that points its own SecretReference at that path.
	// A NotFoundError means no connection.
	KeyPathRef(ctx context.Context, ocOrgID string) (modelconn.Connection, SecretRefTriplet, error)
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

// SecretRefTriplet is a resolved secret reference: its name and the key a
// consumer mounts, which is all a SecretKeyRef needs (C10), plus its vault
// path when known, for a consumer that points another SecretReference at the
// same vault entry. KVPath may be empty.
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

// SecretReferenceReader reads one SecretReference's spec (names and vault
// paths, never a value). Satisfied by the OpenChoreo SecretReference client.
type SecretReferenceReader interface {
	GetSecretReference(ctx context.Context, cpNS, name string) (*secretmanagersvc.SecretReference, error)
}

// ModelConnectionService — see file doc.
type ModelConnectionService struct {
	conns   OrgModelConnectionRepository
	subs    OrgAnthropicRepository
	rates   RateCard
	probers modelProbers

	// orgSecrets reads the default-key and coding-agent-key rows, the
	// references the readers below hand out (R7).
	orgSecrets OrgSecretRefReader

	// secretRefs reads the default-key SecretReference's vault path
	// (KeyPathRef). nil: KeyPathRef fails as not configured.
	secretRefs SecretReferenceReader

	// secretRefWriter writes and removes the key's default-key reference.
	// nil, or not enabled: there is no secret store, and no key can be saved.
	secretRefWriter *SecretRefWriter
}

var (
	_ ConnectionReader         = (*ModelConnectionService)(nil)
	_ CodingCredentialResolver = (*ModelConnectionService)(nil)
)

// NewModelConnectionService wires the service over the connection rows, the
// Claude subscription rows (subs), the org secret reference rows (refs) and
// the rate card. All must be non-nil: there is no reading a key's reference
// without its row. The probe calls public endpoints only (netguard).
func NewModelConnectionService(conns OrgModelConnectionRepository, subs OrgAnthropicRepository, refs OrgSecretRefReader, rates RateCard) *ModelConnectionService {
	if conns == nil || subs == nil || refs == nil || rates == nil {
		panic("organization: NewModelConnectionService needs the connection, subscription and org secret repositories and a rate card")
	}
	return &ModelConnectionService{
		conns:      conns,
		subs:       subs,
		orgSecrets: refs,
		rates:      rates,
		probers:    newModelProbers(defaultModelProbeClient()),
	}
}

// WithSecretRefWriter injects the writer of the key's default-key reference;
// chainable. nil leaves the installation without a secret store: a key save
// is refused.
func (s *ModelConnectionService) WithSecretRefWriter(w *SecretRefWriter) *ModelConnectionService {
	s.secretRefWriter = w
	return s
}

// WithSecretReferences attaches the SecretReference reader KeyPathRef reads
// the key's vault path from; chainable.
func (s *ModelConnectionService) WithSecretReferences(r SecretReferenceReader) *ModelConnectionService {
	s.secretRefs = r
	return s
}

// WithProbeClient replaces the probe's HTTP client; chainable. Tests aim it at
// an httptest server the real guard would (rightly) refuse to reach. The
// probers still follow no redirects, whatever client they are given.
func (s *ModelConnectionService) WithProbeClient(c *http.Client) *ModelConnectionService {
	s.probers = newModelProbers(c)
	return s
}

// --- reads --------------------------------------------------------------------

// Connection returns the org's connection without its key; ok=false when
// the org has none.
func (s *ModelConnectionService) Connection(ctx context.Context, ocOrgID string) (modelconn.Connection, bool, error) {
	row, err := s.stored(ctx, ocOrgID)
	if err != nil || row == nil {
		return modelconn.Connection{}, false, err
	}
	return row.Connection(), true, nil
}

// KeyRef returns the connection and its key's reference as a mount needs it:
// the default-key row's name and key, no vault path (R7). It never reads the
// key, for a caller that mounts the reference (e.g. the build's evaluation
// key). No default-key row is an error: the key was never saved to vault.
func (s *ModelConnectionService) KeyRef(ctx context.Context, ocOrgID string) (modelconn.Connection, SecretRefTriplet, error) {
	row, err := s.connectionRow(ctx, ocOrgID)
	if err != nil {
		return modelconn.Connection{}, SecretRefTriplet{}, err
	}
	ref, err := s.recordedRef(ctx, ocOrgID, OrgSecretDefaultKey)
	if err != nil {
		return modelconn.Connection{}, SecretRefTriplet{}, err
	}
	return row.Connection(), ref, nil
}

// KeyPathRef returns the connection and its key's reference whole (name,
// vault path and key), for a caller that points its own SecretReference at
// the key's vault entry (the ai-agent model access). The name is the
// default-key row's; the path is the one that SecretReference reads its
// api-key from, so it is the path the write chose, read back by name, never
// derived. A missing row or SecretReference fails closed.
func (s *ModelConnectionService) KeyPathRef(ctx context.Context, ocOrgID string) (modelconn.Connection, SecretRefTriplet, error) {
	row, err := s.connectionRow(ctx, ocOrgID)
	if err != nil {
		return modelconn.Connection{}, SecretRefTriplet{}, err
	}
	ref, err := s.recordedRef(ctx, ocOrgID, OrgSecretDefaultKey)
	if err != nil {
		return modelconn.Connection{}, SecretRefTriplet{}, err
	}
	if s.secretRefs == nil {
		return modelconn.Connection{}, SecretRefTriplet{}, errors.New("the default-key vault path cannot be read: no SecretReference reader is configured")
	}
	sr, err := s.secretRefs.GetSecretReference(ctx, ocOrgID, ref.Name)
	if err != nil {
		return modelconn.Connection{}, SecretRefTriplet{}, fmt.Errorf("read the default-key reference %s: %w", ref.Name, err)
	}
	for _, d := range sr.Data {
		if d.SecretKey == ref.Property && d.RemoteKey != "" {
			ref.KVPath = d.RemoteKey
			if d.Property != "" {
				ref.Property = d.Property
			}
			return row.Connection(), ref, nil
		}
	}
	return modelconn.Connection{}, SecretRefTriplet{}, fmt.Errorf("the default-key reference %s has no %s vault entry", ref.Name, ref.Property)
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
// Fails closed. A subscription that exists but has no usable reference is an
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
	ref, err := s.recordedRef(ctx, ocOrgID, OrgSecretDefaultKey)
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
	ref, err := s.recordedRef(ctx, ocOrgID, OrgSecretCodingAgentKey)
	if err != nil {
		return SecretRefTriplet{}, false, fmt.Errorf(
			"the Claude subscription for org %q is configured but %w — save its token again in Settings, "+
				"or remove the subscription so coding bills the connection's key", ocOrgID, err)
	}
	return ref, true, nil
}

// recordedRef is the reference of the org secret sec that a mount reads: the
// name its org_secrets row records with sec's fixed key (R7), so a rotation
// never hands out the reference the write already deleted. A mount needs only
// the name and the key (C10), so no vault path is carried; a consumer of the
// path reads KeyPathRef. No row is an error: the secret was never written to
// vault.
func (s *ModelConnectionService) recordedRef(ctx context.Context, ocOrgID string, sec OrgSecret) (SecretRefTriplet, error) {
	row, err := s.orgSecrets.Get(ctx, ocOrgID, sec)
	if err != nil {
		return SecretRefTriplet{}, fmt.Errorf("read the %s row: %w", sec, err)
	}
	if row == nil || row.Name == "" {
		return SecretRefTriplet{}, fmt.Errorf("the %s reference is not recorded", sec)
	}
	return SecretRefTriplet{Name: row.Name, Property: sec.ValueKey()}, nil
}

// keySet reports whether the org's default-key reference row exists: the
// record that the connection's key was written to vault.
func (s *ModelConnectionService) keySet(ctx context.Context, ocOrgID string) (bool, error) {
	row, err := s.orgSecrets.Get(ctx, ocOrgID, OrgSecretDefaultKey)
	return row != nil, err
}

// Projection is the org's connection as GET /config shows it, nil when it has
// none. A connection whose default-key reference row is missing (saved before
// the key lived in vault) has no usable key, so it reads as none: the
// onboarding wizard then asks for the key again.
func (s *ModelConnectionService) Projection(ctx context.Context, ocOrgID string) (*orgconfig.LLMProjection, error) {
	row, err := s.conns.GetByOrg(ctx, ocOrgID)
	if err != nil || row == nil {
		return nil, err
	}
	set, err := s.keySet(ctx, ocOrgID)
	if err != nil || !set {
		return nil, err
	}
	conn := row.Connection()
	return &orgconfig.LLMProjection{
		Kind:         row.Format,
		BaseURL:      row.BaseURL,
		Model:        row.Model,
		ConnectedAt:  row.ConnectedAt,
		UpdatedAt:    row.UpdatedAt,
		UpdatedBy:    row.UpdatedBy,
		Priced:       s.rates.Priced(row.Host, row.Model),
		Capabilities: orgconfig.LLMCapabilitiesFrom(modelconn.CapabilitiesOf(conn)),
	}, nil
}

// --- the card's connection half -------------------------------------------------

// probe checks d against its endpoint with the key d carries. A draft with
// no key is never probed: the stored key is never read back to probe with.
func (s *ModelConnectionService) probe(ctx context.Context, ocOrgID string, d connectionDraft) (ProbeResult, error) {
	if d.Key == "" {
		return ProbeResult{}, errKeyRequired("a probe needs the apiKey; a stored key is never read back")
	}
	return s.probers.probe(ctx, probeTarget{
		Org: ocOrgID, Format: d.Format, BaseURL: d.BaseURL, Host: d.Host, Model: d.Model, Key: d.Key,
	})
}

// unprobed is what a model-only edit stores in place of a probe's result:
// the stored auth scheme (the host and its key did not change) and the
// host's defaults for what the model decides (limits, image input).
func unprobed(stored *OrgModelConnection, d connectionDraft) ProbeResult {
	res := ProbeResult{AuthScheme: stored.AuthScheme, ModelListed: modelconn.Unknown}
	hostDefaults(d.Host, &res)
	return res
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

// writeTx stores the connection inside the card's transaction. stored is the
// row it replaces (nil on first connect); connected_at survives a save on the
// same host. The key is not here: the save wrote it to vault already.
func (s *ModelConnectionService) writeTx(tx AgentsCardTx, ocOrgID, actor string,
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
	if err := tx.UpsertConnection(row); err != nil {
		return nil, fmt.Errorf("model connection: upsert: %w", err)
	}
	return row, nil
}

// deleteTx removes the connection row inside the card's transaction.
// Idempotent.
func (s *ModelConnectionService) deleteTx(tx AgentsCardTx, ocOrgID string) error {
	if err := tx.DeleteConnection(ocOrgID); err != nil {
		return fmt.Errorf("model connection: delete row: %w", err)
	}
	return nil
}

// canWriteKey reports whether a key can be saved: only to vault, so only
// with a secret store.
func (s *ModelConnectionService) canWriteKey() bool {
	return s.secretRefWriter.Enabled()
}

// writeKey writes key as a new default-key reference (OrgSecretWriter.Write);
// commit runs while the write's lock is held, after the row names the new
// reference: when it fails the write is undone and the previous reference
// stays. ref is the new reference with its vault path.
func (s *ModelConnectionService) writeKey(ctx context.Context, ocOrgID, key string, commit func() error) (OrgSecretWrite, SecretRefTriplet, error) {
	var ref SecretRefTriplet
	written, err := s.secretRefWriter.WriteModelKey(ctx, ocOrgID, key, func(r SecretRefTriplet) error {
		ref = r
		return commit()
	})
	if err != nil {
		return OrgSecretWrite{}, SecretRefTriplet{}, err
	}
	slog.InfoContext(ctx, "model connection: key reference written", "ocOrgId", ocOrgID, "secretRefName", written.Name)
	return written, ref, nil
}

// repointConsumers moves the key's path consumers (ModelKeyConsumers) onto
// ref after the save committed, and reports whether the previous reference
// may be retired: not when the repoint failed, since a consumer may still
// read it.
func (s *ModelConnectionService) repointConsumers(ctx context.Context, ocOrgID string, ref SecretRefTriplet) bool {
	if err := s.secretRefWriter.repointModelKeyConsumers(ctx, ocOrgID, ref); err != nil {
		slog.WarnContext(ctx, "model connection: ai-agent model access repoint failed; the previous reference is kept",
			"ocOrgId", ocOrgID, "secretRefName", ref.Name, "error", err)
		return false
	}
	return true
}

// forgetKey removes a deleted connection's default-key reference after the
// save committed, best-effort: its row and then its reference, under the
// secret's lock. The caller holds the card's lock, so no connection was
// saved since.
func (s *ModelConnectionService) forgetKey(ctx context.Context, ocOrgID string) {
	if !s.secretRefWriter.Enabled() {
		return
	}
	if err := s.secretRefWriter.ForgetModelKey(ctx, ocOrgID); err != nil {
		slog.WarnContext(ctx, "model connection: reference removal failed (orphaned copy until the next save)",
			"ocOrgId", ocOrgID, "error", err)
	}
}
