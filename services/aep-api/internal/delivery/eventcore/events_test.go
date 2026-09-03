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

package eventcore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/webhook"
)

// WEBHOOK TIER — the primary one. Synthetic GitHub payloads go through the
// REAL router, dispatched by event name exactly as the receiver does it, so
// each test exercises the registration keys as well as the handler. Every row
// of the routing table is covered here or in builds_test.go.
//
// The payload literals carry only the fields the handlers read, plus the two
// the pipeline needs (repository.full_name for routing, action for dispatch).
// That is deliberate: a payload copied from GitHub would hide which fields the
// code actually depends on.

const platformBot = "aep-platform[bot]"

type harness struct {
	events *Events
	router *webhook.Router
	runs   *fakeRuns
	cycles *fakeCycles
	issues *fakeIssues
	prs    *fakePRs
	merger *fakeMerger
	builds *fakeBuilds
	comps  *fakeComponents
	sup    *fakeSupervisor
	oracle *fakeOracle
}

// newHarness wires the event plane onto a real router. rows seeds the run
// store — pass none for the inertness case (which is production today).
func newHarness(t *testing.T, rows ...delivery.MilestoneRun) *harness {
	t.Helper()
	h := &harness{
		runs:   newFakeRuns(rows...),
		cycles: newFakeCycles(nil),
		issues: newFakeIssues(),
		prs:    &fakePRs{},
		merger: &fakeMerger{},
		builds: newFakeBuilds(),
		comps:  &fakeComponents{},
		sup:    &fakeSupervisor{},
		oracle: &fakeOracle{has: true},
	}
	// The real supervisor admits the run row as part of starting a run.
	h.sup.admits = h.runs
	h.events = New(Ports{
		Runs:           h.runs,
		Cycles:         h.cycles,
		Issues:         h.issues,
		Writer:         h.issues.writer(),
		PRs:            h.prs,
		Merger:         h.merger,
		Repos:          fakeRepoLookup{},
		Design:         fakeDesign{paths: map[string]string{"order-service": "services/order", "web": "apps/web"}},
		Builds:         h.builds,
		Components:     h.comps,
		Criteria:       h.oracle,
		Signaler:       h.sup,
		Starter:        h.sup,
		PlatformSender: platformBot,
	})
	h.router = webhook.NewRouter()
	h.events.RegisterHandlers(func(event, action string, fn func(ctx context.Context, event, action string, payload []byte) error) {
		h.router.Register(event, action, webhook.EventHandlerFunc(fn))
	})
	return h
}

// deliver drives one synthetic delivery through the real router.
func (h *harness) deliver(t *testing.T, event string, payload []byte) error {
	t.Helper()
	return h.router.Dispatch(context.Background(), event, payload)
}

// prURL is the host's own link, spelled exactly as GitHub spells it in a
// pull_request payload — the platform records this string and never builds one.
func prURL(number int) string {
	return fmt.Sprintf("https://github.com/%s/pull/%d", testRepo, number)
}

func prBody(action, branch, body string, number int, draft, merged bool, mergeSHA string) []byte {
	return []byte(fmt.Sprintf(`{
	  "action": %q,
	  "pull_request": {"number": %d, "draft": %t, "merged": %t, "state": "open",
	                   "body": %q, "html_url": %q, "merge_commit_sha": %q, "head": {"ref": %q}},
	  "repository": {"full_name": %q}
	}`, action, number, draft, merged, body, prURL(number), mergeSHA, branch, testRepo))
}

func issueBody(action string, issue, milestone int, label, sender string, topLevelMilestone bool) []byte {
	ms := "null"
	top := ""
	if milestone > 0 {
		ms = fmt.Sprintf(`{"number": %d, "title": "v%d"}`, milestone, milestone)
		if topLevelMilestone {
			top = fmt.Sprintf(`"milestone": {"number": %d, "title": "v%d"},`, milestone, milestone)
			ms = "null" // demilestoned: the issue no longer carries it
		}
	}
	return []byte(fmt.Sprintf(`{
	  "action": %q,
	  %s
	  "issue": {"number": %d, "state": "open", "title": "a task", "milestone": %s, "labels": [{"name": %q}]},
	  "label": {"name": %q},
	  "repository": {"full_name": %q},
	  "sender": {"login": %q}
	}`, action, top, issue, ms, label, label, testRepo, sender))
}

// issueBodyWithLabels is issueBody where the issue's WHOLE label set differs
// from the one label that fired the delivery. That gap is the whole subject of
// the adoption-routing tests: arming an issue that is ALREADY classified sends
// `aep` as the fired label while the kind sits in the issue's own set.
func issueBodyWithLabels(action string, issue, milestone int, fired string, labels []string, sender string) []byte {
	ms := "null"
	if milestone > 0 {
		ms = fmt.Sprintf(`{"number": %d, "title": "v%d"}`, milestone, milestone)
	}
	quoted := make([]string, 0, len(labels))
	for _, l := range labels {
		quoted = append(quoted, fmt.Sprintf(`{"name": %q}`, l))
	}
	return []byte(fmt.Sprintf(`{
	  "action": %q,
	  "issue": {"number": %d, "state": "open", "title": "a task", "milestone": %s, "labels": [%s]},
	  "label": {"name": %q},
	  "repository": {"full_name": %q},
	  "sender": {"login": %q}
	}`, action, issue, ms, strings.Join(quoted, ","), fired, testRepo, sender))
}

// ---- §8 row 1: auto-merge policy seam -------------------------------------

func TestPullRequestOpened_MergesWhenItResolvesMilestoneWork(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.issues.withWork(7, 12, 13)

	if err := h.deliver(t, "pull_request", prBody("opened", "aep/m7-c1", "Resolves #12\nResolves #13", 42, false, false, "")); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(h.merger.merged) != 1 || h.merger.merged[0] != 42 {
		t.Fatalf("the PR must be squash-merged exactly once, got %v", h.merger.merged)
	}
	// The cycle learns branch + PR from the webhook — the platform never
	// dictates branch identity.
	if len(h.cycles.notedPR) != 1 || h.cycles.notedPR[0] != "cycle-1:aep/m7-c1:42:"+prURL(42) {
		t.Fatalf("the cycle must learn branch + PR + its host link, got %v", h.cycles.notedPR)
	}
	// And it records the MATCHED set, which is the only durable answer to "what
	// did this cycle work" once the merge closes those issues.
	if len(h.cycles.decisions) != 1 || h.cycles.decisions[0] != "cycle-1::[12 13]" {
		t.Fatalf("the cycle must record its resolved set with no verdict, got %v", h.cycles.decisions)
	}
}

// The validation cycle's pull request must merge too, and this asserts it through
// the REAL fetch rather than against a hand-built issue list. A policy that
// accepts the validation task is not enough on its own: the fetch used to narrow
// on a label the task did not carry, so it never reached the policy and every
// validation pull request was declined with "no resolved issue is this run's
// work" — a green agent, a report in the branch, and a run that sat at its
// landing deadline until a human merged by hand. The policy_test cases cannot
// see that, because they call the pure function with the population already in
// hand.
func TestPullRequestOpened_MergesTheValidationCyclesPullRequest(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	// The milestone as it really stands when validation runs: the coding work is
	// closed, so all that is left open is the validation issue.
	h.issues.withValidationIssue(7, 3)

	if err := h.deliver(t, "pull_request", prBody("opened", "aep/m7-validation", "Resolves #3", 4, false, false, "")); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(h.merger.merged) != 1 || h.merger.merged[0] != 4 {
		t.Fatalf("the validation cycle's PR must be squash-merged, got %v (decline reason: %q)",
			h.merger.merged, h.cycles.latest.MergeReason)
	}
	// Without the matched set the merge closes #3 and nothing records that this
	// cycle is what worked it.
	if len(h.cycles.decisions) != 1 || h.cycles.decisions[0] != "cycle-1::[3]" {
		t.Fatalf("the cycle must record the validation issue as its resolved set, got %v", h.cycles.decisions)
	}
}

// A draft is the agent saying it is not finished, but the cycle still records
// WHICH pull request it is parked behind: without it, a cycle waiting on a draft
// is indistinguishable from one whose agent never opened a pull request, and the
// wait looks like the agent hanging.
func TestPullRequestDraft_IsRecordedOnTheCycleButNotDecided(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.issues.withWork(7, 12)

	if err := h.deliver(t, "pull_request", prBody("opened", "aep/m7-c1", "Resolves #12", 42, true, false, "")); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(h.cycles.notedPR) != 1 || h.cycles.notedPR[0] != "cycle-1:aep/m7-c1:42:"+prURL(42) {
		t.Fatalf("a draft's identity must still reach the cycle, got %v", h.cycles.notedPR)
	}
	if !h.cycles.latest.PRDraft {
		t.Fatalf("the cycle must record the pull request AS a draft, got %+v", h.cycles.latest)
	}
	// The policy is not consulted at all: nothing may merge behind a draft, so
	// there is no verdict to record either.
	if len(h.cycles.decisions) != 0 {
		t.Fatalf("no merge decision may be recorded for a draft, got %v", h.cycles.decisions)
	}

	// ready_for_review is the SAME pull request arriving by another route.
	if err := h.deliver(t, "pull_request", prBody("ready_for_review", "aep/m7-c1", "Resolves #12", 42, false, false, "")); err != nil {
		t.Fatalf("ready_for_review: %v", err)
	}
	if h.cycles.latest.PRDraft {
		t.Fatalf("ready_for_review must clear the draft flag, got %+v", h.cycles.latest)
	}
	if len(h.merger.merged) != 1 {
		t.Fatalf("the pull request must merge once it is ready, got %v", h.merger.merged)
	}
}

func TestPullRequestSynchronize_RetriesTheMergeAfterARebase(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.issues.withWork(7, 12)

	if err := h.deliver(t, "pull_request", prBody("synchronize", "aep/m7-c1", "Resolves #12", 42, false, false, "")); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(h.merger.merged) != 1 {
		t.Fatalf("a push to the PR branch must re-run the merge policy, got %v", h.merger.merged)
	}
}

func TestPullRequestDraft_IsNeverMerged(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.issues.withWork(7, 12)

	if err := h.deliver(t, "pull_request", prBody("opened", "aep/m7-c1", "Resolves #12", 42, true, false, "")); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(h.merger.merged) != 0 {
		t.Fatalf("a draft is the agent saying it is not finished; got merges %v", h.merger.merged)
	}
}

func TestPullRequestOpened_DeclinedWhenItResolvesNothingInTheMilestone(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.issues.withWork(7, 12) // the PR claims #99, which is not milestone work

	if err := h.deliver(t, "pull_request", prBody("opened", "aep/m7-c1", "Resolves #99", 42, false, false, "")); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(h.merger.merged) != 0 {
		t.Fatalf("a PR claiming nothing in this milestone must be left for a human, got %v", h.merger.merged)
	}
	// The decline is RECORDED, not only logged. It is the loudest silence in the
	// loop: the agent exited green and the cycle then sits at its landing
	// deadline with nothing on screen to say why.
	if h.cycles.latest.MergeVerdict != delivery.CycleMergeDeclined {
		t.Fatalf("the cycle must record the declined verdict, got %+v", h.cycles.latest)
	}
	if h.cycles.latest.MergeReason == "" {
		t.Fatalf("a recorded verdict without its reason explains nothing, got %+v", h.cycles.latest)
	}
}

// A verdict is a snapshot of the LATEST decision: the agent clears a decline by
// pushing, and the row must not keep saying "declined" after the merge lands.
func TestPullRequestSynchronize_AMergingDecisionClearsAnEarlierDecline(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.issues.withWork(7, 12)

	if err := h.deliver(t, "pull_request", prBody("opened", "aep/m7-c1", "Resolves #99", 42, false, false, "")); err != nil {
		t.Fatalf("opened: %v", err)
	}
	if h.cycles.latest.MergeVerdict != delivery.CycleMergeDeclined {
		t.Fatalf("precondition: the first decision must decline, got %+v", h.cycles.latest)
	}
	// The agent fixes the Resolves list and pushes.
	if err := h.deliver(t, "pull_request", prBody("synchronize", "aep/m7-c1", "Resolves #12", 42, false, false, "")); err != nil {
		t.Fatalf("synchronize: %v", err)
	}
	if h.cycles.latest.MergeVerdict != "" {
		t.Fatalf("a merging decision must clear the stale verdict, got %+v", h.cycles.latest)
	}
	if len(h.cycles.latest.Resolves) != 1 || h.cycles.latest.Resolves[0] != 12 {
		t.Fatalf("the matched set must be the latest one, got %v", h.cycles.latest.Resolves)
	}
}

// TestPullRequestOpened_IdempotentUnderRedelivery is the redelivery property:
// GitHub re-runs a delivery whose handler failed, and the second pass must not
// merge again. The ground-truth read is what stops it.
func TestPullRequestOpened_IdempotentUnderRedelivery(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.issues.withWork(7, 12)
	// First read: open. Every read after the merge: merged — as GitHub answers.
	h.prs.states = append(h.prs.states, openPR(), mergedPR("abc123def456"))

	payload := prBody("opened", "aep/m7-c1", "Resolves #12", 42, false, false, "")
	if err := h.deliver(t, "pull_request", payload); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	if err := h.deliver(t, "pull_request", payload); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	if len(h.merger.merged) != 1 {
		t.Fatalf("a redelivered pull_request must merge once in total, got %v", h.merger.merged)
	}
}

// ---- §8 row 3: un-mergeable → conflict issue ------------------------------

func TestPullRequestOpened_MergeRefusedOnOpenPR_MintsOneConflictIssue(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.issues.withWork(7, 12)
	h.merger.err = errors.New("github merge failed (status 405): Pull Request is not mergeable")

	payload := prBody("opened", "aep/m7-c1", "Resolves #12", 42, false, false, "")
	if err := h.deliver(t, "pull_request", payload); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// Redelivery must not file a second one (the DedupeKey is per pull request).
	if err := h.deliver(t, "pull_request", payload); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	titles := h.issues.titles()
	if len(titles) != 1 || titles[0] != "Resolve the merge conflict on pull request #42" {
		t.Fatalf("exactly one conflict issue naming the PR must be minted, got %v", titles)
	}
	if got := h.issues.created[0].Body; !strings.Contains(got, "#42") || !strings.Contains(got, "aep/m7-c1") {
		t.Fatalf("the conflict issue must name the PR and its branch (the agent's rebase target), got:\n%s", got)
	}
	// The refusal is the HOST's, not the policy's, and it is recorded as such —
	// otherwise the merge stage can only say "not merged" about a cycle that is
	// in fact waiting on a rebase.
	if h.cycles.latest.MergeVerdict != delivery.CycleMergeRefused {
		t.Fatalf("the cycle must record the refusal, got %+v", h.cycles.latest)
	}
	if !strings.Contains(h.cycles.latest.MergeReason, "not mergeable") {
		t.Fatalf("the recorded reason must carry the host's words, got %q", h.cycles.latest.MergeReason)
	}
	if sigs := h.sup.named(delivery.SigRunConflict); len(sigs) != 2 || sigs[0].PRNumber != 42 {
		// One signal per delivery is correct — signalling is cheap and the
		// supervisor is idempotent; what must not double is the ISSUE.
		t.Fatalf("the supervisor must be told about the conflict, got %+v", sigs)
	}
}

// ---- §8 row 2: merged → path-diff fan-out ---------------------------------

func TestPullRequestMerged_BuildsEveryTouchedComponentAtTheMergeSHA(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.prs.files = []string{"services/order/main.go", "apps/web/src/app.tsx", "README.md"}

	if err := h.deliver(t, "pull_request", prBody("closed", "aep/m7-c1", "Resolves #12", 42, false, true, "abc123def456789")); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := len(h.builds.triggered); got != 2 {
		t.Fatalf("both touched components must build (README.md matches none), got %v", h.builds.triggered)
	}
	for _, component := range []string{"order-service", "web"} {
		names := h.builds.triggeredFor(component)
		if len(names) != 1 || names[0] != delivery.BuildRunName(testProject, component, "abc123def456789", 1) {
			t.Fatalf("component %s must have exactly one attempt-1 run pinned to the merge SHA, got %v", component, names)
		}
	}
	// The cycle closes with the merge SHA and the supervisor is told.
	if len(h.cycles.closed) != 1 || h.cycles.closed[0] != "cycle-1:abc123def456789" {
		t.Fatalf("the cycle must close with the merge SHA, got %v", h.cycles.closed)
	}
	if sigs := h.sup.named(delivery.SigRunPRMerged); len(sigs) != 1 || sigs[0].MergeSHA != "abc123def456789" {
		t.Fatalf("the supervisor must be told the PR merged, got %+v", sigs)
	}
}

// TestPullRequestMerged_StagesTheCloneCredentialOncePerFanOut pins the fix for
// the race that produced a build with an empty secretRef.
//
// The clone credential is ONE per-org object and OpenChoreo has no update verb,
// so staging it is delete-then-create. Staging per component therefore had every
// goroutine in the fan-out racing to delete and recreate the same object; the
// loser's create collided with a sibling's in-flight delete, degraded to an
// empty reference, and shipped a build that cloned anonymously and died at
// checkout against a private repo.
//
// Two assertions, and both are load-bearing: staged exactly once (no contention
// is possible if there is only one writer), and every component carrying that
// same reference (staging once is worthless if a component builds without it).
func TestPullRequestMerged_StagesTheCloneCredentialOncePerFanOut(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.prs.files = []string{"services/order/main.go", "apps/web/src/app.tsx"}

	if err := h.deliver(t, "pull_request", prBody("closed", "aep/m7-c1", "Resolves #12", 42, false, true, "abc123def456789")); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if h.builds.staged != 1 {
		t.Fatalf("a fan-out over 2 components must stage the org credential exactly once, staged %d times", h.builds.staged)
	}
	if len(h.builds.secretRef) != 2 {
		t.Fatalf("both components must build, got %v", h.builds.triggered)
	}
	for i, ref := range h.builds.secretRef {
		if ref != h.builds.stageRef {
			t.Fatalf("build %d cloned with %q, want the staged reference %q — an empty or stale ref is the bug this pins",
				i, ref, h.builds.stageRef)
		}
	}
}

// TestPullRequestMerged_StagingRefusalBlocksTheFanOut pins that a credential the
// platform REFUSED to stage stops the fan-out rather than dispatching builds
// that cannot clone. A staging error is distinct from an empty reference with a
// nil error, which means "this repo is public" and is allowed through.
func TestPullRequestMerged_StagingRefusalBlocksTheFanOut(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.prs.files = []string{"services/order/main.go"}
	h.builds.stageErr = errors.New("openchoreo unreachable")

	err := h.deliver(t, "pull_request", prBody("closed", "aep/m7-c1", "Resolves #12", 42, false, true, "abc123def456789"))
	if err == nil {
		t.Fatal("a staging refusal must surface, not dispatch a build that cannot clone")
	}
	if len(h.builds.triggered) != 0 {
		t.Fatalf("no build may be triggered when the credential could not be staged, got %v", h.builds.triggered)
	}
}

// The `opened` delivery can be missed (an install that predates the run, a
// dropped delivery), and the merge is then the only place the cycle can learn
// which pull request it landed — link included, or the finished session would
// name a pull request the reader cannot open.
func TestPullRequestMerged_BackfillsTheHostLinkWhenTheOpenedDeliveryWasMissed(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1") // never saw its pull request

	if err := h.deliver(t, "pull_request", prBody("closed", "aep/m7-c1", "Resolves #12", 42, false, true, "abc123def456789")); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(h.cycles.notedPR) != 1 || h.cycles.notedPR[0] != "cycle-1:aep/m7-c1:42:"+prURL(42) {
		t.Fatalf("the backfill must record the pull request WITH its link, got %v", h.cycles.notedPR)
	}
	if h.cycles.latest.PRURL != prURL(42) {
		t.Fatalf("cycle PR link = %q, want %q", h.cycles.latest.PRURL, prURL(42))
	}
}

// The link is the console's ONLY route to the pull request — nothing composes
// one from the repo row — so a delivery that carries no `html_url` must leave a
// recorded link alone rather than blanking it.
func TestCycleWrites_NeverBlankARecordedPullRequestLink(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.cycles.latest.Branch, h.cycles.latest.PRNumber = "aep/m7-c1", 42
	h.cycles.latest.PRURL = prURL(42)
	h.issues.withWork(7, 12)

	// A payload with no html_url at all: an older redelivery, or a host that
	// omits it.
	sparse := []byte(fmt.Sprintf(`{
	  "action": "synchronize",
	  "pull_request": {"number": 42, "draft": false, "merged": false, "state": "open",
	                   "body": "Resolves #12", "head": {"ref": "aep/m7-c1"}},
	  "repository": {"full_name": %q}
	}`, testRepo))
	if err := h.deliver(t, "pull_request", sparse); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if h.cycles.latest.PRURL != prURL(42) {
		t.Fatalf("cycle PR link = %q, want the recorded %q kept", h.cycles.latest.PRURL, prURL(42))
	}
	// Nothing changed, so nothing was written — the identity already matched.
	if len(h.cycles.notedPR) != 0 {
		t.Fatalf("a delivery that adds no fact must write nothing, got %v", h.cycles.notedPR)
	}
}

// A component the design gained THIS cycle has never been built, so its
// OpenChoreo Component CR does not exist and the build would fail "Component
// not found". The fan-out is the last point that knows which component is about
// to be built — a cycle spans a whole milestone, so no dispatch can do it — and
// it therefore ensures the CR immediately before triggering.
func TestPullRequestMerged_EnsuresEachComponentBeforeItsBuild(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.prs.files = []string{"services/order/main.go", "apps/web/src/app.tsx"}

	if err := h.deliver(t, "pull_request", prBody("closed", "aep/m7-c1", "Resolves #12", 42, false, true, "abc123def456789")); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	sort.Strings(h.comps.ensured)
	if !reflect.DeepEqual(h.comps.ensured, []string{"order-service", "web"}) {
		t.Fatalf("every component about to build must be ensured first, got %v", h.comps.ensured)
	}
}

// A component the platform cannot provision must NOT be built: triggering a
// build for a CR that does not exist only fails later, and less clearly. Its
// siblings still build — the fan-out is per component.
func TestPullRequestMerged_UnensurableComponentIsNotBuilt(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.prs.files = []string{"services/order/main.go", "apps/web/src/app.tsx"}
	h.comps.failFor = "web"

	err := h.deliver(t, "pull_request", prBody("closed", "aep/m7-c1", "Resolves #12", 42, false, true, "abc123def456789"))
	if err == nil {
		t.Fatal("a component that cannot be ensured must surface as an error, not a silent skip")
	}
	if got := h.builds.triggeredFor("web"); len(got) != 0 {
		t.Fatalf("the unensurable component must not be built, got %v", got)
	}
	if got := h.builds.triggeredFor("order-service"); len(got) != 1 {
		t.Fatalf("its sibling must still build, got %v", got)
	}
}

func TestPullRequestMerged_IdempotentUnderRedelivery(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.prs.files = []string{"services/order/main.go"}

	payload := prBody("closed", "aep/m7-c1", "Resolves #12", 42, false, true, "abc123def456789")
	if err := h.deliver(t, "pull_request", payload); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	if err := h.deliver(t, "pull_request", payload); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	if got := h.builds.triggeredFor("order-service"); len(got) != 1 {
		t.Fatalf("a redelivered merge must trigger one build in total, got %v", got)
	}
}

// TestPullRequestMerged_HumanBranchStillBuilds proves the fan-out is generic:
// a pull request that followed none of the agent's branch conventions still
// belongs to the live spec run and still rebuilds what it touched.
func TestPullRequestMerged_HumanBranchStillBuilds(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.prs.files = []string{"services/order/main.go"}

	if err := h.deliver(t, "pull_request", prBody("closed", "hotfix/typo", "", 43, false, true, "feedfacefeed")); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := h.builds.triggeredFor("order-service"); len(got) != 1 {
		t.Fatalf("a human PR merged during a run must rebuild what it touched, got %v", got)
	}
	// …but it is not the cycle's work: the agent's open cycle keeps its own
	// branch and PR, and does not close on somebody else's merge.
	if len(h.cycles.notedPR) != 0 || len(h.cycles.closed) != 0 {
		t.Fatalf("a human PR must not write the agent's cycle record, got %v / %v", h.cycles.notedPR, h.cycles.closed)
	}
}

// ---- §8 row 5: milestone-matched issues → predicate re-evaluation ---------

func TestIssues_PredicateTrue_SignalsWorkable(t *testing.T) {
	for _, action := range []string{"closed", "reopened", "labeled", "unlabeled", "milestoned", "demilestoned"} {
		t.Run(action, func(t *testing.T) {
			h := newHarness(t, aRun("run-1", 7, delivery.RunStateWaiting))
			h.issues.withCounts(7, 0, 3, 3)
			// milestoned/demilestoned carry the milestone at TOP LEVEL — the only
			// place a demilestone names the milestone the issue just left.
			topLevel := action == "milestoned" || action == "demilestoned"

			if err := h.deliver(t, "issues", issueBody(action, 12, 7, "aep", "someone", topLevel)); err != nil {
				t.Fatalf("dispatch: %v", err)
			}
			if sigs := h.sup.named(delivery.SigRunWorkable); len(sigs) != 1 || sigs[0].MilestoneNumber != 7 {
				t.Fatalf("issues.%s on a milestone with work must wake the waiting run, got %+v", action, sigs)
			}
		})
	}
}

func TestIssues_OpenGateHoldsDispatch(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateWaiting))
	h.issues.withCounts(7, 1, 2, 3) // a provision gate is open

	if err := h.deliver(t, "issues", issueBody("closed", 12, 7, "aep", "someone", false)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if sigs := h.sup.named(delivery.SigRunWorkable); len(sigs) != 0 {
		t.Fatalf("an open gate must hold dispatch, got %+v", sigs)
	}
}

func TestIssues_RunningRunIsNotWokenMidCycle(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.issues.withCounts(7, 0, 3, 3)

	if err := h.deliver(t, "issues", issueBody("labeled", 12, 7, "aep", "someone", false)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if sigs := h.sup.named(delivery.SigRunWorkable); len(sigs) != 0 {
		t.Fatalf("a running run re-reads its milestone at the cycle boundary; got %+v", sigs)
	}
}

func TestIssues_EchoFromThePlatformIsDropped(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateWaiting))
	h.issues.withCounts(7, 0, 3, 3)

	if err := h.deliver(t, "issues", issueBody("labeled", 12, 7, "aep", platformBot, false)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if sigs := h.sup.named(delivery.SigRunWorkable); len(sigs) != 0 {
		t.Fatalf("the platform's own label write must not re-enter as an event, got %+v", sigs)
	}
}

// ---- §8 row 6: adoption ---------------------------------------------------

func TestAdoption_BareIssueJoinsTheDeployedVersionAndStartsAnIncidentRun(t *testing.T) {
	deployed := aRun("run-old", 5, delivery.RunStateSucceeded)
	h := newHarness(t, deployed)

	if err := h.deliver(t, "issues", issueBody("labeled", 31, 0, delivery.LabelAgentWork, "human", false)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(h.issues.assigned) != 1 || h.issues.assigned[0] != "31->5" {
		t.Fatalf("a bare adopted issue must join the deployed version's milestone, got %v", h.issues.assigned)
	}
	if len(h.sup.started) != 1 || h.sup.started[0].Kind != delivery.RunKindTask ||
		h.sup.started[0].MilestoneNumber != 5 {
		t.Fatalf("an incident run must start over the deployed milestone, got %+v", h.sup.started)
	}
}

func TestAdoption_ExistingMilestoneIsRespected(t *testing.T) {
	h := newHarness(t, aRun("run-old", 5, delivery.RunStateSucceeded))

	if err := h.deliver(t, "issues", issueBody("labeled", 31, 9, delivery.LabelAgentWork, "human", false)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(h.issues.assigned) != 0 {
		t.Fatalf("an issue that already has a milestone keeps it, got %v", h.issues.assigned)
	}
	if len(h.sup.started) != 1 || h.sup.started[0].MilestoneNumber != 9 {
		t.Fatalf("the run must start over the issue's own milestone, got %+v", h.sup.started)
	}
}

func TestAdoption_IntoALiveRunIsANoOp(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))

	if err := h.deliver(t, "issues", issueBody("labeled", 31, 7, delivery.LabelAgentWork, "human", false)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("a second run on one milestone would put two agents on one branch, got %+v", h.sup.started)
	}
}

func TestAdoption_NeverDeployedProjectRefusesClearly(t *testing.T) {
	h := newHarness(t) // no runs at all

	err := h.events.AdoptIssue(context.Background(), testOrg, testProject, AdoptTarget{Number: 31})
	if !errors.Is(err, ErrNoDeployedMilestone) {
		t.Fatalf("adoption without a deployed version must refuse with the actionable error, got %v", err)
	}
	if len(h.issues.assigned) != 0 || len(h.sup.started) != 0 {
		t.Fatal("a refused adoption must write nothing")
	}
}

// ---- revalidation ---------------------------------------------------------

// TestRevalidate_StartsARunOverTheVersionsMilestone is the happy path, and the
// assertion that matters is the ORIGIN: it is what carries the run past the
// boundary's park guard and into validation, and what keeps it out of the
// build mutex so a re-check never holds up the next build.
func TestRevalidate_StartsARunOverTheVersionsMilestone(t *testing.T) {
	h := newHarness(t, aRun("run-old", 5, delivery.RunStateSucceeded))

	runID, err := h.events.Revalidate(context.Background(), testOrg, testProject,
		MilestoneRef{Number: 5, Title: "v3"}, 1, 4)
	if err != nil {
		t.Fatalf("revalidate: %v", err)
	}
	if len(h.sup.started) != 1 {
		t.Fatalf("exactly one run must start, got %+v", h.sup.started)
	}
	got := h.sup.started[0]
	if got.Kind != delivery.RunKindValidation || got.MilestoneNumber != 5 {
		t.Fatalf("a revalidate run must start over the version's own milestone, got %+v", got)
	}
	if got.ValidationAttempts != 1 || got.CycleCeiling != 4 {
		t.Fatalf("the caller's budgets must ride through untouched, got %+v", got)
	}
	if runID == "" {
		t.Fatal("the caller needs the run id — its progress stream is keyed by it")
	}
	// The budgets must reach the ROW, not merely the request: the workflow reads
	// them from there, so a run admitted without them would silently take the
	// platform defaults and repair a version the caller asked only to re-check.
	admitted := h.runs.rows[len(h.runs.rows)-1]
	if admitted.ValidationAttempts != 1 || admitted.CycleCeiling != 4 {
		t.Fatalf("the admitted row must carry the caller's budgets, got attempts=%d ceiling=%d",
			admitted.ValidationAttempts, admitted.CycleCeiling)
	}
}

// TestRevalidate_RefusesWhileARunIsLive. Adoption treats a live run as a no-op
// because the run picks the issue up at its next boundary; a revalidation has
// nothing to hand off, so a silent success would answer nothing.
//
// The guard also has to run BEFORE admission: the revalidate origin is outside
// the per-project build mutex, so nothing in the database would refuse a second
// row — the supervisor would admit one, find the workflow id taken, and return
// successfully, leaving a row nobody drives.
func TestRevalidate_RefusesWhileARunIsLive(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))

	_, err := h.events.Revalidate(context.Background(), testOrg, testProject,
		MilestoneRef{Number: 7, Title: "v4"}, 1, 0)
	if !errors.Is(err, delivery.ErrRunAlreadyLive) {
		t.Fatalf("a live run must refuse the revalidation, got %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatal("a refused revalidation must start nothing")
	}
}

// TestRevalidate_RefusesWhileWorkIsOpen. The loop polls before it validates, so
// a milestone with a working set would get a CODING cycle first — the version
// rebuilt rather than re-judged. Refusing keeps the button meaning one thing.
func TestRevalidate_RefusesWhileWorkIsOpen(t *testing.T) {
	h := newHarness(t, aRun("run-old", 5, delivery.RunStateSucceeded))
	h.issues.withOpenIssues(5,
		[]string{delivery.LabelAgentWork},
		[]string{delivery.LabelAgentWork})

	_, err := h.events.Revalidate(context.Background(), testOrg, testProject,
		MilestoneRef{Number: 5, Title: "v3"}, 1, 0)
	if !errors.Is(err, delivery.ErrMilestoneHasOpenWork) {
		t.Fatalf("open work must refuse the revalidation, got %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatal("a refused revalidation must start nothing")
	}
}

// TestRevalidate_GateOrValidationIssueIsNotOpenWork guards the WORKING-SET
// reading of that refusal. A dispatch gate and the version's own validation task
// are both open issues in the milestone and neither is work a coding cycle would
// pick up — counting them would make a version permanently unrevalidatable, since
// the validation task is reopened by every attempt. Note the task IS armed now,
// so only the KIND test keeps it out; a predicate reading the arming label alone
// would refuse every revalidation.
func TestRevalidate_GateOrValidationIssueIsNotOpenWork(t *testing.T) {
	h := newHarness(t, aRun("run-old", 5, delivery.RunStateSucceeded))
	h.issues.withOpenIssues(5,
		[]string{delivery.KindProvision},
		[]string{delivery.LabelAgentWork, delivery.KindValidation})

	if _, err := h.events.Revalidate(context.Background(), testOrg, testProject,
		MilestoneRef{Number: 5, Title: "v3"}, 1, 0); err != nil {
		t.Fatalf("excluded issues are not open work: %v", err)
	}
	if len(h.sup.started) != 1 {
		t.Fatalf("the revalidation must start, got %+v", h.sup.started)
	}
}

// TestRevalidate_RefusesWithoutAnOracle. A run with nothing to validate concludes
// `skipped`, and because the newest run on a milestone owns the version's
// verdict, that would replace a real answer with "not validated". Refusing up
// front is what keeps a settled verdict safe from a click.
func TestRevalidate_RefusesWithoutAnOracle(t *testing.T) {
	h := newHarness(t, aRun("run-old", 5, delivery.RunStateSucceeded))
	h.oracle.has = false

	_, err := h.events.Revalidate(context.Background(), testOrg, testProject,
		MilestoneRef{Number: 5, Title: "v3"}, 1, 0)
	if !errors.Is(err, delivery.ErrNoValidationCriteria) {
		t.Fatalf("a version with no criteria must refuse, got %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatal("a refused revalidation must start nothing")
	}
}

// ---- the inertness gate ---------------------------------------------------

// TestNoRunRow_EveryHandlerIsInert is the safety property that lets this
// package land wired while the legacy pull_request handlers are still
// registered on the same keys. With no milestone run rows — production, today
// — every handler returns having touched nothing.
func TestNoRunRow_EveryHandlerIsInert(t *testing.T) {
	h := newHarness(t) // no run rows

	deliveries := []struct {
		event   string
		payload []byte
	}{
		{"pull_request", prBody("opened", "aep/m7-c1", "Resolves #12", 42, false, false, "")},
		{"pull_request", prBody("synchronize", "aep/m7-c1", "Resolves #12", 42, false, false, "")},
		{"pull_request", prBody("closed", "aep/m7-c1", "Resolves #12", 42, false, true, "abc123")},
		{"issues", issueBody("closed", 12, 7, "aep", "human", false)},
		{"issues", issueBody("milestoned", 12, 7, "aep", "human", true)},
		{"issues", issueBody("labeled", 12, 0, delivery.LabelAgentWork, "human", false)},
	}
	for _, d := range deliveries {
		if err := h.deliver(t, d.event, d.payload); err != nil {
			t.Fatalf("%s must not error with no run row: %v", d.event, err)
		}
	}
	// A build terminal too — the other detection route into this package.
	if err := h.events.OnBuildTerminal(context.Background(), delivery.BuildTerminal{
		OrgID: testOrg, ProjectID: testProject, Component: "order-service", CommitSHA: "abc123", Succeeded: false,
	}); err != nil {
		t.Fatalf("build terminal must not error with no run row: %v", err)
	}

	if len(h.merger.merged) != 0 {
		t.Errorf("no run row must mean no merge, got %v", h.merger.merged)
	}
	if len(h.issues.created) != 0 {
		t.Errorf("no run row must mean no issue minted, got %v", h.issues.titles())
	}
	if len(h.issues.assigned) != 0 {
		t.Errorf("no run row must mean no milestone assignment, got %v", h.issues.assigned)
	}
	if len(h.builds.triggered) != 0 {
		t.Errorf("no run row must mean no build, got %v", h.builds.triggered)
	}
	if len(h.comps.ensured) != 0 {
		t.Errorf("no run row must mean no component provisioned, got %v", h.comps.ensured)
	}
	if len(h.sup.signals) != 0 || len(h.sup.started) != 0 {
		t.Errorf("no run row must mean nothing signalled or started, got %+v / %+v", h.sup.signals, h.sup.started)
	}
	if len(h.cycles.notedPR) != 0 || len(h.cycles.closed) != 0 {
		t.Errorf("no run row must mean no cycle write, got %v / %v", h.cycles.notedPR, h.cycles.closed)
	}
}

// TestAdoption_NeverAdoptsThePlatformsOwnValidationTask is the live failure this
// guard exists for, and it is a webhook-level test on purpose: the defect was
// invisible at the unit level because it depended on WHO GitHub said the sender
// was.
//
// The platform mints the version's validation task with `aep` + `validation`.
// On an install with a GitHub App that create returns as a self-sender delivery
// and echo suppression drops it — but an install with no App writes through a
// human PAT, so the same delivery arrives indistinguishable from a human arming
// an issue. Adoption then started a bug-fix run over the milestone, that run
// parked on an empty working set, and because it was non-terminal it held the
// per-milestone live-run index — so the validation run could never be admitted
// and the version was never judged.
//
// The sender here is a HUMAN precisely so suppression cannot be what saves it.
func TestAdoption_NeverAdoptsThePlatformsOwnValidationTask(t *testing.T) {
	h := newHarness(t, aRun("run-old", 5, delivery.RunStateSucceeded))

	body := issueBodyWithLabels("labeled", 6, 5, delivery.LabelAgentWork,
		[]string{delivery.LabelAgentWork, delivery.KindValidation}, "a-human-pat")
	if err := h.deliver(t, "issues", body); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("the platform's own validation task started a run: %+v — it is the "+
			"reconcile sweep's to route, as a validation run", h.sup.started)
	}
}

// TestAdoption_NeverAdoptsPlannedWork: a dev run owns the version's planned work
// and holds its build mutex. A task run picking it up would work it with
// different budgets, and the two would be on one branch.
func TestAdoption_NeverAdoptsPlannedWork(t *testing.T) {
	h := newHarness(t, aRun("run-old", 5, delivery.RunStateSucceeded))

	body := issueBodyWithLabels("labeled", 9, 5, delivery.LabelAgentWork,
		[]string{delivery.LabelAgentWork, delivery.KindDevelopment}, "a-human-pat")
	if err := h.deliver(t, "issues", body); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("planned work started a bug-fix run: %+v", h.sup.started)
	}
}

// TestAdoption_StillAdoptsARealDefect is the other half — the guard must not
// have closed the door on the path adoption exists for.
func TestAdoption_StillAdoptsARealDefect(t *testing.T) {
	h := newHarness(t, aRun("run-old", 5, delivery.RunStateSucceeded))

	body := issueBodyWithLabels("labeled", 12, 0, delivery.LabelAgentWork,
		[]string{delivery.LabelAgentWork, delivery.KindBug, delivery.SrcIncident}, "a-human-pat")
	if err := h.deliver(t, "issues", body); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(h.sup.started) != 1 || h.sup.started[0].Kind != delivery.RunKindTask {
		t.Fatalf("a real defect must still start a task run, got %+v", h.sup.started)
	}
}
