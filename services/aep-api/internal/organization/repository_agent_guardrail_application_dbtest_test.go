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

package organization_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// The record the govern stage merges against: which guardrails AEP itself
// wrote to an agent's binding, and what became of each declared one. Absent
// until written; one row per (org, project, component, environment); a later
// write replaces the earlier one.
func TestAgentGuardrailApplicationRepository_RecordsTheLatestApplication_DB(t *testing.T) {
	t.Parallel()
	repo := organization.NewAgentGuardrailApplicationRepository(dbtest.New(t))
	ctx := context.Background()

	if got, err := repo.Get(ctx, "acme", "shop", "receipt-agent", "development"); err != nil || got != nil {
		t.Fatalf("before any write: %+v, %v; want none", got, err)
	}
	first := organization.AgentGuardrailApplication{
		OcOrgID: "acme", Project: "shop", Component: "receipt-agent", Environment: "development",
		AppliedNames: []string{"pii-masking-regex"},
		Outcomes:     json.RawMessage(`[{"policy":"pii-masking-regex","status":"applied"}]`),
	}
	if err := repo.Put(ctx, first); err != nil {
		t.Fatalf("put: %v", err)
	}
	second := first
	second.AppliedNames = []string{"pii-masking-regex", "regex-guardrail"}
	second.Outcomes = json.RawMessage(`[{"policy":"pii-masking-regex","status":"applied"},{"policy":"regex-guardrail","status":"applied"}]`)
	if err := repo.Put(ctx, second); err != nil {
		t.Fatalf("put again: %v", err)
	}

	got, err := repo.Get(ctx, "acme", "shop", "receipt-agent", "development")
	if err != nil || got == nil {
		t.Fatalf("after two writes: %+v, %v", got, err)
	}
	if !reflect.DeepEqual(got.AppliedNames, second.AppliedNames) {
		t.Errorf("applied names = %v, want %v", got.AppliedNames, second.AppliedNames)
	}
	var outcomes []map[string]any
	if err := json.Unmarshal(got.Outcomes, &outcomes); err != nil || len(outcomes) != 2 {
		t.Errorf("outcomes = %s (%v), want both", got.Outcomes, err)
	}

	// The same component name in another project is another agent.
	if other, err := repo.Get(ctx, "acme", "other-shop", "receipt-agent", "development"); err != nil || other != nil {
		t.Fatalf("another project: %+v, %v; want none", other, err)
	}
	rows, err := repo.ListForComponent(ctx, "acme", "shop", "receipt-agent")
	if err != nil || len(rows) != 1 || rows[0].Environment != "development" {
		t.Fatalf("list = %+v, %v; want the one environment", rows, err)
	}
}

// A project delete purges that project's records — every agent, every
// environment — and nothing of a same-org neighbour's or another org's
// same-named project.
func TestAgentGuardrailApplicationRepository_DeleteByProject_DB(t *testing.T) {
	t.Parallel()
	repo := organization.NewAgentGuardrailApplicationRepository(dbtest.New(t))
	ctx := context.Background()

	put := func(org, project, component, env string) {
		t.Helper()
		if err := repo.Put(ctx, organization.AgentGuardrailApplication{
			OcOrgID: org, Project: project, Component: component, Environment: env,
			AppliedNames: []string{"pii-masking-regex"}, Outcomes: json.RawMessage(`[]`),
		}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	put("acme", "shop", "receipt-agent", "development")
	put("acme", "shop", "receipt-agent", "staging")
	put("acme", "shop", "triage-agent", "development")
	put("acme", "other", "receipt-agent", "development")
	put("globex", "shop", "receipt-agent", "development")

	if err := repo.DeleteByProject(ctx, "acme", "shop"); err != nil {
		t.Fatalf("DeleteByProject: %v", err)
	}
	for _, c := range []string{"receipt-agent", "triage-agent"} {
		if rows, err := repo.ListForComponent(ctx, "acme", "shop", c); err != nil || len(rows) != 0 {
			t.Fatalf("acme/shop/%s after delete: %d rows, %v; want none", c, len(rows), err)
		}
	}
	for _, kept := range [][2]string{{"acme", "other"}, {"globex", "shop"}} {
		if rows, err := repo.ListForComponent(ctx, kept[0], kept[1], "receipt-agent"); err != nil || len(rows) != 1 {
			t.Fatalf("%s/%s must be untouched: %d rows, %v", kept[0], kept[1], len(rows), err)
		}
	}
	if err := repo.DeleteByProject(ctx, "acme", "shop"); err != nil {
		t.Fatalf("a second delete of an already-purged project must succeed: %v", err)
	}
}
