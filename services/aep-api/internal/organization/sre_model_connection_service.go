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

// sre_model_connection_service.go — the org's SRE model connection: the
// OpenAI-compatible endpoint, over a Bearer key of its own, that the
// OpenChoreo SRE agent calls instead of the org's model connection (the
// /config `sreLlm` section).
//
// A save follows the org connection's rules (ADR-0038 §5): https only, a key
// of at least 12 characters, and a move to another origin only with the key
// for it. It is probed before it is written; the row and the key's bytes are
// written in one transaction under the card's per-org lock, the same lock the
// org connection's saves take, so the two serialize. After commit OnChange
// tells whoever pushes the SRE agent's configuration to look again.
//
// EffectiveSRE is the one reader of which connection the SRE agent runs on
// (ResolveEffectiveSRE): this connection when set, else the org's when it can
// run the agent, else none.
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

// sreSection is the /config section this service answers for.
const sreSection = "sreLlm"

// SreModelConnectionService — see file doc.
type SreModelConnectionService struct {
	conns    OrgSreModelConnectionRepository
	store    secrets.CredentialStore
	card     AgentsCardRepository
	org      ConnectionReader
	probers  modelProbers
	now      func() time.Time
	onChange func(org string)
}

// NewSreModelConnectionService wires the service over the SRE connection rows,
// the key's bytes, the card's unit of work (which writes both) and the org's
// model connection (the fallback EffectiveSRE reads). All must be non-nil.
// The probe calls public endpoints only (netguard).
func NewSreModelConnectionService(conns OrgSreModelConnectionRepository, store secrets.CredentialStore,
	card AgentsCardRepository, org ConnectionReader) *SreModelConnectionService {
	return &SreModelConnectionService{
		conns:   conns,
		store:   store,
		card:    card,
		org:     org,
		probers: newModelProbers(defaultModelProbeClient()),
		now:     time.Now,
	}
}

// WithProbeClient replaces the probe's HTTP client; chainable. Tests aim it at
// an httptest server the real guard would (rightly) refuse to reach. The
// probers still follow no redirects, whatever client they are given.
func (s *SreModelConnectionService) WithProbeClient(c *http.Client) *SreModelConnectionService {
	s.probers = newModelProbers(c)
	return s
}

// OnChange registers f to run after every committed save or clear, with the
// org it changed. One callback: a second call replaces the first.
func (s *SreModelConnectionService) OnChange(f func(org string)) {
	s.onChange = f
}

// --- writes -------------------------------------------------------------------

// sreDraft is the connection a save leaves, with the key the probe used and
// the stored row it was judged against.
type sreDraft struct {
	BaseURL, Host, Model string
	// Key is the key the connection authenticates with: the one sent, or the
	// stored one on the stored origin. keyWritten says it was sent.
	Key        string
	keyWritten bool
	basis      *OrgSreModelConnection
}

// Check validates w merged over the org's stored connection and probes the
// result, writing nothing: the /config PATCH's probe phase, so a refusal here
// leaves every other section of the patch unwritten too. The returned draft
// is already probed; hand it to Persist to write it without probing again.
func (s *SreModelConnectionService) Check(ctx context.Context, org string, w orgconfig.SreLlmWrite) (sreDraft, error) {
	return s.probed(ctx, org, w)
}

// Set saves w merged over the org's stored connection, field by field: it
// validates and probes the result, then writes the row and, when w carries
// one, the key, in one transaction under the card's lock. A refusal is a
// SectionError on sreLlm and writes nothing. OnChange runs after the commit.
func (s *SreModelConnectionService) Set(ctx context.Context, org, actor string, w orgconfig.SreLlmWrite) error {
	d, err := s.Check(ctx, org, w)
	if err != nil {
		return err
	}
	return s.Persist(ctx, org, actor, d)
}

// Persist writes a draft Check already validated and probed, without probing
// it again: the /config PATCH's persist phase, once every section's probe
// phase has passed. The row it was judged against is re-read inside the
// transaction, under the card's lock, so a connection that moved between
// Check and Persist is still a conflict (sameRow) rather than a write of a
// connection nothing probed. OnChange runs after the commit.
func (s *SreModelConnectionService) Persist(ctx context.Context, org, actor string, d sreDraft) error {
	now := s.now().UTC()
	err := s.card.Tx(ctx, func(tx AgentsCardTx) error {
		if err := lockCard(tx.AdvisoryLock, org); err != nil {
			return err
		}
		stored, err := tx.GetSreModelConnection(org)
		if err != nil {
			return fmt.Errorf("sre model connection: read: %w", err)
		}
		if !sameRow(stored, d.basis) {
			return sectionErrorFrom(sreSection, &ConflictError{
				Reason: "the SRE model connection changed while this save was being tested; save again"})
		}
		row := &OrgSreModelConnection{
			OcOrgID: org, BaseURL: d.BaseURL, Host: d.Host, Model: d.Model,
			ConnectedAt: now, UpdatedAt: now, UpdatedBy: actor,
		}
		if stored != nil && stored.Host == d.Host {
			row.ConnectedAt = stored.ConnectedAt
		}
		if d.keyWritten {
			if err := tx.Secrets().Put(ctx, org, sreModelKeyStoreKey, []byte(d.Key)); err != nil {
				return fmt.Errorf("sre model connection: store put: %w", err)
			}
		}
		if err := tx.UpsertSreModelConnection(row); err != nil {
			return fmt.Errorf("sre model connection: upsert: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "sre_model.set", "org", org, "actor", actor, "host", d.Host)
	s.changed(org)
	return nil
}

// Clear removes the org's SRE model connection, row and key, so the SRE agent
// falls back to the org's connection. Idempotent. OnChange runs after the
// commit.
func (s *SreModelConnectionService) Clear(ctx context.Context, org, actor string) error {
	err := s.card.Tx(ctx, func(tx AgentsCardTx) error {
		if err := lockCard(tx.AdvisoryLock, org); err != nil {
			return err
		}
		if err := tx.DeleteSreModelConnection(org); err != nil {
			return fmt.Errorf("sre model connection: delete row: %w", err)
		}
		if err := tx.Secrets().Delete(ctx, org, sreModelKeyStoreKey); err != nil {
			return fmt.Errorf("sre model connection: store delete: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "sre_model.cleared", "org", org, "actor", actor)
	s.changed(org)
	return nil
}

func (s *SreModelConnectionService) changed(org string) {
	if s.onChange != nil {
		s.onChange(org)
	}
}

// probed drafts w over the stored connection and probes it. Every refusal is
// a SectionError on sreLlm.
func (s *SreModelConnectionService) probed(ctx context.Context, org string, w orgconfig.SreLlmWrite) (sreDraft, error) {
	stored, err := s.conns.GetByOrg(ctx, org)
	if err != nil {
		return sreDraft{}, fmt.Errorf("sre model connection: read: %w", err)
	}
	d, err := s.draft(ctx, org, stored, w)
	if err != nil {
		return sreDraft{}, sectionErrorFrom(sreSection, err)
	}
	if _, err := s.probers.probe(ctx, probeTarget{
		Org: org, Format: modelconn.FormatOpenAICompatible, BaseURL: d.BaseURL, Host: d.Host, Model: d.Model, Key: d.Key,
	}); err != nil {
		return sreDraft{}, sectionErrorFrom(sreSection, err)
	}
	return d, nil
}

// draft merges w over stored (nil = none yet), refusing what needs no network:
// a first save without every field, a URL that is not https, a key under 12
// characters, and a move to another origin without a key (ADR-0038 §5).
func (s *SreModelConnectionService) draft(ctx context.Context, org string, stored *OrgSreModelConnection, w orgconfig.SreLlmWrite) (sreDraft, error) {
	rawURL, key, model := trimmed(w.BaseURL), trimmed(w.APIKey), trimmed(w.Model)
	if stored == nil {
		if rawURL == "" || key == "" || model == "" {
			return sreDraft{}, errFieldRequired("a first SRE model connection needs a baseURL, an apiKey and a model")
		}
	} else {
		if rawURL == "" {
			rawURL = stored.BaseURL
		}
		if model == "" {
			model = stored.Model
		}
	}
	baseURL, host, err := normalizeBaseURL(modelconn.FormatOpenAICompatible, rawURL)
	if err != nil {
		return sreDraft{}, err
	}
	d := sreDraft{BaseURL: baseURL, Host: host, Model: model, Key: key, keyWritten: key != "", basis: stored}
	if d.keyWritten {
		if err := checkKeyShape(host, key); err != nil {
			return sreDraft{}, err
		}
		return d, nil
	}
	if err := requireKeyForNewOrigin(stored.BaseURL, baseURL); err != nil {
		return sreDraft{}, err
	}
	if d.Key, err = s.storedKey(ctx, org); err != nil {
		return sreDraft{}, err
	}
	if d.Key == "" {
		return sreDraft{}, errFieldRequired("the stored SRE model key could not be read; send the apiKey again")
	}
	return d, nil
}

// sameRow reports whether the row a save was judged against is still the one
// stored: same host, last saved at the same moment.
func sameRow(stored, basis *OrgSreModelConnection) bool {
	if stored == nil || basis == nil {
		return stored == nil && basis == nil
	}
	return stored.Host == basis.Host && stored.UpdatedAt.Equal(basis.UpdatedAt)
}

func trimmed(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

// --- reads --------------------------------------------------------------------

// storedKey reads the SRE model key's bytes; "" when there are none.
func (s *SreModelConnectionService) storedKey(ctx context.Context, org string) (string, error) {
	key, err := s.store.Get(ctx, org, sreModelKeyStoreKey)
	if errors.Is(err, secrets.ErrSecretNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("sre model connection: read key: %w", err)
	}
	return string(key), nil
}

// EffectiveSRE is the connection the SRE agent runs on for org, and its key
// (ResolveEffectiveSRE). A stored SRE row whose key bytes are missing is not
// usable, so the org's connection is considered instead.
func (s *SreModelConnectionService) EffectiveSRE(ctx context.Context, org string) (EffectiveSRE, error) {
	override, err := s.conns.GetByOrg(ctx, org)
	if err != nil {
		return EffectiveSRE{}, fmt.Errorf("sre model connection: read: %w", err)
	}
	var overrideKey string
	if override != nil {
		if overrideKey, err = s.storedKey(ctx, org); err != nil {
			return EffectiveSRE{}, err
		}
		if overrideKey == "" {
			slog.WarnContext(ctx, "sre model connection: row exists but its key bytes are missing", "org", org)
		}
	}
	conn, orgKey, ok, err := s.org.Effective(ctx, org)
	if err != nil {
		return EffectiveSRE{}, fmt.Errorf("sre model connection: read the org connection: %w", err)
	}
	var orgConn *modelconn.Connection
	if ok {
		orgConn = &conn
	}
	return ResolveEffectiveSRE(override, overrideKey, orgConn, orgKey), nil
}

// Projection is the org's SRE model connection as GET /config shows it, nil
// when it has none. The key is previewed, never returned.
func (s *SreModelConnectionService) Projection(ctx context.Context, org string) (*orgconfig.SreLlmProjection, error) {
	row, err := s.conns.GetByOrg(ctx, org)
	if err != nil || row == nil {
		return nil, err
	}
	key, err := s.storedKey(ctx, org)
	if err != nil {
		return nil, err
	}
	preview := ""
	if key != "" {
		preview = keyPreview(key)
	}
	return &orgconfig.SreLlmProjection{
		BaseURL:     row.BaseURL,
		Host:        row.Host,
		Model:       row.Model,
		KeyPreview:  preview,
		ConnectedAt: row.ConnectedAt,
		UpdatedAt:   row.UpdatedAt,
		UpdatedBy:   row.UpdatedBy,
	}, nil
}
