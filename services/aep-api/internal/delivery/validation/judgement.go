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

// judgement.go — a version's report read against the one before it (B4).
//
// "Was passing" compares with the PREVIOUS VALIDATED VERSION: the newest
// earlier version whose validation finished, at its final attempt. Not the
// version that last built the feature — v3 building Reports would otherwise be
// told Approvals "was passing in v1" and be blamed for a break v2 made.
// Versions never validated are skipped. A scenario absent from the baseline
// (new, or rewritten when its feature's design changed, which changes its
// key) is a plain failure.

// How a failing scenario stood in the baseline.
const (
	// WasPassing: it passed in the baseline, so this version broke it.
	WasPassing = "regression"
	// WasFailing: it failed in the baseline too.
	WasFailing = "still-failing"
)

// Baseline is the previous validated version's final report.
type Baseline struct {
	Version string
	// Commit is the commit its final attempt validated (its merge commit).
	Commit string
	Report Report
}

// Judgement is one attempt's report, read within its version's scope and
// against the previous validated version.
type Judgement struct {
	// Version is the version judged.
	Version string
	// Scope is what it validates; nil when it validates the whole oracle.
	Scope  *Scope
	Report Report
	// Baseline is nil when no earlier version was validated.
	Baseline *Baseline
	// Built names what this version built ("F3 Payroll export"), for a
	// regression's issue: the change most likely to have broken it.
	Built []string
}

// Failure is a failed scenario and how it stood in the baseline.
type Failure struct {
	FailedScenario
	// Was is WasPassing, WasFailing, or "" for a plain failure.
	Was string
}

// Failures are the report's failed scenarios, each classified.
func (j Judgement) Failures() []Failure {
	var then map[string]string
	if j.Baseline != nil {
		then = j.Baseline.Report.outcomes()
	}
	failed := j.Report.Failed()
	out := make([]Failure, 0, len(failed))
	for _, f := range failed {
		was := ""
		switch then[f.ID] {
		case outcomePassed:
			was = WasPassing
		case outcomeFailed:
			was = WasFailing
		}
		out = append(out, Failure{FailedScenario: f, Was: was})
	}
	return out
}

// Standing keys the failures by how they stood in the baseline: those that
// passed there, and those that failed there too.
func (j Judgement) Standing() (regressions, stillFailing []string) {
	for _, f := range j.Failures() {
		switch f.Was {
		case WasPassing:
			regressions = append(regressions, f.ID)
		case WasFailing:
			stillFailing = append(stillFailing, f.ID)
		}
	}
	return regressions, stillFailing
}

// Regressions counts the failures that passed in the baseline.
func (j Judgement) Regressions() int {
	n := 0
	for _, f := range j.Failures() {
		if f.Was == WasPassing {
			n++
		}
	}
	return n
}
