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

package spec

import (
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/reqspec"
)

func TestAcceptanceFindings(t *testing.T) {
	reqs := map[string]string{}
	for k, v := range gateRequirements {
		reqs[k] = v
	}
	spec := reqspec.Parse(reqs)

	files := map[string]string{
		// F1's file tags F1.1 and F1.2, a retired F1.3 and a feature ID, and
		// leaves F1.4 on no rule.
		"F1-ordering.feature": "Feature: F1 Ordering\n\n  @story-F1.1 @story-F1.2\n  Rule: a\n\n  @story-F1.3\n  Rule: b\n\n  @story-F1\n  Rule: c\n",
		// F2's file covers its one story.
		"F2-notifications.feature": "Feature: F2 Notifications\n\n  @story-F2.1\n  Rule: d\n\n    @negative\n    Scenario: e\n",
		// A file from before features: its numeric tag is unknown.
		"lunch.feature": "Feature: Lunch\n\n  @story-4\n  Rule: f\n",
	}
	var got []string
	for _, f := range acceptanceFindings(spec, files, []string{"F1", "F2"}) {
		got = append(got, f.Path+" "+f.Code+" "+f.Message)
	}
	want := []string{
		"specs/validation/acceptance/F1-ordering.feature ACCEPTANCE_STALE_STORY @story-F1.3 — F1.3 is retired and nothing replaced it; drop it",
		"specs/validation/acceptance/F1-ordering.feature ACCEPTANCE_STALE_STORY @story-F1 — F1 is not a story; tag the stories it holds",
		"specs/validation/acceptance/lunch.feature ACCEPTANCE_STALE_STORY @story-4 — 4 is not in the requirements; tag a real story or drop it",
		"specs/validation/acceptance/F1-ordering.feature ACCEPTANCE_UNCOVERED_STORY F1.4 is on no rule — add a rule tagged @story-F1.4, or drop the story",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings =\n%v\nwant\n%v", got, want)
	}
	if got := acceptanceFindings(spec, nil, []string{"F1", "F2"}); len(got) != 0 {
		t.Errorf("no acceptance files is validation's business, got %+v", got)
	}
}
