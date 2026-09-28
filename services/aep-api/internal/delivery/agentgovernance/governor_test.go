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
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// --- fakes -------------------------------------------------------------------

// proxyURL is what Agent Manager generated for this agent's binding. The
// default is set by newFakeAMP; a zero value stands for "AMP answered without
// one".
type fakeAMP struct {
	providerIn agentmanager.EnsureProviderInput
	// puts is every provider write that re-asserted the connection — what
	// reaches Agent Manager as a PUT on an existing provider.
	puts     []agentmanager.EnsureProviderInput
	agentIn  agentmanager.EnsureAgentInput
	configIn agentmanager.EnsureModelConfigInput
	keyRef   agentmanager.ModelKeyRef
	proxyURL string
	keys     []string
	issued   bool
	rotated  bool
	calls    int
	err      error

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

// UpdateProviderCredential is the organization domain's write, never the
// governor's: a deploy that reached it would be a regression.
func (f *fakeAMP) UpdateProviderCredential(context.Context, agentmanager.EnsureProviderInput) (bool, error) {
	panic("the governor must not write a provider credential outside EnsureProvider")
}

func (f *fakeAMP) EnsureProvider(_ context.Context, in agentmanager.EnsureProviderInput) (agentmanager.ProviderRef, error) {
	f.calls++
	f.providerIn = in
	if in.ReassertCredential {
		f.puts = append(f.puts, in)
	}
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

// fakeEndpoints is the durable record of the endpoint each key was stored
// beside, keyed like the table (org/component/environment). Shared between two
// governors it stands for two replicas over one database.
type fakeEndpoints struct {
	rows      map[string]string
	recordErr error
}

// knownEndpoint is what the default fakes compose for checkout-agent on
// Anthropic's API.
const knownEndpoint = "http://ai-gateway.amp.localhost:8084/aep-checkout-3d748f50/v1"

// recorded is a store that already holds checkout-agent's endpoint.
func recorded(endpoint string) *fakeEndpoints {
	return &fakeEndpoints{rows: map[string]string{"default/checkout-agent/default": endpoint}}
}

func (f *fakeEndpoints) StoredAMPModelEndpoint(_ context.Context, org, component, env string) (string, bool, error) {
	e, ok := f.rows[org+"/"+component+"/"+env]
	return e, ok, nil
}

func (f *fakeEndpoints) RecordAMPModelEndpoint(_ context.Context, org, component, env, endpoint string) error {
	if f.recordErr != nil {
		return f.recordErr
	}
	if f.rows == nil {
		f.rows = map[string]string{}
	}
	f.rows[org+"/"+component+"/"+env] = endpoint
	return nil
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

// fakeConnections is the org's model connection. The zero value is Anthropic's
// own API with a key; none stands for an org that has connected nothing.
type fakeConnections struct {
	conn *modelconn.Connection
	key  string
	none bool
}

func (f *fakeConnections) Effective(context.Context, string) (modelconn.Connection, string, bool, error) {
	if f.none {
		return modelconn.Connection{}, "", false, nil
	}
	conn, key := firstPartyConn(), "sk-ant-org"
	if f.conn != nil {
		conn = *f.conn
	}
	if f.key != "" {
		key = f.key
	}
	return conn, key, true, nil
}

func firstPartyConn() modelconn.Connection {
	return modelconn.Connection{
		Format: modelconn.FormatAnthropic, BaseURL: "https://api.anthropic.com/v1",
		Host: modelconn.AnthropicHost, Model: "claude-sonnet-5", AuthScheme: modelconn.AuthXAPIKey,
	}
}

func ollamaOpenAIConn() modelconn.Connection {
	return modelconn.Connection{
		Format: modelconn.FormatOpenAICompatible, BaseURL: "https://ollama.com/v1",
		Host: modelconn.OllamaHost, Model: "gpt-oss:20b", AuthScheme: modelconn.AuthBearer,
	}
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
			g := New(Deps{Endpoints: recorded(knownEndpoint), AMP: amp, Keys: store, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

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
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{},
		Bindings: fakeBindings{err: openchoreo.ErrNoAIGatewayBinding}, Connections: &fakeConnections{}})

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

// An org that has connected no model has no provider to build. The agent
// deploys unconfigured and 503s.
func TestGovernSkipsWithoutAConnection(t *testing.T) {
	amp := newFakeAMP()
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{},
		Bindings: fakeBindings{}, Connections: &fakeConnections{none: true}})

	out, err := g.GovernAgent(context.Background(), input())
	if err != nil {
		t.Fatalf("no connection is not an error: %v", err)
	}
	if !out.Skipped {
		t.Errorf("outcome = %+v, want skipped", out)
	}
	if amp.calls != 0 {
		t.Errorf("made %d AMP calls with no connection; want none", amp.calls)
	}
}

// A bound environment PROMISED governance. If Agent Manager cannot deliver it,
// the deploy must fail rather than quietly produce an ungoverned agent.
func TestGovernFailsClosedWhenAMPErrors(t *testing.T) {
	amp := newFakeAMP()
	amp.err = errors.New("amp down")
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

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
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: store, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

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
		Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, Connections: &fakeConnections{},
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
		Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, Connections: &fakeConnections{},
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
		Endpoints: &fakeEndpoints{}, AMP: newFakeAMP(), Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, Connections: &fakeConnections{},
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
// connection's `/v1` produces `/aep-…/messages` and the gateway answers 404.
func TestGovernStoresThisAgentsOwnProxyEndpoint(t *testing.T) {
	store := &fakeKeyStore{}
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: newFakeAMP(), Keys: store, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

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
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

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
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: store, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

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
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: store, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

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
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

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
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

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

// The provider is written when the connection it is built from CHANGES, not
// on every deploy and every converge tick (EnsureProviderInput.ReassertCredential),
// and a change to any one of key, upstream, template or header is one write
// carrying all four.
func TestGovernReassertsTheProviderOnlyWhenTheConnectionChanges(t *testing.T) {
	anthropicOnOllama := modelconn.Connection{
		Format: modelconn.FormatAnthropic, BaseURL: "https://ollama.com",
		Host: modelconn.OllamaHost, Model: "gpt-oss:20b", AuthScheme: modelconn.AuthBearer,
	}
	bearerFirstParty := firstPartyConn()
	bearerFirstParty.AuthScheme = modelconn.AuthBearer
	otherHost := firstPartyConn()
	otherHost.BaseURL, otherHost.Host = "https://anthropic.example.com/v1", "anthropic.example.com"

	for _, tc := range []struct {
		name  string
		next  modelconn.Connection
		key   string
		field func(agentmanager.EnsureProviderInput) string
		want  string
	}{
		{"key", firstPartyConn(), "sk-ant-two",
			func(in agentmanager.EnsureProviderInput) string { return in.APIKey }, "sk-ant-two"},
		{"upstream", otherHost, "",
			func(in agentmanager.EnsureProviderInput) string { return in.UpstreamURL }, "https://anthropic.example.com"},
		{"template", ollamaOpenAIConn(), "",
			func(in agentmanager.EnsureProviderInput) string { return in.Template }, "openai"},
		{"header", bearerFirstParty, "",
			func(in agentmanager.EnsureProviderInput) string { return in.AuthHeader }, "Authorization"},
		{"template on the same upstream", anthropicOnOllama, "",
			func(in agentmanager.EnsureProviderInput) string { return in.Template }, "anthropic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			amp := newFakeAMP()
			conns := &fakeConnections{}
			g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, Connections: conns})

			// First deploy of this process: nothing is known, so assert it.
			if _, err := g.GovernAgent(context.Background(), input()); err != nil {
				t.Fatalf("first: %v", err)
			}
			if len(amp.puts) != 1 {
				t.Fatalf("first deploy wrote the provider %d time(s); a restart must re-establish it once", len(amp.puts))
			}

			// Same connection again — a redeploy, or a converge tick.
			if _, err := g.GovernAgent(context.Background(), input()); err != nil {
				t.Fatalf("second: %v", err)
			}
			if len(amp.puts) != 1 {
				t.Fatal("re-asserted an unchanged connection — every proxy in the org redeploys for nothing")
			}

			// The org changes one thing.
			next := tc.next
			conns.conn, conns.key = &next, tc.key
			if _, err := g.GovernAgent(context.Background(), input()); err != nil {
				t.Fatalf("after the change: %v", err)
			}
			if len(amp.puts) != 2 {
				t.Fatalf("provider writes = %d after a %s change, want exactly one more", len(amp.puts), tc.name)
			}
			put := amp.puts[1]
			if got := tc.field(put); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
			}
			want, err := ProviderInputFor("default", next, keyOr(tc.key, "sk-ant-org"), "gw-uuid")
			if err != nil {
				t.Fatalf("ProviderInputFor: %v", err)
			}
			want.ReassertCredential = true
			if put != want {
				t.Errorf("the PUT carries\n %+v\nwant all four from the new connection\n %+v", put, want)
			}
		})
	}
}

func keyOr(key, fallback string) string {
	if key != "" {
		return key
	}
	return fallback
}

// The fingerprint lives in memory, so the first deploy after an upgrade (or
// any restart) writes the provider once for an org whose provider already
// matches. That write must be the same body every time — a PUT of what is
// already there — so it redeploys the proxies once and changes nothing else.
func TestGovernTheFirstWriteAfterARestartIsTheSameBody(t *testing.T) {
	var puts []agentmanager.EnsureProviderInput
	for range 2 {
		amp := newFakeAMP()
		g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, Connections: &fakeConnections{}})
		if _, err := g.GovernAgent(context.Background(), input()); err != nil {
			t.Fatalf("GovernAgent: %v", err)
		}
		if len(amp.puts) != 1 {
			t.Fatalf("a fresh process wrote the provider %d time(s), want once", len(amp.puts))
		}
		puts = append(puts, amp.puts[0])
	}
	if puts[0] != puts[1] {
		t.Errorf("two restarts wrote different providers:\n %+v\n %+v", puts[0], puts[1])
	}
}

// A provider write that failed is not remembered: the next deploy writes the
// provider again, or Agent Manager keeps the old upstream, template and header
// until the process restarts.
func TestGovernRetriesAProviderWriteThatFailed(t *testing.T) {
	amp := newFakeAMP()
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

	amp.err = errors.New("agent manager: 503")
	if _, err := g.GovernAgent(context.Background(), input()); err == nil {
		t.Fatal("GovernAgent succeeded over a failed provider write")
	}
	amp.err = nil
	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(amp.puts) != 2 || !amp.providerIn.ReassertCredential {
		t.Fatalf("provider writes = %d, last reassert = %v; the retry must re-send the provider",
			len(amp.puts), amp.providerIn.ReassertCredential)
	}

	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("third: %v", err)
	}
	if len(amp.puts) != 2 {
		t.Error("re-asserted after the successful retry — the success must be remembered")
	}
}

// The fingerprint map must never hold the credential itself.
func TestGovernorRemembersAFingerprintNotTheKey(t *testing.T) {
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: newFakeAMP(), Keys: &fakeKeyStore{}, Bindings: fakeBindings{},
		Connections: &fakeConnections{key: "sk-ant-secret-value"}})
	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	if len(g.providerFingerprint) != 1 {
		t.Fatalf("remembered %d provider(s), want 1", len(g.providerFingerprint))
	}
	for org, fp := range g.providerFingerprint {
		if strings.Contains(fp, "sk-ant") {
			t.Errorf("providerFingerprint[%q] = %q — that is the key, not a fingerprint", org, fp)
		}
	}
}

// --- the connection's base path ------------------------------------------------

// The provider's upstream is the connection's origin, so the base path the host
// serves under goes on the agent's endpoint — or the gateway asks for
// `/messages` (404), or with the path on both sides `/v1/v1/…` (404).
func TestProxyEndpointEndsInTheConnectionsBasePath(t *testing.T) {
	const proxy = "http://ai-gateway.amp.localhost:8084/aep-checkout-3d748f50"
	const gateway = "http://gw.internal:8084/"
	for _, tc := range []struct{ baseURL, want string }{
		{"https://api.anthropic.com/v1", "http://gw.internal:8084/aep-checkout-3d748f50/v1"},
		{"https://openrouter.ai/api/v1", "http://gw.internal:8084/aep-checkout-3d748f50/api/v1"},
		{"https://dashscope-intl.aliyuncs.com/compatible-mode/v1", "http://gw.internal:8084/aep-checkout-3d748f50/compatible-mode/v1"},
		{"https://ollama.com", "http://gw.internal:8084/aep-checkout-3d748f50"},
		{"https://ollama.com/v1/", "http://gw.internal:8084/aep-checkout-3d748f50/v1"},
	} {
		got, err := proxyEndpoint(proxy, gateway, tc.baseURL)
		if err != nil {
			t.Fatalf("proxyEndpoint(%q): %v", tc.baseURL, err)
		}
		if got != tc.want {
			t.Errorf("proxyEndpoint(%q) = %q, want %q", tc.baseURL, got, tc.want)
		}
	}
}

// A switch that moves the base path reaches an agent whose key both sides
// already hold: its stored URL would ask the new upstream for the old path. The
// URL cannot be rewritten without the key, so the key rotates and both are
// stored together, on the deploy path only. The record is durable, so the
// deploy after the switch may land on a replica that never stored the key.
func TestGovernRewritesAStoredEndpointWhenTheBasePathMoves(t *testing.T) {
	amp := newFakeAMP()
	store := &fakeKeyStore{}
	endpoints := &fakeEndpoints{}
	conns := &fakeConnections{}
	replica := func() *Governor {
		return New(Deps{Endpoints: endpoints, AMP: amp, Keys: store, Bindings: fakeBindings{}, Connections: conns})
	}

	// Deploy on Anthropic's API: issued, stored and recorded with /v1.
	if _, err := replica().GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("first: %v", err)
	}
	if !amp.issued || store.endpoint != knownEndpoint || endpoints.rows["default/checkout-agent/default"] != knownEndpoint {
		t.Fatalf("issued=%v endpoint=%q record=%v", amp.issued, store.endpoint, endpoints.rows)
	}
	// Both sides now hold it.
	amp.keys, store.stored = []string{AgentKeyName("checkout-agent", "default")}, "amp-model-checkout-agent-default"

	// A redeploy on the same connection, on another replica, reuses it.
	if _, err := replica().GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("redeploy: %v", err)
	}
	if amp.rotated || store.writes != 1 {
		t.Fatalf("rotated=%v writes=%d on an unchanged endpoint, want reuse", amp.rotated, store.writes)
	}

	// The org switches to a host under /compatible-mode/v1; the next deploy
	// lands on yet another replica.
	conns.conn = &modelconn.Connection{
		Format: modelconn.FormatOpenAICompatible, BaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
		Host: "dashscope-intl.aliyuncs.com", Model: "qwen-plus", AuthScheme: modelconn.AuthBearer,
	}
	conns.key = "sk-dashscope-fake"
	out, err := replica().GovernAgent(context.Background(), input())
	if err != nil {
		t.Fatalf("after the switch: %v", err)
	}
	want := "http://ai-gateway.amp.localhost:8084/aep-checkout-3d748f50/compatible-mode/v1"
	if !amp.rotated || store.writes != 2 || store.endpoint != want {
		t.Fatalf("rotated=%v writes=%d endpoint=%q, want one rotation storing %q", amp.rotated, store.writes, store.endpoint, want)
	}
	if endpoints.rows["default/checkout-agent/default"] != want {
		t.Errorf("record = %q, want %q", endpoints.rows["default/checkout-agent/default"], want)
	}
	if out.ProxyURL != want {
		t.Errorf("ProxyURL = %q, want %q", out.ProxyURL, want)
	}

	// And the next redeploy reuses the new one.
	amp.rotated = false
	if _, err := replica().GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("redeploy after the switch: %v", err)
	}
	if amp.rotated || store.writes != 2 {
		t.Errorf("rotated=%v writes=%d after the endpoint settled, want reuse", amp.rotated, store.writes)
	}
}

// An agent whose key has no recorded endpoint holds it beside a URL nothing
// can read. Unknown is treated as moved: one rotation on its next
// deploy, which records the endpoint, and reuse from then on.
func TestGovernRotatesOnceWhenNoEndpointWasRecorded(t *testing.T) {
	amp := newFakeAMP(AgentKeyName("checkout-agent", "default"))
	store := &fakeKeyStore{stored: "amp-model-checkout-agent-default"}
	endpoints := &fakeEndpoints{}
	g := New(Deps{Endpoints: endpoints, AMP: amp, Keys: store, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

	for range 2 {
		if _, err := g.GovernAgent(context.Background(), input()); err != nil {
			t.Fatalf("GovernAgent: %v", err)
		}
	}
	if !amp.rotated || amp.issued || store.writes != 1 {
		t.Errorf("rotated=%v issued=%v writes=%d, want exactly one rotation", amp.rotated, amp.issued, store.writes)
	}
	if endpoints.rows["default/checkout-agent/default"] != knownEndpoint {
		t.Errorf("record = %v, want the stored endpoint", endpoints.rows)
	}
}

// The record follows the secret, never precedes it; a record that fails fails
// the deploy so the retry rotates again rather than leaving an unrecorded key.
func TestGovernFailsWhenTheEndpointCannotBeRecorded(t *testing.T) {
	store := &fakeKeyStore{}
	g := New(Deps{Endpoints: &fakeEndpoints{recordErr: errors.New("db down")}, AMP: newFakeAMP(), Keys: store,
		Bindings: fakeBindings{}, Connections: &fakeConnections{}})

	if _, err := g.GovernAgent(context.Background(), input()); err == nil {
		t.Fatal("want an error when the stored endpoint cannot be recorded")
	}
	if store.writes != 1 {
		t.Errorf("secret writes = %d, want the key stored before the record", store.writes)
	}
}

// EnsureRegistration never reaches the key, so a moved endpoint is left for the
// deploy that follows — the same reason it never rotates.
func TestEnsureRegistrationLeavesAMovedEndpointToTheDeploy(t *testing.T) {
	amp := newFakeAMP()
	store := &fakeKeyStore{}
	conns := &fakeConnections{}
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: store, Bindings: fakeBindings{}, Connections: conns})
	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("first: %v", err)
	}
	amp.keys, store.stored = []string{AgentKeyName("checkout-agent", "default")}, "amp-model-checkout-agent-default"
	next := ollamaOpenAIConn()
	next.BaseURL = "https://ollama.com/api/v1"
	conns.conn = &next

	if _, err := g.EnsureRegistration(context.Background(), input()); err != nil {
		t.Fatalf("EnsureRegistration: %v", err)
	}
	if amp.rotated || store.writes != 1 {
		t.Errorf("rotated=%v writes=%d at planning, want none", amp.rotated, store.writes)
	}
}

// --- the provider, from the connection ------------------------------------------

// For Anthropic's own API the provider keeps the `anthropic` template,
// upstream, x-api-key auth and ID that existing providers carry; only the
// display name is format-neutral. The ID is ProviderID's.
func TestProviderInputForAnthropicsOwnAPIIsTodaysProvider(t *testing.T) {
	got, err := ProviderInputFor("default", firstPartyConn(), "sk-ant-api03-FAKE-golden-key", "gw-1")
	if err != nil {
		t.Fatalf("ProviderInputFor: %v", err)
	}
	want := agentmanager.EnsureProviderInput{
		Org: "default", ID: "aep-default-anthropic", Name: "AEP default model connection",
		Version: "v1.0", Context: "/aep-default-anthropic", Template: "anthropic",
		UpstreamURL: "https://api.anthropic.com", AuthType: "api-key", AuthHeader: "x-api-key",
		APIKey: "sk-ant-api03-FAKE-golden-key", GatewayID: "gw-1",
	}
	if got != want {
		t.Errorf("provider\n %+v\nwant\n %+v", got, want)
	}
}

func TestProviderInputForAnOpenAICompatibleConnection(t *testing.T) {
	got, err := ProviderInputFor("default", ollamaOpenAIConn(), "fake-ollama-key", "gw-1")
	if err != nil {
		t.Fatalf("ProviderInputFor: %v", err)
	}
	if got.Template != "openai" || got.UpstreamURL != "https://ollama.com" ||
		got.AuthHeader != "Authorization" || got.APIKey != "Bearer fake-ollama-key" || got.AuthType != "api-key" {
		t.Errorf("provider = %+v", got)
	}
	if got.ID != "aep-default-anthropic" {
		t.Errorf("ID = %q; the provider keeps its handle across formats", got.ID)
	}
}

// A connection the provider cannot be built from is an error, never a provider
// with an empty template or upstream that answers far from the cause.
func TestProviderInputForRefusesWhatItCannotBuild(t *testing.T) {
	for name, mutate := range map[string]func(*modelconn.Connection){
		"unknown format":      func(c *modelconn.Connection) { c.Format = "gemini" },
		"unknown auth scheme": func(c *modelconn.Connection) { c.AuthScheme = "" },
		"no host":             func(c *modelconn.Connection) { c.BaseURL = "/v1" },
	} {
		t.Run(name, func(t *testing.T) {
			c := firstPartyConn()
			mutate(&c)
			if _, err := ProviderInputFor("default", c, "k", "gw"); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

// --- tracing token ------------------------------------------------------------

// The deploy path mints the agent's OTLP credential and stores it beside the
// model key, with the endpoint it authenticates against — the same "useless
// apart" reasoning that puts the proxy URL in that secret.
func TestGovernMintsAndStoresTheTracingToken(t *testing.T) {
	amp, keys := newFakeAMP(), &fakeKeyStore{}
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: keys, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

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
	g := New(Deps{Endpoints: recorded(knownEndpoint), AMP: amp, Keys: keys, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

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
	g := New(Deps{Endpoints: recorded(knownEndpoint), AMP: amp, Keys: keys, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

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
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: keys, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

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
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: keys, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("GovernAgent failed on a tracing error: %v", err)
	}
	if keys.writes != 1 {
		t.Errorf("model key writes = %d, want the model key still stored", keys.writes)
	}
}
