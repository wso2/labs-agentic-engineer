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

package app

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/validation"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The judge reads an attempt as its version (B4): within what the version
// built, and against the previous VALIDATED version — skipping one that was
// never judged — at that version's final attempt.

const approvalsOracle = `Feature: F2 Approvals

  @story-F2.1
  Rule: A manager sees pending claims
    Scenario: The queue
      Then the claim is there
`

func reportWith(outcome string) string {
	return `{"schemaVersion":2,"scenarios":[{"feature":"F2 Approvals","featureFile":"specs/validation/acceptance/F2-approvals.feature",` +
		`"rule":"A manager sees pending claims","scenario":"The queue","outcome":"` + outcome + `",` +
		`"steps":[{"keyword":"Then","text":"the claim is there","command":"x","exit":1,"observed":"empty"}],"evidence":{"network":[]}}]}`
}

// judgeFiles serves a report and the oracle per commit.
type judgeFiles struct {
	spec.FilesService
	reports map[string]string
}

func (f judgeFiles) ReadAt(_ context.Context, _, _, path, at string) (*spec.FileContent, error) {
	r, ok := f.reports[at]
	if !ok || path != validation.ReportFilePath {
		return nil, spec.ErrFileNotFound
	}
	return &spec.FileContent{Path: path, Content: r}, nil
}

func (f judgeFiles) Bundle(context.Context, string, string, string, string) (*spec.FileBundle, error) {
	return &spec.FileBundle{Files: []spec.FileContent{{Path: "specs/validation/acceptance/F2-approvals.feature", Content: approvalsOracle}}}, nil
}

type judgeScopes map[string]spec.ValidationScope

func (s judgeScopes) ValidationScope(_ context.Context, _, _, version string) (spec.ValidationScope, bool, error) {
	vs, ok := s[version]
	return vs, ok, nil
}

// judgeRuns: one milestone per version, each with its validation runs.
type judgeRuns struct {
	delivery.MilestoneRunRepository
	milestones map[string]int
	runs       map[int][]delivery.MilestoneRun
}

func (r judgeRuns) MilestoneNumberForTag(_ context.Context, _, _, tag string) (int, bool, error) {
	n, ok := r.milestones[tag]
	return n, ok, nil
}

func (r judgeRuns) ListByMilestone(_ context.Context, _, _ string, n int) ([]delivery.MilestoneRun, error) {
	return r.runs[n], nil
}

type judgeCycles struct {
	delivery.RunCycleRepository
	byRun map[string][]delivery.RunCycle
}

func (c judgeCycles) ListByRun(_ context.Context, _, runID string) ([]delivery.RunCycle, error) {
	return c.byRun[runID], nil
}

func TestRunValidation_JudgesAgainstThePreviousValidatedVersion(t *testing.T) {
	validated := func(id string) delivery.MilestoneRun {
		return delivery.MilestoneRun{ID: id, Kind: delivery.RunKindValidation, State: delivery.RunStateFailed}
	}
	cycle := func(sha, verdict string) delivery.RunCycle {
		return delivery.RunCycle{Kind: delivery.CycleKindValidation, MergeSHA: sha, ValidationVerdict: verdict}
	}
	a := runValidation{
		files: judgeFiles{reports: map[string]string{
			"v1-final": reportWith("passed"),
			"v1-first": reportWith("failed"),
			"v3-now":   reportWith("failed"),
		}},
		versions: judgeScopes{
			"v1": {Features: []string{"F2"}},
			"v3": {Features: []string{"F2", "F4"}, Built: []string{"F4 Spending reports"}, Earlier: []string{"v2", "v1"}},
		},
		runs: judgeRuns{
			milestones: map[string]int{"v1": 1, "v2": 2},
			runs: map[int][]delivery.MilestoneRun{
				1: {validated("r1")},
				2: {{ID: "dev2", Kind: delivery.RunKindDev, State: delivery.RunStateSucceeded}}, // never validated
			},
		},
		cycles: judgeCycles{byRun: map[string][]delivery.RunCycle{
			"r1": {cycle("v1-first", delivery.ValidationVerdictFailed), cycle("v1-final", delivery.ValidationVerdictPassed)},
		}},
	}
	j, err := a.judge(context.Background(), "org", "proj", "v3", "v3-now")
	if err != nil {
		t.Fatal(err)
	}
	if j.Baseline == nil || j.Baseline.Version != "v1" || j.Baseline.Commit != "v1-final" {
		t.Fatalf("baseline = %+v, want v1 at its final attempt (v2 was never validated)", j.Baseline)
	}
	if f := j.Failures(); len(f) != 1 || f[0].Was != validation.WasPassing || f[0].Stories[0] != "F2.1" {
		t.Errorf("failures = %+v, want one regression on F2.1", f)
	}
	if v, _, n, _ := a.Verdict(context.Background(), "org", "proj", "v3", "v3-now"); v != delivery.ValidationVerdictFailed || n != 1 {
		t.Errorf("verdict = %q with %d regressions", v, n)
	}
	st, err := a.Standing(context.Background(), "org", "proj", "v3", "v3-now")
	if err != nil || !st.Scoped || st.BaselineVersion != "v1" || len(st.Regressions) != 1 {
		t.Errorf("standing = %+v, %v", st, err)
	}
}
