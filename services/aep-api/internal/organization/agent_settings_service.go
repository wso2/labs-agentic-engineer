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
// The card is two /config sections: `llm` (the org's Anthropic API key) and
// `agents` (the one model every agent uses, the coding agent's runtime, and an
// optional Claude subscription the coding agent bills instead of the key). One
// save of it is one transaction under one per-org lock, covering the credential
// rows, the setting row and the encrypted secret bytes — see
// repository_agents_card.go. What a save does is decided by agents_rule.go.
//
// The model is read by two kinds of caller, for two lifetimes: the spec agents
// resolve it at the start of every turn, and coding dispatch copies it (with the
// runtime) onto the run it launches, so a run in flight keeps what it started
// with.

package organization

import (
	"context"
	"fmt"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// AgentSettingsService owns the AI agents card. See the file doc.
type AgentSettingsService struct {
	settings OrgAgentSettingsRepository
	orgs     OrganizationRepository
	creds    *AnthropicCredentialService
	card     AgentsCardRepository
	now      func() time.Time
}

// NewAgentSettingsService wires the service. creds validates, reads and mirrors
// the credentials; card is the unit of work the saves run in.
func NewAgentSettingsService(
	settings OrgAgentSettingsRepository,
	orgs OrganizationRepository,
	creds *AnthropicCredentialService,
	card AgentsCardRepository,
) *AgentSettingsService {
	return &AgentSettingsService{settings: settings, orgs: orgs, creds: creds, card: card, now: time.Now}
}

// Effective returns how the org's agents run: its chosen model and runtime (or
// the platform defaults when nobody chose), and its Claude subscription, masked,
// when it has one. Never an error for "not set": the defaults ARE the answer.
func (s *AgentSettingsService) Effective(ctx context.Context, ocOrgID string) (orgconfig.AgentsProjection, error) {
	out := orgconfig.DefaultAgents()
	row, err := s.settings.GetByOrg(ctx, ocOrgID)
	if err != nil {
		return orgconfig.AgentsProjection{}, fmt.Errorf("agent settings: %w", err)
	}
	if row != nil {
		updatedAt, updatedBy := row.UpdatedAt, row.UpdatedBy
		out.Model, out.Runtime = row.Model, row.Runtime
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

// Model is the one model the org's agents use this turn.
func (s *AgentSettingsService) Model(ctx context.Context, ocOrgID string) (string, error) {
	row, err := s.settings.GetByOrg(ctx, ocOrgID)
	if err != nil {
		return "", fmt.Errorf("agent settings: %w", err)
	}
	if row == nil {
		return orgconfig.DefaultAgentModel, nil
	}
	return row.Model, nil
}

// KeyDisconnectedAt is when the org's API key was last disconnected, or nil
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

// probe validates the card's part of p WITHOUT writing anything: the patch is
// judged against the org's current state (so a refusal costs no live probe),
// then every new credential is probed against Anthropic. A failure is a
// SectionError naming the section to fix; nothing is written by any section.
func (s *AgentSettingsService) probe(ctx context.Context, ocOrgID string, p orgconfig.ConfigPatch) error {
	state, err := s.currentState(ctx, ocOrgID)
	if err != nil {
		return err
	}
	eff, err := judgeCard(state, p)
	if err != nil {
		return err
	}
	if eff.writeKey != "" {
		if err := s.creds.ValidateKey(ctx, AnthropicRoleDefault, eff.writeKey); err != nil {
			return sectionErrorFrom("llm", err)
		}
	}
	if eff.writeToken != "" {
		if err := s.creds.ValidateKey(ctx, AnthropicRoleCoding, eff.writeToken); err != nil {
			return sectionErrorFrom("agents", err)
		}
	}
	return nil
}

// apply saves the card's part of p as ONE transaction under the org's card
// lock. The patch is judged again inside it, against the rows it is about to
// write over, so a concurrent save cannot slip a state between probe and write
// that the rule would refuse. The SM-API copies follow the commit, best-effort,
// and never decide whether the save happened.
func (s *AgentSettingsService) apply(ctx context.Context, ocOrgID, actor string, p orgconfig.ConfigPatch) error {
	var (
		eff       cardEffects
		forgotten = map[AnthropicRole]string{} // role → SM-API ref name of a deleted credential
	)
	err := s.card.Tx(ctx, func(tx AgentsCardTx) error {
		if err := tx.AdvisoryLock("org_anthropic:" + ocOrgID); err != nil {
			return fmt.Errorf("agents card: lock: %w", err)
		}
		state, err := stateInTx(tx, ocOrgID)
		if err != nil {
			return err
		}
		if eff, err = judgeCard(state, p); err != nil {
			return err
		}
		// Deletes first: the token goes before the key it sits beside.
		for _, del := range []struct {
			on   bool
			role AnthropicRole
		}{{eff.deleteToken, AnthropicRoleCoding}, {eff.deleteKey, AnthropicRoleDefault}} {
			if !del.on {
				continue
			}
			ref, existed, err := s.creds.deleteKeyTx(ctx, tx, ocOrgID, del.role)
			if err != nil {
				return err
			}
			if existed && ref != "" {
				forgotten[del.role] = ref
			}
		}
		if eff.deleteKey {
			now := s.now().UTC()
			if err := tx.SetKeyDisconnectedAt(ocOrgID, &now); err != nil {
				return fmt.Errorf("agents card: record disconnect: %w", err)
			}
		}
		if eff.writeKey != "" {
			if err := s.creds.writeKeyTx(ctx, tx, ocOrgID, AnthropicRoleDefault, eff.writeKey); err != nil {
				return err
			}
			if err := tx.SetKeyDisconnectedAt(ocOrgID, nil); err != nil {
				return fmt.Errorf("agents card: clear disconnect: %w", err)
			}
		}
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
			row.OcOrgID, row.UpdatedBy, row.UpdatedAt = ocOrgID, actor, s.now().UTC()
			if err := tx.UpsertSettings(&row); err != nil {
				return fmt.Errorf("agents card: write setting: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for role, ref := range forgotten {
		s.creds.forgetKey(ctx, ocOrgID, role, ref)
	}
	if eff.writeKey != "" {
		s.creds.mirrorKey(ctx, ocOrgID, AnthropicRoleDefault, eff.writeKey)
		s.creds.publishModelKey(ctx, ocOrgID, eff.writeKey)
	}
	if eff.writeToken != "" {
		s.creds.mirrorKey(ctx, ocOrgID, AnthropicRoleCoding, eff.writeToken)
	}
	return nil
}

// currentState reads the card's state from the pool, for the probe phase.
func (s *AgentSettingsService) currentState(ctx context.Context, ocOrgID string) (cardState, error) {
	return readCardState(
		func() (*OrgAgentSettings, error) { return s.settings.GetByOrg(ctx, ocOrgID) },
		func(role AnthropicRole) (bool, error) { return s.creds.Holds(ctx, ocOrgID, role) },
	)
}

// stateInTx reads the same state through the card's transaction.
func stateInTx(tx AgentsCardTx, ocOrgID string) (cardState, error) {
	return readCardState(
		func() (*OrgAgentSettings, error) { return tx.GetSettings(ocOrgID) },
		func(role AnthropicRole) (bool, error) {
			row, err := tx.GetCredential(ocOrgID, role)
			return row != nil, err
		},
	)
}

// readCardState assembles a cardState from one source's reads, so the probe
// phase and the transaction judge the patch against the same shape of state.
func readCardState(settings func() (*OrgAgentSettings, error), holds func(AnthropicRole) (bool, error)) (cardState, error) {
	row, err := settings()
	if err != nil {
		return cardState{}, fmt.Errorf("agents card: setting: %w", err)
	}
	hasKey, err := holds(AnthropicRoleDefault)
	if err != nil {
		return cardState{}, fmt.Errorf("agents card: key: %w", err)
	}
	hasToken, err := holds(AnthropicRoleCoding)
	if err != nil {
		return cardState{}, fmt.Errorf("agents card: subscription: %w", err)
	}
	return cardState{settings: row, hasKey: hasKey, hasToken: hasToken}, nil
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
