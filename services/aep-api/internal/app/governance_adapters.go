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

// governance_adapters.go — the adapters the Agent Manager governance stage is
// built from. Each is a thin translation between something aep-api already has
// and one of agentgovernance's collaborator interfaces; none holds logic.
package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/dependencies/provisioning"
	"log/slog"
	"sort"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/delivery/agentgovernance"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ampClientFactory builds an Agent Manager client per base URL, so the address
// stays a property of the environment's binding record rather than of this
// process's configuration.
type ampClientFactory struct {
	cfg agentmanager.Config
}

func (f ampClientFactory) For(baseURL string) agentmanager.Client {
	c := f.cfg
	c.BaseURL = baseURL
	return agentmanager.New(c)
}

// ampOrgKeyReader yields the org's connected Anthropic key VALUE — what Agent
// Manager's provider holds on the org's behalf.
//
// EffectiveKey rather than the vault triplet: the provider needs the secret
// itself, once, at create time. An org that has connected nothing answers "",
// which the governor reads as "nothing to govern" rather than as a failure.
type ampOrgKeyReader struct {
	creds *organization.AnthropicCredentialService
}

func (r ampOrgKeyReader) AnthropicKeyValue(ctx context.Context, ocOrgID string) (string, error) {
	if r.creds == nil {
		return "", nil
	}
	eff, err := r.creds.EffectiveKey(ctx, ocOrgID)
	if err != nil {
		return "", fmt.Errorf("read org anthropic key: %w", err)
	}
	if eff == nil || eff.Source != "org" {
		return "", nil
	}
	return eff.Key, nil
}

// ampKeyStore persists an agent's Agent-Manager-issued model key and reports
// whether one is already stored.
//
// IT SUPPLIES ITS OWN IDENTITY. SM-API derives a secret's namespace from the
// JWT it authenticates, and this runs inside a Temporal activity where there is
// no user request and therefore no user JWT. The org's own OU id — the same
// value a user's token would carry — is read from the organization row and
// attached to the context, so the key lands on exactly the vault path a
// Settings-driven write would have used.
type ampKeyStore struct {
	writer *organization.SecretRefWriter
	refs   secretmanagersvc.OpenChoreoSecretReferenceClient
	orgs   organization.OrganizationRepository
}

// StoredAMPModelKey returns the SecretReference name holding this agent's key,
// or "" when none is stored.
//
// The name is DERIVED from (component, environment) rather than recorded:
// SM-API names a SecretReference deterministically from its location, so the
// writer and this reader agree without a row to join on. Both go through
// organization.AMPModelKeySecretRefName.
func (s ampKeyStore) StoredAMPModelKey(ctx context.Context, ocOrgID, component, environment string) (string, error) {
	if s.refs == nil {
		return "", nil
	}
	name := organization.AMPModelKeySecretRefName(component, environment)
	if _, err := s.refs.GetSecretReference(ctx, ocOrgID, name); err != nil {
		if errors.Is(err, secretmanagersvc.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("read stored AMP model key: %w", err)
	}
	return name, nil
}

func (s ampKeyStore) WriteAMPModelKey(ctx context.Context, ocOrgID, component, environment, apiKey, proxyURL string) (string, string, error) {
	if s.writer == nil {
		return "", "", nil
	}
	ctx, err := s.orgIdentity(ctx, ocOrgID)
	if err != nil {
		return "", "", err
	}
	return s.writer.WriteAMPModelKey(ctx, ocOrgID, component, environment, apiKey, proxyURL)
}

func (s ampKeyStore) WriteAMPTracingToken(ctx context.Context, ocOrgID, component, environment, token string) error {
	if s.writer == nil {
		return nil
	}
	ctx, err := s.orgIdentity(ctx, ocOrgID)
	if err != nil {
		return err
	}
	return s.writer.WriteAMPTracingToken(ctx, ocOrgID, component, environment, token)
}

// orgIdentity attaches the org's OU id to the context as token claims.
//
// Returns the context unchanged when the org row carries no Thunder OU — the
// write then fails with the writer's own "no ouId claim" error, which names the
// problem, rather than silently writing to a path nothing reads.
func (s ampKeyStore) orgIdentity(ctx context.Context, ocOrgID string) (context.Context, error) {
	if jwtassertion.GetTokenClaims(ctx) != nil {
		return ctx, nil // already in a request context; use the caller's identity
	}
	if s.orgs == nil {
		return ctx, nil
	}
	org, err := s.orgs.GetByName(ctx, ocOrgID)
	if err != nil || org == nil || org.ThunderOrgUUID == nil {
		return ctx, nil
	}
	return jwtassertion.ContextWithTokenClaims(ctx,
		&jwtassertion.TokenClaims{OuId: org.ThunderOrgUUID.String()}), nil
}

// ampComponentKinds answers what kind each of a project's components is, read
// from the design the project itself declares.
//
// The design is the source of truth for a component's kind everywhere else in
// aep-api (spec.ComponentTypeAIAgent gates model access in
// ai_agent_model_access.go), so governance asks the same question of the same
// document rather than inferring a kind from a name or from OpenChoreo.
type ampComponentKinds struct {
	store *spec.ArtifactStore
}

func (k ampComponentKinds) ComponentKinds(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	if k.store == nil {
		return nil, fmt.Errorf("component kinds: no artifact store wired")
	}
	design, err := k.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("read design for %s/%s: %w", orgID, projectID, err)
	}
	if design == nil {
		return map[string]string{}, nil
	}
	kinds := make(map[string]string, len(design.Components))
	for _, c := range design.Components {
		kinds[c.Name] = c.ComponentType
	}
	return kinds, nil
}

// ampModelProviderPublisher pushes the org's current Anthropic key onto its
// Agent Manager provider when the key changes.
//
// It resolves the environment's binding itself rather than taking an address:
// an org with no governed environment has nothing to publish to, and that is a
// no-op rather than an error.
type ampModelProviderPublisher struct {
	amp      agentgovernance.ClientFactory
	bindings agentgovernance.BindingReader
}

func (p ampModelProviderPublisher) PublishOrgModelKey(ctx context.Context, ocOrgID, apiKey string) error {
	if p.amp == nil || p.bindings == nil || strings.TrimSpace(apiKey) == "" {
		return nil
	}
	binding, err := p.bindings.GetAIGatewayBinding(ctx, ocOrgID, openchoreo.DevEnvironmentName)
	if errors.Is(err, openchoreo.ErrNoAIGatewayBinding) {
		return nil // nothing governed here
	}
	if err != nil {
		return fmt.Errorf("resolve AI gateway binding: %w", err)
	}

	client := p.amp.For(binding.AdminURL)
	tmpl, err := client.ProviderTemplate(ctx, ocOrgID, agentgovernance.AnthropicTemplate)
	if err != nil {
		return fmt.Errorf("read provider template: %w", err)
	}
	// THIS path always re-asserts, and it is the one place that must.
	//
	// The govern stage writes the credential only when its fingerprint changed
	// (agentgovernance.Governor.credentialChanged), because a provider update
	// redeploys every proxy bound to it. Rotation is exactly the case where it
	// DID change, and it is reached from the organization domain, which holds
	// no fingerprint of its own — so it says so explicitly rather than relying
	// on a later deploy to notice.
	providerIn := agentgovernance.ProviderInputFor(ocOrgID, tmpl, apiKey, binding.GatewayID)
	providerIn.ReassertCredential = true
	if _, err := client.EnsureProvider(ctx, providerIn); err != nil {
		return fmt.Errorf("publish org key to the provider: %w", err)
	}
	slog.InfoContext(ctx, "governance: org key published to the Agent Manager provider", "org", ocOrgID)
	return nil
}

// ampAgentRegistrar satisfies provisioning.AgentRegistrar: the BUILD-TIME half
// of agent governance, reached from the version's `provision` gate.
//
// It registers and stops. The credential is the deploy's business — see
// agentgovernance.Governor.EnsureRegistration for why a rotation at planning
// time would cut off the agent that is currently running.
type ampAgentRegistrar struct {
	governor *agentgovernance.Governor
	kinds    ampComponentKinds
}

// Enabled reports whether anything is wired. A deployment with no Agent Manager
// skips the gate entirely rather than failing every build.
func (r ampAgentRegistrar) Enabled() bool { return r.governor != nil }

// GovernedAgents names the project's ai-agent components, from the design.
//
// It reads the same document, through the same adapter, that the deploy path's
// kind gate reads — so the two can never disagree about which components are
// agents, which would show up as a gate that registers one set and a deploy
// that governs another.
func (r ampAgentRegistrar) GovernedAgents(ctx context.Context, orgID, projectID string) ([]string, error) {
	kinds, err := r.kinds.ComponentKinds(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	// Sorted, so the gate's ticket lists agents in a stable order across builds
	// rather than in map order.
	names := make([]string, 0, len(kinds))
	for name, kind := range kinds {
		if kind == spec.ComponentTypeAIAgent {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// RegisterAgentsForBuild registers every agent in the version, or fails.
//
// FAIL ON THE FIRST ERROR, deliberately. A partial registration is the state
// this gate exists to prevent: the run would settle failed anyway (the caller
// collapses failures into one error), and carrying on would spend more calls on
// an Agent Manager that has already answered once that it cannot serve this
// build. A SKIP is different and is collected — an org with no connected
// Anthropic key is a fact for the ticket to state, not a failure.
func (r ampAgentRegistrar) RegisterAgentsForBuild(ctx context.Context, orgID, projectID string, components []string) (provisioning.AgentRegistrationOutcome, error) {
	out := provisioning.AgentRegistrationOutcome{
		Provider: agentgovernance.ProviderID(orgID),
		Agents:   make([]provisioning.RegisteredAgent, 0, len(components)),
	}
	governed := false
	for _, component := range components {
		res, err := r.governor.EnsureRegistration(ctx, delivery.GovernAgentInput{
			OrgID:     orgID,
			ProjectID: projectID,
			Component: component,
			// The single environment the deploy targets. Governance follows the
			// platform here rather than carrying an environment of its own: it
			// must change where the rest of the deploy path changes, in one
			// sweep, or it becomes the only environment-aware step in a
			// pipeline that is not.
			Environment: openchoreo.DevEnvironmentName,
		})
		if err != nil {
			return provisioning.AgentRegistrationOutcome{}, fmt.Errorf("register %q with Agent Manager: %w", component, err)
		}
		out.Agents = append(out.Agents, provisioning.RegisteredAgent{
			Component: component,
			ProxyURL:  res.ProxyURL,
			Skipped:   res.Skipped,
			Reason:    res.Reason,
		})
		if !res.Skipped {
			governed = true
		}
	}
	if !governed {
		// Every agent skipped: this environment or org is not governed at all,
		// so naming a provider nothing is bound to would be a lie on the ticket.
		out.Provider = ""
	}
	return out, nil
}
