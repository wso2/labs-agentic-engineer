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
	"reflect"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// fakeGuardrailStore is the record of what AEP last wrote to a binding.
type fakeGuardrailStore struct {
	found    bool
	owned    []string
	outcomes []GuardrailOutcome
	puts     int
	// ownedAtEachPut is what was recorded as owned on every write, in order.
	ownedAtEachPut [][]string
	getErr         error
	putErr         error
}

func (f *fakeGuardrailStore) Get(context.Context, GuardrailRecordKey) ([]string, bool, error) {
	return f.owned, f.found || len(f.owned) > 0, f.getErr
}

func (f *fakeGuardrailStore) Put(_ context.Context, _ GuardrailRecordKey, owned []string, outcomes []GuardrailOutcome) error {
	f.puts++
	f.ownedAtEachPut = append(f.ownedAtEachPut, append([]string{}, owned...))
	if f.putErr != nil {
		return f.putErr
	}
	f.found, f.owned, f.outcomes = true, owned, outcomes
	return nil
}

func guardedGovernor(t *testing.T, amp *fakeAMP, store *fakeGuardrailStore) *Governor {
	t.Helper()
	amp.catalog = testCatalog(t)
	amp.binding = agentmanager.Binding{Name: "aep-checkout", ProviderHandle: "aep-acme-anthropic"}
	return New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{},
		Connections: &fakeConnections{}, Guardrails: store})
}

func withGuardrails(decls ...delivery.GuardrailDeclaration) delivery.GovernAgentInput {
	in := input()
	in.Guardrails = decls
	return in
}

func TestGovernAgent_AppliesADeclaredGuardrailToTheAgentsBinding(t *testing.T) {
	amp, store := newFakeAMP(), &fakeGuardrailStore{}
	g := guardedGovernor(t, amp, store)

	if _, err := g.GovernAgent(context.Background(),
		withGuardrails(declare("pii-masking-regex", map[string]any{"email": true}))); err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	if len(amp.policyWrites) != 1 || len(amp.policyWrites[0]) != 1 || amp.policyWrites[0][0].Name != "pii-masking-regex" {
		t.Fatalf("policy writes = %+v, want one carrying pii-masking-regex", amp.policyWrites)
	}
	if len(store.owned) != 1 || store.owned[0] != "pii-masking-regex" {
		t.Errorf("owned = %v", store.owned)
	}
	if len(store.outcomes) != 1 || store.outcomes[0].Status != GuardrailApplied {
		t.Errorf("outcomes = %+v", store.outcomes)
	}
}

// Most agents declare no guardrail and never had one: that must cost no write
// to Agent Manager, which would redeploy the agent's proxy for nothing.
func TestGovernAgent_NothingDeclaredNothingOwnedIsNoWrite(t *testing.T) {
	amp, store := newFakeAMP(), &fakeGuardrailStore{}
	g := guardedGovernor(t, amp, store)

	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	if len(amp.policyWrites) != 0 || store.puts != 0 {
		t.Fatalf("writes=%d store puts=%d, want none", len(amp.policyWrites), store.puts)
	}
}

// A guardrail that cannot be applied never fails the deploy: the agent ships
// and the outcome says why its guardrail did not.
func TestGovernAgent_AnUnreadableCatalogIsRecordedNotFatal(t *testing.T) {
	amp, store := newFakeAMP(), &fakeGuardrailStore{}
	g := guardedGovernor(t, amp, store)
	amp.catalogErr = errors.New("amp unreachable")

	if _, err := g.GovernAgent(context.Background(),
		withGuardrails(declare("pii-masking-regex", map[string]any{"email": true}))); err != nil {
		t.Fatalf("a guardrail failure must not fail governance: %v", err)
	}
	if len(store.outcomes) != 1 || store.outcomes[0].Status != GuardrailFailed ||
		!strings.Contains(store.outcomes[0].Reason, "amp unreachable") {
		t.Fatalf("outcomes = %+v, want failed with the cause", store.outcomes)
	}
	if len(amp.policyWrites) != 0 {
		t.Fatalf("wrote %+v without a catalog", amp.policyWrites)
	}
}

func TestGovernAgent_ARefusedWriteIsRecordedAndKeepsWhatWasOwned(t *testing.T) {
	amp, store := newFakeAMP(), &fakeGuardrailStore{owned: []string{"regex-guardrail"}}
	g := guardedGovernor(t, amp, store)
	amp.policyWriteErr = errors.New("agentmanager: 400 invalid policy")

	if _, err := g.GovernAgent(context.Background(),
		withGuardrails(declare("pii-masking-regex", map[string]any{"email": true}))); err != nil {
		t.Fatalf("a refused write must not fail governance: %v", err)
	}
	if len(store.outcomes) != 1 || store.outcomes[0].Status != GuardrailFailed ||
		!strings.Contains(store.outcomes[0].Reason, "400 invalid policy") {
		t.Fatalf("outcomes = %+v, want failed with Agent Manager's message", store.outcomes)
	}
	// A refused write may still have landed (a timeout after Agent Manager
	// applied it), so ownership stays with everything AEP may have written:
	// the previous entries and the new ones.
	if !reflect.DeepEqual(store.owned, []string{"regex-guardrail", "pii-masking-regex"}) {
		t.Errorf("owned = %v, want the previous entries and the attempted ones", store.owned)
	}
}

func TestGovernAgent_AnOperatorsSameNamedGuardrailIsAConflict(t *testing.T) {
	amp, store := newFakeAMP(), &fakeGuardrailStore{}
	g := guardedGovernor(t, amp, store)
	amp.binding.Policies = []agentmanager.BindingPolicy{policy("pii-masking-regex")}

	if _, err := g.GovernAgent(context.Background(),
		withGuardrails(declare("pii-masking-regex", map[string]any{"email": true}))); err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	if len(store.outcomes) != 1 || store.outcomes[0].Status != GuardrailConflict {
		t.Fatalf("outcomes = %+v, want conflict", store.outcomes)
	}
	if len(amp.policyWrites) != 0 {
		t.Fatalf("the operator's guardrail was overwritten: %+v", amp.policyWrites)
	}
}

func TestGovernAgent_NoGuardrailStoreSkipsTheStep(t *testing.T) {
	amp := newFakeAMP()
	amp.catalog = testCatalog(t)
	g := New(Deps{Endpoints: &fakeEndpoints{}, AMP: amp, Keys: &fakeKeyStore{}, Bindings: fakeBindings{}, Connections: &fakeConnections{}})

	if _, err := g.GovernAgent(context.Background(),
		withGuardrails(declare("pii-masking-regex", map[string]any{"email": true}))); err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	if len(amp.policyWrites) != 0 {
		t.Fatalf("wrote guardrails with no store to record them: %+v", amp.policyWrites)
	}
}

// Ownership is written BEFORE the binding. A write that lands with its record
// lost would otherwise leave AEP's own entry looking like an operator's —
// reported as a conflict on every deploy, and never removable.
func TestGovernAgent_RecordsOwnershipBeforeWritingTheBinding(t *testing.T) {
	amp, store := newFakeAMP(), &fakeGuardrailStore{}
	g := guardedGovernor(t, amp, store)

	if _, err := g.GovernAgent(context.Background(),
		withGuardrails(declare("pii-masking-regex", map[string]any{"email": true}))); err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	if len(store.ownedAtEachPut) < 2 || !reflect.DeepEqual(store.ownedAtEachPut[0], []string{"pii-masking-regex"}) {
		t.Fatalf("records = %v, want ownership recorded before the write, then confirmed", store.ownedAtEachPut)
	}
	if len(amp.policyWrites) != 1 {
		t.Fatalf("policy writes = %d, want 1", len(amp.policyWrites))
	}
}

// A record whose write-ahead cannot be saved means AEP would lose track of
// what it writes, so it writes nothing.
func TestGovernAgent_AnUnsavableRecordWritesNothing(t *testing.T) {
	amp, store := newFakeAMP(), &fakeGuardrailStore{putErr: errors.New("db down")}
	g := guardedGovernor(t, amp, store)

	if _, err := g.GovernAgent(context.Background(),
		withGuardrails(declare("pii-masking-regex", map[string]any{"email": true}))); err != nil {
		t.Fatalf("a record failure must not fail governance: %v", err)
	}
	if len(amp.policyWrites) != 0 {
		t.Fatalf("wrote %+v with no record of it", amp.policyWrites)
	}
}

// Guardrails no longer declared leave no stale outcomes on the Deployments page.
func TestGovernAgent_ClearsOutcomesWhenNothingIsDeclaredAnyMore(t *testing.T) {
	amp, store := newFakeAMP(), &fakeGuardrailStore{found: true,
		outcomes: []GuardrailOutcome{{Policy: "url-guardrail", Status: GuardrailUnavailable}}}
	g := guardedGovernor(t, amp, store)

	if _, err := g.GovernAgent(context.Background(), input()); err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	if store.puts != 1 || len(store.outcomes) != 0 {
		t.Fatalf("puts=%d outcomes=%+v, want the record cleared", store.puts, store.outcomes)
	}
	if len(amp.policyWrites) != 0 {
		t.Fatalf("cleared the record by writing the binding: %+v", amp.policyWrites)
	}
}

// What a spec says is unknown when it cannot be read — which is not "declares
// nothing". AEP leaves every guardrail it applied in place.
func TestGovernAgent_AnUnreadableSpecLeavesGuardrailsAsTheyAre(t *testing.T) {
	amp, store := newFakeAMP(), &fakeGuardrailStore{owned: []string{"pii-masking-regex"}}
	g := guardedGovernor(t, amp, store)
	amp.binding.Policies = []agentmanager.BindingPolicy{policy("pii-masking-regex")}
	in := input()
	in.GuardrailsUnreadable = true

	if _, err := g.GovernAgent(context.Background(), in); err != nil {
		t.Fatalf("GovernAgent: %v", err)
	}
	if len(amp.policyWrites) != 0 || store.puts != 0 {
		t.Fatalf("writes=%+v puts=%d, want the binding and record untouched", amp.policyWrites, store.puts)
	}
}
