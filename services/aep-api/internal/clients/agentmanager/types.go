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

// Package agentmanager is the only place in aep-api that speaks Agent
// Manager's REST API.
//
// Agent Manager publishes no OpenAPI document, so every request shape here was
// read out of its console's own API client and then verified against a running
// amp-api. The contract tests in this package are what hold that knowledge:
// when AMP changes shape, they are what fails, rather than a deploy.
package agentmanager

import (
	"context"
	"errors"
)

// Config is what the client needs to reach one Agent Manager.
//
// BaseURL is per ENVIRONMENT, taken from that environment's AI gateway binding
// record — not configuration. Two environments may be governed by different
// Agent Managers, and the binding is where that is written down.
type Config struct {
	BaseURL      string // e.g. http://api.amp.localhost:8080/api/v1
	TokenURL     string
	ClientID     string
	ClientSecret string
	Resource     string // "urn:wso2:amp"
	// HostHeader is the vhost the token endpoint is routed by.
	//
	// REQUIRED WHEN THE TOKEN URL IS AN ADDRESS RATHER THAN A HOSTNAME. This
	// deployment reaches the platform IdP through the k3d load balancer
	// (http://k3d-openchoreo-serverlb:8080), and kgateway routes it by Host —
	// so a request without one matches no vhost and answers 404, which reads
	// like a wrong path and is not one. The service's other OAuth clients carry
	// the same field for the same reason (SERVICE_AUTH_HOST_HEADER,
	// OBSERVER_OAUTH_HOST_HEADER). Empty when the URL's own host already routes.
	HostHeader string
}

// Client is Agent Manager as aep-api needs it: enough to register an agent,
// bind it to the org's provider, and give it a credential of its own.
//
// THE PROJECT IS ABSENT ON PURPOSE. Agent Manager projects OpenChoreo's own
// Projects into its catalogue — an AEP project appears there the moment the CR
// is created, with the same name and the same creation timestamp, whether or
// not it holds an agent. An earlier version of this client called a
// get-or-create on /orgs/{org}/projects and never once created anything: the
// projection always won. Re-adding it buys a round-trip per deploy and a second
// writer on a record Agent Manager owns.
type Client interface {
	ProviderTemplate(ctx context.Context, org, template string) (ProviderTemplate, error)
	EnsureProvider(ctx context.Context, in EnsureProviderInput) (ProviderRef, error)
	EnsureAgent(ctx context.Context, in EnsureAgentInput) (AgentRef, error)
	EnsureModelConfig(ctx context.Context, in EnsureModelConfigInput) (ModelConfigRef, error)
	ListModelKeys(ctx context.Context, in ModelKeyRef) ([]string, error)
	IssueModelKey(ctx context.Context, in ModelKeyRef, keyName string) (IssuedKey, error)
	RotateModelKey(ctx context.Context, in ModelKeyRef, keyName string) (IssuedKey, error)

	// IssueTracingToken mints the credential an externally-hosted agent
	// authenticates its OTLP export with. Unlike the two above it is NOT a
	// stored key: see TracingToken.
	IssueTracingToken(ctx context.Context, in TracingTokenRef) (TracingToken, error)
}

// EnsureProviderInput is one org's LLM provider, as AEP declares it.
//
// UpstreamURL, AuthType and AuthHeader come from AMP's provider TEMPLATE rather
// than from constants here: `anthropic` answers with
// {type: "api-key", header: "x-api-key"} and https://api.anthropic.com, and a
// different template answers differently. A hardcoded header reaches the
// upstream as a 401 that reads like a bad key.
type EnsureProviderInput struct {
	Org         string
	ID          string // slug; also the provider handle a model config names
	Name        string
	Version     string // AMP validates v<major>.<minor>
	Context     string // the gateway path segment, e.g. /aep-default-anthropic
	Template    string
	UpstreamURL string
	AuthType    string
	AuthHeader  string
	APIKey      string // the ORG's Anthropic key, held by AMP and never by an agent
	GatewayID   string
	// ReassertCredential re-PUTs APIKey onto a provider that already exists.
	//
	// IT IS NOT FREE, which is why the caller decides. Updating a provider
	// redeploys every LLM proxy bound to it — measured at twelve proxy
	// redeploys per governed deploy in a single-agent org — and a redeploy is
	// the window in which a proxy can lose the API keys broadcast to it. So the
	// credential is re-asserted when it has CHANGED, not on the chance that it
	// might have.
	//
	// Creation always carries the key: a provider cannot exist without one.
	ReassertCredential bool
}

// ProviderRef is what later calls bind to.
//
// Context is the gateway path the provider is served on. With it and the
// environment's gateway endpoint an agent's MODEL_ENDPOINT is derivable, so
// nothing has to record a generated address.
type ProviderRef struct {
	UUID    string
	Handle  string
	Context string
}

// ProviderTemplate is the half of a provider that AMP already knows.
type ProviderTemplate struct {
	ID          string
	EndpointURL string
	AuthType    string
	AuthHeader  string
}

// EnsureAgentInput registers one externally-hosted agent.
//
// There is no endpoint field, and that is not an omission: AMP's own
// "Register an Externally-Hosted Agent" form collects name, displayName,
// description and labels only. The record is an identity to govern by, and the
// agent is governed because its model traffic leaves through the gateway.
type EnsureAgentInput struct {
	Org, Project string
	Name         string
	DisplayName  string
	Description  string
}

// AgentRef names the agent AMP now knows about.
type AgentRef struct{ Name string }

// EnsureModelConfigInput binds one agent to one provider, per environment.
//
// The binding is what gives Agent Manager a per-agent view: the console shows
// the provider under the agent, a guardrail attaches to THIS agent's traffic,
// and usage is attributed to it. AMP creates a proxy of its own for each
// binding, with its own URL and its own key.
type EnsureModelConfigInput struct {
	Org, Project, Agent string
	// Name is the config's own name in Agent Manager, and the identity
	// EnsureModelConfig is idempotent on.
	Name string
	// Environment keys the mapping: a provider is deployed per environment and
	// a key is issued against that pair.
	Environment    string
	ProviderHandle string
	URLVar         string
	APIKeyVar      string
}

// ModelConfigRef identifies the binding a key is issued against, and carries
// the URL that binding is reached at.
//
// ProxyURL is PER AGENT and generated by Agent Manager (…:8084/aep-<generated>),
// so it is read back rather than constructed.
type ModelConfigRef struct {
	ConfigID string
	ProxyURL string
}

// ModelKeyRef addresses the one key a (config, environment) pair may hold.
type ModelKeyRef struct {
	Org, Project, Agent string
	ConfigID            string
	Environment         string
}

// IssuedKey is returned by AMP exactly once and cannot be read back. Everything
// about how the govern stage is sequenced follows from that.
type IssuedKey struct {
	APIKey string
	KeyID  string
}

// TracingTokenRef addresses the tracing credential of one agent in one
// environment. There is no config id: the token is issued against the AGENT,
// not against a model binding, which is why it survives a rebinding.
type TracingTokenRef struct {
	Org, Project, Agent string
	Environment         string
}

// TracingToken is what an agent sends as `x-amp-api-key` when it POSTs spans to
// <gateway>/otel/v1/traces.
//
// A SIGNED JWT, NOT A STORED KEY, and every decision around it follows from
// that. Agent Manager signs it on demand and keeps no record: minting a second
// one neither revokes the first nor accumulates anything to clean up, so unlike
// a model key it can be re-minted freely. What it cannot do is outlive its own
// exp (~90 days), which is why ExpiresAt is carried rather than discarded — it
// is the only thing that says when a running agent will go quiet.
type TracingToken struct {
	Token string
	// ExpiresAt is unix seconds, as AMP reports it.
	ExpiresAt int64
}

// ErrProviderNotFound is the answer for an org with no provider yet — one that
// has never deployed a governed agent. Callers act on it: key rotation treats
// it as success rather than as something to report.
var ErrProviderNotFound = errors.New("agentmanager: org has no LLM provider")
