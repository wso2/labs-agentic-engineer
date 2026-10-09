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
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

func catalogGovernor(t *testing.T, amp *fakeAMP, conns *fakeConnections) *Governor {
	t.Helper()
	amp.catalog = testCatalog(t)
	amp.providers[ProviderID("acme")] = agentmanager.ProviderRef{UUID: "prov-uuid", Handle: ProviderID("acme")}
	return New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, Connections: conns})
}

func catalogNames(c []CatalogGuardrail) []string {
	out := []string{}
	for _, g := range c {
		out = append(out, g.Name)
	}
	return out
}

// The design agent picks from what this gateway offers AND what the deploy
// will accept — the same filter, so a policy it is offered never comes back
// unavailable or uncheckable.
func TestGuardrailCatalog_OffersOnlyGuardrailsTheDeployCanApply(t *testing.T) {
	amp := newFakeAMP()
	g := catalogGovernor(t, amp, &fakeConnections{})

	got, err := g.GuardrailCatalog(context.Background(), "acme", "development")
	if err != nil {
		t.Fatalf("GuardrailCatalog: %v", err)
	}
	if amp.catalogOf != "prov-uuid" {
		t.Errorf("catalog read for provider %q; want the org's provider prov-uuid", amp.catalogOf)
	}
	names := catalogNames(got)
	for _, want := range []string{"pii-masking-regex", "regex-guardrail", "word-count-guardrail"} {
		if !strings.Contains(strings.Join(names, ","), want) {
			t.Errorf("catalog %v lacks %s", names, want)
		}
	}
	if strings.Contains(strings.Join(names, ","), "basic-ratelimit") {
		t.Errorf("catalog %v offers a rate limit, which is not a guardrail", names)
	}
}

// The platform sets every JSONPath. Offering the setting would invite the
// design to write one, which the spec gate then refuses.
func TestGuardrailCatalog_HidesThePlatformOwnedSettings(t *testing.T) {
	g := catalogGovernor(t, newFakeAMP(), &fakeConnections{})

	got, err := g.GuardrailCatalog(context.Background(), "acme", "development")
	if err != nil {
		t.Fatalf("GuardrailCatalog: %v", err)
	}
	for _, c := range got {
		raw := string(c.Parameters)
		if strings.Contains(raw, `"jsonPath"`) || strings.Contains(raw, `"streamingJsonPath"`) {
			t.Errorf("%s still offers a platform-owned path: %s", c.Name, raw)
		}
		var doc map[string]any
		if err := json.Unmarshal(c.Parameters, &doc); err != nil {
			t.Errorf("%s parameters no longer parse: %v", c.Name, err)
		}
	}
}

func TestGuardrailCatalog_AnUncheckableGuardrailIsNotOffered(t *testing.T) {
	amp := newFakeAMP()
	g := catalogGovernor(t, amp, &fakeConnections{})
	amp.catalog = append(amp.catalog, agentmanager.PolicyDefinition{Name: "odd-guardrail", Version: "v1",
		Parameters: json.RawMessage(`{"type":"object","oneOf":[{"required":["a"]}]}`)})

	got, err := g.GuardrailCatalog(context.Background(), "acme", "development")
	if err != nil {
		t.Fatalf("GuardrailCatalog: %v", err)
	}
	if strings.Contains(strings.Join(catalogNames(got), ","), "odd-guardrail") {
		t.Fatalf("offered a guardrail whose settings AEP cannot check: %v", catalogNames(got))
	}
}

// No Agent Manager on the environment, or no model connection, means there is
// nothing to apply guardrails to: an empty list, not an error that would stall
// a design turn.
func TestGuardrailCatalog_NothingToGovernIsAnEmptyList(t *testing.T) {
	cases := map[string]*Governor{
		"no gateway binding": New(Deps{Endpoints: &fakeEndpoints{}, AMP: newFakeAMP(), Keys: &fakeKeyStore{},
			Bindings: fakeBindings{err: openchoreo.ErrNoAIGatewayBinding}, Connections: &fakeConnections{}}),
		"no model connection": catalogGovernor(t, newFakeAMP(), &fakeConnections{none: true}),
		"no provider yet": func() *Governor {
			amp := newFakeAMP()
			g := catalogGovernor(t, amp, &fakeConnections{})
			amp.providers = map[string]agentmanager.ProviderRef{}
			return g
		}(),
		"a non-Anthropic connection": func() *Governor {
			conn := ollamaOpenAIConn()
			return catalogGovernor(t, newFakeAMP(), &fakeConnections{conn: &conn})
		}(),
	}
	for name, g := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := g.GuardrailCatalog(context.Background(), "acme", "development")
			if err != nil || len(got) != 0 {
				t.Fatalf("got %v, %v; want an empty list", catalogNames(got), err)
			}
		})
	}
}

// The catalog's word on where each guardrail applies, so the design does not
// declare one that comes out unsupported for this agent.
func TestGuardrailCatalog_SaysWhereEachGuardrailApplies(t *testing.T) {
	g := catalogGovernor(t, newFakeAMP(), &fakeConnections{})

	got, err := g.GuardrailCatalog(context.Background(), "acme", "development")
	if err != nil {
		t.Fatalf("GuardrailCatalog: %v", err)
	}
	notes := map[string]string{}
	for _, c := range got {
		notes[c.Name] = c.Applies
	}
	if !strings.Contains(notes["word-count-guardrail"], "no tools") {
		t.Errorf("word-count note = %q, want it limited to agents with no tools or file uploads", notes["word-count-guardrail"])
	}
	if !strings.Contains(notes["regex-guardrail"], "request") || !strings.Contains(notes["pii-masking-regex"], "every message") {
		t.Errorf("notes = %+v", notes)
	}
}

func TestGuardrailCatalog_LeavesOutGuardrailsNeedingGatewayConfiguration(t *testing.T) {
	amp := newFakeAMP()
	g := catalogGovernor(t, amp, &fakeConnections{})
	amp.catalog = append(amp.catalog, agentmanager.PolicyDefinition{Name: "nvidia-nemoguard-content-safety", Version: "v0.9.0",
		Parameters:       json.RawMessage(`{"type":"object","properties":{"request":{"type":"object"},"response":{"type":"object"}}}`),
		SystemParameters: json.RawMessage(`{"type":"object","properties":{"endpoint":{"type":"string"}}}`)})

	got, err := g.GuardrailCatalog(context.Background(), "acme", "development")
	if err != nil {
		t.Fatalf("GuardrailCatalog: %v", err)
	}
	if strings.Contains(strings.Join(catalogNames(got), ","), "nemoguard") {
		t.Fatalf("offered a guardrail the gateway is not configured for: %v", catalogNames(got))
	}
}
