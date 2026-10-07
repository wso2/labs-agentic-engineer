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
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/platform/jsonschema"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// CatalogGuardrail is one guardrail the design may declare: what this org's
// gateway offers AND the deploy will accept, with the settings the spec may
// set.
type CatalogGuardrail struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"displayName,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	// Applies says where the platform applies it — which part of the request
	// it reads, and for which agents — so the design does not declare one
	// that comes out unsupported.
	Applies string `json:"applies"`
}

// GuardrailCatalog lists the guardrails an agent of this org may declare.
//
// It reads the same live catalog the deploy resolves against (Agent Manager's,
// as that environment's gateways report it) and applies the same filter, so a
// guardrail the design is offered is never one the deploy then reports
// unavailable or uncheckable. The settings the platform owns — every JSONPath
// — are removed, so the design is never invited to write one.
//
// Nothing to govern — no AI gateway binding, no model connection, no provider
// yet, or a format guardrails do not support — is an empty list, not an
// error: a design turn reads it as "no guardrails available here".
func (g *Governor) GuardrailCatalog(ctx context.Context, org, environment string) ([]CatalogGuardrail, error) {
	binding, err := g.deps.Bindings.GetAIGatewayBinding(ctx, org, environment)
	if errors.Is(err, openchoreo.ErrNoAIGatewayBinding) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve AI gateway binding: %w", err)
	}
	conn, ok, err := g.deps.Connections.Connection(ctx, org)
	if err != nil {
		return nil, fmt.Errorf("read org model connection: %w", err)
	}
	if !ok || conn.Format != modelconn.FormatAnthropic {
		return nil, nil
	}
	amp := g.deps.AMP.For(binding.AdminURL)
	provider, found, err := amp.FindProvider(ctx, org, ProviderID(org))
	if err != nil {
		return nil, fmt.Errorf("find LLM provider: %w", err)
	}
	if !found {
		// No agent has been governed yet, so no provider exists to read a
		// catalog for.
		return nil, nil
	}
	policies, err := amp.ListPolicies(ctx, org, provider.UUID)
	if err != nil {
		return nil, fmt.Errorf("read the guardrail catalog: %w", err)
	}
	out := make([]CatalogGuardrail, 0, len(policies))
	for _, p := range policies {
		if !IsGuardrailPolicy(p.Name) || needsGatewayConfiguration(p) {
			continue
		}
		schema, _, err := checkableSchema(p.Parameters)
		if err != nil {
			continue
		}
		params, err := withoutPlatformOwnedSettings(p.Parameters)
		if err != nil {
			continue
		}
		out = append(out, CatalogGuardrail{Name: p.Name, DisplayName: p.DisplayName, Description: p.Description,
			Parameters: params, Applies: appliesNote(p.Name, schema)})
	}
	return out, nil
}

// platformOwnedSettings are the settings the deploy sets itself.
var platformOwnedSettings = []string{"jsonPath", "streamingJsonPath"}

// withoutPlatformOwnedSettings removes the platform-owned settings from a
// parameters schema, at the top level and inside its request/response blocks.
func withoutPlatformOwnedSettings(raw json.RawMessage) (json.RawMessage, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	strip := func(schema map[string]any) {
		props, _ := schema["properties"].(map[string]any)
		for _, k := range platformOwnedSettings {
			delete(props, k)
		}
	}
	strip(doc)
	if props, ok := doc["properties"].(map[string]any); ok {
		for _, phase := range []string{"request", "response"} {
			if block, ok := props[phase].(map[string]any); ok {
				strip(block)
			}
		}
	}
	return json.Marshal(doc)
}

// appliesNote is the catalog's word on where a guardrail applies, matching
// what resolveGuardrails does with it.
func appliesNote(name string, schema *jsonschema.Schema) string {
	if schema != nil {
		if _, has := schema.Properties["jsonPath"]; has {
			return "Rewrites every message sent to the model, history included; the reply gets the real values back."
		}
	}
	if measuresRequest[name] {
		return "Request side only, and only for an agent with no tools and no file uploads, where it reads the user's latest message. Reply-side checks are not applied: they are not enforced on streamed replies."
	}
	return "Request side only. Reads the user's latest message for an agent with no tools or file uploads, else the whole request — so keep what it blocks out of the agent's instructions and tool descriptions, and know that a blocked word anywhere in the conversation so far refuses every later turn of it. Reply-side checks are not applied: they are not enforced on streamed replies."
}
