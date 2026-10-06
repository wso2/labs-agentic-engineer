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

package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func newTestTap(issues *fakeIssues) *planTap {
	tap := newPlanTap(context.Background(), "org1", "proj1", issues, issues.writer())
	// Every plan turn the build click drives plans INTO a milestone; 5 is this
	// version's.
	tap.milestone = 5
	tap.appPaths = map[string]string{"order-service": "src/order-service"}
	return tap
}

func planOK(component, title string, deps []string) string {
	depsJSON := "[]"
	if len(deps) > 0 {
		depsJSON = `["` + strings.Join(deps, `","`) + `"]`
	}
	return fmt.Sprintf(`{"ok":true,"op":"plan","component":%q,"title":%q,"dependsOn":%s,"origin":"spec-plan","rationale":"do it"}`, component, title, depsJSON)
}

func updateByTitleBody(title, body string) string {
	return fmt.Sprintf(`{"ok":true,"op":"update","ref":{"title":%q},"set":{"body":%q}}`, title, body)
}

// taskOp is one task-op line of the pod's turn stream: the ok output of a
// planTask or updateTask call, op taken from it.
func taskOp(output string) aestudiotools.TurnEvent {
	var head struct {
		Op string `json:"op"`
	}
	_ = json.Unmarshal([]byte(output), &head)
	return aestudiotools.TurnEvent{Type: aestudiotools.EventTaskOp, Op: head.Op, Output: json.RawMessage(output)}
}

var (
	keepAlive = aestudiotools.TurnEvent{Type: aestudiotools.EventKeepAlive}
	completed = aestudiotools.TurnEvent{Type: aestudiotools.EventResult, Status: "completed"}
)

// turn is a pod turn stream of evs, ending with a completed result unless
// evs ends with a result of its own.
func turn(evs ...aestudiotools.TurnEvent) iter.Seq2[aestudiotools.TurnEvent, error] {
	if len(evs) == 0 || evs[len(evs)-1].Type != aestudiotools.EventResult {
		evs = append(evs, completed)
	}
	return func(yield func(aestudiotools.TurnEvent, error) bool) {
		for _, ev := range evs {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func noAbort() {}

// A planned Task is ONE call: prose body, the arming label + the `development`
// kind, and the milestone assigned at creation. Nothing structured is written
// into the body — the milestone is the version pin and the label is the
// population marker, so a machine block would be a second source of truth
// nobody reads.
func TestPlanTap_PlanMintsProseIssueIntoTheMilestone(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)

	if err := tap.Stream(turn(taskOp(planOK("order-service", "Implement order-service", []string{"user-service"}))), noAbort); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if len(issues.created) != 1 {
		t.Fatalf("expected 1 issue created, got %d", len(issues.created))
	}
	got := issues.created[0]
	// Armed, and PLANNED work. The kind is what keeps a bug-fix run — which works
	// the DEPLOYED version — off the work of the version still being built.
	if !delivery.InDevWorkingSet(got.Labels) {
		t.Errorf("labels = %v, want a planned Task in the dev working set", got.Labels)
	}
	if delivery.InTaskWorkingSet(got.Labels) {
		t.Errorf("labels = %v — planned work must never be in a bug-fix run's working set", got.Labels)
	}
	if got.Labels[0] != delivery.LabelAgentWork || delivery.KindOf(got.Labels) != delivery.KindDevelopment {
		t.Errorf("labels = %v, want [%s %s]", got.Labels, delivery.LabelAgentWork, delivery.KindDevelopment)
	}
	if got.Milestone == nil || *got.Milestone != 5 {
		t.Errorf("milestone = %v, want 5 assigned at creation (1+N, not create-then-patch)", got.Milestone)
	}
	if strings.Contains(got.Body, "aep:task/v1") {
		t.Errorf("created body still carries a machine block — bodies are prose:\n%s", got.Body)
	}
	for _, want := range []string{"**Component:** `order-service`", "**App Path:** `src/order-service`", "do it"} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("body missing %q:\n%s", want, got.Body)
		}
	}
	// user-service has no issue yet (forward reference) — the dependency is
	// named rather than dropped.
	if !strings.Contains(got.Body, "Depends on the `user-service` task") {
		t.Errorf("body lost its unresolved dependency line:\n%s", got.Body)
	}
}

// A dependency whose own Task was planned earlier in the SAME turn resolves to
// a real issue number — the reference the agent follows.
func TestPlanTap_DependsOnResolvesToIssueNumber(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)

	if err := tap.Stream(turn(
		taskOp(planOK("user-service", "Implement user-service", nil)),
		taskOp(planOK("order-service", "Implement order-service", []string{"user-service"})),
	), noAbort); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if len(issues.created) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues.created))
	}
	want := fmt.Sprintf("Depends on #%d", issues.byNumber[100].Number)
	if !strings.Contains(issues.created[1].Body, want) {
		t.Errorf("body missing %q:\n%s", want, issues.created[1].Body)
	}
}

// Only task-op lines carry work: a keep-alive mints nothing, and the pod
// projects only ok tool results (an ok:false never reaches the tap). A
// task-op whose output is not a successful result is still refused by the
// decoders rather than minted.
func TestPlanTap_OnlyASuccessfulTaskOpMints(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)

	if err := tap.Stream(turn(
		keepAlive,
		taskOp(`{"ok":false,"op":"plan","code":"UNKNOWN_COMPONENT","message":"nope"}`),
		aestudiotools.TurnEvent{Type: "something-new", Output: json.RawMessage(planOK("x", "Implement x", nil))},
	), noAbort); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(issues.created) != 0 {
		t.Fatalf("created %d issues, want none", len(issues.created))
	}
}

func TestPlanTap_UpdateByTitle_SetsBody(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)

	if err := tap.Stream(turn(
		taskOp(planOK("order-service", "Implement order-service", nil)),
		taskOp(updateByTitleBody("Implement order-service", "## Scope\nWrite the order service.")),
	), noAbort); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if len(issues.created) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues.created))
	}
	body := issues.bodyOf(100) // first created number
	if !strings.Contains(body, "Write the order service.") {
		t.Errorf("expected body updated via updateTask by title, got %q", body)
	}
	// The whole body is re-rendered from the tracked facts, so the platform
	// parts survive the patch rather than being overwritten by it.
	if !strings.Contains(body, "**Component:** `order-service`") {
		t.Errorf("patched body lost its component line: %q", body)
	}
}

func TestPlanTap_UpdateByIssueNumber_PreExisting(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(agentIssue(42, "Implement user-service", "brief"))
	tap := newTestTap(issues)
	tap.state[42] = plannedTask{Component: "user-service", Rationale: "orig"}
	tap.contextNumbers[42] = true // #42 was preloaded into the turn's context

	if err := tap.Stream(turn(
		taskOp(`{"ok":true,"op":"update","ref":{"issueNumber":42},"set":{"body":"## Scope\nnew scope","rationale":"revised"}}`),
	), noAbort); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	got := issues.bodyOf(42)
	if !strings.Contains(got, "new scope") || !strings.Contains(got, "revised") {
		t.Errorf("expected pre-existing issue patched, got %q", got)
	}
}

// TestPlanTap_UpdateByIssueNumber_OutOfContext_NoWrite pins the gate-review
// fence: an updateTask{issueNumber} pointing at an issue NOT preloaded into the
// turn's context (e.g. a human bug report sharing the id space) must NOT be
// written — no title/body edit, no attention label — and must be recorded in the
// write-failure accounting.
func TestPlanTap_UpdateByIssueNumber_OutOfContext_NoWrite(t *testing.T) {
	issues := newFakeIssues()
	// #999 exists on the repo but was never part of the plan context (unrelated).
	issues.seed(sourcecontrol.IssueInfo{Number: 999, Title: "Prod bug: checkout 500", Body: "Users can't check out.", State: "open"})
	tap := newTestTap(issues) // contextNumbers is empty → 999 is out of context

	if err := tap.Stream(turn(
		taskOp(`{"ok":true,"op":"update","ref":{"issueNumber":999},"set":{"body":"## Scope\nclobbered","title":"clobbered"}}`),
	), noAbort); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// The unrelated issue is untouched — body, title, and labels unchanged.
	if got := issues.bodyOf(999); got != "Users can't check out." {
		t.Errorf("out-of-context issue body must be untouched, got %q", got)
	}
	if got := issues.byNumber[999].Title; got != "Prod bug: checkout 500" {
		t.Errorf("out-of-context issue title must be untouched, got %q", got)
	}
	if len(issues.labelsOf(999)) != 0 {
		t.Errorf("out-of-context issue must not be labeled, got %v", issues.labelsOf(999))
	}
	if tap.failures != 1 {
		t.Errorf("out-of-context update must be recorded as a write-failure, got %d", tap.failures)
	}
}

func TestPlanTap_Rename_RemapsTitleRef(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)

	if err := tap.Stream(turn(
		taskOp(planOK("order-service", "Old title", nil)),
		// Rename via updateTask (ref.title is the canonical pre-rename title).
		taskOp(`{"ok":true,"op":"update","ref":{"title":"Old title"},"set":{"title":"New title"}}`),
		// A subsequent update addressing the NEW title must resolve.
		taskOp(updateByTitleBody("New title", "## Scope\nafter rename")),
	), noAbort); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if issues.byNumber[100].Title != "New title" {
		t.Errorf("expected title renamed, got %q", issues.byNumber[100].Title)
	}
	if !strings.Contains(issues.bodyOf(100), "after rename") {
		t.Errorf("expected post-rename body update to resolve via the new title")
	}
}

func TestPlanTap_Dedupe_SamePlanTwice(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)

	if err := tap.Stream(turn(
		taskOp(planOK("order-service", "Implement order-service", nil)),
		taskOp(planOK("order-service", "Implement order-service", nil)),
	), noAbort); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if len(issues.created) != 1 {
		t.Fatalf("duplicate planTask (same title slug) must dedupe to one create, got %d", len(issues.created))
	}
}

// Re-plan reconcile is ADDITIVE-ONLY: a title already in the milestone is
// skipped whatever punctuation or casing the planner emits it with, and
// anything new is minted.
func TestPlanTap_DedupesAgainstTheMilestonesExistingTitles(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)
	tap.existingSlugs[titleSlug("Implement order-service")] = true

	if err := tap.Stream(turn(
		taskOp(planOK("order-service", "  implement ORDER-service!  ", nil)),
		taskOp(planOK("user-service", "Implement user-service", nil)),
	), noAbort); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if len(issues.created) != 1 {
		t.Fatalf("want exactly the ONE new Task minted, got %d: %+v", len(issues.created), issues.created)
	}
	if issues.created[0].Title != "Implement user-service" {
		t.Errorf("created %q, want the only title the milestone did not already hold", issues.created[0].Title)
	}
}

// A write the tap could not land is recorded twice: as a comment on the issue
// whose brief is now incomplete, and in the failure count the plan path reads
// back — a short plan settles the run it was filling rather than supervising a
// milestone that is missing work.
func TestPlanTap_WriteFailure_CommentsAndCounts(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(agentIssue(42, "Implement user-service", "brief"))
	tap := newTestTap(issues)
	tap.state[42] = plannedTask{Component: "user-service"}
	tap.contextNumbers[42] = true // in-context; the failure is at the GitHub write
	issues.failEditBody = true

	if err := tap.Stream(turn(
		taskOp(`{"ok":true,"op":"update","ref":{"issueNumber":42},"set":{"body":"x"}}`),
	), noAbort); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if len(issues.comments[42]) != 1 || !strings.Contains(issues.comments[42][0], "failed to apply") {
		t.Errorf("expected one write-failure comment on the issue, got %v", issues.comments[42])
	}
	if tap.failures != 1 {
		t.Errorf("expected 1 recorded failure, got %d", tap.failures)
	}
}

// A turn the pod ends `failed` is an error, carrying the pod's code: the
// planning activity retries it rather than settling a half-planned milestone
// as done. What it planned before failing stays minted.
func TestPlanTap_AFailedTurnIsAnError(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)

	err := tap.Stream(turn(
		taskOp(planOK("order-service", "Implement order-service", nil)),
		aestudiotools.TurnEvent{Type: aestudiotools.EventResult, Status: "failed", Code: "shutdown"},
	), noAbort)
	var failed *aestudiotools.TurnFailedError
	if !errors.As(err, &failed) || failed.Code != "shutdown" {
		t.Fatalf("err = %v, want a TurnFailedError with the pod's code", err)
	}
	if len(issues.created) != 1 {
		t.Fatalf("created %d issues, want the one planned before the failure", len(issues.created))
	}
}

// A provider_limit result's reset time rides on the error, so the planning
// activity can wait until then.
func TestPlanTap_AProviderLimitCarriesTheResetTime(t *testing.T) {
	resetAt := time.Date(2026, 10, 4, 10, 15, 0, 0, time.UTC)
	tap := newTestTap(newFakeIssues())

	err := tap.Stream(turn(
		aestudiotools.TurnEvent{Type: aestudiotools.EventResult, Status: "failed", Code: aestudiotools.TurnCodeProviderLimit, ResetAt: resetAt},
	), noAbort)
	var failed *aestudiotools.TurnFailedError
	if !errors.As(err, &failed) || !failed.ProviderLimited() || !failed.ResetAt.Equal(resetAt) {
		t.Fatalf("err = %v, want a provider_limit TurnFailedError reset at %v", err, resetAt)
	}
}

// A stream that breaks (the adapter yields an error: the pod went away, a
// malformed line) is an error, never a quiet success.
func TestPlanTap_ABrokenStreamIsAnError(t *testing.T) {
	tap := newTestTap(newFakeIssues())
	broken := func(yield func(aestudiotools.TurnEvent, error) bool) {
		if !yield(keepAlive, nil) {
			return
		}
		yield(aestudiotools.TurnEvent{}, sourcecontrol.ErrAEStudioUnavailable)
	}

	if err := tap.Stream(broken, noAbort); !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Fatalf("err = %v, want the stream's error", err)
	}
}

// Every event — keep-alives included — reports progress, so the planning
// activity heartbeats per event rather than only on its own clock.
func TestPlanTap_EveryEventReportsProgress(t *testing.T) {
	var beats atomic.Int32
	ctx := delivery.WithProgress(context.Background(), func() { beats.Add(1) })
	issues := newFakeIssues()
	tap := newPlanTap(ctx, "org1", "proj1", issues, issues.writer())

	if err := tap.Stream(turn(keepAlive, taskOp(planOK("a", "Implement a", nil)), keepAlive), noAbort); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got := beats.Load(); got != 4 { // two keep-alives, one task-op, the result
		t.Fatalf("progress beats = %d, want 4 (one per event)", got)
	}
}

// silentUntilAborted is a turn that sends nothing until abort is called.
func silentUntilAborted(aborted <-chan struct{}) iter.Seq2[aestudiotools.TurnEvent, error] {
	return func(yield func(aestudiotools.TurnEvent, error) bool) {
		<-aborted
		yield(aestudiotools.TurnEvent{}, context.Canceled)
	}
}

// A turn that goes silent past the idle deadline — no task-op, no keep-alive
// — is aborted, so a hung pod cannot pin the planning activity for its whole
// 30-minute timeout. The abort ends the stream (closing its body) and is
// reported as its own error, not as the read error the abort caused.
func TestPlanTap_IdleWatchdogAbortsASilentTurn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tap := newTestTap(newFakeIssues())
		aborted := make(chan struct{})
		start := time.Now()

		err := tap.Stream(silentUntilAborted(aborted), func() { close(aborted) })

		if !errors.Is(err, errPlanTurnSilent) {
			t.Fatalf("err = %v, want errPlanTurnSilent", err)
		}
		if waited := time.Since(start); waited != planDrainIdleTimeout {
			t.Fatalf("aborted after %v, want exactly the %v idle deadline", waited, planDrainIdleTimeout)
		}
	})
}

// A Plan turn that reads files for two minutes between two Tasks sends only
// keep-alives in that time. They are proof of life: the watchdog resets on
// each, so the turn is NOT aborted and the Task planned after the quiet
// stretch is minted.
func TestPlanTap_KeepAlivesHoldAQuietTurnOpen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		issues := newFakeIssues()
		tap := newTestTap(issues)
		abortCalled := false
		quiet := func(yield func(aestudiotools.TurnEvent, error) bool) {
			if !yield(taskOp(planOK("a", "Implement a", nil)), nil) {
				return
			}
			for elapsed := time.Duration(0); elapsed < 2*time.Minute; elapsed += 15 * time.Second {
				time.Sleep(15 * time.Second)
				if !yield(keepAlive, nil) {
					return
				}
			}
			if !yield(taskOp(planOK("b", "Implement b", nil)), nil) {
				return
			}
			yield(completed, nil)
		}

		if err := tap.Stream(quiet, func() { abortCalled = true }); err != nil {
			t.Fatalf("Stream: %v", err)
		}
		if abortCalled {
			t.Fatal("a turn sending keep-alives was aborted")
		}
		if len(issues.created) != 2 {
			t.Fatalf("created %d issues, want both Tasks", len(issues.created))
		}
	})
}

// The Turn socket's golden stream (packages/contracts/sockets/ae-studio/turn/
// golden/completed.ndjson) is what the pod sends, ae-studio-tools relays
// unchanged and this tap consumes: its planTask op mints a Task and
// its updateTask op (by this-turn title) reaches the same issue, with no op
// skipped as undecodable.
func TestPlanTap_TheGoldenStreamMintsAndUpdates(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "packages", "contracts", "sockets", "ae-studio", "turn", "golden", "completed.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	var evs []aestudiotools.TurnEvent
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var ev struct {
			Type, Op, Status, Code string
			Output                 json.RawMessage
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("golden line %q: %v", line, err)
		}
		evs = append(evs, aestudiotools.TurnEvent{Type: ev.Type, Op: ev.Op, Output: ev.Output, Status: ev.Status, Code: ev.Code})
	}
	issues := newFakeIssues()
	tap := newTestTap(issues)

	if err := tap.Stream(turn(evs...), noAbort); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(issues.created) != 1 || issues.created[0].Title != "Add the greeting endpoint" {
		t.Fatalf("created = %+v, want the golden planTask's one Task", issues.created)
	}
	if tap.failures != 0 {
		t.Fatalf("failures = %d", tap.failures)
	}
	if st := tap.state[tap.titleToNumber["add the greeting endpoint"]]; st.Rationale != "Stories 1 and 2 both need it." {
		t.Fatalf("state = %+v, want the golden updateTask's rationale applied", st)
	}
}
