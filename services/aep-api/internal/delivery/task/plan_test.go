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
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// SERVICE tier for the plan path over the in-memory pod (aestudiotest.Fake):
// the Plan turn runs in the org's AE Studio pod, aep-api states what to plan
// and mints what the pod's task-op lines describe.

type planVersions struct {
	specTag string
	scope   spec.BuildScope
}

func (p planVersions) ListSpecVersionTags(context.Context, string, string) (*spec.TagList, error) {
	if p.specTag == "" {
		return &spec.TagList{}, nil
	}
	return &spec.TagList{Tags: []string{p.specTag}, Latest: p.specTag}, nil
}

func (p planVersions) BuildScopeAtTag(context.Context, string, string, string) (spec.BuildScope, error) {
	return p.scope, nil
}

// planRig is a plan service over the fake pod, for project proj1 of org1
// whose repository is github.com/acme/widgets.
type planRig struct {
	pod    *aestudiotest.Fake
	issues *fakeIssues
	svc    *PlanService
}

func newPlanRig(t *testing.T, versions planVersions) *planRig {
	t.Helper()
	pod := aestudiotest.New()
	issues := newFakeIssues()
	row := &sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "proj1", RepoURL: "https://github.com/acme/widgets", Status: "ready"}
	svc := NewPlanService(fakeRepos{repo: row}, versions, pod, issues, issues.writer())
	return &planRig{pod: pod, issues: issues, svc: svc}
}

// plan plans into milestone 7 and returns the one turn the pod saw.
func (r *planRig) plan(t *testing.T) aestudiotest.TurnCall {
	t.Helper()
	if err := r.svc.PlanIntoMilestone(context.Background(), "org1", "proj1", 7); err != nil {
		t.Fatalf("PlanIntoMilestone: %v", err)
	}
	calls := r.pod.TurnCalls()
	if len(calls) != 1 {
		t.Fatalf("pod saw %d turns, want 1", len(calls))
	}
	return calls[0]
}

// The Plan turn starts in the project's pod: kind plan, the project named,
// a fresh UUID turn id per plan, and no credit (no run records a requester).
func TestPlanMilestone_StartsAPlanTurnInThePod(t *testing.T) {
	r := newPlanRig(t, planVersions{specTag: "v1"})
	call := r.plan(t)

	if call.Ref != (aestudiotools.RepoRef{Org: "org1", Owner: "acme", Repo: "widgets", DefaultBranch: "main"}) {
		t.Errorf("ref = %+v, want the project's repository in org1", call.Ref)
	}
	req := call.Request
	if req.Kind != aestudiotools.TurnKindPlan || req.Project != "proj1" {
		t.Errorf("request = %+v, want a plan turn for proj1", req)
	}
	if _, err := uuid.Parse(req.TurnID); err != nil {
		t.Errorf("turn id %q is not a UUID", req.TurnID)
	}
	if req.Credit != (aestudiotools.Credit{}) {
		t.Errorf("credit = %+v, want none (the run records no requester)", req.Credit)
	}
	if req.Scope != nil || len(req.TaskContext) != 0 {
		t.Errorf("scope/context = %+v/%+v, want none for a scope-less first pass", req.Scope, req.TaskContext)
	}

	// A second plan is a second turn: plan turn ids are never reused.
	if err := r.svc.PlanIntoMilestone(context.Background(), "org1", "proj1", 7); err != nil {
		t.Fatal(err)
	}
	if calls := r.pod.TurnCalls(); calls[0].Request.TurnID == calls[1].Request.TurnID {
		t.Error("two plans shared a turn id")
	}
}

// The pod's task-op lines are the plan: two planTask results mint two issues
// in the milestone.
func TestPlanMilestone_TwoTaskOpsMintTwoIssues(t *testing.T) {
	r := newPlanRig(t, planVersions{specTag: "v1"})
	r.pod.ScriptTurn(
		taskOp(planOK("user-service", "Implement user-service", nil)),
		keepAlive,
		taskOp(planOK("order-service", "Implement order-service", []string{"user-service"})),
	)
	r.plan(t)

	if len(r.issues.created) != 2 {
		t.Fatalf("created %d issues, want 2", len(r.issues.created))
	}
	for _, c := range r.issues.created {
		if c.Milestone == nil || *c.Milestone != 7 {
			t.Errorf("%q: milestone = %v, want 7", c.Title, c.Milestone)
		}
	}
}

// Build-first: no version, no plan, and nothing reaches the pod.
func TestPlanMilestone_RequiresAVersion(t *testing.T) {
	r := newPlanRig(t, planVersions{})
	if err := r.svc.PlanIntoMilestone(context.Background(), "org1", "proj1", 7); !errors.Is(err, ErrNoSpecVersion) {
		t.Fatalf("err = %v, want ErrNoSpecVersion", err)
	}
	if n := len(r.pod.TurnCalls()); n != 0 {
		t.Fatalf("pod saw %d turns, want none", n)
	}
}

// One turn per project is the pod's lock now: a different turn running is
// ErrTurnInProgress, which the planning activity retries.
func TestPlanMilestone_ATurnInProgressSurfaces(t *testing.T) {
	r := newPlanRig(t, planVersions{specTag: "v1"})
	r.pod.FailOp(aestudiotest.OpStartTurn, fmt.Errorf("%w (active turn x)", aestudiotools.ErrTurnInProgress))

	if err := r.svc.PlanIntoMilestone(context.Background(), "org1", "proj1", 7); !errors.Is(err, aestudiotools.ErrTurnInProgress) {
		t.Fatalf("err = %v, want ErrTurnInProgress", err)
	}
}

// A turn the pod ends failed is a failed plan.
func TestPlanMilestone_AFailedTurnIsAnError(t *testing.T) {
	r := newPlanRig(t, planVersions{specTag: "v1"})
	r.pod.ScriptTurn(aestudiotools.TurnEvent{Type: aestudiotools.EventResult, Status: "failed", Code: "agent-error"})

	if err := r.svc.PlanIntoMilestone(context.Background(), "org1", "proj1", 7); err == nil {
		t.Fatal("a failed plan turn returned no error")
	}
}

// A milestone plan reads its context from MILESTONE MEMBERSHIP, not a label
// query: the version's own issues are the additive-only dedupe set (§6 plans
// fresh from the new spec, so nothing carries over from the previous version),
// and the version's gate and validation issues are not the planner's to touch.
func TestPlanMilestone_ContextIsTheMilestonesOwnWork(t *testing.T) {
	r := newPlanRig(t, planVersions{specTag: "v2"})

	// The version's own milestone: one Task already planned (a re-plan or a
	// crash re-run), one gate, one ledger-only human issue, and the version's
	// validation task — which is ARMED like every other issue an agent works, so
	// only the KIND test keeps it out of the planner's context set.
	r.issues.seedInMilestone(sourcecontrol.IssueInfo{
		Number: 201, Title: "Implement hello-world-api", Body: "Build the API.",
		State: "open", Labels: []string{delivery.LabelAgentWork, delivery.KindDevelopment},
	}, 7)
	r.issues.seedInMilestone(sourcecontrol.IssueInfo{
		Number: 202, Title: "Provision orders-db", Body: "Waiting on the drawer.",
		State: "open", Labels: []string{delivery.KindProvision},
	}, 7)
	r.issues.seedInMilestone(sourcecontrol.IssueInfo{
		Number: 203, Title: "Flaky checkout", Body: "Sometimes 500s.",
		State: "open", Labels: nil,
	}, 7)
	r.issues.seedInMilestone(sourcecontrol.IssueInfo{
		Number: 204, Title: "Validate hello-world against its validation criteria",
		Body:  "Author e2e tests.",
		State: "open", Labels: []string{delivery.LabelAgentWork, delivery.KindValidation},
	}, 7)
	// A bug the platform minted into this version IS the planner's context: a
	// re-plan must be able to see it and must not re-propose it under a new
	// title.
	r.issues.seedInMilestone(sourcecontrol.IssueInfo{
		Number: 205, Title: "Fix the failing build for hello-world-api", Body: "It went red.",
		State: "open", Labels: []string{delivery.LabelAgentWork, delivery.KindBug, delivery.SrcBuild},
	}, 7)
	// A Task of the PREVIOUS version, in another milestone: superseded, and
	// therefore not context for this one.
	r.issues.seedInMilestone(sourcecontrol.IssueInfo{
		Number: 199, Title: "Implement legacy-thing", Body: "Old.",
		State: "open", Labels: []string{delivery.LabelAgentWork, delivery.KindDevelopment},
	}, 6)

	call := r.plan(t)
	paths := make([]string, 0, len(call.Request.TaskContext))
	for _, f := range call.Request.TaskContext {
		paths = append(paths, f.Path)
	}
	if !slices.Equal(paths, []string{"tasks/201.md", "tasks/205.md"}) {
		t.Errorf("plan context = %v, want exactly the milestone's own agent work, in path order", paths)
	}
}

// The plan path settles the run it armed on a failed plan, so a write the tap
// could not land has to surface as an ERROR rather than a warning.
func TestPlanMilestone_WriteFailureIsAnError(t *testing.T) {
	r := newPlanRig(t, planVersions{specTag: "v2"})
	r.issues.failCreate = true
	r.pod.ScriptTurn(taskOp(planOK("hello-world-api", "Implement hello-world-api", nil)))

	err := r.svc.PlanIntoMilestone(context.Background(), "org1", "proj1", 7)
	if err == nil {
		t.Fatal("a plan whose issue writes all failed must return an error")
	}
	if !strings.Contains(err.Error(), "milestone 7") {
		t.Errorf("error = %v, want it to name the milestone", err)
	}
}

// The scope-native plan (#369): the turn carries the platform-computed
// milestone scope with per-story coverage, and a created Task is stamped with
// its component's claimed stories — zero LLM discretion on either.
func TestPlanMilestone_DeltaScopeAndStamp(t *testing.T) {
	r := newPlanRig(t, planVersions{specTag: "v2", scope: spec.BuildScope{
		Tag: "v2", InScope: []int{1, 2},
		StoryTitles:      map[int]string{1: "As a user, I want A.", 2: "As a user, I want B."},
		ComponentStories: map[string][]int{"svc": {1, 2}},
	}})
	r.pod.ScriptTurn(taskOp(`{"ok":true,"op":"plan","component":"svc","title":"Build svc","dependsOn":[],"origin":"spec-plan","rationale":"core"}`))

	scope := r.plan(t).Request.Scope
	if scope == nil {
		t.Fatalf("turn carries no milestone scope")
	}
	if scope.Tag != "v2" {
		t.Errorf("scope tag = %q, want v2", scope.Tag)
	}
	if want := (aestudiotools.PlanStory{Number: 1, Title: "As a user, I want A.", Covered: false}); !slices.Contains(scope.Stories, want) {
		t.Errorf("scope missing the uncovered story: %+v", scope.Stories)
	}

	created := r.issues.created
	if len(created) != 1 {
		t.Fatalf("created %d issues, want 1", len(created))
	}
	if got := delivery.ParseServesStories(created[0].Body); fmt.Sprint(got) != "[1 2]" {
		t.Errorf("stamped stories = %v, want [1 2] (body: %q)", got, created[0].Body)
	}
}
