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

package validation

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// B4: a version validates what it built, and each failure is read against the
// previous validated version.

const approvalsFeature = `Feature: F2 Approvals

  @story-F2.1
  Rule: A manager sees pending claims

    Scenario: The queue
      Then the claim is there

  @story-F2.4
  Rule: A deputy approves in the manager's place

    Scenario: Deputy approves
      Then the claim is approved
`

const reportsFeature = `Feature: F4 Spending reports

  Rule: Totals by month
    Scenario: The monthly total
      Then it adds up
`

var b4Criteria = []AcceptanceCriteriaFile{
	{Path: "specs/validation/acceptance/F2-approvals.feature", Content: approvalsFeature},
	{Path: "specs/validation/acceptance/F4-spending-reports.feature", Content: reportsFeature},
}

func scenarioJSON(feature, file, rule, name, outcome string) string {
	return `{"feature":"` + feature + `","featureFile":"specs/validation/acceptance/` + file + `","rule":"` + rule +
		`","scenario":"` + name + `","outcome":"` + outcome + `","steps":[{"keyword":"Then","text":"it holds","command":"x","exit":1,"observed":"no"}],"evidence":{"network":[]}}`
}

func reportOf(scenarios ...string) Report {
	return ParseReport([]byte(`{"schemaVersion":2,"scenarios":[` + strings.Join(scenarios, ",") + `]}`))
}

var (
	queuePassed   = scenarioJSON("F2 Approvals", "F2-approvals.feature", "A manager sees pending claims", "The queue", "passed")
	queueFailed   = scenarioJSON("F2 Approvals", "F2-approvals.feature", "A manager sees pending claims", "The queue", "failed")
	deputyFailed  = scenarioJSON("F2 Approvals", "F2-approvals.feature", "A deputy approves in the manager's place", "Deputy approves", "failed")
	monthlyFailed = scenarioJSON("F4 Spending reports", "F4-spending-reports.feature", "Totals by month", "The monthly total", "failed")
)

func TestScope_KeepsBuiltFeaturesAndUnheldStories(t *testing.T) {
	scope := NewScope("v2", []string{"F2"}, []string{"F2.4"}, b4Criteria)
	r := reportOf(queuePassed, deputyFailed, monthlyFailed).Within(scope)
	if got := r.Verdict(); got != delivery.ValidationVerdictPassed {
		t.Errorf("verdict = %q, want passed: the deputy's story is held back and Spending reports is not built", got)
	}
	if files := scope.files(b4Criteria); len(files) != 1 || files[0].Path != b4Criteria[0].Path {
		t.Errorf("files = %v, want Approvals' only", files)
	}
	if got := reportOf(monthlyFailed).Within(scope).Verdict(); got != delivery.ValidationVerdictUnreported {
		t.Errorf("a report of nothing in scope = %q, want unreported", got)
	}
}

func TestScenarioKey_IsTheFeatureID(t *testing.T) {
	renamed := scenarioJSON("F2 Claim review", "F2-claim-review.feature", "A manager sees pending claims", "The queue", "failed")
	a, b := reportOf(queueFailed).Failed()[0].ID, reportOf(renamed).Failed()[0].ID
	if a != b || !strings.HasPrefix(a, "F2 / ") {
		t.Errorf("keys %q and %q: a renamed feature must keep its scenarios' keys", a, b)
	}
}

func TestJudgement_ClassifiesAgainstTheBaseline(t *testing.T) {
	scope := NewScope("v3", []string{"F2", "F4"}, nil, b4Criteria)
	j := Judgement{
		Version:  "v3",
		Report:   reportOf(queueFailed, deputyFailed, monthlyFailed).Within(scope),
		Baseline: &Baseline{Version: "v2", Commit: "abc1234def", Report: reportOf(queuePassed, deputyFailed)},
		Built:    []string{"F4 Spending reports"},
	}
	was := map[string]string{}
	for _, f := range j.Failures() {
		was[f.Scenario] = f.Was
	}
	want := map[string]string{"The queue": WasPassing, "Deputy approves": WasFailing, "The monthly total": ""}
	for k, v := range want {
		if was[k] != v {
			t.Errorf("%s: was = %q, want %q", k, was[k], v)
		}
	}
	if j.Regressions() != 1 {
		t.Errorf("regressions = %d, want 1", j.Regressions())
	}
	if f := j.Failures()[0]; !slices.Equal(f.Stories, []string{"F2.1"}) {
		t.Errorf("stories = %v, want the rule's F2.1", f.Stories)
	}
}

func TestMintRepairIssues_RegressionsGetTheirOwnIssue(t *testing.T) {
	iss := &fakeIssues{}
	svc := newSvc(iss, fakeCriteria{raw: []byte(sampleCriteria), found: true})
	j := Judgement{
		Version:  "v3",
		Report:   reportOf(queueFailed, monthlyFailed),
		Baseline: &Baseline{Version: "v2", Commit: "abc1234def", Report: reportOf(queuePassed)},
		Built:    []string{"F4 Spending reports"},
	}
	if _, err := svc.MintRepairIssues(context.Background(), "org", "proj", thisMilestone, j); err != nil {
		t.Fatal(err)
	}
	if len(iss.created) != 2 {
		t.Fatalf("created %d issues, want 2", len(iss.created))
	}
	reg, plain := iss.created[0], iss.created[1]
	if reg.Title != "Regression in F2 Approvals: The queue" || !slices.Contains(reg.Labels, delivery.LabelRegression) {
		t.Errorf("regression issue = %q %v", reg.Title, reg.Labels)
	}
	for _, want := range []string{"passed in v2 (`abc1234`)", "fails in v3", "`v2...v3`", "Built in v3: F4 Spending reports."} {
		if !strings.Contains(reg.Body, want) {
			t.Errorf("regression body lacks %q:\n%s", want, reg.Body)
		}
	}
	if plain.Title != "Fix the failing scenario in F4 Spending reports: The monthly total" || slices.Contains(plain.Labels, delivery.LabelRegression) {
		t.Errorf("plain issue = %q %v", plain.Title, plain.Labels)
	}
}

func TestMintRepairIssues_StillFailingCommentsOnTheOpenIssue(t *testing.T) {
	failed := reportOf(deputyFailed)
	iss := &fakeIssues{openByDedupe: map[string]int{delivery.DedupeKeyValidationFix(failed.Failed()[0].ID): 7}}
	svc := newSvc(iss, fakeCriteria{raw: []byte(sampleCriteria), found: true})
	j := Judgement{Version: "v3", Report: failed, Baseline: &Baseline{Version: "v2", Report: reportOf(deputyFailed)}}
	if _, err := svc.MintRepairIssues(context.Background(), "org", "proj", thisMilestone, j); err != nil {
		t.Fatal(err)
	}
	if len(iss.created) != 0 || len(iss.comments) != 1 || !strings.Contains(iss.comments[0].body, "still failing in v3 — it failed in v2 as well") {
		t.Errorf("created %d, comments %+v", len(iss.created), iss.comments)
	}
}

func TestEnsureValidationIssue_NamesTheVersionScope(t *testing.T) {
	iss := &fakeIssues{}
	svc := NewService(Deps{Issues: iss, Writer: iss.writer(), Criteria: multiCriteria(b4Criteria)})
	scope := NewScope("v2", []string{"F2"}, []string{"F2.4"}, nil)
	if _, err := svc.EnsureValidationIssue(context.Background(), "org", "proj", thisMilestone, scope); err != nil {
		t.Fatal(err)
	}
	body := iss.created[0].Body
	for _, want := range []string{"F2-approvals.feature", "## Not run in v2", "`specs/validation/acceptance/F4-spending-reports.feature` — not built yet", "F2.4", "`--features F2 --held-back F2.4`"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
}

// multiCriteria serves several acceptance files.
type multiCriteria []AcceptanceCriteriaFile

func (m multiCriteria) ReadAcceptanceCriteria(context.Context, string, string) ([]AcceptanceCriteriaFile, bool, error) {
	return m, len(m) > 0, nil
}
