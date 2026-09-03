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
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// MintRepairIssues files ONE issue per failed acceptance criterion and returns
// their numbers.
//
// One per criterion rather than one per attempt, because the no-progress rule
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
// cycleID is the idempotence key. It is the ATTEMPT's identity, so a redelivered
// or retried activity within one attempt files nothing new, while a criterion that
// fails again on the NEXT attempt files fresh work rather than being suppressed by
// the closed issue the last repair produced.
func (s *Service) MintRepairIssues(ctx context.Context, orgID, projectID string, milestoneNumber int, report []byte, cycleID string) ([]int, error) {
	if milestoneNumber <= 0 {
		return nil, fmt.Errorf("validation: a milestone is required to file repair issues under")
	}
	if strings.TrimSpace(cycleID) == "" {
		// Without it two attempts would share a dedupe key, and the second attempt's
		// repair work would be silently suppressed by the first attempt's issues.
		return nil, fmt.Errorf("validation: a cycle id is required — it is the repair issues' dedupe key")
	}
	failed := FailedCriteria(report)
	if len(failed) == 0 {
		return nil, nil
	}

	// The generator echoes each criterion's `must` into the report, so the common
	// path needs no second read. The oracle is consulted only to fill a gap — an
	// older report that omitted it — and an unusable oracle is not fatal: the
	// failure message alone is actionable, and refusing to file would leave the run
	// with nothing to work and settle it GREEN over a failure.
	musts := s.mustStatements(ctx, orgID, projectID, failed)

	out := make([]int, 0, len(failed))
	for _, c := range failed {
		must := c.Must
		if must == "" {
			must = musts[c.ID]
		}
		number, _, err := s.writer.Mint(ctx, orgID, projectID, delivery.IssueSpec{
			Title:     fmt.Sprintf("Fix the failing acceptance criterion %s", c.ID),
			Body:      repairIssueBody(c, must),
			Labels:    []string{delivery.LabelAgentWork, delivery.KindBug, delivery.SrcValidation},
			Milestone: milestoneNumber,
			DedupeKey: delivery.DedupeKeyValidationFix(c.ID, cycleID),
		})
		if err != nil {
			return out, fmt.Errorf("validation: create repair issue for %s: %w", c.ID, err)
		}
		if number == 0 {
			// Same hazard EnsureValidationIssue names: an issue exists and we cannot
			// name it. Erroring retries the activity, and the retry dedupes onto it.
			return out, fmt.Errorf("validation: filed a repair issue for %s but got no number back", c.ID)
		}
		out = append(out, number)
	}
	slog.InfoContext(ctx, "validation: filed repair issues for a failed attempt",
		"project", projectID, "milestone", milestoneNumber, "issues", out)
	return out, nil
}

// mustStatements maps criterion id → its `must` text from the oracle at HEAD, for
// the criteria whose report entry did not carry one. It reads nothing when every
// failure is already self-described, which is the normal case.
//
// Empty when the oracle is absent or unusable — see MintRepairIssues on why that is
// tolerated rather than fatal.
func (s *Service) mustStatements(ctx context.Context, orgID, projectID string, failed []FailedCriterion) map[string]string {
	needed := false
	for _, c := range failed {
		if c.Must == "" {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	raw, found, err := s.criteria.ReadValidationCriteria(ctx, orgID, projectID)
	if err != nil || !found {
		return nil
	}
	doc, err := parseCriteria(raw)
	if err != nil {
		return nil
	}
	return doc.mustByID()
}

// repairIssueBody is the prose a coding agent reads. It names the criterion, what
// it demanded, and what the assertion actually said.
//
// The closing paragraph tells the agent the oracle and the validation tests are
// not its to change. That is guidance, not enforcement — nothing checks it yet —
// but the cheapest path to a green report is to weaken the failing assertion, and
// the issue that hands the agent the failure is the right place to say so.
func repairIssueBody(c FailedCriterion, must string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Acceptance criterion **%s** failed when the deployed system was validated. "+
		"This is a defect in the implementation, not in the test.\n\n", c.ID)
	if must != "" {
		fmt.Fprintf(&b, "What it must do:\n\n> %s\n\n", must)
	}
	b.WriteString("What the validation run reported:\n\n")
	fmt.Fprintf(&b, "- Criterion: %s (%s)\n", c.ID, orUnknown(c.Method))
	if c.Spec != "" {
		fmt.Fprintf(&b, "- Spec: %s\n", c.Spec)
	}
	if c.Location != "" {
		fmt.Fprintf(&b, "- Location: %s\n", c.Location)
	}
	if c.Message != "" {
		fmt.Fprintf(&b, "\n```\n%s\n```\n", c.Message)
	}
	b.WriteString("\nFix the implementation so this criterion holds, then include this issue in " +
		"your pull request's Resolves list.\n\n" +
		"Do not change the validation criteria or anything under `tests/` — they are the " +
		"question, not the answer. Validation re-runs every criterion as it stands once your " +
		"fix is built and deployed.\n")
	return b.String()
}

// orUnknown keeps a body from rendering an empty parenthesis for a report that
// omitted the criterion's method.
func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "method unknown"
	}
	return s
}
