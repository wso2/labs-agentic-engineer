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
// connection's key). One save of it is one transaction under the card's
// per-org lock, covering the connection row, the subscription row, the
// setting row and the encrypted secret bytes — see repository_agents_card.go.
// What a save does is decided by agents_rule.go; the connection is probed
// (model_probe.go) before the transaction opens, so no save holds the lock
// across a probe. The copies outside Postgres (the SM-API mirrors, the Agent
// Manager provider) follow the commit under the same lock, so they land in
// save order.
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

// cardLockPrefixes are the card's per-org advisory lock names, taken in this
// order by everything that writes the card's rows or their copies: a save, its
// copies after commit, and ModelKeyRename. `org_anthropic:` is the name the
// previous release takes; holding it too keeps a replica of that release,
// saving during a rolling deploy, serialized with this one.
var cardLockPrefixes = []string{"org_anthropic:", "org_model:"}

// lockCard takes the card's per-org locks, in cardLockPrefixes order.
func lockCard(lock func(key string) error, ocOrgID string) error {
	for _, prefix := range cardLockPrefixes {
		if err := lock(prefix + ocOrgID); err != nil {
			return fmt.Errorf("agents card: lock: %w", err)
		}
	}
	return nil
}

// AgentSettingsService owns the AI agents card. See the file doc.
type AgentSettingsService struct {
	settings OrgAgentSettingsRepository
	orgs     OrganizationRepository
	creds    *AnthropicCredentialService
	conns    *ModelConnectionService
	card     AgentsCardRepository
	runtimes []orgconfig.AgentRuntime
	now      func() time.Time
}

// NewAgentSettingsService wires the service. creds validates, reads and mirrors
// the Claude subscription; conns probes, writes and mirrors the connection;
// card is the unit of work the saves run in; runtimes are the runtimes this
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
	out := cardProbe{basis: state.conn}
	if eff.writeConn != nil {
		res, err := s.conns.probe(ctx, ocOrgID, *eff.writeConn)
		if err != nil {
			return cardProbe{}, sectionErrorFrom("llm", err)
		}
		check := s.conns.check(*eff.writeConn, res)
		out.draft, out.result, out.check = eff.writeConn, res, &check
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
// as a save's llm section; a body that changes nothing tests the saved
// connection with its stored key.
func (s *AgentSettingsService) testConnection(ctx context.Context, ocOrgID string, w orgconfig.LLMPatch) (orgconfig.LLMCheck, error) {
	stored, err := s.conns.stored(ctx, ocOrgID)
	if err != nil {
		return orgconfig.LLMCheck{}, fmt.Errorf("model connection test: %w", err)
	}
	draft, _, err := draftConnection(stored, w)
	if err != nil {
		return orgconfig.LLMCheck{}, sectionErrorFrom("llm", err)
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

// apply saves the card's part of p as ONE transaction under the org's card
// lock. The patch is judged again inside it, against the rows it is about to
// write over, so a concurrent save cannot slip a state between probe and write
// that the rule would refuse; a connection that changed since it was probed
// is a conflict, never a write of an unprobed connection. The copies outside
// Postgres follow the commit (syncCopies), best-effort, and never decide
// whether the save happened.
func (s *AgentSettingsService) apply(ctx context.Context, ocOrgID, actor string, p orgconfig.ConfigPatch, probed cardProbe) error {
	var (
		eff           cardEffects
		before, after *modelconn.Connection // the org's connection either side of the save
		forgotToken   string                // SM-API ref name of a deleted subscription
		forgotKey     string                // SM-API ref name of a deleted connection key
	)
	err := s.card.Tx(ctx, func(tx AgentsCardTx) error {
		if err := lockCard(tx.AdvisoryLock, ocOrgID); err != nil {
			return err
		}
		state, err := stateInTx(tx, ocOrgID)
		if err != nil {
			return err
		}
		if eff, err = judgeCard(state, s.runtimes, p); err != nil {
			return err
		}
		if eff.writeConn != nil && !probed.covers(*eff.writeConn, state.conn) {
			return sectionErrorFrom("llm", &ConflictError{Reason: "the model connection changed while this save was being tested; save again"})
		}
		now := s.now().UTC()
		// Deletes first: the token goes before the connection it sits beside.
		if eff.deleteToken {
			ref, existed, err := s.creds.deleteKeyTx(ctx, tx, ocOrgID, AnthropicRoleCoding)
			if err != nil {
				return err
			}
			if existed {
				forgotToken = ref
			}
		}
		var written *OrgModelConnection
		switch {
		case eff.deleteConn:
			if forgotKey, err = s.conns.deleteTx(ctx, tx, ocOrgID); err != nil {
				return err
			}
			if err := tx.SetKeyDisconnectedAt(ocOrgID, &now); err != nil {
				return fmt.Errorf("agents card: record disconnect: %w", err)
			}
		case eff.writeConn != nil:
			if written, err = s.conns.writeTx(ctx, tx, ocOrgID, actor, *eff.writeConn, probed.result, state.conn, now); err != nil {
				return err
			}
			if err := tx.SetKeyDisconnectedAt(ocOrgID, nil); err != nil {
				return fmt.Errorf("agents card: clear disconnect: %w", err)
			}
		}
		before, after = connectionsAround(state, eff, written)
		if eff.writeToken != "" {
			if err := s.creds.writeKeyTx(ctx, tx, ocOrgID, AnthropicRoleCoding, eff.writeToken); err != nil {
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
	if err != nil {
		return err
	}
	s.syncCopies(ctx, ocOrgID, cardCopies{
		forgotToken:  forgotToken,
		forgotKey:    forgotKey,
		keyWritten:   eff.writeConn != nil && eff.writeConn.Key != "",
		tokenWritten: eff.writeToken != "",
		before:       before,
		after:        after,
	})
	return nil
}

// cardCopies is what a committed save changed that the card's copies outside
// Postgres follow: the SM-API mirrors and the Agent Manager provider.
type cardCopies struct {
	forgotToken, forgotKey   string // SM-API ref names of deleted credentials
	keyWritten, tokenWritten bool
	before, after            *modelconn.Connection // the org's connection either side of the save
}

// none reports a save that changed nothing the copies hold (a runtime-only or
// model-only save).
func (c cardCopies) none() bool {
	return c.forgotToken == "" && c.forgotKey == "" && !c.keyWritten && !c.tokenWritten &&
		modelProviderStepFor(c.before, c.after, c.keyWritten) == modelProviderLeave
}

// syncCopies brings the copies in line with a committed save, best-effort, in
// a second transaction under the card's locks. Each copy is made from the rows
// as they stand, not as the save left them: the SM-API paths are fixed per
// org, so two saves' copies finishing out of order would otherwise leave the
// earlier key in the vault beside the later host, and a stored key never
// follows the host (ADR-0038). Under the lock, whichever copy runs last copies
// the last save. A failure is logged and never undoes the save.
func (s *AgentSettingsService) syncCopies(ctx context.Context, ocOrgID string, c cardCopies) {
	if c.none() {
		return
	}
	err := s.card.Tx(ctx, func(tx AgentsCardTx) error {
		if err := lockCard(tx.AdvisoryLock, ocOrgID); err != nil {
			return err
		}
		s.creds.forgetKey(ctx, tx, ocOrgID, AnthropicRoleCoding, c.forgotToken)
		s.conns.forgetKey(ctx, tx, ocOrgID, c.forgotKey)
		if c.keyWritten {
			s.conns.mirrorKey(ctx, tx, ocOrgID)
		}
		s.creds.syncModelProvider(ctx, tx, ocOrgID, c.before, c.after, c.keyWritten)
		if c.tokenWritten {
			s.creds.mirrorKey(ctx, tx, ocOrgID, AnthropicRoleCoding)
		}
		return nil
	})
	if err != nil {
		slog.WarnContext(ctx, "agents card: the saved card's copies were not brought in line (org_secrets still authoritative)",
			"ocOrgId", ocOrgID, "error", err)
	}
}

// covers reports whether this probe vouches for writing draft over stored: it
// probed the same draft, against the same stored connection (the same key on
// the same host, last saved at the same moment).
func (p cardProbe) covers(draft connectionDraft, stored *OrgModelConnection) bool {
	if p.draft == nil || *p.draft != draft {
		return false
	}
	if (p.basis == nil) != (stored == nil) {
		return false
	}
	return stored == nil || (p.basis.Host == stored.Host && p.basis.KeyPreview == stored.KeyPreview &&
		p.basis.UpdatedAt.Equal(stored.UpdatedAt))
}

// currentState reads the card's state from the pool, for the probe phase.
func (s *AgentSettingsService) currentState(ctx context.Context, ocOrgID string) (cardState, error) {
	return readCardState(
		func() (*OrgAgentSettings, error) { return s.settings.GetByOrg(ctx, ocOrgID) },
		func() (*OrgModelConnection, error) { return s.conns.stored(ctx, ocOrgID) },
		func() (bool, error) { return s.creds.Holds(ctx, ocOrgID, AnthropicRoleCoding) },
	)
}

// stateInTx reads the same state through the card's transaction.
func stateInTx(tx AgentsCardTx, ocOrgID string) (cardState, error) {
	return readCardState(
		func() (*OrgAgentSettings, error) { return tx.GetSettings(ocOrgID) },
		func() (*OrgModelConnection, error) { return tx.GetConnection(ocOrgID) },
		func() (bool, error) {
			row, err := tx.GetCredential(ocOrgID, AnthropicRoleCoding)
			return row != nil, err
		},
	)
}

// readCardState assembles a cardState from one source's reads, so the probe
// phase and the transaction judge the patch against the same shape of state.
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
		KeyPrefix:       p.KeyPrefix,
		KeyLast4:        p.KeyLast4,
		Status:          p.Status,
		ConnectedAt:     p.ConnectedAt,
		LastValidatedAt: p.LastValidatedAt,
		ValidationError: p.ValidationError,
	}
}
