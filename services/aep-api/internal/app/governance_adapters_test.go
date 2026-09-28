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
	"context"
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

func ollamaConnection() modelconn.Connection {
	return modelconn.Connection{
		Format: modelconn.FormatOpenAICompatible, BaseURL: "https://ollama.com/v1",
		Host: modelconn.OllamaHost, Model: "gpt-oss:20b", AuthScheme: modelconn.AuthBearer,
	}
}

// A saved connection reaches the provider as the same input the deploy path
// builds, re-asserted, on any format: one write carrying the template, the
// upstream, the auth and the key.
func TestAMPModelProviderPublisher_PublishesTheConnection(t *testing.T) {
	client := &providerClient{exists: true}
	pub := ampModelProviderPublisher{amp: providerClients{client}, bindings: oneBinding{}}

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
	want.ReassertCredential = true
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
	pub := ampModelProviderPublisher{amp: providerClients{client}, bindings: oneBinding{}}

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
