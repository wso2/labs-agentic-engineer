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
	"io"
	"os"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/credentials"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
	"github.com/wso2/aep/aep-api/internal/platform/gittest"
	"github.com/wso2/aep/aep-api/models"
)

type planVersions struct{ designTag string }

func (p planVersions) ListRequirementsVersions(context.Context, string, string) ([]artifacts.RequirementsVersionInfo, error) {
	return []artifacts.RequirementsVersionInfo{{Tag: "requirements-v1"}}, nil
}
func (p planVersions) ListDesignVersions(context.Context, string, string) ([]artifacts.DesignVersionInfo, error) {
	return []artifacts.DesignVersionInfo{{Tag: p.designTag}}, nil
}
func (p planVersions) LatestDesignTag(context.Context, string, string) string {
	return p.designTag
}
func (planVersions) GetRequirementsAtTag(context.Context, string, string, string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (planVersions) GetDesignAtTag(context.Context, string, string, string) (map[string]string, error) {
	return map[string]string{}, nil
}

// capturingTurn records the TurnRequest and returns an already-finished stream.
type capturingTurn struct{ req *agentsvc.TurnRequest }

func (c *capturingTurn) Turn(_ context.Context, _, _, _ string, req agentsvc.TurnRequest) (io.ReadCloser, error) {
	c.req = &req
	return io.NopCloser(strings.NewReader("data: [DONE]\n\n")), nil
}

type nilResolver struct{}

func (nilResolver) Resolve(context.Context, string) (credentials.Credential, error) {
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

func newPlanRig(t *testing.T, seed map[string]string, designTag string) *planRig {
	t.Helper()
	fx := workspacetest.New(t, seed)
	skillsOrigin := gittest.NewRemote(t, gittest.WithSeed(map[string]string{
		"skills/task-planning/SKILL.md": "---\nname: task-planning\ndescription: plan tasks\nmetadata:\n  aep:\n    kind: platform\n---\n# Task planning",
	}, "seed skills"))
	repoRow := &models.GitRepository{OrgID: "org1", ProjectID: "proj1", RepoURL: fx.Origin.URL(),
		DefaultBranch: "main", RepoSlug: workspacetest.DefaultSlug, Status: "ready"}
	skillsRow := &models.GitRepository{OrgID: "org1", ProjectID: models.SkillsRepoSentinelProjectID,
		RepoURL: skillsOrigin.URL(), DefaultBranch: "main", RepoSlug: "org-skills", Status: "ready"}

	turn := &capturingTurn{}
	issues := newFakeIssues()
	svc := NewPlanService(
		fakeRepos{repo: repoRow},
		planVersions{designTag: designTag},
		gitrepo.NewGitOpsService(nilResolver{}, fx.Engine),
		func(context.Context, string) (string, error) { return "sk-test", nil },
		turn,
		issues,
		fx.Engine,
		func(context.Context, string) (*models.GitRepository, error) { return skillsRow, nil },
	)
	return &planRig{fx: fx, skillsOrigin: skillsOrigin, turn: turn, issues: issues, svc: svc}
}

func (r *planRig) start(t *testing.T) *agentsvc.TurnRequest {
	t.Helper()
	session, err := r.svc.StartPlan(context.Background(), "org1", "proj1")
	if err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	session.Stream(io.Discard, func() {})
	if r.turn.req == nil {
		t.Fatal("turn was never started")
	}
	return r.turn.req
}

// TestStartPlan_DispatchesWorkspaceShape pins the D9 plan dispatch: no inline
// files or skills — a WorkspaceRef naming the repo snapshot at HEAD and the
// _skills snapshot, toolset task-plan, snapshots materialized on the mount.
func TestStartPlan_DispatchesWorkspaceShape(t *testing.T) {
	r := newPlanRig(t, map[string]string{
		"specs/design/design.md":                              "# design",
		"specs/design/components/hello-world-api/design.json": `{"name":"hello-world-api"}`,
		"specs/requirements/requirements.md":                  "# reqs",
	}, "design-v1")
	req := r.start(t)

	if req.Toolset != "task-plan" {
		t.Errorf("toolset = %q, want task-plan", req.Toolset)
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
		gitfs.RepoRef{OrgID: "org1", ProjectID: models.SkillsRepoSentinelProjectID, RepoSlug: "org-skills"}, ws.SkillsRef)
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
	if !strings.HasPrefix(req.Instruction, planInstruction) {
		t.Errorf("instruction must start with the plan directive: %q", req.Instruction)
	}
	if strings.Contains(req.Instruction, "## Existing open Tasks") {
		t.Errorf("no existing tasks — instruction must carry no context section: %q", req.Instruction)
	}
}

// TestStartPlan_InstructionCarriesExistingTasksAndLineageDiff pins the context
// channel: existing open Tasks render as tasks/<n>.md sections appended to the
// instruction (they are platform state, not repo files, so they cannot ride in
// the snapshot), and an older lineage tag yields a Workspace.Diff-rendered
// section including real patch hunks.
func TestStartPlan_InstructionCarriesExistingTasksAndLineageDiff(t *testing.T) {
	r := newPlanRig(t, map[string]string{
		"specs/design/design.md": "# design v0\n",
	}, "design-v1")
	r.fx.Origin.Tag(t, "design-v0", "Design v0")
	r.fx.Origin.Seed(t, map[string]string{"specs/design/design.md": "# design v1 CHANGED\n"}, "design change")
	r.fx.Origin.Tag(t, "design-v1", "Design v1")

	block := taskmeta.Block{Component: "hello-world-api", Origin: taskmeta.OriginSpecPlan,
		SpecTag: "requirements-v1", DesignTag: "design-v0"}
	block.Key = taskmeta.Key("proj1", "design-v0", block.Target(), "Implement hello-world-api")
	r.issues.seed(gitrepo.IssueInfo{
		Number: 104,
		Title:  "Implement hello-world-api",
		Body:   taskmeta.ComposeBody(block, taskmeta.Human{Rationale: "seed", Body: "## Scope"}),
		State:  "open",
		Labels: taskmeta.NewTaskLabels(taskmeta.ClassCoding, taskmeta.OriginSpecPlan),
	})

	req := r.start(t)
	instr := req.Instruction
	if !strings.Contains(instr, "## Existing open Tasks and lineage diffs (reference)") {
		t.Fatalf("context section missing: %q", instr)
	}
	if !strings.Contains(instr, "--- tasks/104.md ---") || !strings.Contains(instr, "hello-world-api") {
		t.Errorf("existing task render missing: %q", instr)
	}
	if !strings.Contains(instr, "# Lineage diff: design-v0 → design-v1") {
		t.Errorf("lineage diff section missing: %q", instr)
	}
	// The diff carries real hunks (the gitfs Diff Patch extension).
	if !strings.Contains(instr, "```diff") || !strings.Contains(instr, "+# design v1 CHANGED") {
		t.Errorf("lineage diff patch hunks missing: %q", instr)
	}
}

// TestStartPlan_SkillsRepoGone_TypedError pins the incident's plan-turn failure
// shape: a stale _skills row over a gone repo must surface as the typed
// ErrSkillsRepoUnavailable (mapped to a logged 503 at the edge), never an
// anonymous wrap that falls into the opaque 500 — and the turn must not start.
func TestStartPlan_SkillsRepoGone_TypedError(t *testing.T) {
	fx := workspacetest.New(t, map[string]string{
		"specs/design/design.md":                              "# design",
		"specs/design/components/hello-world-api/design.json": `{"name":"hello-world-api"}`,
		"specs/requirements/requirements.md":                  "# reqs",
	})
	repoRow := &models.GitRepository{OrgID: "org1", ProjectID: "proj1", RepoURL: fx.Origin.URL(),
		DefaultBranch: "main", RepoSlug: workspacetest.DefaultSlug, Status: "ready"}
	staleSkills := &models.GitRepository{OrgID: "org1", ProjectID: models.SkillsRepoSentinelProjectID,
		RepoURL: "file:///nonexistent/skills-repo-gone.git", DefaultBranch: "main", RepoSlug: "org-skills", Status: "ready"}

	turn := &capturingTurn{}
	svc := NewPlanService(
		fakeRepos{repo: repoRow},
		planVersions{designTag: "design-v1"},
		gitrepo.NewGitOpsService(nilResolver{}, fx.Engine),
		func(context.Context, string) (string, error) { return "sk-test", nil },
		turn,
		newFakeIssues(),
		fx.Engine,
		func(context.Context, string) (*models.GitRepository, error) { return staleSkills, nil },
	)

	_, err := svc.StartPlan(context.Background(), "org1", "proj1")
	if !errors.Is(err, ErrSkillsRepoUnavailable) {
		t.Fatalf("StartPlan error = %v, want ErrSkillsRepoUnavailable", err)
	}
	if turn.req != nil {
		t.Error("plan turn dispatched despite an unavailable skills repo")
	}
}
