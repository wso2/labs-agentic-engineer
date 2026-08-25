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
	"testing"
	"time"

	"strings"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// ---- issue body prose ------------------------------------------------------

// A Task body is a brief for the agent: what to build, where, and which issues
// come first. Nothing parses it, so the test pins what a READER needs, not a
// round-trip.
func TestComposeTaskBody_ProseWithResolvedAndUnresolvedDependencies(t *testing.T) {
	planned := plannedTask{
		Component: "order-service",
		AppPath:   "src/order-service",
		DependsOn: []string{"user-service", "cart-service"},
		Rationale: "core of the plan",
		Body:      "## Scope\nWrite it.",
	}
	body := composeTaskBody(planned, func(component string) (int, bool) {
		if component == "user-service" {
			return 12, true
		}
		return 0, false
	})
	for _, want := range []string{
		"core of the plan",
		"**Component:** `order-service`",
		"**App Path:** `src/order-service`",
		"Depends on #12",
		"Depends on the `cart-service` task",
		"## Scope",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "aep:task/v1") {
		t.Errorf("a Task body must carry no machine block:\n%s", body)
	}
}

// An empty plan renders an empty body rather than a skeleton of blank headings.
func TestComposeTaskBody_EmptyFactsRenderNothing(t *testing.T) {
	if got := composeTaskBody(plannedTask{}, func(string) (int, bool) { return 0, false }); got != "" {
		t.Errorf("empty plannedTask rendered %q, want an empty body", got)
	}
}

// ---- reads -----------------------------------------------------------------

func newReads(issues *fakeIssues, execs *fakeExecReader, runs fakeMilestones) *Reads {
	return NewReads(issues, fakeRepos{repo: defaultRepo()}, execs, runs)
}

// The derived status is the issue's own state and nothing else: open reads
// pending, closed reads merged. Both strings are members of the retired
// ten-value set, because the console consumes derivedStatus through an untyped
// contract field and a chip keyed on an unknown string renders as nothing.
func TestReads_DerivedStatusIsIssueStateAlone(t *testing.T) {
	issues := newFakeIssues()
	open := agentIssue(1, "Implement user-service", "brief")
	closed := agentIssue(2, "Implement order-service", "brief")
	closed.State = "closed"
	issues.seed(open).seed(closed)

	views, err := newReads(issues, newFakeExecReader(), nil).
		ListByTag(context.Background(), "org1", "proj1", "all", "", false)
	if err != nil {
		t.Fatalf("ListByTag: %v", err)
	}
	byNum := map[int]delivery.TaskView{}
	for _, v := range views {
		byNum[v.IssueNumber] = v
	}
	if got := byNum[1].DerivedStatus; got != delivery.DerivedStatusPending {
		t.Errorf("open issue status = %q, want %q", got, delivery.DerivedStatusPending)
	}
	if got := byNum[2].DerivedStatus; got != delivery.DerivedStatusMerged {
		t.Errorf("closed issue status = %q, want %q", got, delivery.DerivedStatusMerged)
	}
}

// The populations an UNTAGGED list is made of: armed agent work and dispatch
// gates. A bare human issue is invisible here by construction — the untagged
// read is two label queries, and a ledger issue is defined by carrying no label
// to query on.
//
// The validation task is the case that changed. It is ARMED now, so the `aep`
// query returns it where it used to be invisible to that query entirely; the
// only thing keeping it off the list is the KIND test in buildView. A read that
// still tested the arming label would surface a phase of the run as a Task.
//
// Every row also carries its raw kind, which is what lets the console tell a
// defect from planned work — `executorClass` is deliberately `coding` for both.
func TestReads_ListPopulations(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(agentIssue(1, "Implement user-service", "brief"))
	issues.seed(gateIssue(2, "postgres"))
	issues.seed(validationIssue(3))
	issues.seed(ledgerIssue(4, "Login is slow"))
	issues.seed(bugIssue(5, "Fix the failing build for user-service"))

	views, err := newReads(issues, newFakeExecReader(), nil).
		ListByTag(context.Background(), "org1", "proj1", "all", "", false)
	if err != nil {
		t.Fatalf("ListByTag: %v", err)
	}
	classes := map[int]string{}
	kinds := map[int]string{}
	for _, v := range views {
		classes[v.IssueNumber] = v.ExecutorClass
		kinds[v.IssueNumber] = v.Kind
	}
	if len(views) != 3 {
		t.Fatalf("want the Task, the bug and the gate, got %d: %+v", len(views), classes)
	}
	if classes[1] != "coding" || classes[5] != "coding" {
		t.Errorf("planned work and a bug are both dispatched the same way; classes = %+v", classes)
	}
	if classes[2] != "provision" {
		t.Errorf("gate class = %q, want provision", classes[2])
	}
	if _, listed := classes[3]; listed {
		t.Errorf("the validation task must stay hidden even though it is armed, got %+v", classes)
	}
	if kinds[1] != delivery.KindDevelopment || kinds[5] != delivery.KindBug || kinds[2] != delivery.KindProvision {
		t.Errorf("kinds = %+v, want 1:development 5:bug 2:provision", kinds)
	}
}

// A version's LEDGER: bare human issues that joined the milestone carrying none
// of the platform's labels. They are never worked and never stall settle, but
// they belong to the version, so a milestone-scoped read returns them — marked
// `ledger` so the console can section them apart from agent work rather than
// mistaking one for a task. The validation task stays hidden even here — the
// milestone-scoped read returns every member issue, so the KIND test is the only
// thing excluding it.
func TestReads_ListByTag_IncludesTheLedger(t *testing.T) {
	issues := newFakeIssues()
	issues.seedInMilestone(agentIssue(1, "Implement user-service", "brief"), 7)
	issues.seedInMilestone(gateIssue(2, "postgres"), 7)
	issues.seedInMilestone(validationIssue(3), 7)
	issues.seedInMilestone(ledgerIssue(4, "Login is slow"), 7)
	runs := fakeMilestones{"v3": 7}

	views, err := newReads(issues, newFakeExecReader(), runs).
		ListByTag(context.Background(), "org1", "proj1", "all", "v3", false)
	if err != nil {
		t.Fatalf("ListByTag(v3): %v", err)
	}
	kinds := map[int]string{}
	for _, v := range views {
		kinds[v.IssueNumber] = v.ExecutorClass
	}
	if len(views) != 3 {
		t.Fatalf("want the Task, its gate and the ledger issue, got %d: %+v", len(views), kinds)
	}
	if kinds[1] != "coding" || kinds[2] != "provision" || kinds[4] != "ledger" {
		t.Errorf("kinds = %+v, want 1:coding 2:provision 4:ledger", kinds)
	}
	if _, hidden := kinds[3]; hidden {
		t.Errorf("the validation issue must stay hidden from the list, got %+v", kinds)
	}
}

// `?tag=` is MILESTONE MEMBERSHIP resolved through the run rows — never a title
// match against GitHub. A tag the platform never built answers empty rather
// than erroring: that version has no Tasks by definition.
func TestReads_ListByTag_IsMilestoneMembership(t *testing.T) {
	issues := newFakeIssues()
	issues.seedInMilestone(agentIssue(1, "Implement user-service", "brief"), 7)
	issues.seedInMilestone(agentIssue(2, "Implement order-service", "brief"), 7)
	issues.seedInMilestone(agentIssue(3, "Implement cart-service", "brief"), 6) // an earlier version
	runs := fakeMilestones{"v3": 7, "v2": 6}

	views, err := newReads(issues, newFakeExecReader(), runs).
		ListByTag(context.Background(), "org1", "proj1", "all", "v3", false)
	if err != nil {
		t.Fatalf("ListByTag(v3): %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("want v3's two Tasks, got %d: %+v", len(views), views)
	}
	for _, v := range views {
		if v.Lineage.SpecTag != "v3" {
			t.Errorf("view %d specTag = %q, want v3", v.IssueNumber, v.Lineage.SpecTag)
		}
	}

	unknown, err := newReads(issues, newFakeExecReader(), runs).
		ListByTag(context.Background(), "org1", "proj1", "all", "v99", false)
	if err != nil {
		t.Fatalf("ListByTag(v99): %v", err)
	}
	if len(unknown) != 0 {
		t.Fatalf("a version the platform never built must list no Tasks, got %d", len(unknown))
	}

	all, err := newReads(issues, newFakeExecReader(), runs).
		ListByTag(context.Background(), "org1", "proj1", "all", "", false)
	if err != nil {
		t.Fatalf("ListByTag(all): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("an empty tag must span every version, got %d", len(all))
	}
}

// A Task's body is the prose brief the planner wrote, carried through verbatim.
func TestReads_CarriesTheProseBody(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(agentIssue(1, "Implement user-service", "## Scope\n\nImplement the login endpoint."))

	views, err := newReads(issues, newFakeExecReader(), nil).
		ListByTag(context.Background(), "org1", "proj1", "open", "", false)
	if err != nil {
		t.Fatalf("ListByTag: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("expected 1 view, got %d", len(views))
	}
	if views[0].Body != "## Scope\n\nImplement the login endpoint." {
		t.Errorf("body = %q, want the prose brief verbatim", views[0].Body)
	}
	if views[0].Attention == nil {
		t.Error("attention must be a non-nil empty slice — it marshals as [] and the console maps over it")
	}
}

// Get answers for any issue reachable by number, including the ones the list
// hides, and carries the full execution history a provisioning gate still keeps.
func TestReads_Get_IncludesHistory(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(gateIssue(5, "postgres"))
	execs := newFakeExecReader()
	execs.put(5, delivery.Execution{ID: "a", Kind: string(taskmeta.KindProvision), Status: string(taskmeta.ExecFailed), CreatedAt: time.Now().Add(-time.Hour)})
	execs.put(5, delivery.Execution{ID: "b", Kind: string(taskmeta.KindProvision), Status: string(taskmeta.ExecSucceeded), CreatedAt: time.Now()})

	detail, err := newReads(issues, execs, nil).Get(context.Background(), "org1", "proj1", 5)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(detail.ExecutionHistory) != 2 {
		t.Errorf("expected 2 history rows, got %d", len(detail.ExecutionHistory))
	}
	if detail.Executions[string(taskmeta.KindProvision)].ID != "b" {
		t.Errorf("latest-per-kind must be the newest row, got %+v", detail.Executions)
	}
}

func TestReads_Get_NotFound(t *testing.T) {
	issues := newFakeIssues()
	_, err := newReads(issues, newFakeExecReader(), nil).Get(context.Background(), "org1", "proj1", 999)
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}
