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

package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery/agentgovernance"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// providerClient records the provider writes; nothing else is used.
type providerClient struct {
	agentmanager.Client
	exists  bool
	updates []agentmanager.EnsureProviderInput
	ensured []agentmanager.EnsureProviderInput
}

func (c *providerClient) EnsureProvider(_ context.Context, in agentmanager.EnsureProviderInput) (agentmanager.ProviderRef, error) {
	c.ensured = append(c.ensured, in)
	return agentmanager.ProviderRef{}, nil
}

func (c *providerClient) UpdateProviderCredential(_ context.Context, in agentmanager.EnsureProviderInput) (bool, error) {
	c.updates = append(c.updates, in)
	return c.exists, nil
}

type providerClients struct{ client *providerClient }

func (f providerClients) For(string) agentmanager.Client { return f.client }

type oneBinding struct{}

func (oneBinding) GetAIGatewayBinding(context.Context, string, string) (openchoreo.AIGatewayBinding, error) {
	return openchoreo.AIGatewayBinding{AdminURL: "http://amp.example", GatewayID: "gw-1"}, nil
}

// fakeOrgTargets returns a fixed set of write targets for the org.
type fakeOrgTargets struct {
	envs       []string
	unresolved map[string]error
	err        error
}

func (f fakeOrgTargets) OrgWriteTargets(context.Context, string) ([]string, map[string]error, error) {
	return f.envs, f.unresolved, f.err
}

// envBindings answers per environment; an absent environment has no binding.
type envBindings struct {
	byEnv map[string]openchoreo.AIGatewayBinding
	reads []string
}

func (b *envBindings) GetAIGatewayBinding(_ context.Context, _, env string) (openchoreo.AIGatewayBinding, error) {
	b.reads = append(b.reads, env)
	binding, ok := b.byEnv[env]
	if !ok {
		return openchoreo.AIGatewayBinding{}, openchoreo.ErrNoAIGatewayBinding
	}
	return binding, nil
}

func oneTarget() fakeOrgTargets { return fakeOrgTargets{envs: []string{"development"}} }

func ollamaConnection() modelconn.Connection {
	return modelconn.Connection{
		Format: modelconn.FormatOpenAICompatible, BaseURL: "https://ollama.com/v1",
		Host: modelconn.OllamaHost, Model: "gpt-oss:20b", AuthScheme: modelconn.AuthBearer,
	}
}

// A saved connection reaches the provider on any format: one write carrying
// the template, the upstream, the auth and the key.
func TestAMPModelProviderPublisher_PublishesTheConnection(t *testing.T) {
	client := &providerClient{exists: true}
	pub := ampModelProviderPublisher{amp: providerClients{client}, bindings: oneBinding{}, targets: oneTarget()}

	if err := pub.PublishOrgModelConnection(context.Background(), "acme", ollamaConnection(), "fake-ollama-key"); err != nil {
		t.Fatalf("PublishOrgModelConnection: %v", err)
	}
	if len(client.ensured) != 1 {
		t.Fatalf("provider writes = %d, want 1", len(client.ensured))
	}
	want, err := agentgovernance.ProviderInputFor("acme", ollamaConnection(), "fake-ollama-key", "gw-1")
	if err != nil {
		t.Fatalf("ProviderInputFor: %v", err)
	}
	if got := client.ensured[0]; got != want {
		t.Fatalf("write = %+v, want %+v", got, want)
	}
}

// Clearing overwrites the provider's copy of the key in place — never through
// EnsureProvider, whose create branch would conjure a provider no deploy asked
// for — with a value that authenticates nowhere, under the header of the
// connection the copy belonged to.
func TestAMPModelProviderPublisher_ClearOverwritesTheKey(t *testing.T) {
	client := &providerClient{exists: true}
	pub := ampModelProviderPublisher{amp: providerClients{client}, bindings: oneBinding{}, targets: oneTarget()}

	if err := pub.ClearOrgModelKey(context.Background(), "acme", ollamaConnection()); err != nil {
		t.Fatalf("ClearOrgModelKey: %v", err)
	}
	if len(client.ensured) != 0 {
		t.Fatalf("EnsureProvider called %d time(s); a clear must not create a provider", len(client.ensured))
	}
	if len(client.updates) != 1 {
		t.Fatalf("credential writes = %d, want 1", len(client.updates))
	}
	in := client.updates[0]
	if in.APIKey != "Bearer "+clearedProviderCredential || in.ID != "aep-acme-anthropic" || in.GatewayID != "gw-1" ||
		in.Template != "openai" || in.AuthHeader != "Authorization" {
		t.Fatalf("write = %+v, want the org's provider holding the cleared value", in)
	}
}

var (
	bindingA = openchoreo.AIGatewayBinding{AdminURL: "http://amp-a.example", GatewayID: "gw-a"}
	bindingB = openchoreo.AIGatewayBinding{AdminURL: "http://amp-b.example", GatewayID: "gw-b"}
)

func publishWith(t *testing.T, targets fakeOrgTargets, bindings *envBindings, client *providerClient) error {
	t.Helper()
	pub := ampModelProviderPublisher{amp: providerClients{client}, bindings: bindings, targets: targets}
	return pub.PublishOrgModelConnection(context.Background(), "acme", ollamaConnection(), "fake-key")
}

func clearWith(t *testing.T, targets fakeOrgTargets, bindings *envBindings, client *providerClient) error {
	t.Helper()
	pub := ampModelProviderPublisher{amp: providerClients{client}, bindings: bindings, targets: targets}
	return pub.ClearOrgModelKey(context.Background(), "acme", ollamaConnection())
}

func TestAMPModelProviderPublisher_DistinctBindingsEachWrittenOnce(t *testing.T) {
	targets := fakeOrgTargets{envs: []string{"development", "dev-b"}}
	bindings := &envBindings{byEnv: map[string]openchoreo.AIGatewayBinding{"development": bindingA, "dev-b": bindingB}}
	client := &providerClient{exists: true}
	if err := publishWith(t, targets, bindings, client); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(client.ensured) != 2 {
		t.Fatalf("publish writes = %d, want 2", len(client.ensured))
	}
	client = &providerClient{exists: true}
	if err := clearWith(t, targets, bindings, client); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(client.updates) != 2 {
		t.Fatalf("clear writes = %d, want 2", len(client.updates))
	}
}

func TestAMPModelProviderPublisher_SameBindingWrittenOnce(t *testing.T) {
	targets := fakeOrgTargets{envs: []string{"development", "dev-b"}}
	bindings := &envBindings{byEnv: map[string]openchoreo.AIGatewayBinding{"development": bindingA, "dev-b": bindingA}}
	client := &providerClient{exists: true}
	if err := publishWith(t, targets, bindings, client); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(client.ensured) != 1 {
		t.Fatalf("publish writes = %d, want 1", len(client.ensured))
	}
	client = &providerClient{exists: true}
	if err := clearWith(t, targets, bindings, client); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(client.updates) != 1 {
		t.Fatalf("clear writes = %d, want 1", len(client.updates))
	}
}

func TestAMPModelProviderPublisher_UnboundTargetIsSkipped(t *testing.T) {
	targets := fakeOrgTargets{envs: []string{"development", "dev-b"}}
	bindings := &envBindings{byEnv: map[string]openchoreo.AIGatewayBinding{"development": bindingA}}
	client := &providerClient{exists: true}
	if err := publishWith(t, targets, bindings, client); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(client.ensured) != 1 {
		t.Fatalf("publish writes = %d, want 1", len(client.ensured))
	}
	client = &providerClient{exists: true}
	if err := clearWith(t, targets, bindings, client); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(client.updates) != 1 {
		t.Fatalf("clear writes = %d, want 1", len(client.updates))
	}
}

// One project with a broken pipeline must not block the others, and the skip
// must be visible in the log.
func TestAMPModelProviderPublisher_UnresolvedProjectIsLoggedNotFatal(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	targets := fakeOrgTargets{
		envs:       []string{"development"},
		unresolved: map[string]error{"broken": &openchoreo.ErrNoWriteTarget{}},
	}
	bindings := &envBindings{byEnv: map[string]openchoreo.AIGatewayBinding{"development": bindingA}}
	client := &providerClient{exists: true}
	if err := publishWith(t, targets, bindings, client); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(client.ensured) != 1 {
		t.Fatalf("publish writes = %d, want 1", len(client.ensured))
	}
	if out := logs.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "project=broken") {
		t.Fatalf("expected a WARN naming the project, got %q", out)
	}
	client = &providerClient{exists: true}
	if err := clearWith(t, targets, bindings, client); err != nil || len(client.updates) != 1 {
		t.Fatalf("clear: err=%v writes=%d, want nil and 1", err, len(client.updates))
	}
}

func TestAMPModelProviderPublisher_ZeroProjectsIsNoOp(t *testing.T) {
	bindings := &envBindings{}
	client := &providerClient{exists: true}
	if err := publishWith(t, fakeOrgTargets{}, bindings, client); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := clearWith(t, fakeOrgTargets{}, bindings, client); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(bindings.reads) != 0 || len(client.ensured) != 0 || len(client.updates) != 0 {
		t.Fatalf("expected no reads or writes, got reads=%v", bindings.reads)
	}
}

// failingEnsureClient fails every provider write.
type failingEnsureClient struct{ providerClient }

func (c *failingEnsureClient) EnsureProvider(context.Context, agentmanager.EnsureProviderInput) (agentmanager.ProviderRef, error) {
	return agentmanager.ProviderRef{}, errors.New("amp down")
}

func (c *failingEnsureClient) UpdateProviderCredential(context.Context, agentmanager.EnsureProviderInput) (bool, error) {
	return false, errors.New("amp down")
}

func TestAMPModelProviderPublisher_OneFailureDoesNotStopTheOthers(t *testing.T) {
	ok := &providerClient{exists: true}
	bad := &failingEnsureClient{}
	pub := ampModelProviderPublisher{
		amp: clientsByURL{bindingA.AdminURL: bad, bindingB.AdminURL: ok},
		bindings: &envBindings{byEnv: map[string]openchoreo.AIGatewayBinding{
			"development": bindingA, "dev-b": bindingB}},
		targets: fakeOrgTargets{envs: []string{"development", "dev-b"}},
	}
	err := pub.PublishOrgModelConnection(context.Background(), "acme", ollamaConnection(), "fake-key")
	if err == nil || !strings.Contains(err.Error(), "amp down") {
		t.Fatalf("publish err = %v, want the joined failure", err)
	}
	if len(ok.ensured) != 1 {
		t.Fatalf("healthy gateway writes = %d, want 1", len(ok.ensured))
	}
	ok.updates = nil
	err = pub.ClearOrgModelKey(context.Background(), "acme", ollamaConnection())
	if err == nil || !strings.Contains(err.Error(), "amp down") {
		t.Fatalf("clear err = %v, want the joined failure", err)
	}
	if len(ok.updates) != 1 {
		t.Fatalf("healthy gateway clears = %d, want 1", len(ok.updates))
	}
}

type clientsByURL map[string]agentmanager.Client

func (c clientsByURL) For(url string) agentmanager.Client { return c[url] }

type fixedTarget struct {
	env string
	err error
}

func (f fixedTarget) Resolve(context.Context, string, string) (string, error) { return f.env, f.err }

func TestAMPAgentRegistrar_ResolveErrorIsReturned(t *testing.T) {
	cause := &openchoreo.ErrNoWriteTarget{}
	r := ampAgentRegistrar{targets: fixedTarget{err: cause}}
	_, err := r.RegisterAgentsForBuild(context.Background(), "acme", "proj", []string{"agent"})
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want it to wrap the resolve error", err)
	}
}

// The governor reads the binding for GovernAgentInput.Environment, so the
// environment it asks about is the one the registrar passed.
func TestAMPAgentRegistrar_RegistersAtTheProjectsWriteTarget(t *testing.T) {
	bindings := &envBindings{}
	r := ampAgentRegistrar{
		governor: agentgovernance.New(agentgovernance.Deps{Bindings: bindings}),
		targets:  fixedTarget{env: "dev-b"},
	}
	out, err := r.RegisterAgentsForBuild(context.Background(), "acme", "proj", []string{"agent"})
	if err != nil {
		t.Fatalf("RegisterAgentsForBuild: %v", err)
	}
	if len(bindings.reads) != 1 || bindings.reads[0] != "dev-b" {
		t.Fatalf("binding reads = %v, want [dev-b]", bindings.reads)
	}
	if len(out.Agents) != 1 || !out.Agents[0].Skipped {
		t.Fatalf("outcome = %+v, want one skipped agent (no binding)", out)
	}
}
