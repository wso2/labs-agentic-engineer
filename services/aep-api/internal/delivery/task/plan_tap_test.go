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
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

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

func toolResult(output string) string {
	return fmt.Sprintf("data: {\"type\":\"tool-result\",\"output\":%s}\n\n", output)
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

func stream(frames ...string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(strings.Join(frames, "")))
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("client gone") }

// A planned Task is ONE call: prose body, the arming label + the `development`
// kind, and the
// milestone assigned at creation. Nothing structured is written into the body —
// the milestone is the version pin and the label is the population marker, so a
// machine block would be a second source of truth nobody reads.
func TestPlanTap_PlanMintsProseIssueIntoTheMilestone(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)
	var buf bytes.Buffer

	tap.Stream(stream(
		toolResult(planOK("order-service", "Implement order-service", []string{"user-service"})),
		"data: [DONE]\n\n",
	), &buf, func() {})

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
	// The stream must have been forwarded verbatim.
	if !strings.Contains(buf.String(), "[DONE]") {
		t.Errorf("stream not forwarded verbatim")
	}
}

// A dependency whose own Task was planned earlier in the SAME turn resolves to
// a real issue number — the reference the agent follows.
func TestPlanTap_DependsOnResolvesToIssueNumber(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)
	var buf bytes.Buffer

	tap.Stream(stream(
		toolResult(planOK("user-service", "Implement user-service", nil)),
		toolResult(planOK("order-service", "Implement order-service", []string{"user-service"})),
		"data: [DONE]\n\n",
	), &buf, func() {})

	if len(issues.created) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues.created))
	}
	want := fmt.Sprintf("Depends on #%d", issues.byNumber[100].Number)
	if !strings.Contains(issues.created[1].Body, want) {
		t.Errorf("body missing %q:\n%s", want, issues.created[1].Body)
	}
}

// orderWriter snapshots the fake's created-count at the moment each line is
// forwarded to the client.
type orderWriter struct {
	issues        *fakeIssues
	createdAtLine []int
	buf           bytes.Buffer
}

func (w *orderWriter) Write(p []byte) (int, error) {
	w.createdAtLine = append(w.createdAtLine, len(w.issues.created))
	return w.buf.Write(p)
}

// The FE refreshes its task list the moment an ok tool-result frame arrives, so
// the tap MUST perform the GitHub write BEFORE forwarding that frame (§6/§8) —
// otherwise the row cannot materialize in the pending section on that refresh.
func TestPlanTap_WritesLandBeforeFrameIsForwarded(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)
	w := &orderWriter{issues: issues}

	tap.Stream(stream(
		toolResult(planOK("order-service", "Implement order-service", nil)),
		"data: [DONE]\n\n",
	), w, func() {})

	if len(w.createdAtLine) == 0 {
		t.Fatal("nothing forwarded")
	}
	// Line 0 is the planTask tool-result: the issue must already exist.
	if w.createdAtLine[0] != 1 {
		t.Errorf("issue created AFTER its result frame was forwarded (created=%d at forward time)", w.createdAtLine[0])
	}
	if !strings.Contains(w.buf.String(), "[DONE]") {
		t.Errorf("stream not forwarded verbatim")
	}
}

func TestPlanTap_PlanNotOK_NoCreate(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)
	var buf bytes.Buffer

	tap.Stream(stream(
		toolResult(`{"ok":false,"op":"plan","code":"UNKNOWN_COMPONENT","message":"nope","knownComponents":["a"]}`),
	), &buf, func() {})

	if len(issues.created) != 0 {
		t.Fatalf("ok:false planTask must not create an issue, got %d", len(issues.created))
	}
}

func TestPlanTap_UpdateByTitle_SetsBody(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)
	var buf bytes.Buffer

	tap.Stream(stream(
		toolResult(planOK("order-service", "Implement order-service", nil)),
		toolResult(updateByTitleBody("Implement order-service", "## Scope\nWrite the order service.")),
		"data: [DONE]\n\n",
	), &buf, func() {})

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
	var buf bytes.Buffer

	tap.Stream(stream(
		toolResult(`{"ok":true,"op":"update","ref":{"issueNumber":42},"set":{"body":"## Scope\nnew scope","rationale":"revised"}}`),
		"data: [DONE]\n\n",
	), &buf, func() {})

	got := issues.bodyOf(42)
	if !strings.Contains(got, "new scope") || !strings.Contains(got, "revised") {
		t.Errorf("expected pre-existing issue patched, got %q", got)
	}
}

// TestPlanTap_UpdateByIssueNumber_OutOfContext_NoWrite pins the gate-review
// fence: an updateTask{issueNumber} pointing at an issue NOT preloaded into the
// turn's context (e.g. a human bug report sharing the id space) must NOT be
// written — no title/body edit, no attention label — and must be recorded in the
// write-failure accounting so the terminal surface reports it.
func TestPlanTap_UpdateByIssueNumber_OutOfContext_NoWrite(t *testing.T) {
	issues := newFakeIssues()
	// #999 exists on the repo but was never part of the plan context (unrelated).
	issues.seed(sourcecontrol.IssueInfo{Number: 999, Title: "Prod bug: checkout 500", Body: "Users can't check out.", State: "open"})
	tap := newTestTap(issues) // contextNumbers is empty → 999 is out of context
	var buf bytes.Buffer

	tap.Stream(stream(
		toolResult(`{"ok":true,"op":"update","ref":{"issueNumber":999},"set":{"body":"## Scope\nclobbered","title":"clobbered"}}`),
		"data: [DONE]\n\n",
	), &buf, func() {})

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
	// The skipped op is recorded and surfaced.
	if tap.failures != 1 {
		t.Errorf("out-of-context update must be recorded as a write-failure, got %d", tap.failures)
	}
	if !strings.Contains(buf.String(), "aep-plan-write-failures 1") {
		t.Errorf("expected terminal in-band failure surface, got %q", buf.String())
	}
}

func TestPlanTap_Rename_RemapsTitleRef(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)
	var buf bytes.Buffer

	tap.Stream(stream(
		toolResult(planOK("order-service", "Old title", nil)),
		// Rename via updateTask (ref.title is the canonical pre-rename title).
		toolResult(`{"ok":true,"op":"update","ref":{"title":"Old title"},"set":{"title":"New title"}}`),
		// A subsequent update addressing the NEW title must resolve.
		toolResult(updateByTitleBody("New title", "## Scope\nafter rename")),
		"data: [DONE]\n\n",
	), &buf, func() {})

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
	var buf bytes.Buffer

	tap.Stream(stream(
		toolResult(planOK("order-service", "Implement order-service", nil)),
		toolResult(planOK("order-service", "Implement order-service", nil)),
		"data: [DONE]\n\n",
	), &buf, func() {})

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
	var buf bytes.Buffer

	tap.Stream(stream(
		toolResult(planOK("order-service", "  implement ORDER-service!  ", nil)),
		toolResult(planOK("user-service", "Implement user-service", nil)),
		"data: [DONE]\n\n",
	), &buf, func() {})

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
	var buf bytes.Buffer

	tap.Stream(stream(
		toolResult(`{"ok":true,"op":"update","ref":{"issueNumber":42},"set":{"body":"x"}}`),
		"data: [DONE]\n\n",
	), &buf, func() {})

	if len(issues.comments[42]) != 1 || !strings.Contains(issues.comments[42][0], "failed to apply") {
		t.Errorf("expected one write-failure comment on the issue, got %v", issues.comments[42])
	}
	if tap.failures != 1 {
		t.Errorf("expected 1 recorded failure, got %d", tap.failures)
	}
	if !strings.Contains(buf.String(), "aep-plan-write-failures 1") {
		t.Errorf("expected terminal in-band failure surface, got %q", buf.String())
	}
}

func TestPlanTap_DrainOnDisconnect(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)

	// The client writer errors immediately (disconnect), but the tap must keep
	// reading upstream and perform the GitHub write.
	tap.Stream(stream(
		toolResult(planOK("order-service", "Implement order-service", nil)),
		"data: [DONE]\n\n",
	), failWriter{}, func() {})

	if len(issues.created) != 1 {
		t.Fatalf("tap must drain and create the issue even after client disconnect, got %d", len(issues.created))
	}
}

// hangingBody blocks on Read until Close is called (a hung agents turn sending
// no bytes / keep-alives), then returns EOF — the idle watchdog's Close unblocks it.
type hangingBody struct {
	closed chan struct{}
	once   sync.Once
}

func newHangingBody() *hangingBody { return &hangingBody{closed: make(chan struct{})} }

func (b *hangingBody) Read([]byte) (int, error) {
	<-b.closed
	return 0, io.EOF
}
func (b *hangingBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

// TestPlanTap_IdleDeadline_AbortsHungDrain pins the gate-review idle-read
// deadline: a turn that goes silent past the idle timeout must abort the drain
// (so the per-project plan lock releases) rather than block forever, and record
// the abort in the write-failure accounting.
func TestPlanTap_IdleDeadline_AbortsHungDrain(t *testing.T) {
	issues := newFakeIssues()
	tap := newTestTap(issues)
	tap.idleTimeout = 20 * time.Millisecond
	var buf bytes.Buffer

	done := make(chan struct{})
	go func() {
		tap.Stream(newHangingBody(), &buf, func() {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stream did not abort a hung drain — the plan lock would be pinned")
	}
	if tap.failures != 1 {
		t.Errorf("an idle-aborted drain must be recorded as a write-failure, got %d", tap.failures)
	}
	if !strings.Contains(buf.String(), "aep-plan-write-failures 1") {
		t.Errorf("expected terminal in-band failure surface, got %q", buf.String())
	}
}
