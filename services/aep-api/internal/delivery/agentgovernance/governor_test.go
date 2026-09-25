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

package agentgovernance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// --- fakes -------------------------------------------------------------------

// proxyURL is what Agent Manager generated for this agent's binding. The
// default is set by newFakeAMP; a zero value stands for "AMP answered without
// one".
type fakeAMP struct {
	providerIn agentmanager.EnsureProviderInput
	agentIn    agentmanager.EnsureAgentInput
	configIn   agentmanager.EnsureModelConfigInput
	keyRef     agentmanager.ModelKeyRef
	proxyURL   string
	keys       []string
	issued     bool
	rotated    bool
	calls      int
	err        error

	tracingRef    agentmanager.TracingTokenRef
	tracingMints  int
	tracingExpiry int64
	tracingErr    error
}

// newFakeAMP is an Agent Manager that answers the way the real one does.
func newFakeAMP(keys ...string) *fakeAMP {
	return &fakeAMP{proxyURL: "http://ai-gateway.amp.localhost:8084/aep-checkout-3d748f50", keys: keys}
}

func (f *fakeAMP) For(string) agentmanager.Client { return f }

func (f *fakeAMP) ProviderTemplate(context.Context, string, string) (agentmanager.ProviderTemplate, error) {
	f.calls++
	if f.err != nil {
		return agentmanager.ProviderTemplate{}, f.err
	}
	return agentmanager.ProviderTemplate{
		ID: "anthropic", EndpointURL: "https://api.anthropic.com",
		AuthType: "api-key", AuthHeader: "x-api-key",
	}, nil
}

func (f *fakeAMP) EnsureProvider(_ context.Context, in agentmanager.EnsureProviderInput) (agentmanager.ProviderRef, error) {
	f.calls++
	f.providerIn = in
	if f.err != nil {
		return agentmanager.ProviderRef{}, f.err
	}
	return agentmanager.ProviderRef{
		UUID: "prov", Handle: "aep-default-anthropic", Context: "/aep-default-anthropic",
	}, nil
}

func (f *fakeAMP) EnsureAgent(_ context.Context, in agentmanager.EnsureAgentInput) (agentmanager.AgentRef, error) {
	f.calls++
	f.agentIn = in
	if f.err != nil {
		return agentmanager.AgentRef{}, f.err
	}
	return agentmanager.AgentRef{Name: in.Name}, nil
}

func (f *fakeAMP) EnsureModelConfig(_ context.Context, in agentmanager.EnsureModelConfigInput) (agentmanager.ModelConfigRef, error) {
	f.calls++
	f.configIn = in
	if f.err != nil {
		return agentmanager.ModelConfigRef{}, f.err
	}
	return agentmanager.ModelConfigRef{ConfigID: "cfg", ProxyURL: f.proxyURL}, nil
}

func (f *fakeAMP) ListModelKeys(_ context.Context, in agentmanager.ModelKeyRef) ([]string, error) {
	f.calls++
	f.keyRef = in
	if f.err != nil {
		return nil, f.err
	}
	return f.keys, nil
}

func (f *fakeAMP) IssueModelKey(context.Context, agentmanager.ModelKeyRef, string) (agentmanager.IssuedKey, error) {
	f.calls++
	f.issued = true
	if f.err != nil {
		return agentmanager.IssuedKey{}, f.err
	}
	return agentmanager.IssuedKey{APIKey: "fresh-key", KeyID: "aep-checkout-agent-default"}, nil
}

func (f *fakeAMP) RotateModelKey(context.Context, agentmanager.ModelKeyRef, string) (agentmanager.IssuedKey, error) {
	f.calls++
	f.rotated = true
	if f.err != nil {
		return agentmanager.IssuedKey{}, f.err
	}
	return agentmanager.IssuedKey{APIKey: "rotated-key", KeyID: "aep-checkout-agent-default"}, nil
}

func (f *fakeAMP) IssueTracingToken(_ context.Context, in agentmanager.TracingTokenRef) (agentmanager.TracingToken, error) {
	f.calls++
	f.tracingMints++
	f.tracingRef = in
	if f.tracingErr != nil {
		return agentmanager.TracingToken{}, f.tracingErr
	}
	exp := f.tracingExpiry
	if exp == 0 {
		exp = time.Now().Add(90 * 24 * time.Hour).Unix()
	}
	return agentmanager.TracingToken{Token: "trace-jwt", ExpiresAt: exp}, nil
}

type fakeKeyStore struct {
	stored   string
	endpoint string
	writes   int
	err      error

	tracingToken  string
	tracingWrites int
}

func (f *fakeKeyStore) WriteAMPTracingToken(_ context.Context, _, _, _, token string) error {
	f.tracingWrites++
	f.tracingToken = token
	return nil
}

func (f *fakeKeyStore) StoredAMPModelKey(context.Context, string, string, string) (string, error) {
	return f.stored, nil
}

func (f *fakeKeyStore) WriteAMPModelKey(_ context.Context, _, _, _, _, proxyURL string) (string, string, error) {
	f.writes++
	f.endpoint = proxyURL
	if f.err != nil {
		return "", "", f.err
	}
	return "amp-model-checkout-agent-default", "api-key", nil
}

type fakeBindings struct{ err error }

func (f fakeBindings) GetAIGatewayBinding(context.Context, string, string) (openchoreo.AIGatewayBinding, error) {
	if f.err != nil {
		return openchoreo.AIGatewayBinding{}, f.err
	}
	return openchoreo.AIGatewayBinding{
		OrgID: "default", Environment: "default",
		Endpoint:  "http://ai-gateway.amp.localhost:8084",
		AdminURL:  "http://api.amp.localhost:8080/api/v1",
		GatewayID: "gw-uuid",
	}, nil
}

type fakeOrgKey struct{ key string }

func (f *fakeOrgKey) AnthropicKeyValue(context.Context, string) (string, error) {
	if f.key == "" {
		return "sk-ant-org", nil
	}
	if f.key == "none" {
		return "", nil
	}
	return f.key, nil
}

func input() delivery.GovernAgentInput {
	return delivery.GovernAgentInput{
		OrgID: "default", ProjectID: "shop",
		Component: "checkout-agent", Environment: "default",
	}
}

// --- tests -------------------------------------------------------------------

// The four states of the one-time key. Agent Manager returns a key value once,
// so every row but the first is a way back to a known state.
func TestGovernKeyStates(t *testing.T) {
	tests := []struct {
		name       string
		storedKey  string
		ampKeys    []string
		wantIssue  bool
		wantRotate bool
		wantWrites int
	}{
		{
			name:      "both present: reuse, never regenerate",
			storedKey: "amp-model-checkout-agent-default",
			ampKeys:   []string{AgentKeyName("checkout-agent", "default")},
		},
		{
			name:       "neither: issue and store",
			wantIssue:  true,
			wantWrites: 1,
		},
		{
			name:       "amp has it, we do not: rotate",
			ampKeys:    []string{AgentKeyName("checkout-agent", "default")},
			wantRotate: true,
			wantWrites: 1,
		},
		{
			name:       "we have it, amp does not: issue fresh",
			storedKey:  "amp-model-checkout-agent-default",
			wantIssue:  true,
			wantWrites: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			amp := newFakeAMP(tc.ampKeys...)
			store := &fakeKeyStore{stored: tc.storedKey}
			g := New(Deps{AMP: amp, Keys: store, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

			if _, err := g.GovernAgent(context.Background(), input()); err != nil {
				t.Fatalf("GovernAgent: %v", err)
			}
			if amp.issued != tc.wantIssue {
				t.Errorf("issued = %v, want %v", amp.issued, tc.wantIssue)
			}
			if amp.rotated != tc.wantRotate {
				t.Errorf("rotated = %v, want %v", amp.rotated, tc.wantRotate)
			}
			if store.writes != tc.wantWrites {
				t.Errorf("writes = %d, want %d", store.writes, tc.wantWrites)
			}
		})
	}
}

// An environment nobody provisioned for Agent Manager is the ordinary case on
// upgrade. It must cost zero AMP calls and must not fail the deploy.
func TestGovernSkipsWithoutBinding(t *testing.T) {
	amp := newFakeAMP()
	g := New(Deps{AMP: amp, Keys: &fakeKeyStore{},
		Bindings: fakeBindings{err: openchoreo.ErrNoAIGatewayBinding}, OrgKeys: &fakeOrgKey{}})

	out, err := g.GovernAgent(context.Background(), input())
	if err != nil {
		t.Fatalf("an environment with no AI gateway is not an error: %v", err)
	}
	if !out.Skipped {
		t.Errorf("outcome = %+v, want skipped", out)
	}
	if amp.calls != 0 {
		t.Errorf("made %d AMP calls with no binding; want none", amp.calls)
	}
}

// An org that has connected no Anthropic key has no provider to build. The
// agent deploys unconfigured and 503s, as it does today.
func TestGovernSkipsWithoutOrgKey(t *testing.T) {
	amp := newFakeAMP()
	g := New(Deps{AMP: amp, Keys: &fakeKeyStore{},
		Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{key: "none"}})

	out, err := g.GovernAgent(context.Background(), input())
	if err != nil {
		t.Fatalf("no org key is not an error: %v", err)
	}
	if !out.Skipped {
		t.Errorf("outcome = %+v, want skipped", out)
	}
	if amp.calls != 0 {
		t.Errorf("made %d AMP calls with no org key; want none", amp.calls)
	}
}

// A bound environment PROMISED governance. If Agent Manager cannot deliver it,
// the deploy must fail rather than quietly produce an ungoverned agent.
func TestGovernFailsClosedWhenAMPErrors(t *testing.T) {
	amp := newFakeAMP()
	amp.err = errors.New("amp down")
	g := New(Deps{AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

	if _, err := g.GovernAgent(context.Background(), input()); err == nil {
		t.Fatal("want an error: a bound environment must not deploy ungoverned")
	}
}

// A failed store leaves a key in AMP and nowhere else. The error must surface so
// the activity retries and lands in the rotate branch, rather than reporting
// success with no credential stored.
func TestGovernFailsWhenTheKeyCannotBeStored(t *testing.T) {
	amp := newFakeAMP()
	store := &fakeKeyStore{err: errors.New("vault down")}
	g := New(Deps{AMP: amp, Keys: store, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

	if _, err := g.GovernAgent(context.Background(), input()); err == nil {
		t.Fatal("want an error when the issued key cannot be persisted")
	}
}

type fakeKinds struct {
	kinds map[string]string
	err   error
}

func (f fakeKinds) ComponentKinds(context.Context, string, string) (map[string]string, error) {
	return f.kinds, f.err
}

// A deploy wave carries every component being promoted. Registering a service or
// a web app in Agent Manager would put a non-agent in the agent catalogue and
// mint it a model credential it has no use for.
func TestGovernOnlyGovernsAgents(t *testing.T) {
	amp := newFakeAMP()
	g := New(Deps{
		AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{},
		Kinds: fakeKinds{kinds: map[string]string{"checkout-agent": "service"}},
	})

	out, err := g.GovernAgent(context.Background(), input())
	if err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	if !out.Skipped {
		t.Errorf("outcome = %+v, want skipped for a non-agent", out)
	}
	if amp.calls != 0 {
		t.Errorf("made %d Agent Manager calls for a service component; want none", amp.calls)
	}
}

func TestGovernGovernsAnAIAgent(t *testing.T) {
	amp := newFakeAMP()
	g := New(Deps{
		AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{},
		Kinds: fakeKinds{kinds: map[string]string{"checkout-agent": "ai-agent"}},
	})

	out, err := g.GovernAgent(context.Background(), input())
	if err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	if out.Skipped {
		t.Fatalf("outcome = %+v, want governed", out)
	}
	if amp.calls == 0 {
		t.Error("governed an agent without calling Agent Manager")
	}
}

// An unreadable design must fail, not quietly govern nothing: the environment's
// binding promised governance, and skipping silently would deploy the agent on
// the org's raw key.
func TestGovernFailsWhenKindsCannotBeRead(t *testing.T) {
	g := New(Deps{
		AMP: newFakeAMP(), Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{},
		Kinds: fakeKinds{err: errors.New("design unavailable")},
	})
	if _, err := g.GovernAgent(context.Background(), input()); err == nil {
		t.Fatal("want an error when component kinds cannot be read")
	}
}

// The endpoint an agent is given must be its OWN proxy path, on the address a
// POD can reach, and a BASE the SDK can append to.
//
// Each of the three has already failed once. The proxy path is what makes the
// traffic attributable to this agent — the shared provider path is not. The
// in-cluster address matters because AMP answers with an outside-the-cluster
// one. And the Anthropic SDK requests `<base>/messages`, so a base missing the
// version segment produces `/aep-…/messages` and the gateway answers 404.
func TestGovernStoresThisAgentsOwnProxyEndpoint(t *testing.T) {
	store := &fakeKeyStore{}
	g := New(Deps{AMP: newFakeAMP(), Keys: store, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	want := "http://ai-gateway.amp.localhost:8084/aep-checkout-3d748f50/v1"
	if store.endpoint != want {
		t.Errorf("stored endpoint = %q, want %q", store.endpoint, want)
	}
}

// A binding with no proxy URL must fail rather than store a bare gateway
// address: the agent would reach the gateway, match no proxy, and the only
// symptom would be a 404 at the first turn.
func TestGovernFailsWhenAMPReturnsNoProxyURL(t *testing.T) {
	amp := newFakeAMP()
	amp.proxyURL = ""
	g := New(Deps{AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

	if _, err := g.GovernAgent(context.Background(), input()); err == nil {
		t.Fatal("want an error when Agent Manager returns no proxy URL")
	}
}

// EnsureRegistration is the BUILD-TIME entry point and must not touch the key.
//
// A rotate at planning time cuts off the agent that is currently running for
// the whole coding cycle — and permanently if the run then fails. This is the
// test that stops someone "simplifying" the two entry points back into one.
func TestEnsureRegistrationNeverTouchesTheKey(t *testing.T) {
	// The rotate-provoking state: Agent Manager holds this agent's key and AEP
	// cannot read its value. On the deploy path this rotates.
	amp := newFakeAMP(AgentKeyName("checkout-agent", "default"))
	store := &fakeKeyStore{}
	g := New(Deps{AMP: amp, Keys: store, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

	out, err := g.EnsureRegistration(context.Background(), input())
	if err != nil {
		t.Fatalf("EnsureRegistration: %v", err)
	}
	if out.Skipped {
		t.Fatalf("outcome = %+v, want registered", out)
	}
	if amp.issued || amp.rotated {
		t.Errorf("issued=%v rotated=%v — registration must not mint or rotate a credential", amp.issued, amp.rotated)
	}
	if store.writes != 0 {
		t.Errorf("wrote %d keys; registration stores none", store.writes)
	}
	// The gate names the proxy on its ticket, so the address has to come back.
	if out.ProxyURL != "http://ai-gateway.amp.localhost:8084/aep-checkout-3d748f50/v1" {
		t.Errorf("ProxyURL = %q", out.ProxyURL)
	}
}

// The same state on the DEPLOY path must still rotate — the split must not have
// quietly disabled the recovery branch.
func TestGovernAgentStillRotatesOnTheDeployPath(t *testing.T) {
	amp := newFakeAMP(AgentKeyName("checkout-agent", "default"))
	store := &fakeKeyStore{}
	g := New(Deps{AMP: amp, Keys: store, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	if !amp.rotated || store.writes != 1 {
		t.Errorf("rotated=%v writes=%d, want a rotation stored once", amp.rotated, store.writes)
	}
}

// An agent's record name must be unique across the ORG, because Agent Manager
// stores it as an OpenChoreo Component and a Component name is unique per
// NAMESPACE — one namespace per org, with the project only a label.
//
// Two projects each holding a `library-agent` is not hypothetical: it happened,
// and the second registration silently resolved to the first project's record.
// The agent then appeared under the wrong project, and the guardrail a human
// attaches is attached on that page.
func TestAgentRecordNameIsUniquePerOrgNotPerProject(t *testing.T) {
	a := AgentRecordName("very-agent-give", "library-agent")
	b := AgentRecordName("agent-help-see", "library-agent")
	if a == b {
		t.Fatalf("two projects' same-named agents share a record name: %q", a)
	}
	// `<project>-<component>` is what AEP names the agent's own workload
	// Component, so the record must not take that name either.
	for _, name := range []string{a, b} {
		if name == "very-agent-give-library-agent" || name == "agent-help-see-library-agent" {
			t.Errorf("record name %q collides with the workload Component's name", name)
		}
	}
}

// Agent Manager caps an agent's name at 25 characters and answers 400 above it
// — which fails the whole deploy, not just the registration. The first version
// of this naming shipped `<project>-<component>` and produced 38.
func TestAgentRecordNameFitsAgentManagersCap(t *testing.T) {
	for _, tc := range []struct{ project, component string }{
		{"very-book-search", "book-search-agent"},
		{"agent-help-see", "library-agent"},
		{"a", "b"},
		{strings.Repeat("long-project", 5), strings.Repeat("long-component", 5)},
	} {
		got := AgentRecordName(tc.project, tc.component)
		if len(got) > maxAgentRecordName {
			t.Errorf("AgentRecordName(%q, %q) = %q (%d chars), want <= %d",
				tc.project, tc.component, got, len(got), maxAgentRecordName)
		}
		if strings.Contains(got, "--") || strings.HasSuffix(got, "-") {
			t.Errorf("name %q is not a legal DNS-1035 label", got)
		}
	}
}

// Truncation must not merge two long components in ONE project — which is why
// the hash covers the pair rather than the project alone.
func TestAgentRecordNameSeparatesLongComponentsInOneProject(t *testing.T) {
	a := AgentRecordName("shop", "a-very-long-agent-name-one")
	b := AgentRecordName("shop", "a-very-long-agent-name-two")
	if a == b {
		t.Fatalf("two long components in one project share a record name: %q", a)
	}
}

// The name is DETERMINISTIC: a redeploy must resolve to the record it made last
// time, not mint a second one beside it.
func TestAgentRecordNameIsStable(t *testing.T) {
	if AgentRecordName("shop", "checkout-agent") != AgentRecordName("shop", "checkout-agent") {
		t.Fatal("AgentRecordName is not deterministic")
	}
}

// The record carries the unique name; the console-facing name stays friendly.
func TestGovernRegistersWithTheUniqueNameAndAFriendlyDisplayName(t *testing.T) {
	amp := newFakeAMP()
	g := New(Deps{AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	if want := AgentRecordName("shop", "checkout-agent"); amp.agentIn.Name != want {
		t.Errorf("registered name = %q, want %q", amp.agentIn.Name, want)
	}
	if amp.agentIn.DisplayName != "checkout-agent" {
		t.Errorf("displayName = %q, want the plain component name", amp.agentIn.DisplayName)
	}
}

// THE AGENT RECORD, ITS BINDING AND ITS KEYS MUST SHARE ONE NAME.
//
// The binding lives at …/agents/{agent}/model-configs and the keys under it, so
// registering the record as X while ensuring the binding under Y leaves X empty
// and hangs the binding off whatever else answers to Y. The first version of
// the rename did exactly that: the new record had no bindings, the pod kept
// calling the old record's proxy, and a guardrail attached to the record the
// console showed would have governed nothing.
func TestGovernAddressesRecordBindingAndKeyByTheSameName(t *testing.T) {
	amp := newFakeAMP()
	g := New(Deps{AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	want := AgentRecordName("shop", "checkout-agent")
	if amp.agentIn.Name != want {
		t.Errorf("registered record as %q, want %q", amp.agentIn.Name, want)
	}
	if amp.configIn.Agent != want {
		t.Errorf("ensured the binding under agent %q, want %q — a binding under a "+
			"different name than the record hangs off nothing", amp.configIn.Agent, want)
	}
	if amp.keyRef.Agent != want {
		t.Errorf("addressed keys under agent %q, want %q", amp.keyRef.Agent, want)
	}
}

// Re-asserting an UNCHANGED org key is not free: updating a provider redeploys
// every LLM proxy bound to it — twelve per governed deploy was measured in a
// single-agent org — and a redeploy is the window in which a proxy can lose the
// API keys broadcast to it. So the key is written when it CHANGES, not on every
// deploy and every converge tick.
func TestGovernReassertsTheOrgKeyOnlyWhenItChanges(t *testing.T) {
	amp := newFakeAMP()
	keys := &fakeOrgKey{key: "sk-ant-one"}
	g := New(Deps{AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, OrgKeys: keys})

	// First deploy of this process: nothing is known, so assert it.
	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("first: %v", err)
	}
	if !amp.providerIn.ReassertCredential {
		t.Error("first deploy did not assert the org key; a restart must re-establish it")
	}

	// Same key again — a redeploy, or one of the converge sweep's ticks.
	amp.providerIn = agentmanager.EnsureProviderInput{}
	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("second: %v", err)
	}
	if amp.providerIn.ReassertCredential {
		t.Error("re-asserted an unchanged key — every proxy in the org redeploys for nothing")
	}

	// The org rotates its key.
	keys.key = "sk-ant-two"
	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("after rotation: %v", err)
	}
	if !amp.providerIn.ReassertCredential {
		t.Error("a rotated key was not pushed — every governed agent in the org fails at Anthropic")
	}
}

// The fingerprint map must never hold the credential itself.
func TestGovernorRemembersAFingerprintNotTheKey(t *testing.T) {
	g := New(Deps{})
	g.credentialChanged("acme", "sk-ant-secret-value")
	for org, fp := range g.pushedKey {
		if strings.Contains(fp, "sk-ant") {
			t.Errorf("pushedKey[%q] = %q — that is the key, not a fingerprint", org, fp)
		}
	}
}

// --- tracing token ------------------------------------------------------------

// The deploy path mints the agent's OTLP credential and stores it beside the
// model key, with the endpoint it authenticates against — the same "useless
// apart" reasoning that puts the proxy URL in that secret.
func TestGovernMintsAndStoresTheTracingToken(t *testing.T) {
	amp, keys := newFakeAMP(), &fakeKeyStore{}
	g := New(Deps{AMP: amp, Keys: keys, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	if keys.tracingToken != "trace-jwt" {
		t.Errorf("stored tracing token = %q, want the minted one", keys.tracingToken)
	}
	// Issued against the AGENT record, not the model binding — the two are
	// addressed by the same name but by different calls.
	if amp.tracingRef.Agent != amp.agentIn.Name {
		t.Errorf("tracing token issued for %q but the agent registered as %q",
			amp.tracingRef.Agent, amp.agentIn.Name)
	}
	if amp.tracingRef.Environment != "default" {
		t.Errorf("tracing ref environment = %q", amp.tracingRef.Environment)
	}
}

// Converge re-runs the deploy path on a schedule. A token minted every sweep
// would write the agent's secret forever — the same needless-write cost that
// motivated the provider fingerprint.
func TestGovernMintsTheTracingTokenOnceWhileItIsLive(t *testing.T) {
	amp, keys := newFakeAMP("aep-checkout-agent-default"), &fakeKeyStore{stored: "amp-model-checkout-agent-default"}
	g := New(Deps{AMP: amp, Keys: keys, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

	for range 3 {
		if _, err := g.GovernAgent(context.Background(), input()); err != nil {
			t.Fatalf("GovernAgent: %v", err)
		}
	}
	if amp.tracingMints != 1 {
		t.Errorf("tracing mints = %d across three converges, want 1", amp.tracingMints)
	}
	if keys.tracingWrites != 1 {
		t.Errorf("tracing writes = %d, want 1", keys.tracingWrites)
	}
}

// A token that is about to expire is worth replacing while a deploy is in
// flight to carry it: the agent goes quiet the moment it lapses, and nothing
// reports that.
func TestGovernReMintsTheTracingTokenAsItNearsExpiry(t *testing.T) {
	amp, keys := newFakeAMP("aep-checkout-agent-default"), &fakeKeyStore{stored: "amp-model-checkout-agent-default"}
	amp.tracingExpiry = time.Now().Add(24 * time.Hour).Unix()
	g := New(Deps{AMP: amp, Keys: keys, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

	for range 2 {
		if _, err := g.GovernAgent(context.Background(), input()); err != nil {
			t.Fatalf("GovernAgent: %v", err)
		}
	}
	if amp.tracingMints != 2 {
		t.Errorf("tracing mints = %d, want a re-mint inside the refresh window", amp.tracingMints)
	}
}

// The build-time gate holds no rollout. Everything it does must be safe to
// repeat with no deploy behind it, which is why it settles registration only.
func TestEnsureRegistrationNeverMintsATracingToken(t *testing.T) {
	amp, keys := newFakeAMP(), &fakeKeyStore{}
	g := New(Deps{AMP: amp, Keys: keys, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

	if _, err := g.EnsureRegistration(context.Background(), input()); err != nil {
		t.Fatalf("EnsureRegistration: %v", err)
	}
	if amp.tracingMints != 0 {
		t.Errorf("tracing mints = %d on the gate path, want 0", amp.tracingMints)
	}
}

// TRACING IS NOT MODEL ACCESS. Without a model key the agent cannot answer at
// all, so the deploy fails closed. Without a tracing token it runs correctly
// and is merely unobserved — failing the deploy would trade a working agent for
// a missing graph.
func TestGovernSurvivesATracingTokenFailure(t *testing.T) {
	amp, keys := newFakeAMP(), &fakeKeyStore{}
	amp.tracingErr = errors.New("403 insufficient permissions")
	g := New(Deps{AMP: amp, Keys: keys, Bindings: fakeBindings{}, OrgKeys: &fakeOrgKey{}})

	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("GovernAgent failed on a tracing error: %v", err)
	}
	if keys.writes != 1 {
		t.Errorf("model key writes = %d, want the model key still stored", keys.writes)
	}
}
