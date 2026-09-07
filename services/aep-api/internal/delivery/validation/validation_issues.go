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
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// criteriaFilePath is the acceptance-oracle path in the project repo, authored
// by the validation-criteria skill and read by the runner.
const criteriaFilePath = "specs/validation/validation-criteria.json"

// validationTitle is the fixed title of a version's validation issue. It names
// no version: the MILESTONE is the version pin, and a title is renamable display
// text that nothing matches on.
const validationTitle = "Validate the deployed system against its validation criteria"

// Service mints the project's validation Task issue. It holds only consumer
// ports; concrete providers are wired at the composition root.
type Service struct {
	issues   IssueClient
	writer   *delivery.IssueWriter
	criteria CriteriaReader
}

// Deps is the validation service's collaborator set.
type Deps struct {
	Issues IssueClient
	// Writer is the domain's issue-write surface: the validation issue, its
	// reopen across attempts, and a failed attempt's repair issues all go
	// through it rather than being written here.
	Writer   *delivery.IssueWriter
	Criteria CriteriaReader
}

// NewService wires the validation service from its collaborator set.
func NewService(d Deps) *Service {
	return &Service{issues: d.Issues, writer: d.Writer, criteria: d.Criteria}
}

// EnsureValidationIssue mints ONE validation task per version — filed into
// that version's milestone — and returns its number. 0 means there is nothing to
// validate, which settles the run `skipped`; a missing or malformed criteria file
// is that clean no-op.
//
// It is idempotent ACROSS ATTEMPTS, which is stronger than it sounds. A version
// may be judged more than once (RunMaxValidationAttempts), and the PLATFORM closes
// the task at the end of every attempt (CloseValidationIssue), so by the time a
// repeat attempt asks, the version's validation issue is CLOSED. This therefore
// looks for the issue in any state and reopens a closed one rather than filing a
// second — the previous filter was `state: open`, which was only ever right for a
// version judged once.
//
// An issue found already OPEN is ADOPTED as this attempt's, not re-filed: the task
// is the version's persistent handle, and a second one would split the thread the
// attempts comment on and double what the reconcile sweep sees as unworked.
//
// The body is NOT rewritten on reopen. It embeds the oracle as rendered at first
// mint, which is the question this version is being asked; each attempt's own
// summary comment (the skill posts one when it opens its pull request) is what
// makes the thread readable across attempts.
//
// Per VERSION and not per project, because the issue body embeds the criteria
// table rendered at mint time. Adopting the previous version's issue would hand
// this version's agent the previous version's oracle, and re-filing it would
// erase it from the ledger of the version it actually validated.
//
// The number comes from whichever step DECIDED it — the milestone lookup or the
// create's own result — and is never re-discovered by listing afterwards.
// GitHub's issue index lags a write by a beat, so a read-back answered "no
// validation issue" for an issue this call had just filed, and the run reported
// `skipped` over an oracle it was holding in its hand.
//
// The issue is PROSE with two labels: ARMED (`aep`) and of kind `validation`.
// Armed because it is real agent work — an agent is dispatched at it and opens a
// pull request the platform must auto-merge — and of a kind no working set
// includes, which is what keeps it from holding a run's settle predicate open
// forever. Those two facts used to be one label decision pulling in opposite
// directions: the issue carried NO arming label so it would stay out of the
// working set, and the auto-merge policy then had to name it a second time by
// hand or its pull request never landed. Both now read the kind.
func (s *Service) EnsureValidationIssue(ctx context.Context, orgID, projectID string, milestoneNumber int) (int, error) {
	if milestoneNumber <= 0 {
		// A validation issue with no version is the state this refuses to create:
		// invisible to every milestone-scoped read, and belonging to no ledger.
		return 0, fmt.Errorf("validation: a milestone is required to file the validation issue under")
	}
	raw, found, err := s.criteria.ReadValidationCriteria(ctx, orgID, projectID)
	if err != nil {
		return 0, fmt.Errorf("validation: read criteria: %w", err)
	}
	if !found {
		// The design agent has not authored the oracle yet — a later planning
		// pass re-mints once it exists.
		return 0, nil
	}
	doc, err := parseCriteria(raw)
	if err != nil {
		// A malformed oracle is the design agent's bug, not a reason to fail the
		// save; skip and let a corrected pass re-mint.
		slog.WarnContext(ctx, "validation: skipping mint — criteria file unusable", "project", projectID, "error", err)
		return 0, nil
	}

	// Does THIS version already have one? Nothing has been written at this point,
	// so the read is a straight question about GitHub's settled state rather than
	// a read-back of our own write. It also adopts an issue a human filed by hand.
	existing, open, err := s.findValidationIssue(ctx, orgID, projectID, milestoneNumber)
	if err != nil {
		return 0, err
	}
	if existing > 0 {
		if !open {
			// A repeat attempt: the previous attempt's pull request closed it.
			if rerr := s.writer.Reopen(ctx, orgID, projectID, existing); rerr != nil {
				return 0, fmt.Errorf("validation: reopen issue #%d: %w", existing, rerr)
			}
			slog.InfoContext(ctx, "validation: reopened validation issue for a repeat attempt",
				"project", projectID, "milestone", milestoneNumber, "issue", existing)
		}
		return existing, nil
	}

	number, _, cerr := s.writer.Mint(ctx, orgID, projectID, delivery.IssueSpec{
		Title:  validationTitle,
		Body:   rationale(doc.summarize()) + "\n\n" + renderScope(doc),
		Labels: []string{delivery.LabelAgentWork, delivery.KindValidation},
		// The version pin RIDES the create — one call, so the issue is never
		// versionless, not even for the beat a follow-up patch would take.
		Milestone: milestoneNumber,
		// Version-scoped so a later version's mint is never deduped against this
		// one. Mirrors the provision gate's gate:<project>:<tag>:<dep>.
		DedupeKey: delivery.DedupeKeyValidationIssue(projectID, milestoneNumber),
	})
	if cerr != nil {
		return 0, fmt.Errorf("validation: create issue: %w", cerr)
	}
	if number == 0 {
		// An issue exists somewhere and we cannot name it. Returning 0 here would
		// read as "no oracle" and settle the run `skipped`; an error retries the
		// activity, and the retry finds the open issue above.
		return 0, fmt.Errorf("validation: created the validation issue but got no number back")
	}
	return number, nil
}

// CloseValidationIssue closes the version's validation task, leaving a comment
// that names the verdict the attempt reached — or its absence.
//
// The platform owns this close, and that ownership is the whole reason the method
// exists. The validation pull request references its issue with `Validates #N`,
// which is deliberately not one of GitHub's closing keywords, so merging links the
// two without ending the task. Letting the merge close it instead put the
// lifecycle in two hands: the platform reopens the task for the next attempt, and
// a reopen racing GitHub's own close is indistinguishable from a human reopening
// it.
//
// Single ownership is also what lets a run close the task on an ending where NO
// pull request ever merged — an agent that died through its whole re-dispatch
// budget. That matters more than tidiness: the reconcile sweep starts a validation
// run BECAUSE this issue is open, so a task left open after a dead dispatch would
// be picked up again within a tick, forever, with nothing outside the workflow
// able to repair it.
//
// The comment is prose and nothing parses it. It exists so a human opening the
// closed task can see which attempt closed it and why, without reading the run
// timeline.
func (s *Service) CloseValidationIssue(ctx context.Context, orgID, projectID string, issue int, verdict string) error {
	if issue <= 0 {
		return nil
	}
	if err := s.writer.Close(ctx, orgID, projectID, issue, closeComment(verdict)); err != nil {
		return fmt.Errorf("validation: close issue #%d: %w", issue, err)
	}
	slog.InfoContext(ctx, "validation: closed the version's validation task",
		"project", projectID, "issue", issue, "verdict", verdict)
	return nil
}

// closeComment is what the platform says when it closes the task. An empty
// verdict is its own sentence rather than a blank: the attempt ended without one,
// which is a different thing from a verdict of `inconclusive` and the reader has
// to be able to tell them apart.
func closeComment(verdict string) string {
	if verdict == "" {
		return "Closing this validation task: the attempt ended without reaching a verdict. " +
			"The version is deployed and unjudged — trigger validation again to ask its criteria."
	}
	return fmt.Sprintf("Closing this validation task: the attempt concluded `%s`. "+
		"Reopened automatically if the version is judged again.", verdict)
}

// findValidationIssue returns the number of the milestone's validation task
// and whether it is currently open, or (0, false) when that version has none. The
// milestone and the LABELS are the whole query — nothing parses a body, and
// nothing looks outside the version.
//
// Both labels are listed because this filter is the REST one, whose `?labels=a,b`
// is AND: it demands an armed issue of kind `validation`, which is exactly the
// validation task and nothing else. (The GraphQL argument spelled the same way
// is a UNION and would have matched every armed issue in the version.)
//
// State is `all` rather than `open` on purpose: a closed validation issue is the
// NORMAL state between attempts, because every attempt's pull request closes it.
// Asking only for open ones made a repeat attempt file a second issue for the same
// version, each embedding its own snapshot of the oracle.
func (s *Service) findValidationIssue(ctx context.Context, orgID, projectID string, milestoneNumber int) (number int, open bool, err error) {
	issues, err := s.issues.ListMilestoneIssues(ctx, orgID, projectID, sourcecontrol.MilestoneIssuesFilter{
		Number: milestoneNumber,
		State:  "all",
		Labels: []string{delivery.LabelAgentWork, delivery.KindValidation},
	})
	if err != nil {
		return 0, false, fmt.Errorf("validation: list milestone issues: %w", err)
	}
	if len(issues) == 0 {
		return 0, false, nil
	}
	// Prefer an OPEN one when the version somehow has several — a human may have
	// filed one by hand alongside a platform-minted one, and the open issue is the
	// one an agent would be dispatched at.
	for i := range issues {
		if strings.EqualFold(issues[i].State, "open") {
			return issues[i].Number, true, nil
		}
	}
	return issues[0].Number, false, nil
}

// rationale is the one-line blockquote summary in the issue body.
func rationale(s criteriaSummary) string {
	return fmt.Sprintf("Validate the deployed system against %d e2e, %d scenario, and %d manual acceptance criteria.",
		s.E2E, s.Scenario, s.Manual)
}

// renderScope builds the human markdown body of the validation issue — the
// consumer contract the aep-validation skill reads (validation criteria + test
// layout + report). Deployed endpoints and test credentials are deliberately
// absent: the runner fetches endpoints from the secure validation-context
// endpoint, and the agent reads a test user's login from the roles gate ticket
// in this same milestone. Mirrors scripts/create-validation-issue.mjs
// renderBody minus the Deployed-endpoints section.
func renderScope(doc *criteriaDoc) string {
	sum := doc.summarize()
	var b strings.Builder
	w := func(lines ...string) {
		for _, l := range lines {
			b.WriteString(l)
			b.WriteByte('\n')
		}
	}

	w(
		"Validate the deployed system against its validation criteria: author end-to-end tests, run them against the deployed system, and open a PR containing the tests and a validation report.",
		"",
		"The deployed endpoint URLs are provided to the validation runner by the platform at dispatch time, and a test user's login is published on this milestone's roles gate ticket — neither is in this issue.",
		"",
		"## Validation criteria",
		fmt.Sprintf("The source of truth is `%s` in this repo. It is read-only input for this task — do not modify it or anything else under `specs/`.", criteriaFilePath),
		"",
		fmt.Sprintf("- `e2e` — %d criteria: a committed spec already at `tests/e2e/specs/<AC-ID>.spec.ts` runs as regression; author specs for the rest.", sum.E2E),
		fmt.Sprintf("- `manual` — %d criteria: render as an unchecked human checklist in the report.", sum.Manual),
		fmt.Sprintf("- `scenario` — %d criteria: out of scope for automation in this run; list as not-yet-validated in the report.", sum.Scenario),
		"",
	)

	for _, r := range doc.Requirements {
		w(fmt.Sprintf("### %s — %s", r.ID, r.Statement), "", "| Criterion | Method | Must |", "|---|---|---|")
		for _, c := range r.Criteria {
			w(fmt.Sprintf("| %s | %s | %s |", c.ID, c.Method, strings.ReplaceAll(c.Must, "|", "\\|")))
		}
		w("")
	}

	w(
		"Per-component design docs: `specs/design/components/<name>/design.json` (OpenAPI contract, when present, alongside as `openapi.yaml`); system design: `specs/design/design.cell` (architecture), `specs/design/domain-model.md` (entities), `specs/design/flows/` (key flows).",
		"",
		"## Test layout",
		"- Playwright package at repo root `tests/e2e/` (own `package.json`; do not touch application source under any component app path).",
		"- One spec file per criterion: `tests/e2e/specs/<AC-ID>.spec.ts`; test title MUST start with `<AC-ID>: ` — that prefix is the join key for the report.",
		"- UI criteria: browser specs (`@playwright/test`). API criteria: the built-in `request` fixture. Explore with `playwright-cli` first; never commit exploration sessions.",
		"",
		"## Report",
		"- Commit `tests/validation/report.md` (summary, per-criterion results, manual checklist, scenario not-yet-validated list) and `tests/validation/report.json`.",
		"- Post a summary comment on this issue when done.",
		"",
		"---",
		"Open one PR whose body includes `Validates #<this issue's number>` so the platform links it back. `Validates` is deliberately NOT one of GitHub's closing keywords: the platform owns this task's close, so that merging the PR links it without ending the task. One PR; tests and report only.",
	)

	return strings.TrimRight(b.String(), "\n")
}
