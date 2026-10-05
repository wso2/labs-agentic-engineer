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

// agent_settings_service.go — the AI agents card: how an organization's agents
// run, and the one unit of work that saves it.
//
// The card is two /config sections: `llm` (the org's model connection: format,
// URL, key and model, one for every agent) and `agents` (the coding agent's
// runtime and an optional Claude subscription it bills instead of the
// connection's key). What a save does is decided by agents_rule.go; the
// connection is probed with the key the save carries (model_probe.go) before
// the card's per-org lock is taken, so no save holds the lock across a probe.
//
// Under the lock a save writes each key it carries to vault first, as a new
// reference (the org secrets default-key and coding-agent-key), from the
// request: no key is ever stored in or read from Postgres. Inside the last
// write, the rows (connection, subscription, setting) commit as one
// transaction (repository_agents_card.go). A failed vault write saves
// nothing; a failed transaction undoes the new references. After the commit,
// still under the lock, the key's path consumers move to the new reference,
// a deleted credential's reference goes, Agent Manager's provider gets the
// request's key, the AE Studio pod converges when the save changed what it
// reads, and the replaced references are retired.
//
// The runtime is read by coding dispatch, which copies it (with the
// connection) onto the run it launches, so a run in flight keeps what it
// started with.

package organization

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// AgentSettingsService owns the AI agents card. See the file doc.
type AgentSettingsService struct {
	settings OrgAgentSettingsRepository
	orgs     OrganizationRepository
	creds    *AnthropicCredentialService
	conns    *ModelConnectionService
	card     AgentsCardRepository
	runtimes []orgconfig.AgentRuntime
	now      func() time.Time
	// converger rolls the org's AE Studio after a save changes what its pod
	// reads (the Default key, the connection's non-secret fields). nil:
	// nothing to roll.
	converger StudioConverger
}

// NewAgentSettingsService wires the service. creds validates, writes and
// projects the Claude subscription; conns probes, writes and projects the
// connection; card is the lock and the unit of work the saves run in; runtimes are the runtimes this
// installation can run (a runner image for each), the only ones a save may
// choose.
func NewAgentSettingsService(
	settings OrgAgentSettingsRepository,
	orgs OrganizationRepository,
	creds *AnthropicCredentialService,
	conns *ModelConnectionService,
	card AgentsCardRepository,
	runtimes []orgconfig.AgentRuntime,
) *AgentSettingsService {
	return &AgentSettingsService{settings: settings, orgs: orgs, creds: creds, conns: conns, card: card, runtimes: runtimes, now: time.Now}
}

// WithStudioConverger attaches the AE Studio converger a save triggers when
// it changes what the pod reads; chainable. nil leaves the pod alone.
func (s *AgentSettingsService) WithStudioConverger(c StudioConverger) *AgentSettingsService {
	s.converger = c
	return s
}

// Effective returns how the org's agents run: its chosen runtime (or the
// platform default when nobody chose), the runtimes this installation can run,
// and its Claude subscription, masked, when it has one. Never an error for
// "not set": the default IS the answer. A chosen runtime the installation can
// no longer run is returned as chosen, never substituted: dispatch fails naming
// the missing image, and the projection is what lets a client say why.
func (s *AgentSettingsService) Effective(ctx context.Context, ocOrgID string) (orgconfig.AgentsProjection, error) {
	out := orgconfig.DefaultAgents()
	out.AvailableRuntimes = append([]orgconfig.AgentRuntime{}, s.runtimes...) // [] on the wire, never null
	row, err := s.settings.GetByOrg(ctx, ocOrgID)
	if err != nil {
		return orgconfig.AgentsProjection{}, fmt.Errorf("agent settings: %w", err)
	}
	if row != nil {
		updatedAt, updatedBy := row.UpdatedAt, row.UpdatedBy
		out.Runtime = row.Runtime
		out.UpdatedAt, out.UpdatedBy = &updatedAt, &updatedBy
	}
	sub, err := s.creds.Status(ctx, ocOrgID, AnthropicRoleCoding)
	switch {
	case err == nil:
		out.Subscription = subscriptionProjectionFrom(sub)
	case !isNotFound(err):
		return orgconfig.AgentsProjection{}, fmt.Errorf("agent settings: subscription: %w", err)
	}
	return out, nil
}

// Connection is the org's model connection as GET /config shows it, nil when
// it has none.
func (s *AgentSettingsService) Connection(ctx context.Context, ocOrgID string) (*orgconfig.LLMProjection, error) {
	return s.conns.Projection(ctx, ocOrgID)
}

// Formats are the formats a connection may speak on this installation.
func (s *AgentSettingsService) Formats() []orgconfig.LLMFormatOption {
	return llmFormatsFor(s.runtimes)
}

// llmFormatsFor builds GET /config's llmFormats from modelconn.Formats, each
// with the runtimes among available that run it.
func llmFormatsFor(available []orgconfig.AgentRuntime) []orgconfig.LLMFormatOption {
	out := make([]orgconfig.LLMFormatOption, 0, len(modelconn.Formats))
	for _, f := range modelconn.Formats {
		opt := orgconfig.LLMFormatOption{Kind: f.Format, DefaultModel: f.DefaultModel, Runtimes: runtimesFor(f.Format, available)}
		if f.DefaultBaseURL != "" {
			url := f.DefaultBaseURL
			opt.DefaultBaseURL = &url
		}
		out = append(out, opt)
	}
	return out
}

// KeyDisconnectedAt is when the org's connection was last disconnected, or nil
// while one is connected or none ever was.
func (s *AgentSettingsService) KeyDisconnectedAt(ctx context.Context, ocOrgID string) (*time.Time, error) {
	org, err := s.orgs.GetByName(ctx, ocOrgID)
	if err != nil {
		return nil, fmt.Errorf("agent settings: organization: %w", err)
	}
	if org == nil {
		return nil, nil
	}
	return org.LLMDisconnectedAt, nil
}

// cardProbe is what the probe phase learned, handed to apply: the connection
// draft it probed and the probe's result, and the stored connection it judged
// against (nil when none), so apply can tell a connection changed underneath.
type cardProbe struct {
	draft  *connectionDraft
	result ProbeResult
	basis  *OrgModelConnection
	check  *orgconfig.LLMCheck
}

// probe validates the card's part of p WITHOUT writing anything: the patch is
// judged against the org's current state (so a refusal costs no live probe),
// then the connection is probed against its endpoint and a new subscription
// token against Anthropic. A failure is a SectionError naming the section to
// fix; nothing is written by any section.
func (s *AgentSettingsService) probe(ctx context.Context, ocOrgID string, p orgconfig.ConfigPatch) (cardProbe, error) {
	state, err := s.currentState(ctx, ocOrgID)
	if err != nil {
		return cardProbe{}, err
	}
	eff, err := judgeCard(state, s.runtimes, p)
	if err != nil {
		return cardProbe{}, err
	}
	// A key lives only in vault: with no secret store there is nowhere to
	// keep one, so the save is refused before anything is probed or written.
	if eff.writeConn != nil && eff.writeConn.Key != "" && !s.conns.canWriteKey() {
		return cardProbe{}, sectionErrorFrom("llm", ErrSecretsDeliveryUnavailable)
	}
	if eff.writeToken != "" && !s.creds.canWriteKey() {
		return cardProbe{}, sectionErrorFrom("agents", ErrSecretsDeliveryUnavailable)
	}
	out := cardProbe{basis: state.conn}
	if d := eff.writeConn; d != nil {
		if d.Key == "" {
			// A model-only edit (draftConnection refuses any other edit
			// without a key): there is no key to probe with, so it saves
			// unprobed, keeping what the host decides.
			out.draft, out.result = d, unprobed(state.conn, *d)
		} else {
			res, err := s.conns.probe(ctx, ocOrgID, *d)
			if err != nil {
				return cardProbe{}, sectionErrorFrom("llm", err)
			}
			check := s.conns.check(*d, res)
			out.draft, out.result, out.check = d, res, &check
		}
	}
	if eff.writeToken != "" {
		if err := s.creds.ValidateKey(ctx, eff.writeToken); err != nil {
			return cardProbe{}, sectionErrorFrom("agents", err)
		}
	}
	return out, nil
}

// testConnection probes the connection w describes, merged over the saved one,
// without writing anything: POST /config/llm/test. The same merge and refusals
// as a save's llm section, and it always needs the key in the body: a stored
// key is never read back to probe with.
func (s *AgentSettingsService) testConnection(ctx context.Context, ocOrgID string, w orgconfig.LLMPatch) (orgconfig.LLMCheck, error) {
	stored, err := s.conns.stored(ctx, ocOrgID)
	if err != nil {
		return orgconfig.LLMCheck{}, fmt.Errorf("model connection test: %w", err)
	}
	draft, _, err := draftConnection(stored, w)
	if err != nil {
		return orgconfig.LLMCheck{}, sectionErrorFrom("llm", err)
	}
	if draft.Key == "" {
		return orgconfig.LLMCheck{}, sectionErrorFrom("llm", errKeyRequired("Test connection needs the apiKey; a stored key is never read back"))
	}
	slog.InfoContext(ctx, "model connection test", "org", ocOrgID, "host", draft.Host, "format", draft.Format)
	if len(runtimesFor(draft.Format, s.runtimes)) == 0 {
		return orgconfig.LLMCheck{}, sectionErrorFrom("llm", errFormatHasNoRuntime(draft.Format, s.runtimes))
	}
	res, err := s.conns.probe(ctx, ocOrgID, draft)
	if err != nil {
		return orgconfig.LLMCheck{}, sectionErrorFrom("llm", err)
	}
	return s.conns.check(draft, res), nil
}

// apply saves the card's part of p under the org's card lock (see the file
// doc). The patch is judged again under the lock, against the rows it is about
// to write over, so a concurrent save cannot slip a state between probe and
// write that the rule would refuse; a connection that changed since it was
// probed is a conflict, never a write of an unprobed connection.
//
// The keys the patch carries are written to vault first, each as a new
// reference, connection key outside, subscription token inside (lock order:
// card, then default-key, then coding-agent-key); the row transaction runs
// inside the last write. Whatever fails before the commit returns with nothing
// saved and the new references undone. After the commit the copies follow
// (afterCommit); only the Agent Manager push can still fail the request, as
// *AgentManagerNotUpdatedError (502, the save standing).
func (s *AgentSettingsService) apply(ctx context.Context, ocOrgID, actor string, p orgconfig.ConfigPatch, probed cardProbe) error {
	unlock, err := s.card.Lock(ctx, ocOrgID)
	if err != nil {
		return err
	}
	defer unlock()
	state, err := s.currentState(ctx, ocOrgID)
	if err != nil {
		return err
	}
	eff, err := judgeCard(state, s.runtimes, p)
	if err != nil {
		return err
	}
	if eff.writeConn != nil && !probed.covers(*eff.writeConn, state.conn) {
		return sectionErrorFrom("llm", &ConflictError{Reason: "the model connection changed while this save was being tested; save again"})
	}
	var saved cardCopies
	commit := func() (err error) {
		saved, err = s.commit(ctx, ocOrgID, actor, state, eff, probed)
		return err
	}
	var tokenWrite *OrgSecretWrite
	withToken := commit
	if eff.writeToken != "" {
		withToken = func() error {
			w, err := s.creds.writeKey(ctx, ocOrgID, AnthropicRoleCoding, eff.writeToken, commit)
			tokenWrite = &w
			return err
		}
	}
	var key *keyWrite
	if eff.writeConn != nil && eff.writeConn.Key != "" {
		w, ref, err := s.conns.writeKey(ctx, ocOrgID, eff.writeConn.Key, withToken)
		if err != nil {
			return err
		}
		key = &keyWrite{value: eff.writeConn.Key, write: w, ref: ref}
	} else if err := withToken(); err != nil {
		return err
	}
	saved.key, saved.tokenWrite = key, tokenWrite
	return s.afterCommit(ctx, ocOrgID, saved)
}

// keyWrite is the connection key a save wrote to vault: the request's value
// (the only key Agent Manager's provider is ever given), the write whose
// previous reference is retired after commit, and the new reference with its
// vault path, which the key's path consumers move onto.
type keyWrite struct {
	value string
	write OrgSecretWrite
	ref   SecretRefTriplet
}

// commit writes the card's rows as one transaction: deletes first (the token
// goes before the connection it sits beside), then the connection, the
// subscription row and the setting. It returns what the copies after commit
// follow.
func (s *AgentSettingsService) commit(ctx context.Context, ocOrgID, actor string, state cardState, eff cardEffects, probed cardProbe) (cardCopies, error) {
	var out cardCopies
	err := s.card.Tx(ctx, func(tx AgentsCardTx) error {
		now := s.now().UTC()
		if eff.deleteToken {
			existed, err := s.creds.deleteKeyTx(tx, ocOrgID, AnthropicRoleCoding)
			if err != nil {
				return err
			}
			out.forgotToken = existed
		}
		var written *OrgModelConnection
		switch {
		case eff.deleteConn:
			if err := s.conns.deleteTx(tx, ocOrgID); err != nil {
				return err
			}
			out.forgotKey = true
			if err := tx.SetKeyDisconnectedAt(ocOrgID, &now); err != nil {
				return fmt.Errorf("agents card: record disconnect: %w", err)
			}
		case eff.writeConn != nil:
			var err error
			if written, err = s.conns.writeTx(tx, ocOrgID, actor, *eff.writeConn, probed.result, state.conn, now); err != nil {
				return err
			}
			if err := tx.SetKeyDisconnectedAt(ocOrgID, nil); err != nil {
				return fmt.Errorf("agents card: clear disconnect: %w", err)
			}
		}
		out.before, out.after = connectionsAround(state, eff, written)
		if eff.writeToken != "" {
			if err := s.creds.writeKeyTx(tx, ocOrgID, AnthropicRoleCoding, eff.writeToken); err != nil {
				return err
			}
		}
		if eff.deleteSettings {
			if err := tx.DeleteSettings(ocOrgID); err != nil {
				return fmt.Errorf("agents card: reset setting: %w", err)
			}
		}
		if eff.settings != nil {
			row := *eff.settings
			row.OcOrgID, row.UpdatedBy, row.UpdatedAt = ocOrgID, actor, now
			if err := tx.UpsertSettings(&row); err != nil {
				return fmt.Errorf("agents card: write setting: %w", err)
			}
		}
		return nil
	})
	return out, err
}

// cardCopies is what a committed save changed that the card's copies outside
// Postgres follow: the key references, the Agent Manager provider and the AE
// Studio pod.
type cardCopies struct {
	forgotToken, forgotKey bool                  // a deleted subscription / connection
	before, after          *modelconn.Connection // the org's connection either side of the save
	key                    *keyWrite             // the connection key written; nil for none
	tokenWrite             *OrgSecretWrite       // the subscription token written; nil for none
}

// rollsStudio reports a save that changed what the AE Studio pod reads: the
// Default key (written, or gone with its connection) or a non-secret field of
// the connection it gets as AE_MODEL_CONNECTION. The subscription token
// reaches no pod: the next coding Job reads its row.
func (c cardCopies) rollsStudio() bool {
	return c.key != nil || c.forgotKey || connectionFieldsChanged(c.before, c.after)
}

// connectionFieldsChanged reports whether a save changed the connection's
// non-secret fields, appearing or disappearing included.
func connectionFieldsChanged(before, after *modelconn.Connection) bool {
	if before == nil || after == nil {
		return before != after
	}
	return before.Format != after.Format || before.BaseURL != after.BaseURL || before.Host != after.Host ||
		before.Model != after.Model || before.AuthScheme != after.AuthScheme || before.ImageInput != after.ImageInput ||
		!equalLimit(before.ContextWindow, after.ContextWindow) || !equalLimit(before.OutputLimit, after.OutputLimit)
}

// equalLimit compares two optional limits by value.
func equalLimit(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// afterCommit brings the copies outside Postgres in line with a committed
// save, still under the card's lock, so they land in save order: the key's
// path consumers move onto its new reference, a deleted credential's
// reference goes, Agent Manager's provider gets the request's key, the AE
// Studio pod converges when the save changed what it reads, and then the
// replaced references are retired. A replaced reference is retired only
// after the commit that moved its row off it, and the default-key one only
// once its consumers moved too.
//
// Nothing between the commit and the push can skip the push: a committed key
// save always reaches Agent Manager or answers 502. The one failure it
// returns is that push, as *AgentManagerNotUpdatedError: the save and every
// other copy stand, but the user must save the key again.
func (s *AgentSettingsService) afterCommit(ctx context.Context, ocOrgID string, c cardCopies) error {
	var retire []OrgSecretWrite
	if c.tokenWrite != nil {
		retire = append(retire, *c.tokenWrite)
	}
	key := ""
	if c.key != nil {
		key = c.key.value
		if s.conns.repointConsumers(ctx, ocOrgID, c.key.ref) {
			retire = append(retire, c.key.write)
		}
	}
	if c.forgotToken {
		s.creds.forgetKey(ctx, ocOrgID, AnthropicRoleCoding)
	}
	if c.forgotKey {
		s.conns.forgetKey(ctx, ocOrgID)
	}
	pushErr := s.creds.syncModelProvider(ctx, ocOrgID, c.before, c.after, key)
	if c.rollsStudio() && s.converger != nil {
		s.converger.Trigger(ctx, ocOrgID)
	}
	for _, w := range retire {
		w.Retire(ctx)
	}
	if pushErr != nil {
		return &AgentManagerNotUpdatedError{Err: pushErr}
	}
	return nil
}

// covers reports whether this probe vouches for writing draft over stored: it
// probed the same draft (key included), against the same stored connection
// (the same host, last saved at the same moment).
func (p cardProbe) covers(draft connectionDraft, stored *OrgModelConnection) bool {
	if p.draft == nil || *p.draft != draft {
		return false
	}
	if (p.basis == nil) != (stored == nil) {
		return false
	}
	return stored == nil || (p.basis.Host == stored.Host && p.basis.UpdatedAt.Equal(stored.UpdatedAt))
}

// currentState reads the card's state from the pool: for the probe phase,
// and again under the card's lock, where no other save can move it.
func (s *AgentSettingsService) currentState(ctx context.Context, ocOrgID string) (cardState, error) {
	return readCardState(
		func() (*OrgAgentSettings, error) { return s.settings.GetByOrg(ctx, ocOrgID) },
		func() (*OrgModelConnection, error) { return s.conns.stored(ctx, ocOrgID) },
		func() (bool, error) { return s.creds.Holds(ctx, ocOrgID, AnthropicRoleCoding) },
	)
}

// readCardState assembles a cardState from one source's reads.
func readCardState(settings func() (*OrgAgentSettings, error), conn func() (*OrgModelConnection, error), holdsToken func() (bool, error)) (cardState, error) {
	row, err := settings()
	if err != nil {
		return cardState{}, fmt.Errorf("agents card: setting: %w", err)
	}
	c, err := conn()
	if err != nil {
		return cardState{}, fmt.Errorf("agents card: connection: %w", err)
	}
	hasToken, err := holdsToken()
	if err != nil {
		return cardState{}, fmt.Errorf("agents card: subscription: %w", err)
	}
	return cardState{settings: row, conn: c, hasToken: hasToken}, nil
}

func subscriptionProjectionFrom(p *AnthropicProjection) *orgconfig.SubscriptionProjection {
	return &orgconfig.SubscriptionProjection{
		Kind:            orgconfig.SubscriptionKindClaude,
		Status:          p.Status,
		ConnectedAt:     p.ConnectedAt,
		LastValidatedAt: p.LastValidatedAt,
		ValidationError: p.ValidationError,
	}
}
