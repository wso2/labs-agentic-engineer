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
	"maps"
	"reflect"
	"strings"
	"testing"
)

// Acme Expenses: F1 Submit expenses (F1.4 needs F2); F2 Approvals needs F1;
// F3 Payroll export needs F2 and waits on a blocking question; F4 is a stub;
// F5 Mileage claims. P1, P2, P4 apply to all; P3 to F1 and F3.

func set(ids ...string) map[string]bool {
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func TestPlanBuild(t *testing.T) {
	spec := Parse(readFixture(t, acmeFixture))
	nothing := Built{Features: set(), ProductWide: set()}

	cases := []struct {
		name        string
		built       Built
		pick        Pick
		unavailable map[string]string
		want        BuildPlan
		refused     []string
	}{
		{
			name:  "a feature pulls in the unbuilt feature it needs, and the rules that reach either",
			built: nothing, pick: Pick{Features: []string{"F2"}},
			want: BuildPlan{Features: []string{"F1", "F2"}, PulledIn: []string{"F1"}, ProductWide: []string{"P1", "P2", "P3", "P4"}},
		},
		{
			name:  "a story that needs a feature this build does not bring is held back",
			built: nothing, pick: Pick{Features: []string{"F1"}},
			want: BuildPlan{Features: []string{"F1"}, ProductWide: []string{"P1", "P2", "P3", "P4"}, HeldBack: []string{"F1.4"}},
		},
		{
			name:  "what an earlier build brought is neither pulled in again nor carried again",
			built: Built{Features: set("F1", "F2"), ProductWide: set("P1", "P2", "P3", "P4")},
			pick:  Pick{Features: []string{"F5"}},
			want:  BuildPlan{Features: []string{"F5"}},
		},
		{
			name:  "a stub, a blocked feature and an unknown one are refused",
			built: nothing, pick: Pick{Features: []string{"F3", "F4", "F9"}},
			refused: []string{"F3", "F4", "F9"},
		},
		{
			name:  "a needed feature that cannot be built refuses the pick, saying who needs it",
			built: nothing, pick: Pick{Features: []string{"F2"}},
			unavailable: map[string]string{"F1": "its design is out of date"},
			want:        BuildPlan{Features: []string{"F2"}, ProductWide: []string{"P1", "P2", "P4"}},
			refused:     []string{"F1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, refusals := PlanBuild(spec, tc.built, tc.pick, tc.unavailable)
			var refused []string
			for _, r := range refusals {
				refused = append(refused, r.ID)
			}
			if !reflect.DeepEqual(refused, tc.refused) {
				t.Errorf("refused = %v (%+v), want %v", refused, refusals, tc.refused)
			}
			if len(tc.refused) == 0 || len(tc.want.Features) > 0 {
				if !reflect.DeepEqual(plan, tc.want) {
					t.Errorf("plan = %+v, want %+v", plan, tc.want)
				}
			}
		})
	}
}

func TestPlanBuild_RefusalSaysWhoNeedsIt(t *testing.T) {
	spec := Parse(readFixture(t, acmeFixture))
	_, refusals := PlanBuild(spec, Built{}, Pick{Features: []string{"F2"}}, map[string]string{"F1": "its design is out of date"})
	if len(refusals) != 1 || refusals[0].Reason != "F1 Submit expenses: its design is out of date (F2 needs it)" {
		t.Errorf("refusals = %+v", refusals)
	}
}

// A product-wide rule added after features were built rebuilds every feature
// it reaches that is built or buildable, so it is never in only part of the
// product.
func TestPlanBuild_ANewRulePullsInEveryFeatureItReaches(t *testing.T) {
	files := maps.Clone(readFixture(t, acmeFixture))
	files["product-wide.md"] = strings.Replace(files["product-wide.md"], "## Decisions", "- P5 Amounts in any currency. Applies to: all.\n\n## Decisions", 1)
	spec := Parse(files)
	built := Built{Features: set("F1", "F2", "F5"), ProductWide: set("P1", "P2", "P3", "P4")}
	plan, refusals := PlanBuild(spec, built, Pick{ProductWide: []string{"P5"}}, nil)
	if len(refusals) != 0 {
		t.Fatalf("refusals = %+v", refusals)
	}
	want := BuildPlan{Features: []string{"F1", "F2", "F5"}, PulledIn: []string{"F1", "F2", "F5"}, ProductWide: []string{"P5"}}
	if !reflect.DeepEqual(plan, want) {
		t.Errorf("plan = %+v, want %+v", plan, want)
	}
	if got := plan.Stories(spec); len(got) != 4+4+1 {
		t.Errorf("stories = %v", got)
	}
}
