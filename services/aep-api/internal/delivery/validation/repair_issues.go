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

// repair_issues.go — turning a failed validation attempt into ordinary work.
//
// This is the same rule eventcore/mint.go states for every other
// platform-detected failure: the platform files an issue, the issue joins the
// milestone's working set, and the next cycle works it like any other. Validation
// was the one detected failure that did not follow it — it settled the run instead
// — which is why a failing acceptance criterion needed a human to notice.
//
// The bodies are PROSE. Nothing parses them back: the supervisor learns the
// outcome from the next attempt's report, not from these issues.

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// MintRepairIssues files ONE issue per failed scenario and returns their
// numbers — including the ones it resolved onto rather than filed, so the count
// is "defects outstanding" and not "issues created".
//
// One per scenario rather than one per attempt, because the no-progress rule
// compares WORKING-SET SIZES: an agent that repairs two of three failures takes
// the set from 3 to 1, which reads as progress and lets the loop continue. A
// single issue listing all three cannot be closed until every one is fixed, so the
// same partial repair would look like a cycle that changed nothing and fail the
// run. The granularity is nearly free — a coding cycle is scoped to the milestone,
// so N issues still cost one cycle.
//
// report is the bytes read at the validation cycle's own merge commit; the caller
// owns that read because it already performs it for the verdict. An empty or
// all-green report mints nothing, which is not an error — it is what "there was
// nothing to repair" looks like.
//
// A defect keeps ONE issue across attempts. The dedupe key is the scenario alone,
// so an attempt that meets a scenario still failing resolves onto its open issue
// and leaves the current evidence there as a comment, rather than filing a second
// issue beside the first. See DedupeKeyValidationFix for why the attempt used to
// be part of that key and why it cannot have been buying what it claimed.
func (s *Service) MintRepairIssues(ctx context.Context, orgID, projectID string, milestoneNumber int, report []byte) ([]int, error) {
	if milestoneNumber <= 0 {
		return nil, fmt.Errorf("validation: a milestone is required to file repair issues under")
	}
	failed := FailedScenarios(report)
	if len(failed) == 0 {
		return nil, nil
	}

	// No second read of the oracle: the report carries each scenario's own
	// Given/When/Then, so what the scenario demanded and what the app did are
	// answerable from the report alone.
	out := make([]int, 0, len(failed))
	for _, f := range failed {
		number, deduped, err := s.writer.Mint(ctx, orgID, projectID, delivery.IssueSpec{
			Title:     fmt.Sprintf("Fix the failing scenario: %s", scenarioName(f)),
			Body:      repairIssueBody(f),
			Labels:    []string{delivery.LabelAgentWork, delivery.KindBug, delivery.SrcValidation},
			Milestone: milestoneNumber,
			DedupeKey: delivery.DedupeKeyValidationFix(f.ID),
		})
		if err != nil {
			return out, fmt.Errorf("validation: create repair issue for %s: %w", f.ID, err)
		}
		if number == 0 {
			// Same hazard EnsureValidationIssue names: an issue exists and we cannot
			// name it. Erroring retries the activity, and the retry dedupes onto it.
			return out, fmt.Errorf("validation: filed a repair issue for %s but got no number back", f.ID)
		}
		if deduped {
			// The issue was already open, so its BODY describes an earlier attempt.
			// The evidence a repair agent should act on is this attempt's, and a
			// comment is where it goes.
			//
			// Warned and swallowed rather than returned: the issue exists and still
			// states the scenario is broken, which is the actionable fact. Erroring
			// would retry the whole activity and re-comment every scenario already
			// handled — trading one attempt's trace for duplicate comments on all
			// of them.
			if cerr := s.writer.Comment(ctx, orgID, projectID, number, recurrenceComment(f)); cerr != nil {
				slog.WarnContext(ctx, "validation: could not record a recurrence on the open repair issue",
					"project", projectID, "issue", number, "scenario", f.ID, "error", cerr)
			}
		}
		out = append(out, number)
	}
	slog.InfoContext(ctx, "validation: filed repair issues for a failed attempt",
		"project", projectID, "milestone", milestoneNumber, "issues", out)
	return out, nil
}

// repairIssueBody is the prose a coding agent reads. It names the scenario, quotes
// it as written, and says where the run stopped believing it.
//
// The closing paragraph tells the agent the scenarios are not its to change. That
// is guidance, not enforcement — nothing checks it yet — but the cheapest path to
// a green report is to weaken the failing assertion, and the issue that hands the
// agent the failure is the right place to say so.
func repairIssueBody(f FailedScenario) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The scenario **%s** failed when the deployed system was validated. "+
		"This is a defect in the implementation, not in the specification.\n\n", scenarioName(f))
	if f.Rule != "" {
		fmt.Fprintf(&b, "The rule it illustrates:\n\n> %s\n\n", f.Rule)
	}
	writeTrace(&b, f)
	writeEvidence(&b, f)
	// Trimmed rather than carefully spaced: which sections wrote anything varies
	// per failure, and every arrangement has to end in exactly one blank line.
	return strings.TrimRight(b.String(), "\n") +
		"\n\nFix the implementation so this scenario holds, then include this issue in " +
		"your pull request's Resolves list.\n\n" +
		"Do not change anything under `specs/acceptance/` or `tests/` — the scenarios are " +
		"the question, not the answer. Validation drives every scenario again as it stands " +
		"once your fix is built and deployed.\n"
}

// recurrenceComment is what a LATER attempt leaves on a repair issue that was
// still open when the same scenario failed again.
//
// It exists because the issue's body is a snapshot of the attempt that filed it.
// A repair agent reads the body and the comments, and the evidence it should act
// on is the newest — so the alternative to this comment is not "one tidy issue",
// it is an issue whose only evidence describes a run that has since been
// superseded.
//
// It deliberately does not repeat the instructions: the body already carries
// them, and this issue is the same work it always was.
func recurrenceComment(f FailedScenario) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s** failed again on a later validation attempt, so this issue stays "+
		"open rather than a second one being filed beside it. The body above describes the "+
		"attempt that filed it; this is what the latest run saw.\n\n", scenarioName(f))
	writeTrace(&b, f)
	writeEvidence(&b, f)
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// writeTrace renders the scenario AS EXECUTED — every step, its command, and
// what that command said — with the deciding step marked.
//
// The whole trace rather than the deciding step alone, and the scenario is not
// quoted separately above it: this IS the scenario, plus what happened to each
// line of it. Quoting the Gherkin as well would print every step twice.
//
// What it buys is the distinction a single line could never carry. A `When`
// whose command never ran and a `When` that ran and left the app unchanged are
// opposite defects, and until the trace was rendered a repair agent could not
// tell them apart from anything in the issue.
func writeTrace(b *strings.Builder, f FailedScenario) {
	if len(f.Steps) == 0 {
		return
	}
	b.WriteString("What ran, and what each step did")
	if f.FeatureFile != "" {
		fmt.Fprintf(b, " (`%s`)", withLine(f.FeatureFile, f.Line))
	}
	b.WriteString(":\n\n")
	for i, st := range f.Steps {
		line := strings.TrimSpace(st.Text)
		if st.Keyword != "" {
			line = "**" + st.Keyword + "** " + line
		}
		if i == f.Deciding {
			line += "  ← settled here"
		}
		fmt.Fprintf(b, "%d. %s\n", i+1, line)
		if st.Command != "" {
			if st.Exit != nil {
				fmt.Fprintf(b, "   - `%s` → exit %d\n", st.Command, *st.Exit)
			} else {
				fmt.Fprintf(b, "   - `%s`\n", st.Command)
			}
		}
		if st.Observed != "" {
			fmt.Fprintf(b, "   - observed: %s\n", st.Observed)
		}
	}
	b.WriteString("\n")
}

// writeEvidence renders what the page itself was doing when the scenario failed.
//
// This is the half a compiled suite structurally cannot produce, and it is the
// cheapest signal per byte in the issue: `POST /items → 201` with the list
// unchanged is a rendering defect, and NO request at all is a wiring defect.
// They are fixed in different files.
//
// An empty request list is a finding, not a blank — "nothing left the page" is
// exactly the second case — so it is stated. A run that could not capture at all
// says so in its own words rather than leaving a reader to assume either.
func writeEvidence(b *strings.Builder, f FailedScenario) {
	e := f.Evidence
	if e.NotCaptured != "" {
		fmt.Fprintf(b, "The run could not capture what the page was doing: %s\n", e.NotCaptured)
		return
	}
	if e.Network == nil && len(e.Console) == 0 && e.Snapshot == "" {
		return
	}
	b.WriteString("What the page was doing when it failed:\n\n")
	if len(e.Network) == 0 {
		b.WriteString("- no request left the page\n")
	}
	for _, r := range e.Network {
		fmt.Fprintf(b, "- `%s %s` → %d\n", r.Method, r.URL, r.Status)
	}
	for _, msg := range e.Console {
		fmt.Fprintf(b, "- console: `%s`\n", msg)
	}
	if e.Snapshot != "" {
		b.WriteString("\nThe page as the run saw it is in `tests/acceptance/report.json`, " +
			"under this scenario's `evidence.snapshot`.\n")
	}
}

// scenarioName is what a reader calls this failure. The id is the fallback
// because a scenario with no name still has to be nameable in a title.
func scenarioName(f FailedScenario) string {
	if strings.TrimSpace(f.Scenario) == "" {
		return f.ID
	}
	return f.Scenario
}

// withLine renders `file:line`, or the bare path when the report carried no line.
func withLine(file string, line int) string {
	if line <= 0 {
		return file
	}
	return file + ":" + strconv.Itoa(line)
}
