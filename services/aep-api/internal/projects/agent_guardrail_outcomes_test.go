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

package projects

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
)

// fakeGuardrailOutcomes answers per environment, as the governance record holds them.
type fakeGuardrailOutcomes struct {
	byEnv map[string][]GuardrailOutcome
	err   error
}

func (f fakeGuardrailOutcomes) GuardrailOutcomes(context.Context, string, string, string) (map[string][]GuardrailOutcome, error) {
	return f.byEnv, f.err
}

// guardrailTestEnv is the environment these tests deploy to.
const guardrailTestEnv = "development"

func guardrailService(outcomes GuardrailOutcomeReader) *componentService {
	svc := NewComponentService(deploymentsOf(guardrailTestEnv), nil, modelAccessStore(nil), nil, nil, fakeKeyResolver{}, fakeSecretRefClient{}).(*componentService)
	svc.SetGuardrailOutcomes(outcomes)
	return svc
}

// The Deployments row says what became of each declared guardrail, so a
// guardrail that did not land is visible where the agent is tried.
func TestListDeployments_CarriesTheAgentsGuardrailOutcomes(t *testing.T) {
	svc := guardrailService(fakeGuardrailOutcomes{byEnv: map[string][]GuardrailOutcome{
		guardrailTestEnv: {
			{Policy: "pii-masking-regex", Status: "applied"},
			{Policy: "word-count-guardrail", Status: "partial", Reason: "the reply-side check is not applied"},
		},
	}})

	list, err := svc.ListDeployments(context.Background(), "acme", "shop", "receipt-agent")
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	got := list.Items[0].Guardrails
	want := []gen.DeploymentGuardrail{
		{Policy: "pii-masking-regex", Status: "applied"},
		{Policy: "word-count-guardrail", Status: "partial", Reason: "the reply-side check is not applied"},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("guardrails = %+v, want %+v", got, want)
	}
}

func TestListDeployments_NoRecordNoGuardrails(t *testing.T) {
	svc := guardrailService(fakeGuardrailOutcomes{})

	list, err := svc.ListDeployments(context.Background(), "acme", "shop", "orders-api")
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if list.Items[0].Guardrails != nil {
		t.Fatalf("guardrails = %+v, want none", list.Items[0].Guardrails)
	}
}

// Best effort, like the Agent Manager link: a record that cannot be read
// leaves the row without guardrails and the list intact.
func TestListDeployments_AnUnreadableRecordLeavesTheListIntact(t *testing.T) {
	svc := guardrailService(fakeGuardrailOutcomes{err: errors.New("db down")})

	list, err := svc.ListDeployments(context.Background(), "acme", "shop", "receipt-agent")
	if err != nil {
		t.Fatalf("ListDeployments must not fail on an unreadable guardrail record: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Guardrails != nil {
		t.Fatalf("items = %+v", list.Items)
	}
}
