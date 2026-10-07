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

package agentmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Guardrails on one agent's own traffic. Agent Manager keeps them on the
// agent's binding (its model config), per environment, as
// envMappings.<env>.configuration.policies — the per-agent slot, distinct from
// the org provider's policies, which every agent on the provider shares and
// whose every write redeploys each bound proxy.

// PolicyDefinition is one policy the environment's gateways offer, as the
// catalog reports it: the exact build version a binding must name, and the
// JSON Schema its params are checked against.
type PolicyDefinition struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	DisplayName string          `json:"displayName"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	// SystemParameters are the settings the gateway's own configuration
	// supplies — for an external-service guardrail, that service's endpoint
	// and key. A policy declaring any needs the gateway configured for it.
	SystemParameters json.RawMessage `json:"systemParameters"`
}

// BindingPolicy is one entry of a binding's configuration.policies.
type BindingPolicy struct {
	Name    string              `json:"name"`
	Version string              `json:"version"`
	Paths   []BindingPolicyPath `json:"paths"`
	// Raw is the entry exactly as Agent Manager returned it. An entry read
	// back is written back from Raw, so a field this type does not model — on
	// an operator's guardrail, say — survives the write. AEP's own entries
	// have none and are written from the fields above.
	Raw json.RawMessage `json:"-"`
}

// MarshalJSON writes an entry read back verbatim, and any other from its fields.
func (p BindingPolicy) MarshalJSON() ([]byte, error) {
	if len(p.Raw) > 0 {
		return p.Raw, nil
	}
	type fields BindingPolicy
	return json.Marshal(fields(p))
}

// BindingPolicyPath scopes a policy. An agent-level guardrail is always global:
// path "/*", methods ["*"] — the shape Agent Manager's own console writes.
type BindingPolicyPath struct {
	Path    string         `json:"path"`
	Methods []string       `json:"methods"`
	Params  map[string]any `json:"params,omitempty"`
}

// BindingRef addresses one agent's binding in one environment.
type BindingRef struct {
	Org, Project, Agent string
	ConfigID            string
	Environment         string
}

// Binding is one agent's binding as read: the governed environment's
// provider and policies, and the whole item for the write to carry back.
type Binding struct {
	Name           string
	ProviderHandle string
	Policies       []BindingPolicy
	// EnvironmentVariables round-trip verbatim; AEP never edits them here.
	EnvironmentVariables []map[string]any
	// item is the binding as Agent Manager returned it. The update REPLACES
	// what it names, so the write rebuilds its body from this — every
	// environment's mapping, the description, the resilience settings — and
	// changes only the governed environment's policies.
	item map[string]any
}

// FindProvider looks the org's provider up by handle without creating it:
// reading the guardrail catalog must never write the provider, whose every
// update redeploys each proxy bound to it.
func (c *client) FindProvider(ctx context.Context, org, id string) (string, bool, error) {
	tok, err := c.token(ctx, scopeProvider)
	if err != nil {
		return "", false, err
	}
	return c.findProvider(ctx, tok, org, id)
}

// ListPolicies reads the policies the gateways a provider is deployed to offer.
func (c *client) ListPolicies(ctx context.Context, org, providerUUID string) ([]PolicyDefinition, error) {
	tok, err := c.token(ctx, scopeProvider)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/orgs/%s/llm-providers/policies?providerId=%s", url.PathEscape(org), url.QueryEscape(providerUUID))
	var out struct {
		List []PolicyDefinition `json:"list"`
	}
	if err := c.do(ctx, tok, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.List, nil
}

// ReadBinding reads one binding: the governed environment's policies, and
// everything a write must carry back unchanged.
func (c *client) ReadBinding(ctx context.Context, ref BindingRef) (Binding, error) {
	tok, err := c.token(ctx, scopeAgent)
	if err != nil {
		return Binding{}, err
	}
	var raw json.RawMessage
	if err := c.do(ctx, tok, http.MethodGet, c.bindingPath(ref), nil, &raw); err != nil {
		return Binding{}, err
	}
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil {
		return Binding{}, fmt.Errorf("agentmanager: decode binding: %w", err)
	}
	var typed struct {
		Name        string `json:"name"`
		EnvMappings map[string]struct {
			Configuration struct {
				Policies     []json.RawMessage `json:"policies"`
				ProviderName string            `json:"providerName"`
			} `json:"configuration"`
		} `json:"envMappings"`
		EnvironmentVariables []map[string]any `json:"environmentVariables"`
	}
	if err := json.Unmarshal(raw, &typed); err != nil {
		return Binding{}, fmt.Errorf("agentmanager: decode binding: %w", err)
	}
	mapping := typed.EnvMappings[ref.Environment]
	policies := make([]BindingPolicy, 0, len(mapping.Configuration.Policies))
	for _, entry := range mapping.Configuration.Policies {
		var p BindingPolicy
		if err := json.Unmarshal(entry, &p); err != nil {
			return Binding{}, fmt.Errorf("agentmanager: decode binding policy: %w", err)
		}
		p.Raw = entry
		policies = append(policies, p)
	}
	return Binding{
		Name:                 typed.Name,
		ProviderHandle:       mapping.Configuration.ProviderName,
		Policies:             policies,
		EnvironmentVariables: typed.EnvironmentVariables,
		item:                 item,
	}, nil
}

// WriteBindingPolicies replaces the binding's policies for one environment
// and carries everything else b read back unchanged.
func (c *client) WriteBindingPolicies(ctx context.Context, ref BindingRef, b Binding, policies []BindingPolicy) error {
	tok, err := c.token(ctx, scopeAgent)
	if err != nil {
		return err
	}
	if policies == nil {
		// An empty list clears Agent Manager's copy; a missing one leaves it.
		policies = []BindingPolicy{}
	}
	return c.do(ctx, tok, http.MethodPut, c.bindingPath(ref), updateBody(ref.Environment, b, policies), nil)
}

// updateBody is the update request: the name, description and environment
// variables as read, and every environment's mapping as read — provider,
// policies, resilience — with the governed environment's policies replaced.
// Response-only fields (status, url, uuids) are not sent back.
func updateBody(environment string, b Binding, policies []BindingPolicy) map[string]any {
	body := map[string]any{
		"name":                 b.Name,
		"environmentVariables": b.EnvironmentVariables,
	}
	if desc, ok := b.item["description"]; ok {
		body["description"] = desc
	}
	mappings := map[string]any{}
	read, _ := b.item["envMappings"].(map[string]any)
	for env, m := range read {
		mapping, _ := m.(map[string]any)
		cfg, _ := mapping["configuration"].(map[string]any)
		out := map[string]any{}
		if p, ok := cfg["policies"]; ok {
			out["policies"] = p
		}
		if r, ok := cfg["resilience"]; ok {
			out["resilience"] = r
		}
		mappings[env] = map[string]any{"providerName": cfg["providerName"], "configuration": out}
	}
	target, _ := mappings[environment].(map[string]any)
	if target == nil {
		target = map[string]any{"providerName": b.ProviderHandle, "configuration": map[string]any{}}
		mappings[environment] = target
	}
	target["configuration"].(map[string]any)["policies"] = policies
	body["envMappings"] = mappings
	return body
}

func (c *client) bindingPath(ref BindingRef) string {
	return c.modelConfigPath(ref.Org, ref.Project, ref.Agent) + "/" + url.PathEscape(ref.ConfigID)
}
