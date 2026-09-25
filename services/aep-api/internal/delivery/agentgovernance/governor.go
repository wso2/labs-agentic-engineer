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

// Package agentgovernance makes Agent Manager's view of an AEP-deployed agent
// true, and leaves the agent's model credential where the deploy can compose
// it.
//
// It lives beside internal/delivery/run rather than inside it because the
// composition root has to build it and run only declares the port it satisfies
// — and under internal/delivery because governing an agent is a stage of
// delivery, not a domain of its own.
package agentgovernance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// BindingReader resolves the environment's AI gateway. Satisfied by
// openchoreo.EnvironmentClient.
type BindingReader interface {
	GetAIGatewayBinding(ctx context.Context, orgID, environment string) (openchoreo.AIGatewayBinding, error)
}

// OrgKeyReader yields the org's connected Anthropic key VALUE — the credential
// Agent Manager's provider holds on the org's behalf. Empty means the org has
// connected none.
type OrgKeyReader interface {
	AnthropicKeyValue(ctx context.Context, ocOrgID string) (string, error)
}

// KeyStore persists an agent's AMP key and reports whether one is already
// stored. Satisfied by organization.SecretRefWriter plus a read.
type KeyStore interface {
	StoredAMPModelKey(ctx context.Context, ocOrgID, component, environment string) (string, error)
	// WriteAMPModelKey stores the agent's key AND the proxy URL it must be sent
	// to. They travel together because they are useless apart: the key
	// authenticates against that proxy alone.
	WriteAMPModelKey(ctx context.Context, ocOrgID, component, environment, apiKey, proxyURL string) (string, string, error)
	// WriteAMPTracingToken stores the agent's OTLP credential in a secret of
	// ITS OWN, separate from the model key's.
	//
	// Separate because the two have different failure modes and composition
	// has to tell them apart. A tracing token is optional — minting it can fail
	// without stopping a deploy — so the deployment may only reference it when
	// it is actually there. OpenChoreo's secretKeyRef has no `optional` flag,
	// so a reference to a key that was never written does not degrade to "no
	// traces", it stops the container from starting. One secret per credential
	// lets composition ask the question it can actually answer: does this
	// SecretReference exist?
	WriteAMPTracingToken(ctx context.Context, ocOrgID, component, environment, token string) error
}

// ComponentKinds answers what kind each of a project's components is, so only
// agents are registered as agents.
//
// A deploy wave carries every component being promoted — services and web apps
// included — and registering one of those in Agent Manager would put a thing
// that is not an agent in the agent catalogue, with a model credential it has
// no use for.
type ComponentKinds interface {
	ComponentKinds(ctx context.Context, orgID, projectID string) (map[string]string, error)
}

// ClientFactory builds an Agent Manager client for one base URL. The address is
// per environment, from the binding record, so it cannot be fixed at
// construction.
type ClientFactory interface {
	For(baseURL string) agentmanager.Client
}

// Deps are the governor's collaborators.
type Deps struct {
	AMP      ClientFactory
	Keys     KeyStore
	Bindings BindingReader
	OrgKeys  OrgKeyReader
	// Kinds gates which components are governed. Nil governs every target,
	// which is right only for a caller that has already filtered.
	Kinds ComponentKinds
}

// Governor implements run.AgentGovernor.
type Governor struct {
	deps Deps

	// mu guards pushedKey.
	mu sync.Mutex
	// pushedKey remembers, per org, a FINGERPRINT of the Anthropic key this
	// process last wrote to Agent Manager's provider.
	//
	// It exists to stop a needless write. Updating a provider redeploys every
	// LLM proxy bound to it — twelve redeploys per governed deploy in a
	// single-agent org, and a redeploy is the window in which a proxy can lose
	// the API keys broadcast to it. Re-asserting a key that has not changed
	// bought nothing and paid that cost on every deploy, every converge tick.
	//
	// A FINGERPRINT, never the key: this map outlives a single call and has no
	// business holding a credential.
	//
	// In memory rather than persisted, deliberately. The first governed deploy
	// after a restart re-asserts once and the map is warm again — so the
	// self-healing property that motivated the original unconditional write
	// survives, at the cost of one write per process rather than one per
	// deploy. A rotation still pushes immediately through the organization
	// domain's own path, and changes the fingerprint here on the next deploy.
	pushedKey map[string]string

	// tracingExpiry remembers, per (org, component, environment), when the
	// tracing token this process last minted runs out.
	//
	// IT CANNOT BE READ BACK, which is why it is remembered at all. The secret
	// store is write-only (the OpenBao provider does not implement a value
	// read), so nothing can ask what token an agent holds or when it lapses —
	// the only moment the expiry is knowable is the mint that produced it.
	//
	// In memory, for the same reason and with the same trade as pushedKey: the
	// first governed deploy after a restart mints once more than it strictly
	// needed to. That costs nothing here — a tracing token is a signed JWT that
	// Agent Manager keeps no record of, so a second one neither revokes the
	// first nor accumulates anything to clean up. A model key would not
	// tolerate the same treatment, which is exactly why the two reconcile
	// differently.
	tracingExpiry map[string]int64
}

// New builds the governor.
func New(d Deps) *Governor {
	return &Governor{deps: d, pushedKey: map[string]string{}, tracingExpiry: map[string]int64{}}
}

// credentialChanged reports whether this org's key differs from the one this
// process last wrote, and records the new one.
func (g *Governor) credentialChanged(org, key string) bool {
	sum := sha256.Sum256([]byte(key))
	fp := hex.EncodeToString(sum[:])

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pushedKey[org] == fp {
		return false
	}
	g.pushedKey[org] = fp
	return true
}

// GovernAgent makes Agent Manager's view of this agent true AND settles its
// credential. This is the DEPLOY path.
//
// The order is forced by the API: a provider must exist before a model config
// can name it, and a config before a key can be issued against it.
func (g *Governor) GovernAgent(ctx context.Context, in delivery.GovernAgentInput) (delivery.GovernAgentOutcome, error) {
	reg, out, err := g.register(ctx, in)
	if err != nil || out.Skipped {
		return out, err
	}
	if err := g.reconcileKey(ctx, reg, in); err != nil {
		return delivery.GovernAgentOutcome{}, err
	}
	g.reconcileTracingToken(ctx, reg, in)
	slog.InfoContext(ctx, "governance: agent registered with Agent Manager",
		"org", in.OrgID, "project", in.ProjectID, "component", in.Component,
		"environment", in.Environment, "provider", reg.provider.Handle)
	return out, nil
}

// EnsureRegistration makes Agent Manager's view of this agent true and STOPS
// THERE. This is the BUILD-TIME gate's path.
//
// It deliberately does not touch the key, and that is the whole reason the two
// are separate entry points. The key reconcile has a rotate branch — the only
// route back to a known state when Agent Manager holds a credential AEP cannot
// read — and rotation is safe ONLY because a deploy is in flight to carry the
// new value. At planning time there is no rollout: an entire coding cycle
// stands between here and the next one, so a rotation here would cut off the
// agent that is currently RUNNING for the whole of it, and permanently if the
// run then fails.
//
// Everything this does run is idempotent and safe to repeat, which is what lets
// the deploy and the converge sweep keep doing it afterwards.
func (g *Governor) EnsureRegistration(ctx context.Context, in delivery.GovernAgentInput) (delivery.GovernAgentOutcome, error) {
	_, out, err := g.register(ctx, in)
	return out, err
}

// registration is what one agent's registration settled to: the client it was
// made through, the provider it names, and the address its traffic leaves by.
type registration struct {
	amp      agentmanager.Client
	provider agentmanager.ProviderRef
	config   agentmanager.ModelConfigRef
	// agentName is the record's name in Agent Manager — the SAME identifier the
	// binding and its keys are addressed by. Carried rather than recomputed so
	// the key path can never drift from the one the binding was made under.
	agentName string
	// endpoint is the base an agent's SDK is pointed at — the in-cluster
	// gateway, this agent's own proxy path, and the API version segment.
	endpoint string
}

// register settles everything about an agent that is safe to assert at any
// time: the org's provider, the agent record, and this agent's model binding.
//
// A Skipped outcome is never an error. Each of the three gates below is a
// deliberate "this agent is not governed here", and the caller falls back to
// the path that existed before Agent Manager.
func (g *Governor) register(ctx context.Context, in delivery.GovernAgentInput) (registration, delivery.GovernAgentOutcome, error) {
	agent, err := g.isAgent(ctx, in)
	if err != nil {
		return registration{}, delivery.GovernAgentOutcome{}, err
	}
	if !agent {
		return registration{}, delivery.GovernAgentOutcome{
			Skipped: true,
			Reason:  "component is not an ai-agent",
		}, nil
	}

	binding, err := g.deps.Bindings.GetAIGatewayBinding(ctx, in.OrgID, in.Environment)
	if errors.Is(err, openchoreo.ErrNoAIGatewayBinding) {
		// Not a failure: this environment was never provisioned for Agent
		// Manager, so the agent deploys the way it did before this existed.
		return registration{}, delivery.GovernAgentOutcome{
			Skipped: true,
			Reason:  "environment has no AI gateway binding",
		}, nil
	}
	if err != nil {
		return registration{}, delivery.GovernAgentOutcome{}, fmt.Errorf("resolve AI gateway binding: %w", err)
	}

	orgKey, err := g.deps.OrgKeys.AnthropicKeyValue(ctx, in.OrgID)
	if err != nil {
		return registration{}, delivery.GovernAgentOutcome{}, fmt.Errorf("read org Anthropic key: %w", err)
	}
	if orgKey == "" {
		// No key connected: there is no provider to build. The agent comes up
		// unconfigured and reports 503 from /healthz, exactly as it does today.
		// Not a governance bypass — an agent with no model access reaches no
		// model at all.
		return registration{}, delivery.GovernAgentOutcome{
			Skipped: true,
			Reason:  "org has no connected Anthropic key",
		}, nil
	}

	amp := g.deps.AMP.For(binding.AdminURL)

	tmpl, err := amp.ProviderTemplate(ctx, in.OrgID, AnthropicTemplate)
	if err != nil {
		return registration{}, delivery.GovernAgentOutcome{}, fmt.Errorf("read provider template: %w", err)
	}
	providerIn := ProviderInputFor(in.OrgID, tmpl, orgKey, binding.GatewayID)
	providerIn.ReassertCredential = g.credentialChanged(in.OrgID, orgKey)
	provider, err := amp.EnsureProvider(ctx, providerIn)
	if err != nil {
		return registration{}, delivery.GovernAgentOutcome{}, fmt.Errorf("ensure LLM provider: %w", err)
	}

	// ONE name for all three calls below. The agent record, its model binding
	// and that binding's keys are addressed by the SAME identifier — the
	// binding lives at …/agents/{agent}/model-configs and the keys under it, so
	// a record registered under one name and a binding ensured under another
	// leaves the record empty and the binding hanging off whatever else answers
	// to the second name. That is not hypothetical: it is what the first
	// version of this rename did.
	agentName := AgentRecordName(in.ProjectID, in.Component)

	if _, err := amp.EnsureAgent(ctx, agentmanager.EnsureAgentInput{
		Org:         in.OrgID,
		Project:     in.ProjectID,
		Name:        agentName,
		DisplayName: in.Component,
		Description: "Deployed by AEP",
	}); err != nil {
		return registration{}, delivery.GovernAgentOutcome{}, fmt.Errorf("register agent: %w", err)
	}

	cfg, err := amp.EnsureModelConfig(ctx, agentmanager.EnsureModelConfigInput{
		Org:            in.OrgID,
		Project:        in.ProjectID,
		Agent:          agentName,
		Name:           ModelConfigName(in.Component),
		Environment:    in.Environment,
		ProviderHandle: provider.Handle,
		URLVar:         ModelEndpointEnvVar,
		APIKeyVar:      ModelAPIKeyEnvVar,
	})
	if err != nil {
		return registration{}, delivery.GovernAgentOutcome{}, fmt.Errorf("ensure model config: %w", err)
	}

	// The agent runs in a pod, so it gets the IN-CLUSTER address. aep-api's own
	// control calls above used the binding's admin URL, which is a different
	// address for a different caller.
	//
	// Falling back to the public endpoint here as well as in the annotation
	// parser is deliberate: an empty value would compose a RELATIVE url
	// (`/aep-…/v1`), which an agent cannot call and which no error would name.
	podEndpoint := binding.InternalEndpoint
	if strings.TrimSpace(podEndpoint) == "" {
		podEndpoint = binding.Endpoint
	}
	endpoint, err := proxyEndpoint(cfg.ProxyURL, podEndpoint)
	if err != nil {
		return registration{}, delivery.GovernAgentOutcome{}, err
	}

	return registration{amp: amp, provider: provider, config: cfg, endpoint: endpoint, agentName: agentName},
		delivery.GovernAgentOutcome{ProxyURL: endpoint}, nil
}

// isAgent reports whether this component is an ai-agent.
//
// A design that cannot be read is an error rather than a "no": silently
// governing nothing would deploy every agent in the wave on the org's raw key
// while the environment's binding promises otherwise.
func (g *Governor) isAgent(ctx context.Context, in delivery.GovernAgentInput) (bool, error) {
	if g.deps.Kinds == nil {
		return true, nil
	}
	kinds, err := g.deps.Kinds.ComponentKinds(ctx, in.OrgID, in.ProjectID)
	if err != nil {
		return false, fmt.Errorf("read component kinds: %w", err)
	}
	return kinds[in.Component] == spec.ComponentTypeAIAgent, nil
}

// reconcileKey settles the four states of a credential Agent Manager returns
// once and never again. It runs only on the DEPLOY path — see
// EnsureRegistration for why it is not safe at planning time.
//
// Issuing and storing happen inside this one call, on purpose: split across two
// retryable steps, a crash between them would strand a key neither side can
// recover — Agent Manager would hold one it will not show again, and AEP would
// hold none.
func (g *Governor) reconcileKey(ctx context.Context, reg registration, in delivery.GovernAgentInput) error {
	ref := agentmanager.ModelKeyRef{
		Org:         in.OrgID,
		Project:     in.ProjectID,
		Agent:       reg.agentName,
		ConfigID:    reg.config.ConfigID,
		Environment: in.Environment,
	}
	keyName := AgentKeyName(in.Component, in.Environment)

	ampKeys, err := reg.amp.ListModelKeys(ctx, ref)
	if err != nil {
		return fmt.Errorf("list model keys: %w", err)
	}
	ours := false
	for _, name := range ampKeys {
		if name == keyName {
			ours = true
			break
		}
	}
	stored, err := g.deps.Keys.StoredAMPModelKey(ctx, in.OrgID, in.Component, in.Environment)
	if err != nil {
		return fmt.Errorf("read stored model key: %w", err)
	}

	switch {
	case stored != "" && ours:
		// Both sides hold it. Regenerating here would invalidate the credential
		// a RUNNING agent is using, on every redeploy.
		return nil

	case stored == "" && ours:
		// Agent Manager holds this agent's key but AEP cannot read its value.
		// Rotation is the only route back to a known state, and it is safe only
		// because a deploy is in flight to carry the new value.
		issued, err := reg.amp.RotateModelKey(ctx, ref, keyName)
		if err != nil {
			return fmt.Errorf("rotate model key: %w", err)
		}
		return g.store(ctx, in, issued, reg.endpoint)

	default:
		// Either neither side holds one, or we hold one Agent Manager has
		// forgotten — ours can never authenticate again.
		issued, err := reg.amp.IssueModelKey(ctx, ref, keyName)
		if err != nil {
			return fmt.Errorf("issue model key: %w", err)
		}
		return g.store(ctx, in, issued, reg.endpoint)
	}
}

// reconcileTracingToken gives the agent the credential its OTLP export
// authenticates with, and the address to send spans to.
//
// IT NEVER FAILS THE DEPLOY. Model access is load-bearing — without a key the
// agent cannot answer at all — but an agent with no tracing token runs
// correctly and is merely unobserved. Failing here would trade a working agent
// for a missing graph, so a failure is logged loudly and the deploy continues.
// The loud log is the point: the one thing this must not do is go quiet, which
// is precisely how a missing amp:agent:token-manage scope presents.
func (g *Governor) reconcileTracingToken(ctx context.Context, reg registration, in delivery.GovernAgentInput) {
	if !g.tracingTokenDue(in) {
		return
	}
	issued, err := reg.amp.IssueTracingToken(ctx, agentmanager.TracingTokenRef{
		Org:         in.OrgID,
		Project:     in.ProjectID,
		Agent:       reg.agentName,
		Environment: in.Environment,
	})
	if err != nil {
		slog.WarnContext(ctx, "governance: could not mint the agent's tracing token — the agent will run without observability",
			"org", in.OrgID, "component", in.Component, "environment", in.Environment, "error", err)
		return
	}
	if err := g.deps.Keys.WriteAMPTracingToken(ctx, in.OrgID, in.Component, in.Environment, issued.Token); err != nil {
		slog.WarnContext(ctx, "governance: could not store the agent's tracing token — the agent will run without observability",
			"org", in.OrgID, "component", in.Component, "environment", in.Environment, "error", err)
		return
	}
	g.recordTracingExpiry(in, issued.ExpiresAt)
}

// tracingTokenDue reports whether this agent needs a tracing token minted —
// because this process has never minted one for it, or because the one it
// minted is close enough to lapsing that a deploy in flight should carry a
// fresh one rather than leave the agent to go quiet mid-life.
func (g *Governor) tracingTokenDue(in delivery.GovernAgentInput) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	exp, ok := g.tracingExpiry[tracingKey(in)]
	return !ok || time.Until(time.Unix(exp, 0)) < tracingRefreshWindow
}

func (g *Governor) recordTracingExpiry(in delivery.GovernAgentInput, expiresAt int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tracingExpiry[tracingKey(in)] = expiresAt
}

func tracingKey(in delivery.GovernAgentInput) string {
	return in.OrgID + "/" + in.Component + "/" + in.Environment
}

// proxyEndpoint turns the proxy address Agent Manager generated into the base
// URL an agent's SDK can be pointed at.
//
// Agent Manager answers with an address reachable from OUTSIDE the cluster; an
// agent runs in a pod, so only the path is kept and the environment's
// in-cluster gateway address is put in front of it.
//
// THE /v1 IS NOT DECORATION. An agent builds its client from MODEL_ENDPOINT and
// the SDK appends the operation — the Anthropic SDK asks for
// `<base>/messages` — so a base without the version segment requests
// `/aep-…/messages` and the gateway answers 404. AEP's own ungoverned default
// is `https://api.anthropic.com/v1` for exactly the same reason; the governed
// endpoint has to have the same shape, or swapping one for the other silently
// breaks every agent.
func proxyEndpoint(proxyURL, gatewayEndpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(proxyURL))
	if err != nil || u.Path == "" || u.Path == "/" {
		return "", fmt.Errorf("agent governance: Agent Manager returned no usable proxy URL (%q)", proxyURL)
	}
	return strings.TrimSuffix(gatewayEndpoint, "/") + u.Path + modelAPIVersionPath, nil
}

// store persists the issued key with the endpoint it authenticates against.
//
// The two travel together because they are useless apart, and storing the
// endpoint means the deployment composes MODEL_ENDPOINT without deriving an
// address a second time, in a package that has no business knowing Agent
// Manager's URL shape.
func (g *Governor) store(ctx context.Context, in delivery.GovernAgentInput, issued agentmanager.IssuedKey, endpoint string) error {
	if _, _, err := g.deps.Keys.WriteAMPModelKey(ctx, in.OrgID, in.Component, in.Environment, issued.APIKey, endpoint); err != nil {
		// The key exists in Agent Manager and nowhere else. Returning the error
		// retries the whole activity, which lands in the rotate branch on the
		// next attempt rather than leaking a second key.
		return fmt.Errorf("store model key: %w", err)
	}
	return nil
}

// AgentKeyName is the name this agent's key carries on its own binding. The
// binding is already per agent and per environment, so the name exists to be
// RECOGNISED: the govern stage reconciles on whether a key of this name is
// already there.
func AgentKeyName(component, environment string) string {
	return fmt.Sprintf("aep-%s-%s", component, environment)
}

// AgentRecordName is the name one agent's record carries in Agent Manager.
//
// Two constraints meet here, and missing either produces a failure that looks
// like something else.
//
// IT MUST BE UNIQUE ACROSS THE ORG, not across the project. Agent Manager
// represents an agent as an OpenChoreo Component, and a Component name is
// unique per NAMESPACE — every project in an org shares one, and the project is
// only a label on the object (see ocname.ScopedComponentName, which states the
// same rule for AEP's own components). Registering the bare component name
// collided the moment two projects each held a `library-agent`: the second
// registration resolved to the FIRST project's record, so the agent appeared
// under the wrong project and its own project's page showed nothing. The
// guardrail a human attaches is attached on that page, which is what made it
// more than cosmetic.
//
// IT MUST FIT 25 CHARACTERS, which is Agent Manager's own cap and is why this
// cannot simply be `<project>-<component>` the way AEP's component naming is:
// that overflows for any realistic pair and comes back as a 400 that fails the
// whole deploy.
//
// So the component name leads — it is what a human reads when the console shows
// the record's name rather than its display name — and a hash of the
// project-and-component pair disambiguates. Hashing the PAIR rather than the
// project alone is what keeps two long, similarly-prefixed components in one
// project apart once the head is truncated.
func AgentRecordName(project, component string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(project + "/" + component))
	suffix := fmt.Sprintf("-%08x", h.Sum32()) // 9 chars: '-' + 8 hex

	head := component
	if len(head) > maxAgentRecordName-len(suffix) {
		head = head[:maxAgentRecordName-len(suffix)]
	}
	// A trailing '-' would double up against the suffix's leading one and, on a
	// name OpenChoreo treats as a DNS-1035 label, is not legal on its own.
	return strings.TrimRight(head, "-") + suffix
}

// maxAgentRecordName is Agent Manager's cap on an agent's name. LIVE-VERIFIED:
// a 38-character name answers
// `400 VALIDATION_ERROR: Agent name must be at most 25 characters`.
const maxAgentRecordName = 25

// ModelConfigName is the name of the binding between one agent and the org's
// provider. One per agent, so the console shows the provider under the agent.
//
// It needs no project segment: a binding is addressed under its agent's path,
// and the agent's own name is now unique.
func ModelConfigName(component string) string { return "aep-" + component }

// AnthropicTemplate is Agent Manager's template id for Anthropic. Exported
// because key rotation builds the same provider input from outside this package.
const AnthropicTemplate = "anthropic"

const (
	// modelAPIVersionPath is the version segment an Anthropic-shaped client
	// expects on its base URL — see where the endpoint is composed.
	modelAPIVersionPath = "/v1"

	// tracingRefreshWindow is how close to expiry a tracing token may get
	// before a deploy replaces it. Agent Manager issues them for about ninety
	// days, so a fortnight leaves many ordinary deploys in which to roll over
	// without ever minting on a schedule of its own.
	tracingRefreshWindow = 14 * 24 * time.Hour

	providerVersion = "v1.0"

	// ModelEndpointEnvVar / ModelAPIKeyEnvVar are the names AEP agents already
	// read. Declaring them to Agent Manager is what makes an agent's code work
	// unchanged whether its model access is governed or direct.
	ModelEndpointEnvVar = "MODEL_ENDPOINT"
	ModelAPIKeyEnvVar   = "MODEL_API_KEY"
)

// ProviderInputFor builds the org's provider exactly as AEP declares it.
//
// One builder, used by the deploy path and by key rotation, so the two can
// never describe the same provider differently — which would show up as a
// provider that flips shape depending on which path last wrote it.
func ProviderInputFor(org string, tmpl agentmanager.ProviderTemplate, orgKey, gatewayID string) agentmanager.EnsureProviderInput {
	return agentmanager.EnsureProviderInput{
		Org:         org,
		ID:          ProviderID(org),
		Name:        providerName(org),
		Version:     providerVersion,
		Context:     "/" + ProviderID(org),
		Template:    AnthropicTemplate,
		UpstreamURL: tmpl.EndpointURL,
		AuthType:    tmpl.AuthType,
		AuthHeader:  tmpl.AuthHeader,
		APIKey:      orgKey,
		GatewayID:   gatewayID,
	}
}

// ProviderID is the handle of the org's AEP-owned provider. Exported because
// rotation resolves the same provider by the same name.
func ProviderID(org string) string { return "aep-" + org + "-anthropic" }

func providerName(org string) string { return "AEP " + org + " Anthropic" }
