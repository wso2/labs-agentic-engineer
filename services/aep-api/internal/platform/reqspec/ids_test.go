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

package reqspec

import (
	"reflect"
	"testing"
)

func TestCiteResolvesTheSharedFixture(t *testing.T) {
	spec := Parse(readFixture(t, acmeFixture))
	if got := spec.Problems(); len(got) != 0 {
		t.Fatalf("the fixture has ID problems: %+v", got)
	}
	for id, want := range map[string]Citation{
		"F2":   {Status: Live},
		"F2.5": {Status: Live},
		"P4":   {Status: Live},
		"F2.3": {Status: Retired, Replacement: "F5.1"},
		"F6":   {Status: Retired},
		"F9.9": {Status: Unknown},
	} {
		if got := spec.Cite(id); got != want {
			t.Errorf("Cite(%s) = %+v, want %+v", id, got, want)
		}
	}
}

func TestCiteFollowsAChainOfMoves(t *testing.T) {
	spec := Parse(map[string]string{
		"features/F2-approvals.md": "# Approvals\n\n## Retired\n\n- F2.3 moved to F5.1\n",
		"features/F5-mileage.md":   "# Mileage\n\n## Retired\n\n- F5.1 moved to F7.1\n",
		"features/F7-travel.md":    "# Travel\n\n## User Stories\n\n- F7.1 (was F5.1) As a traveller, I see the route.\n",
		"prd.md":                   "# P\n\n## Retired\n\n- F4 Spending reports merged into F3\n",
		"features/F3-payroll.md":   "# Payroll\n",
		"features/F8-something.md": "# Something\n",
	})
	if got := spec.Cite("F2.3"); got != (Citation{Status: Retired, Replacement: "F7.1"}) {
		t.Errorf("F2.3 → %+v, want F7.1", got)
	}
	if got := spec.Cite("F4"); got != (Citation{Status: Retired, Replacement: "F3"}) {
		t.Errorf("F4 → %+v, want F3", got)
	}
	if got := spec.Cite("F4").Describe("F4"); got != "F4 is retired: it is now F3" {
		t.Errorf("describe = %q", got)
	}
}

func TestProblems(t *testing.T) {
	spec := Parse(map[string]string{
		"features/F2-approvals.md": `# Approvals

## Purpose

Managers decide claims.

Needs: F9.

## User Stories

- F2.1 As a manager, I approve.
- F2.1 As a manager, I approve twice.
- F2.3 As a manager, I come back.
- F3.1 As finance, I was pasted here.

## Retired

- F2.3 dropped
`,
		"product-wide.md": "# Product-wide\n\n## Requirements\n\n- P1 Logged. Applies to: F2, F6.\n- P1 Logged again. Applies to: all.\n",
		"prd.md":          "# P\n\n## Retired\n\n- F6 Budgets dropped\n",
	})
	var got []string
	for _, p := range spec.Problems() {
		got = append(got, p.Path+" "+p.Code)
	}
	want := []string{
		"features/F2-approvals.md STORY_OUTSIDE_FEATURE",
		"features/F2-approvals.md DUPLICATE_ID",
		"features/F2-approvals.md REUSED_ID",
		"features/F2-approvals.md UNKNOWN_FEATURE_REF",
		"product-wide.md DUPLICATE_ID",
		"product-wide.md UNKNOWN_FEATURE_REF",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("problems =\n%v\nwant\n%v\n(%+v)", got, want, spec.Problems())
	}
}
