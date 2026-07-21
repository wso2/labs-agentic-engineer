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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ---- issue_compose round-trip ----------------------------------------------

func TestComposePlannedIssue_RoundTrip(t *testing.T) {
	block, body := composePlannedIssue("proj1", plannedTask{
		Component: "order-service",
		Title:     "Implement order-service",
		DependsOn: []string{"user-service"},
		Origin:    taskmeta.OriginSpecPlan,
		SpecTag:   "req-v1",
		DesignTag: "design-v1",
		Rationale: "core of the plan",
	})
	if block.Key == "" {
		t.Fatal("expected an idempotency key")
	}
	got, human, err := taskmeta.ParseBody(body)
	if err != nil {
		t.Fatalf("round-trip parse: %v", err)
	}
	if got.Component != "order-service" || got.DesignTag != "design-v1" || len(got.DependsOn) != 1 {
		t.Errorf("block did not round-trip: %+v", got)
	}
	if human.Rationale != "core of the plan" {
		t.Errorf("rationale did not round-trip: %q", human.Rationale)
	}
	// Key is stable for the same inputs.
	if k := taskmeta.Key("proj1", "design-v1", "order-service", "Implement order-service"); k != block.Key {
		t.Errorf("key not reproducible: %q != %q", k, block.Key)
	}
}

// ---- reads derive fusion ---------------------------------------------------

func newReads(issues *fakeIssues, execs *fakeExecReader) *Reads {
	return NewReads(issues, fakeRepos{repo: defaultRepo()}, execs, nil, nil, nil)
}

func TestReads_List_DerivesStatusFromExecutions(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(gitrepoIssue(1, "user-service", "design-v1"))  // deployed
	issues.seed(gitrepoIssue(2, "order-service", "design-v1")) // in progress
	issues.seed(gitrepoIssue(3, "cart-service", "design-v1"))  // pending

	execs := newFakeExecReader()
	// 1: coding succeeded + build succeeded → deployed.
	execs.put(1, delivery.Execution{ID: "e1", Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecSucceeded), CreatedAt: time.Now().Add(-2 * time.Hour)})
	execs.put(1, delivery.Execution{ID: "e1b", Kind: string(taskmeta.KindBuild), Status: string(taskmeta.ExecSucceeded), CreatedAt: time.Now().Add(-1 * time.Hour)})
	// 2: coding running → in_progress.
	execs.put(2, delivery.Execution{ID: "e2", Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecRunning), CreatedAt: time.Now()})

	views, err := newReads(issues, execs).List(context.Background(), "org1", "proj1", "open")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byNum := map[int]delivery.TaskView{}
	for _, v := range views {
		byNum[v.IssueNumber] = v
	}
	if got := byNum[1].DerivedStatus; got != string(taskmeta.StatusDeployed) {
		t.Errorf("task 1 status = %q, want deployed", got)
	}
	if got := byNum[2].DerivedStatus; got != string(taskmeta.StatusInProgress) {
		t.Errorf("task 2 status = %q, want in_progress", got)
	}
	if got := byNum[3].DerivedStatus; got != string(taskmeta.StatusPending) {
		t.Errorf("task 3 status = %q, want pending", got)
	}
}

func TestReads_ListByTag_FiltersByBlockLineage(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(taggedIssue(1, "user-service", "v3"))
	issues.seed(taggedIssue(2, "order-service", "v3"))
	issues.seed(taggedIssue(3, "cart-service", "v2")) // an earlier build's task
	// A legacy Task planned before the aep:spec/<tag> label existed (#182):
	// the version lives only in its machine block. Tag scoping must still
	// find it — the block is the durable truth, the label just its mirror.
	legacyBlock := taskmeta.Block{Component: "billing-service", Origin: taskmeta.OriginSpecPlan, SpecTag: "v3", DesignTag: "design-v1"}
	issues.seed(sourcecontrol.IssueInfo{
		Number: 4,
		Title:  "Implement billing-service",
		Body:   taskmeta.ComposeBody(legacyBlock, taskmeta.Human{Rationale: "orig"}),
		State:  "open",
		URL:    "https://github.com/o/r/issues/4",
		Labels: taskmeta.NewTaskLabels(taskmeta.ClassCoding, taskmeta.OriginSpecPlan),
	})

	// Scoped to v3: the two labelled v3 Tasks plus the label-less legacy one.
	views, err := newReads(issues, newFakeExecReader()).ListByTag(context.Background(), "org1", "proj1", "all", "v3")
	if err != nil {
		t.Fatalf("ListByTag(v3): %v", err)
	}
	if len(views) != 3 {
		t.Fatalf("want 3 tasks for v3 (incl. the legacy label-less one), got %d: %+v", len(views), views)
	}
	for _, v := range views {
		if v.Lineage.SpecTag != "v3" {
			t.Errorf("view %d specTag = %q, want v3", v.IssueNumber, v.Lineage.SpecTag)
		}
	}

	// Empty tag is identical to List: every Task, regardless of version.
	all, err := newReads(issues, newFakeExecReader()).ListByTag(context.Background(), "org1", "proj1", "all", "")
	if err != nil {
		t.Fatalf("ListByTag(all): %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("empty tag must return all tasks, got %d", len(all))
	}
}

func TestReads_List_CarriesHumanBodyWithoutMachineBlock(t *testing.T) {
	issues := newFakeIssues()
	block := taskmeta.Block{Component: "user-service", Origin: taskmeta.OriginSpecPlan, DesignTag: "design-v1"}
	human := taskmeta.Human{Rationale: "keeps auth isolated", Body: "## Scope\n\nImplement the login endpoint."}
	issue := gitrepoIssue(1, "user-service", "design-v1")
	issue.Body = taskmeta.ComposeBody(block, human)
	issues.seed(issue)

	views, err := newReads(issues, newFakeExecReader()).List(context.Background(), "org1", "proj1", "open")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("expected 1 view, got %d", len(views))
	}
	if views[0].Rationale != "keeps auth isolated" {
		t.Errorf("rationale = %q, want the planner rationale", views[0].Rationale)
	}
	if views[0].Body != human.Body {
		t.Errorf("body = %q, want the human markdown", views[0].Body)
	}
	if strings.Contains(views[0].Body, "aep:task") {
		t.Errorf("body must not leak the machine block: %q", views[0].Body)
	}
}

func TestReads_Get_IncludesHistory(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(gitrepoIssue(5, "order-service", "design-v1"))
	execs := newFakeExecReader()
	execs.put(5, delivery.Execution{ID: "a", Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecFailed), CreatedAt: time.Now().Add(-time.Hour)})
	execs.put(5, delivery.Execution{ID: "b", Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecSucceeded), CreatedAt: time.Now()})

	detail, err := newReads(issues, execs).Get(context.Background(), "org1", "proj1", 5)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(detail.ExecutionHistory) != 2 {
		t.Errorf("expected 2 history rows, got %d", len(detail.ExecutionHistory))
	}
	// Latest coding succeeded → PR open → ready_for_review.
	if detail.DerivedStatus != string(taskmeta.StatusReadyForReview) {
		t.Errorf("derived status = %q, want ready_for_review", detail.DerivedStatus)
	}
}

func TestReads_Get_NotFound(t *testing.T) {
	issues := newFakeIssues()
	_, err := newReads(issues, newFakeExecReader()).Get(context.Background(), "org1", "proj1", 999)
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

// ---- commands --------------------------------------------------------------

func newCommands(issues *fakeIssues, disp *fakeDispatcher) *Commands {
	return NewCommands(issues, fakeRepos{repo: defaultRepo()}, disp, nil)
}

func TestCommands_Execute_OpenIssue_StampsAndDispatches(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(gitrepoIssue(7, "order-service", "design-v1"))
	disp := newFakeDispatcher()

	if err := newCommands(issues, disp).Execute(context.Background(), "org1", "proj1", 7); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !issueHasAll(issues.labelsOf(7), []string{taskmeta.LabelExecute}) {
		t.Errorf("expected aep:execute stamped, got %v", issues.labelsOf(7))
	}
	select {
	case <-disp.signal:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher.OnExecuteIntent was not called")
	}
	if got := disp.executed(); len(got) != 1 || got[0] != 7 {
		t.Errorf("expected dispatch for issue 7, got %v", got)
	}
}

func TestCommands_Execute_ClosedIssue_409(t *testing.T) {
	issues := newFakeIssues()
	closed := gitrepoIssue(8, "order-service", "design-v1")
	closed.State = "closed"
	issues.seed(closed)

	err := newCommands(issues, newFakeDispatcher()).Execute(context.Background(), "org1", "proj1", 8)
	if !errors.Is(err, ErrIssueClosed) {
		t.Errorf("expected ErrIssueClosed, got %v", err)
	}
}

func TestCommands_HoldUnhold(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(gitrepoIssue(9, "order-service", "design-v1"))
	disp := newFakeDispatcher()
	c := newCommands(issues, disp)

	if err := c.Hold(context.Background(), "org1", "proj1", 9); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	if !issueHasAll(issues.labelsOf(9), []string{taskmeta.LabelHold}) {
		t.Errorf("expected aep:hold, got %v", issues.labelsOf(9))
	}
	if err := c.Unhold(context.Background(), "org1", "proj1", 9); err != nil {
		t.Fatalf("Unhold: %v", err)
	}
	if issueHasAll(issues.labelsOf(9), []string{taskmeta.LabelHold}) {
		t.Errorf("expected aep:hold removed, got %v", issues.labelsOf(9))
	}
	select {
	case <-disp.signal:
	case <-time.After(2 * time.Second):
		t.Fatal("Unhold did not trigger Reevaluate")
	}
}

// ---- issues.* webhook events -----------------------------------------------

func newEvents(issues *fakeIssues, disp *fakeDispatcher) *WebhookEvents {
	return NewWebhookEvents(issues, fakeRepoLocator{}, disp, "aep-platform[bot]")
}

func issuesLabeledPayload(number int, label, sender string, extraLabels ...string) []byte {
	labels := append([]string{taskmeta.LabelMarker, taskmeta.LabelCoding}, extraLabels...)
	labelJSON := ""
	for i, l := range labels {
		if i > 0 {
			labelJSON += ","
		}
		labelJSON += fmt.Sprintf(`{"name":%q}`, l)
	}
	return []byte(fmt.Sprintf(`{
		"action":"labeled",
		"issue":{"number":%d,"state":"open","title":"t","body":"b","labels":[%s]},
		"label":{"name":%q},
		"repository":{"full_name":"o/r"},
		"sender":{"login":%q}
	}`, number, labelJSON, label, sender))
}

func TestEvents_ExternalExecuteLabel_Dispatches(t *testing.T) {
	disp := newFakeDispatcher()
	e := newEvents(newFakeIssues(), disp)

	if err := e.OnLabeled(context.Background(), "issues", "labeled",
		issuesLabeledPayload(11, taskmeta.LabelExecute, "some-human")); err != nil {
		t.Fatalf("OnLabeled: %v", err)
	}
	if got := disp.executed(); len(got) != 1 || got[0] != 11 {
		t.Errorf("expected external execute to dispatch issue 11, got %v", got)
	}
}

func TestEvents_EchoSuppression_DropsPlatformStamp(t *testing.T) {
	disp := newFakeDispatcher()
	e := newEvents(newFakeIssues(), disp)

	if err := e.OnLabeled(context.Background(), "issues", "labeled",
		issuesLabeledPayload(11, taskmeta.LabelExecute, "aep-platform[bot]")); err != nil {
		t.Fatalf("OnLabeled: %v", err)
	}
	if got := disp.executed(); len(got) != 0 {
		t.Errorf("platform-stamped execute must be dropped (echo), got %v", got)
	}
}

func TestEvents_Unlabeled_Hold_Reevaluates(t *testing.T) {
	disp := newFakeDispatcher()
	e := newEvents(newFakeIssues(), disp)
	payload := []byte(fmt.Sprintf(`{"action":"unlabeled","issue":{"number":9,"state":"open"},"label":{"name":%q},"repository":{"full_name":"o/r"},"sender":{"login":"human"}}`, taskmeta.LabelHold))

	if err := e.OnUnlabeled(context.Background(), "issues", "unlabeled", payload); err != nil {
		t.Fatalf("OnUnlabeled: %v", err)
	}
	if disp.reevaluated() != 1 {
		t.Errorf("expected Reevaluate on hold release, got %d", disp.reevaluated())
	}
}

func TestEvents_BlockRepair_OnlyWhenDiffers(t *testing.T) {
	// A canonical body must NOT be rewritten (echo-suppression convergence).
	block := taskmeta.Block{Component: "order-service", Origin: taskmeta.OriginSpecPlan, DesignTag: "design-v1"}
	canonical := taskmeta.ComposeBody(block, taskmeta.Human{Rationale: "r"})

	issues := newFakeIssues()
	issues.seed(gitrepoIssue(20, "order-service", "design-v1"))
	e := newEvents(issues, newFakeDispatcher())

	editedPayload := func(body string) []byte {
		return []byte(fmt.Sprintf(`{
			"action":"edited",
			"issue":{"number":20,"state":"open","title":"t","body":%q,"labels":[{"name":%q},{"name":%q}]},
			"repository":{"full_name":"o/r"},
			"sender":{"login":"human"}
		}`, body, taskmeta.LabelMarker, taskmeta.LabelCoding))
	}

	// Canonical body → no edit written.
	before := issues.bodyOf(20)
	if err := e.OnOpenedOrEdited(context.Background(), "issues", "edited", editedPayload(canonical)); err != nil {
		t.Fatalf("OnOpenedOrEdited canonical: %v", err)
	}
	// A non-canonical (block-not-at-top) body → repaired to canonical.
	messy := "some human preamble\n\n" + canonical
	if err := e.OnOpenedOrEdited(context.Background(), "issues", "edited", editedPayload(messy)); err != nil {
		t.Fatalf("OnOpenedOrEdited messy: %v", err)
	}
	after := issues.bodyOf(20)
	if after == before {
		t.Errorf("expected a repair write for a non-canonical body")
	}
	if _, _, err := taskmeta.ParseBody(after); err != nil {
		t.Errorf("repaired body must still parse: %v", err)
	}
}

func TestEvents_MangledBlock_FlagsAttention(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(gitrepoIssue(21, "order-service", "design-v1"))
	e := newEvents(issues, newFakeDispatcher())
	// A block marker with an unparseable payload (no component/operation).
	mangled := "<!-- aep:task/v1\ngarbage line without colon\n-->\n"
	payload := []byte(fmt.Sprintf(`{"action":"edited","issue":{"number":21,"state":"open","title":"t","body":%q,"labels":[{"name":%q},{"name":%q}]},"repository":{"full_name":"o/r"},"sender":{"login":"human"}}`, mangled, taskmeta.LabelMarker, taskmeta.LabelCoding))

	if err := e.OnOpenedOrEdited(context.Background(), "issues", "edited", payload); err != nil {
		t.Fatalf("OnOpenedOrEdited: %v", err)
	}
	if !issueHasAll(issues.labelsOf(21), []string{taskmeta.LabelAttention}) {
		t.Errorf("expected aep:attention for a mangled block, got %v", issues.labelsOf(21))
	}
}
