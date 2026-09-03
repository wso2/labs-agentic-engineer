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
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
	"github.com/wso2/aep/aep-api/internal/platform/gittest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// contextPaths lists the task-context paths a dispatched plan turn carried.
func contextPaths(turn agentsvc.TurnSpec) []string {
	out := make([]string, 0, len(turn.TaskContext))
	for _, f := range turn.TaskContext {
		out = append(out, f.Path)
	}
	return out
}

func (p planVersions) BuildScopeAtTag(context.Context, string, string, string) (spec.BuildScope, error) {
	return p.scope, nil
}

type planVersions struct {
	specTag string
	scope   spec.BuildScope
}

func (p planVersions) ListRequirementsVersions(context.Context, string, string) ([]spec.RequirementsVersionInfo, error) {
	return []spec.RequirementsVersionInfo{{Tag: p.specTag}}, nil
}
func (p planVersions) LatestSpecTag(context.Context, string, string) string {
	return p.specTag
}
func (planVersions) GetRequirementsAtTag(context.Context, string, string, string) (map[string]string, error) {
	return map[string]string{}, nil
}

// capturingTurn records the TurnRequest and replays a canned upstream stream
// (an immediate [DONE] unless the test scripts frames).
type capturingTurn struct {
	req    *agentsvc.TurnRequest
	script string
}

func (c *capturingTurn) Turn(_ context.Context, _, _, _ string, req agentsvc.TurnRequest) (io.ReadCloser, error) {
	c.req = &req
	body := c.script
	if body == "" {
		body = "data: [DONE]\n\n"
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

type nilResolver struct{}

func (nilResolver) Resolve(context.Context, string) (secrets.Credential, error) {
	return nil, nil
}

// planRig is the workspace-shaped plan harness: a real engine over real
// file:// origins for the project repo and the org _skills repo.
type planRig struct {
	fx           *workspacetest.Fixture
	skillsOrigin *gittest.Remote
	turn         *capturingTurn
	issues       *fakeIssues
	svc          *PlanService
}

// rigScope is the phase scope newPlanRig's version reader serves — zero by
// default (legacy scope-less planning); a test that needs a phase sets it and
// restores it.
var rigScope spec.BuildScope

func newPlanRig(t *testing.T, seed map[string]string, specTag string) *planRig {
	t.Helper()
	fx := workspacetest.New(t, seed)
	skillsOrigin := gittest.NewRemote(t, gittest.WithSeed(map[string]string{
		"skills/task-planning/SKILL.md": "---\nname: task-planning\ndescription: plan tasks\nmetadata:\n  aep:\n    kind: platform\n---\n# Task planning",
	}, "seed skills"))
	repoRow := &sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "proj1", RepoURL: fx.Origin.URL(),
		DefaultBranch: "main", RepoSlug: workspacetest.DefaultSlug, Status: "ready"}
	skillsRow := &sourcecontrol.GitRepository{OrgID: "org1", ProjectID: spec.SkillsRepoSentinelProjectID,
		RepoURL: skillsOrigin.URL(), DefaultBranch: "main", RepoSlug: "org-skills", Status: "ready"}

	turn := &capturingTurn{}
	issues := newFakeIssues()
	svc := NewPlanService(
		fakeRepos{repo: repoRow},
		planVersions{specTag: specTag, scope: rigScope},
		sourcecontrol.NewGitOpsService(nilResolver{}, fx.Engine),
		func(context.Context, string) (string, error) { return "sk-test", nil },
		turn,
		issues,
		issues.writer(),
		fx.Engine,
		func(context.Context, string) (*sourcecontrol.GitRepository, error) { return skillsRow, nil },
	)
	return &planRig{fx: fx, skillsOrigin: skillsOrigin, turn: turn, issues: issues, svc: svc}
}

// start plans into milestone 7 and returns the dispatched turn request.
func (r *planRig) start(t *testing.T) *agentsvc.TurnRequest {
	t.Helper()
	if err := r.svc.PlanIntoMilestone(context.Background(), "org1", "proj1", 7); err != nil {
		t.Fatalf("PlanIntoMilestone: %v", err)
	}
	if r.turn.req == nil {
		t.Fatal("turn was never started")
	}
	return r.turn.req
}

// TestPlanIntoMilestone_DispatchesWorkspaceShape pins the plan dispatch: no
// inline files or skills — a WorkspaceRef naming the repo snapshot at HEAD and
// the _skills snapshot, toolset task-plan, snapshots materialized on the mount.
func TestPlanIntoMilestone_DispatchesWorkspaceShape(t *testing.T) {
	r := newPlanRig(t, map[string]string{
		"specs/design/design.md":                              "# design",
		"specs/design/components/hello-world-api/design.json": `{"name":"hello-world-api"}`,
		"specs/requirements/prd.md":                           "# reqs",
	}, "v1")
	req := r.start(t)

	// The tool set is DERIVED by the agents service from kind:"plan" — the BFF
	// states what the turn is for and stops there.
	if req.Turn.Kind != agentsvc.TurnKindPlan {
		t.Errorf("turn kind = %q, want %q", req.Turn.Kind, agentsvc.TurnKindPlan)
	}
	ws := req.Workspace
	if ws.Ref != r.fx.Origin.HeadSHA(t) {
		t.Errorf("workspace ref = %q, want origin head %q", ws.Ref, r.fx.Origin.HeadSHA(t))
	}
	if ws.SkillsRef != r.skillsOrigin.HeadSHA(t) {
		t.Errorf("skillsRef = %q, want skills head %q", ws.SkillsRef, r.skillsOrigin.HeadSHA(t))
	}
	if ws.RepoSlug != workspacetest.DefaultSlug {
		t.Errorf("repoSlug = %q", ws.RepoSlug)
	}
	if !strings.HasPrefix(ws.ConversationID, "org_org1--proj_proj1--task-plan--") {
		t.Errorf("conversationId = %q", ws.ConversationID)
	}
	if ws.TurnID == "" {
		t.Error("turnId must be set")
	}
	// Both snapshots are materialized before dispatch (agents reads them).
	repoSnap, err := gitfs.SnapshotDir(r.fx.Engine.Root(),
		gitfs.RepoRef{OrgID: "org1", ProjectID: "proj1", RepoSlug: workspacetest.DefaultSlug}, ws.Ref)
	if err != nil {
		t.Fatalf("repo snapshot dir: %v", err)
	}
	skillsSnap, err := gitfs.SnapshotDir(r.fx.Engine.Root(),
		gitfs.RepoRef{OrgID: "org1", ProjectID: spec.SkillsRepoSentinelProjectID, RepoSlug: "org-skills"}, ws.SkillsRef)
	if err != nil {
		t.Fatalf("skills snapshot dir: %v", err)
	}
	for _, dir := range []string{repoSnap, skillsSnap} {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			t.Errorf("snapshot dir not materialized: %s (%v)", dir, err)
		}
	}
	if _, err := os.Stat(skillsSnap + "/skills/task-planning/SKILL.md"); err != nil {
		t.Errorf("task-planning flow skill missing from skills snapshot: %v", err)
	}
	if len(req.Turn.TaskContext) != 0 {
		t.Errorf("no existing tasks — the turn must carry no task context: %+v", req.Turn.TaskContext)
	}
}

// A stale _skills row over a gone repo must surface as the typed
// ErrSkillsRepoUnavailable (which the plan path settles the run on), never an
// anonymous wrap — and the turn must not start.
func TestPlanIntoMilestone_SkillsRepoGone_TypedError(t *testing.T) {
	fx := workspacetest.New(t, map[string]string{
		"specs/design/design.md":                              "# design",
		"specs/design/components/hello-world-api/design.json": `{"name":"hello-world-api"}`,
		"specs/requirements/prd.md":                           "# reqs",
	})
	repoRow := &sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "proj1", RepoURL: fx.Origin.URL(),
		DefaultBranch: "main", RepoSlug: workspacetest.DefaultSlug, Status: "ready"}
	staleSkills := &sourcecontrol.GitRepository{OrgID: "org1", ProjectID: spec.SkillsRepoSentinelProjectID,
		RepoURL: "file:///nonexistent/skills-repo-gone.git", DefaultBranch: "main", RepoSlug: "org-skills", Status: "ready"}

	turn := &capturingTurn{}
	planIssues := newFakeIssues()
	svc := NewPlanService(
		fakeRepos{repo: repoRow},
		planVersions{specTag: "v1"},
		sourcecontrol.NewGitOpsService(nilResolver{}, fx.Engine),
		func(context.Context, string) (string, error) { return "sk-test", nil },
		turn,
		planIssues,
		planIssues.writer(),
		fx.Engine,
		func(context.Context, string) (*sourcecontrol.GitRepository, error) { return staleSkills, nil },
	)

	err := svc.PlanIntoMilestone(context.Background(), "org1", "proj1", 7)
	if !errors.Is(err, ErrSkillsRepoUnavailable) {
		t.Fatalf("PlanIntoMilestone error = %v, want ErrSkillsRepoUnavailable", err)
	}
	if turn.req != nil {
		t.Error("plan turn dispatched despite an unavailable skills repo")
	}
}

// A milestone plan reads its context from MILESTONE MEMBERSHIP, not a label
// query: the version's own issues are the additive-only dedupe set (§6 plans
// fresh from the new spec, so nothing carries over from the previous version),
// and the version's gate and validation issues are not the planner's to touch.
func TestPlanIntoMilestone_ContextIsTheMilestonesOwnWork(t *testing.T) {
	r := newPlanRig(t, map[string]string{"specs/design/design.md": "# design\n"}, "v2")

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

	if err := r.svc.PlanIntoMilestone(context.Background(), "org1", "proj1", 7); err != nil {
		t.Fatalf("PlanIntoMilestone: %v", err)
	}
	paths := contextPaths(r.turn.req.Turn)
	for _, want := range []string{"tasks/201.md", "tasks/205.md"} {
		if !slices.Contains(paths, want) {
			t.Errorf("%s is the milestone's own work and is missing from the plan context: %v", want, paths)
		}
	}
	for _, leaked := range []string{"tasks/202.md", "tasks/203.md", "tasks/204.md", "tasks/199.md"} {
		if slices.Contains(paths, leaked) {
			t.Errorf("%s leaked into the plan context — only the milestone's agent work is context: %v", leaked, paths)
		}
	}
}

// The plan path settles the run it armed on a failed plan, so a write the tap
// could not land has to surface as an ERROR rather than a warning.
func TestPlanIntoMilestone_WriteFailureIsAnError(t *testing.T) {
	r := newPlanRig(t, map[string]string{"specs/design/design.md": "# design\n"}, "v2")
	r.issues.failCreate = true
	r.turn.script = "data: {\"type\":\"tool-result\",\"output\":" +
		`{"ok":true,"op":"plan","component":"hello-world-api","title":"Implement hello-world-api","dependsOn":[],"origin":"spec-plan","rationale":"go"}` +
		"}\n\ndata: [DONE]\n\n"

	err := r.svc.PlanIntoMilestone(context.Background(), "org1", "proj1", 7)
	if err == nil {
		t.Fatal("a plan whose issue writes all failed must return an error")
	}
	if !strings.Contains(err.Error(), "milestone 7") {
		t.Errorf("error = %v, want it to name the milestone", err)
	}
}

// TestPlanIntoMilestone_DeltaScopeAndStamp pins the scope-native plan (#369):
// the instruction carries the platform-computed milestone scope with
// per-story coverage, and a created Task is stamped with its component's
// claimed stories — zero LLM discretion on either.
func TestPlanIntoMilestone_DeltaScopeAndStamp(t *testing.T) {
	rigScope = spec.BuildScope{
		Tag: "v2", InScope: []int{1, 2},
		StoryTitles:      map[int]string{1: "As a user, I want A.", 2: "As a user, I want B."},
		ComponentStories: map[string][]int{"svc": {1, 2}},
	}
	defer func() { rigScope = spec.BuildScope{} }()

	r := newPlanRig(t, map[string]string{"specs/design/design.md": "# d\n"}, "v2")
	r.turn.script = "data: {\"type\":\"tool-result\",\"output\":" +
		`{"ok":true,"op":"plan","component":"svc","title":"Build svc","dependsOn":[],"origin":"spec-plan","rationale":"core"}` +
		"}\n\ndata: [DONE]\n\n"
	if err := r.svc.PlanIntoMilestone(context.Background(), "org1", "proj1", 7); err != nil {
		t.Fatalf("PlanIntoMilestone: %v", err)
	}

	scope := r.turn.req.Turn.Scope
	if scope == nil {
		t.Fatalf("turn carries no milestone scope")
	}
	if scope.Tag != "v2" {
		t.Errorf("scope tag = %q, want v2", scope.Tag)
	}
	want := agentsvc.PlanStory{Number: 1, Title: "As a user, I want A.", Covered: false}
	if !slices.Contains(scope.Stories, want) {
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
