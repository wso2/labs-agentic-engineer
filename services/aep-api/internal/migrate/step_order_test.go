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

package migrate

import (
	"testing"
)

// goldenStepOrder is the exact sequence database.Run applies. It is a GOLDEN
// list, not documentation: migration order is load-bearing and, in places,
// actively counter-intuitive —
//
//   - phase2_pra runs BEFORE phase0 (reads as backwards; it is not);
//   - every raw-SQL schema step precedes automigrate_git_repository, so CHECK
//     constraints and partial indexes win over GORM's struct-tag inference;
//   - git_repositories_composite_unique must follow that AutoMigrate, which
//     creates the new index but never drops the old one;
//   - coding_agent_logs follows both executions (its FK target) and
//     tasks_github_native (which cascade-drops the legacy keyed table).
//
// A reordering is a schema bug that a green unit suite would otherwise hide and
// only a fresh database would reveal. If a domain moves its migration step's
// registration CALL-SITE, its POSITION here must not change: this list is frozen,
// and a diff here is a review stop.
var goldenStepOrder = []string{
	"phase2_pra",
	"phase0",
	"phase2_prd",
	"phase3_tech_lead",
	"phase4_coding_agent",
	"phase5_deploy_gating",
	"phase6_api_platform_idp",
	"phase2_pra_schema",
	"phase2_prc",
	"org_secrets",
	"per_org_secret_name",
	"org_anthropic_credentials",
	"phase3_sm_api_columns",
	"phase3_thunder_org_uuid",
	"phase3_coding_agent_logs",
	"automigrate_git_repository",
	"git_repositories_composite_unique",
	"phase7_skills",
	"phase8_idp_sm_api_columns",
	"executions",
	"agent_turns",
	"tasks_github_native",
	"phase9_dependency_mgmt",
	"workflow_runs",
	"coding_agent_logs",
	"phase10_rca_agent_reports",
	"milestone_runs",
	"run_cycle_logs",
	"agent_usage_ledger",
	"model_rates_seed",
	"phase11_secret_ref_columns",
	"phase12_encrypt_credential_columns",
	"phase13_anthropic_credential_role",
	"project_conversations",
}

// TestStepOrderGolden pins the ordered list. Steps is a pure builder, so this
// needs no database and runs in the fast lane — the point is that the order is
// asserted on every commit, not only when someone runs the DB tier.
func TestStepOrderGolden(t *testing.T) {
	steps := Steps(nil, "dev", nil) // nil *gorm.DB / key: nothing runs, we only read names

	var got []string
	for _, s := range steps {
		got = append(got, s.Name)
	}

	if len(got) != len(goldenStepOrder) {
		t.Fatalf("step COUNT changed: %d, golden %d\n got:    %v\n golden: %v\n"+
			"Adding a migration is expected — append it and extend the golden list in the same "+
			"commit. Removing or reordering one is not.", len(got), len(goldenStepOrder), got, goldenStepOrder)
	}
	for i := range got {
		if got[i] != goldenStepOrder[i] {
			t.Errorf("step %d is %q, golden says %q — migration order is load-bearing; "+
				"if a step moved package, move its call-site, not its position", i, got[i], goldenStepOrder[i])
		}
	}
}

// TestStepsAreNamedAndUnique guards the runner's error reporting: it names the
// offending step, which is useless if two steps share a name or one is blank.
func TestStepsAreNamedAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for i, s := range Steps(nil, "dev", nil) {
		if s.Name == "" {
			t.Errorf("step %d has no name — database.Run reports failures by name", i)
		}
		if seen[s.Name] {
			t.Errorf("duplicate step name %q — the failure report would be ambiguous", s.Name)
		}
		seen[s.Name] = true
		if s.Run == nil {
			t.Errorf("step %q has a nil Run func", s.Name)
		}
	}
}
