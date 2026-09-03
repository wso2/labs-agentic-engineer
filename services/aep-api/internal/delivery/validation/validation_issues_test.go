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
	"reflect"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The version under test, and the one before it. Two distinct milestones are
// what tell a version-scoped lookup apart from a project-wide one.
const (
	thisMilestone = 5
	pastMilestone = 4
)

// ---- fakes ------------------------------------------------------------------

type fakeIssues struct {
	// byMilestone is the host's issue index — milestone number → its issues. The
	// fake HONOURS the filter it is handed, because one that ignored it would let
	// a project-wide query pass for a milestone-scoped one.
	byMilestone map[int][]sourcecontrol.IssueInfo
	// filters records what was asked, so a test can assert the question and not
	// just the answer.
	filters []sourcecontrol.MilestoneIssuesFilter
	created []sourcecontrol.CreateIssueRequest
	// reopened records the issue numbers ReopenIssue was called for, so a test can
	// assert a repeat attempt adopted the version's issue instead of filing a
	// second one.
	reopened []int
	// numberless models a provider that files the issue and reports back no
	// number — the one create outcome the minter must not read as "nothing to
	// validate".
	numberless bool
	// closed and labelled record the writes this package never makes. The fake
	// wears delivery.IssueOps — the domain's WHOLE issue-write surface — so an
	// unexpected write has somewhere to land instead of failing to compile.
	closed        []int
	closeComments []string
	labelled      []string
	// lifecycle records close/reopen in the order they happened. The ORDER is the
	// whole assertion in the race tests below: which of the two landed last is
	// what decides whether the reconcile sweep starts another validation run.
	lifecycle []string
}

// writer is the fake wearing the domain's issue-write surface, which is what
// the validation and repair mints go through.
func (f *fakeIssues) writer() *delivery.IssueWriter { return delivery.NewIssueWriter(f) }

func (f *fakeIssues) CloseIssue(_ context.Context, _, _ string, number int, comment string) error {
	f.closed = append(f.closed, number)
	f.closeComments = append(f.closeComments, comment)
	f.lifecycle = append(f.lifecycle, fmt.Sprintf("close:%d", number))
	f.setState(number, "closed")
	return nil
}

// setState mirrors the host: a close and a reopen move the issue's state, so a
// test can drive a whole task lifecycle through the real service instead of
// hand-editing the index between calls.
func (f *fakeIssues) setState(number int, state string) {
	for milestone := range f.byMilestone {
		for i := range f.byMilestone[milestone] {
			if f.byMilestone[milestone][i].Number == number {
				f.byMilestone[milestone][i].State = state
			}
		}
	}
}

func (f *fakeIssues) CommentIssue(context.Context, string, string, int, string) error { return nil }

func (f *fakeIssues) AddLabels(_ context.Context, _, _ string, number int, labels []string) error {
	for _, l := range labels {
		f.labelled = append(f.labelled, fmt.Sprintf("%d+%s", number, l))
	}
	return nil
}

func (f *fakeIssues) RemoveLabel(_ context.Context, _, _ string, number int, label string) error {
	f.labelled = append(f.labelled, fmt.Sprintf("%d-%s", number, label))
	return nil
}

// SetIssueMilestone exists only so the fake satisfies delivery.IssueOps: the
// writer's port is the domain's WHOLE issue-write surface, and moving an issue
// between versions belongs to the build's supersede, never to this minter.
func (f *fakeIssues) SetIssueMilestone(_ context.Context, _, _ string, number, milestoneNumber int) error {
	f.labelled = append(f.labelled, fmt.Sprintf("%d>m%d", number, milestoneNumber))
	return nil
}

func (f *fakeIssues) ListMilestoneIssues(_ context.Context, _, _ string, filter sourcecontrol.MilestoneIssuesFilter) ([]sourcecontrol.IssueInfo, error) {
	f.filters = append(f.filters, filter)
	var out []sourcecontrol.IssueInfo
	for _, issue := range f.byMilestone[filter.Number] {
		// `all` is GitHub's "do not filter by state", and the fake has to honour that
		// spelling: treating it as a literal state to match would hide every issue and
		// make a state-blind lookup look like a version with no validation issue.
		if filter.State != "" && !strings.EqualFold(filter.State, "all") &&
			!strings.EqualFold(issue.State, filter.State) {
			continue
		}
		// REST narrows as labels are added (sourcecontrol/README.md) — every
		// requested label must be present.
		if !hasEveryLabel(issue.Labels, filter.Labels) {
			continue
		}
		out = append(out, issue)
	}
	return out, nil
}

func (f *fakeIssues) CreateIssue(_ context.Context, _, _ string, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	f.created = append(f.created, req)
	if f.numberless {
		return &sourcecontrol.IssueResult{URL: "https://example/issues/unknown"}, nil
	}
	return &sourcecontrol.IssueResult{Number: 42, URL: "https://example/issues/42"}, nil
}

func (f *fakeIssues) ReopenIssue(_ context.Context, _, _ string, number int) error {
	f.reopened = append(f.reopened, number)
	f.lifecycle = append(f.lifecycle, fmt.Sprintf("reopen:%d", number))
	f.setState(number, "open")
	return nil
}

// stateOf reports an issue's current state in the fake's index.
func (f *fakeIssues) stateOf(number int) string {
	for milestone := range f.byMilestone {
		for _, issue := range f.byMilestone[milestone] {
			if issue.Number == number {
				return issue.State
			}
		}
	}
	return ""
}

func hasEveryLabel(have, want []string) bool {
	for _, label := range want {
		if !delivery.HasLabel(have, label) {
			return false
		}
	}
	return true
}

type fakeCriteria struct {
	raw   []byte
	found bool
}

func (f fakeCriteria) ReadValidationCriteria(_ context.Context, _, _ string) ([]byte, bool, error) {
	return f.raw, f.found, nil
}

const sampleCriteria = `{
  "requirements": [
    { "id": "REQ-001", "statement": "Greets by name",
      "criteria": [
        { "id": "AC-001-a", "must": "A text box is visible", "method": "e2e" },
        { "id": "AC-001-b", "must": "Says Hello, name", "method": "e2e" }
      ] },
    { "id": "REQ-002", "statement": "Copy is clear",
      "criteria": [ { "id": "AC-002-a", "must": "Greeting is friendly", "method": "manual" } ] }
  ]
}`

func newSvc(iss *fakeIssues, crit fakeCriteria) *Service {
	return NewService(Deps{Issues: iss, Writer: iss.writer(), Criteria: crit})
}

// validationIssue is the open validation task as the host would report it:
// armed, and of kind `validation`.
func validationIssue(number int) sourcecontrol.IssueInfo {
	return sourcecontrol.IssueInfo{
		Number: number, State: "open", Labels: []string{delivery.LabelAgentWork, delivery.KindValidation},
	}
}

// ---- tests ------------------------------------------------------------------

func TestEnsureValidationIssue_CreatesFormattedIssue(t *testing.T) {
	iss := &fakeIssues{}
	svc := newSvc(iss, fakeCriteria{raw: []byte(sampleCriteria), found: true})

	if _, err := svc.EnsureValidationIssue(context.Background(), "org", "proj", thisMilestone); err != nil {
		t.Fatalf("EnsureValidationIssue: %v", err)
	}
	if len(iss.created) != 1 {
		t.Fatalf("want 1 created issue, got %d", len(iss.created))
	}
	got := iss.created[0]

	// ONE label, and deliberately not the `aep` working-set one: the validation
	// cycle is dispatched at this issue by number, and working-set membership
	// would hold the run's settle predicate open forever.
	wantLabels := []string{delivery.LabelAgentWork, delivery.KindValidation}
	if !reflect.DeepEqual(got.Labels, wantLabels) {
		t.Errorf("labels = %v; want %v", got.Labels, wantLabels)
	}
	if got.Title != validationTitle {
		t.Errorf("title = %q; want %q", got.Title, validationTitle)
	}

	// The MILESTONE is the version pin, and it rides the create — no follow-up
	// patch, so the issue is never versionless even for a beat.
	if got.Milestone == nil {
		t.Fatal("no milestone on the create: the issue would be born with no version")
	}
	if *got.Milestone != thisMilestone {
		t.Errorf("milestone = %d; want %d", *got.Milestone, thisMilestone)
	}
	// The dedupe key is version-scoped too, so a later version's mint is never
	// deduped against this one.
	if got.DedupeKey != "validation:proj:5" {
		t.Errorf("dedupe key = %q; want the milestone-scoped %q", got.DedupeKey, "validation:proj:5")
	}

	// The body is PROSE: the consumer contract the aep-validation skill reads,
	// with no machine block, and NO deployed endpoints or credentials — the
	// runner fetches endpoints from the secure validation-context endpoint, and
	// a login is published on the roles gate ticket.
	if strings.Contains(got.Body, "aep:task/v1") {
		t.Errorf("a validation issue body must carry no machine block:\n%s", got.Body)
	}
	for _, want := range []string{"## Validation criteria", "## Test layout", "## Report", "AC-001-a", "specs/validation/validation-criteria.json"} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	if strings.Contains(got.Body, "## Deployed endpoints") {
		t.Error("body must NOT carry a Deployed endpoints section (runner fetches endpoints from validation-context)")
	}
	// e2e count reflects the oracle (2 e2e). Coverage is no longer a field —
	// it is derived from committed-spec presence, so the oracle summary just
	// counts by method.
	if !strings.Contains(got.Body, "`e2e` — 2 criteria") {
		t.Errorf("acceptance-oracle counts wrong; body:\n%s", got.Body)
	}
}

// The minted number comes from the create itself and is never re-discovered by
// listing. GitHub's issue index lags the write by a beat, so the re-read this
// replaces came back empty on a real run: the supervisor filed the validation
// issue, failed to find it a second later, and reported the version `skipped`
// over an oracle it had itself just filed. The fake's milestone holds nothing,
// which is exactly how that lagging index behaves.
func TestEnsureValidationIssue_ReturnsTheNumberItMinted(t *testing.T) {
	iss := &fakeIssues{} // the milestone reads empty, always
	svc := newSvc(iss, fakeCriteria{raw: []byte(sampleCriteria), found: true})

	number, err := svc.EnsureValidationIssue(context.Background(), "org", "proj", thisMilestone)
	if err != nil {
		t.Fatalf("EnsureValidationIssue: %v", err)
	}
	if number != 42 {
		t.Fatalf("issue number = %d; want 42 — the number the create reported", number)
	}
}

// 0 is the minter's way of saying "there is nothing to validate", which settles
// the run `skipped`. A create that reports no number is a broken provider, not
// an absent oracle, so it must surface as an error and let the activity retry —
// the retry then finds the open issue and returns its number.
func TestEnsureValidationIssue_NumberlessCreateIsAnError(t *testing.T) {
	iss := &fakeIssues{numberless: true}
	svc := newSvc(iss, fakeCriteria{raw: []byte(sampleCriteria), found: true})

	number, err := svc.EnsureValidationIssue(context.Background(), "org", "proj", thisMilestone)
	if err == nil {
		t.Fatal("want an error when the create reports no number, got nil")
	}
	if number != 0 {
		t.Errorf("number = %d; want 0 alongside the error", number)
	}
}

// One validation issue per version: a re-entered validation cycle must be
// dispatched at the issue this version already has, not mint a second.
func TestEnsureValidationIssue_ReusesTheVersionsOwnOpenIssue(t *testing.T) {
	iss := &fakeIssues{byMilestone: map[int][]sourcecontrol.IssueInfo{
		thisMilestone: {validationIssue(7)},
	}}
	svc := newSvc(iss, fakeCriteria{raw: []byte(sampleCriteria), found: true})

	number, err := svc.EnsureValidationIssue(context.Background(), "org", "proj", thisMilestone)
	if err != nil {
		t.Fatalf("EnsureValidationIssue: %v", err)
	}
	if len(iss.created) != 0 {
		t.Fatalf("dedup failed: created %d issues, want 0", len(iss.created))
	}
	if number != 7 {
		t.Errorf("issue number = %d; want the open issue 7", number)
	}

	// The QUESTION matters as much as the answer: scoped to this milestone, narrowed
	// to the validation label, and deliberately state-BLIND. `all` rather than `open`
	// because a closed validation task is the normal state between attempts — the
	// PLATFORM closes it at the end of every attempt — so asking only for open ones
	// made a repeat attempt file a second issue for the same version.
	if len(iss.filters) != 1 {
		t.Fatalf("want 1 milestone read, got %d", len(iss.filters))
	}
	want := sourcecontrol.MilestoneIssuesFilter{
		Number: thisMilestone, State: "all", Labels: []string{delivery.LabelAgentWork, delivery.KindValidation},
	}
	if !reflect.DeepEqual(iss.filters[0], want) {
		t.Errorf("filter = %+v; want %+v", iss.filters[0], want)
	}
	if len(iss.reopened) != 0 {
		t.Errorf("reopened %v; an already-open issue needs no reopen", iss.reopened)
	}
}

// The repeat attempt: the platform closed the version's validation task at the end
// of the previous attempt, and this call must find and reopen THAT issue rather
// than file a second one. Two validation issues for one version would each embed their own
// snapshot of the oracle, and the run's ValidationIssue would name whichever was
// newest.
func TestEnsureValidationIssue_ReopensTheClosedIssueForARepeatAttempt(t *testing.T) {
	closed := validationIssue(7)
	closed.State = "closed"
	iss := &fakeIssues{byMilestone: map[int][]sourcecontrol.IssueInfo{
		thisMilestone: {closed},
	}}
	svc := newSvc(iss, fakeCriteria{raw: []byte(sampleCriteria), found: true})

	number, err := svc.EnsureValidationIssue(context.Background(), "org", "proj", thisMilestone)
	if err != nil {
		t.Fatalf("EnsureValidationIssue: %v", err)
	}
	if number != 7 {
		t.Errorf("issue number = %d; want the version's own issue 7", number)
	}
	if len(iss.created) != 0 {
		t.Errorf("filed %d issues; a closed validation issue must be reopened, not re-filed", len(iss.created))
	}
	if !reflect.DeepEqual(iss.reopened, []int{7}) {
		t.Errorf("reopened = %v; want [7]", iss.reopened)
	}
}

// The regression that the project-wide lookup caused: v2's run found v1's still
// open validation issue and re-filed it under v2's milestone, erasing it from
// v1's ledger and handing v2's agent the criteria table rendered for v1.
func TestEnsureValidationIssue_DoesNotReuseAnotherVersionsIssue(t *testing.T) {
	iss := &fakeIssues{byMilestone: map[int][]sourcecontrol.IssueInfo{
		pastMilestone: {validationIssue(7)},
	}}
	svc := newSvc(iss, fakeCriteria{raw: []byte(sampleCriteria), found: true})

	number, err := svc.EnsureValidationIssue(context.Background(), "org", "proj", thisMilestone)
	if err != nil {
		t.Fatalf("EnsureValidationIssue: %v", err)
	}
	if number == 7 {
		t.Fatal("adopted the PREVIOUS version's validation issue — its criteria are stale and v1 loses it from its ledger")
	}
	if len(iss.created) != 1 {
		t.Fatalf("want a fresh issue for this version, created %d", len(iss.created))
	}
	if m := iss.created[0].Milestone; m == nil || *m != thisMilestone {
		t.Errorf("fresh issue filed under %v; want milestone %d", m, thisMilestone)
	}
}

// A validation issue with no version is the state this whole change removes, so
// the minter refuses rather than filing one loose in the repo.
func TestEnsureValidationIssue_RefusesWithoutAMilestone(t *testing.T) {
	iss := &fakeIssues{}
	svc := newSvc(iss, fakeCriteria{raw: []byte(sampleCriteria), found: true})

	number, err := svc.EnsureValidationIssue(context.Background(), "org", "proj", 0)
	if err == nil {
		t.Fatal("want an error with no milestone, got nil")
	}
	if number != 0 {
		t.Errorf("number = %d; want 0 alongside the error", number)
	}
	if len(iss.created) != 0 {
		t.Errorf("filed %d versionless issue(s); want 0", len(iss.created))
	}
}

func TestEnsureValidationIssue_SkipsWhenCriteriaAbsent(t *testing.T) {
	iss := &fakeIssues{}
	svc := newSvc(iss, fakeCriteria{found: false})

	number, err := svc.EnsureValidationIssue(context.Background(), "org", "proj", thisMilestone)
	if err != nil {
		t.Fatalf("EnsureValidationIssue: %v", err)
	}
	if len(iss.created) != 0 {
		t.Fatalf("want no issue when criteria file absent, got %d", len(iss.created))
	}
	if number != 0 {
		t.Errorf("number = %d; want 0 — no oracle, nothing to validate", number)
	}
}

func TestEnsureValidationIssue_SkipsWhenCriteriaMalformed(t *testing.T) {
	iss := &fakeIssues{}
	svc := newSvc(iss, fakeCriteria{raw: []byte(`{"requirements": []}`), found: true})

	number, err := svc.EnsureValidationIssue(context.Background(), "org", "proj", thisMilestone)
	if err != nil {
		t.Fatalf("EnsureValidationIssue (malformed should skip, not error): %v", err)
	}
	if len(iss.created) != 0 {
		t.Fatalf("want no issue when criteria empty/malformed, got %d", len(iss.created))
	}
	if number != 0 {
		t.Errorf("number = %d; want 0", number)
	}
}

// ---- the task's lifecycle races ---------------------------------------------
//
// The validation task is the version's PERSISTENT HANDLE, and the platform owns
// its close: the pull request says `Validates #N`, which GitHub does not treat as
// a closing keyword, so nothing but this package moves the issue's state. That
// single ownership is what these three tests are about — each is a way the
// lifecycle went wrong while the merge and the platform were both writing it.

// RACE 1 — reopen, then close. A version judged twice must walk the same ONE
// issue through reopen → close → reopen → close, never accumulate a second.
//
// With two owners this was the sequence that produced duplicates: the merge
// closed the issue, the platform reopened it for the next attempt, and a reopen
// racing the host's own close left the issue open with the run already settled —
// which the reconcile sweep reads as unworked and turns into another validation
// run.
func TestValidationTaskLifecycle_ReopenThenCloseWalksOneIssue(t *testing.T) {
	ctx := context.Background()
	iss := &fakeIssues{}
	svc := newSvc(iss, fakeCriteria{raw: []byte(sampleCriteria), found: true})

	// Attempt 1: mint, judge, close.
	first, err := svc.EnsureValidationIssue(ctx, "org", "proj", thisMilestone)
	if err != nil || first == 0 {
		t.Fatalf("EnsureValidationIssue(attempt 1) = (%d, %v)", first, err)
	}
	// The mint has to land in the fake's index, or the second attempt is looking at
	// a milestone the platform never filed into.
	iss.byMilestone = map[int][]sourcecontrol.IssueInfo{thisMilestone: {validationIssue(first)}}
	if err := svc.CloseValidationIssue(ctx, "org", "proj", first, delivery.ValidationVerdictFailed); err != nil {
		t.Fatalf("CloseValidationIssue(attempt 1): %v", err)
	}

	// Attempt 2: the same issue, reopened.
	second, err := svc.EnsureValidationIssue(ctx, "org", "proj", thisMilestone)
	if err != nil {
		t.Fatalf("EnsureValidationIssue(attempt 2): %v", err)
	}
	if second != first {
		t.Fatalf("attempt 2 judged issue %d, attempt 1 judged %d — the task is the VERSION's handle", second, first)
	}
	if err := svc.CloseValidationIssue(ctx, "org", "proj", second, delivery.ValidationVerdictPassed); err != nil {
		t.Fatalf("CloseValidationIssue(attempt 2): %v", err)
	}

	if len(iss.created) != 1 {
		t.Fatalf("filed %d validation tasks across two attempts, want 1 — each extra one embeds "+
			"its own snapshot of the oracle", len(iss.created))
	}
	want := []string{
		fmt.Sprintf("close:%d", first),
		fmt.Sprintf("reopen:%d", first),
		fmt.Sprintf("close:%d", first),
	}
	if !reflect.DeepEqual(iss.lifecycle, want) {
		t.Fatalf("lifecycle = %v, want %v", iss.lifecycle, want)
	}
	// It ends CLOSED. An open task after a settled run is exactly what the sweep
	// reads as a version nobody has judged.
	if state := iss.stateOf(first); !strings.EqualFold(state, "closed") {
		t.Fatalf("the task is %q after the last attempt settled, want closed", state)
	}
}

// RACE 2 — the close fires before the pull request merges.
//
// It happens on the ordinary path: the platform closes the task the moment the
// run settles, and a redelivered `pull_request` webhook can arrive afterwards.
// With a closing keyword in the body that late delivery was a second write to the
// same issue from a second owner. With `Validates #N` the merge moves nothing, so
// the close stands and the next attempt still finds ONE issue to reopen.
//
// The assertion is that the platform's close is idempotent and does not push the
// task into a state a later attempt reads as "no validation task": that reading
// is what settles a run `skipped` over an oracle it is holding in its hand.
func TestValidationTaskLifecycle_CloseBeforeTheMergeStandsAndDoesNotDuplicate(t *testing.T) {
	ctx := context.Background()
	iss := &fakeIssues{byMilestone: map[int][]sourcecontrol.IssueInfo{
		thisMilestone: {validationIssue(7)},
	}}
	svc := newSvc(iss, fakeCriteria{raw: []byte(sampleCriteria), found: true})

	// The run settles and closes the task…
	if err := svc.CloseValidationIssue(ctx, "org", "proj", 7, ""); err != nil {
		t.Fatalf("CloseValidationIssue: %v", err)
	}
	// …and the same close is delivered again (an activity retry).
	if err := svc.CloseValidationIssue(ctx, "org", "proj", 7, ""); err != nil {
		t.Fatalf("CloseValidationIssue(retry): %v", err)
	}

	// The next attempt reopens the ONE issue rather than filing a second.
	number, err := svc.EnsureValidationIssue(ctx, "org", "proj", thisMilestone)
	if err != nil {
		t.Fatalf("EnsureValidationIssue after a close: %v", err)
	}
	if number != 7 {
		t.Fatalf("next attempt judged issue %d, want the version's own 7", number)
	}
	if len(iss.created) != 0 {
		t.Fatalf("filed %d issues; a closed task must be reopened, not re-filed", len(iss.created))
	}
	if !reflect.DeepEqual(iss.reopened, []int{7}) {
		t.Fatalf("reopened = %v, want [7]", iss.reopened)
	}
	// A close with no verdict says so, rather than inventing one: "the attempt
	// ended without reaching a verdict" is a different fact from `inconclusive`,
	// and a human reading the closed task has to be able to tell them apart.
	if len(iss.closeComments) == 0 || !strings.Contains(iss.closeComments[0], "without reaching a verdict") {
		t.Fatalf("close comment = %q, want it to say no verdict was reached", iss.closeComments)
	}
}

// RACE 3 — a re-file where a reopen was correct.
//
// This is the defect the state-blind lookup exists to prevent, and it is worth
// its own test because it fails SILENTLY: the second task is a valid-looking
// issue, so the run judges the version against an oracle rendered at a different
// moment, and the first task disappears from the version's ledger.
//
// The only thing standing between the two behaviours is the filter's `state:
// "all"`, so the test pins the question as well as the answer.
func TestValidationTaskLifecycle_AClosedTaskIsReopenedNotRefiled(t *testing.T) {
	ctx := context.Background()
	closed := validationIssue(7)
	closed.State = "closed"
	iss := &fakeIssues{byMilestone: map[int][]sourcecontrol.IssueInfo{thisMilestone: {closed}}}
	svc := newSvc(iss, fakeCriteria{raw: []byte(sampleCriteria), found: true})

	number, err := svc.EnsureValidationIssue(ctx, "org", "proj", thisMilestone)
	if err != nil {
		t.Fatalf("EnsureValidationIssue: %v", err)
	}
	if number != 7 || len(iss.created) != 0 {
		t.Fatalf("EnsureValidationIssue = %d with %d creates; want the closed task 7 reopened",
			number, len(iss.created))
	}
	if len(iss.filters) != 1 || !strings.EqualFold(iss.filters[0].State, "all") {
		t.Fatalf("lookup state = %q, want `all` — asking only for OPEN issues is what re-files",
			iss.filters[0].State)
	}
	// The body is NOT rewritten on reopen: it embeds the oracle as rendered at
	// first mint, which is the question THIS version is being asked. Each attempt's
	// own summary comment is what makes the thread readable across attempts.
	if len(iss.created) != 0 {
		t.Fatal("the reopen rewrote the task's body")
	}
}
