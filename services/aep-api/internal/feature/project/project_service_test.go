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

// UNIT tier (bff-component-testing.md §2): the REAL projectService with every
// port mocked — no HTTP, no DB. Proves the service's logic branches under the
// tasks-github-native model: sentinel translation, CreateProject's best-effort
// side-effect chain, the delete cascade (repo cleanup + executions purge — NO
// component_tasks table any more), and the GetProjectStatus phase ladder (which
// no longer counts tasks: it stops at "tasks" once a design exists, §8). The
// HTTP contract lives in project_component_test.go; the DeleteProject executions
// purge over real Postgres lives in project_dbtest_test.go; the
// applyRepoToProjectStatus repo-lifecycle table lives in project_status_test.go.
package project

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/feature/cycle"
	contract "github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts/artifactstest"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/models"
)

// --- port fakes --------------------------------------------------------------

// fakeRepoSvc fakes gitrepo.RepoService. Unset funcs panic loudly.
type fakeRepoSvc struct {
	CreateRepoFunc func(ctx context.Context, orgID, projectID, projectName, repoName string) (*models.GitRepository, error)
	GetRepoFunc    func(ctx context.Context, orgID, projectID string) (*models.GitRepository, error)
	DeleteRepoFunc func(ctx context.Context, orgID, projectID string) error
	ListByOrgFunc  func(ctx context.Context, orgID string) ([]models.GitRepository, error)
}

func (f *fakeRepoSvc) CreateRepo(ctx context.Context, orgID, projectID, projectName, repoName string) (*models.GitRepository, error) {
	if f.CreateRepoFunc == nil {
		panic("fakeRepoSvc: CreateRepo not set")
	}
	return f.CreateRepoFunc(ctx, orgID, projectID, projectName, repoName)
}
func (f *fakeRepoSvc) ListByOrg(ctx context.Context, orgID string) ([]models.GitRepository, error) {
	if f.ListByOrgFunc == nil {
		panic("fakeRepoSvc: ListByOrg not set")
	}
	return f.ListByOrgFunc(ctx, orgID)
}
func (f *fakeRepoSvc) EnsureBareRepo(context.Context, string, string, string) (*models.GitRepository, error) {
	panic("fakeRepoSvc: EnsureBareRepo not expected in project tests")
}
func (f *fakeRepoSvc) GetRepo(ctx context.Context, orgID, projectID string) (*models.GitRepository, error) {
	if f.GetRepoFunc == nil {
		panic("fakeRepoSvc: GetRepo not set")
	}
	return f.GetRepoFunc(ctx, orgID, projectID)
}
func (f *fakeRepoSvc) SetWebhookID(context.Context, string, string, int64) error {
	panic("fakeRepoSvc: SetWebhookID not expected in project tests")
}
func (f *fakeRepoSvc) DeleteRepo(ctx context.Context, orgID, projectID string) error {
	if f.DeleteRepoFunc == nil {
		panic("fakeRepoSvc: DeleteRepo not set")
	}
	return f.DeleteRepoFunc(ctx, orgID, projectID)
}

type fakeWebhookSvc struct {
	RegisterFunc func(ctx context.Context, orgID, projectID string) (*int64, error)
	calls        int
}

func (f *fakeWebhookSvc) Register(ctx context.Context, orgID, projectID string) (*int64, error) {
	f.calls++
	if f.RegisterFunc == nil {
		return nil, nil
	}
	return f.RegisterFunc(ctx, orgID, projectID)
}

// fakeExecs fakes the slice of repositories.ExecutionRepository the project
// feature drives: DeleteByProject (the orphan purge). Every other verb is
// unreachable from the project feature and returns zero.
type fakeExecs struct {
	DeleteByProjectFunc func(ctx context.Context, orgID, projectID string) error
	deleteArgs          [2]string
	deleteCalls         int
}

func (f *fakeExecs) DeleteByProject(ctx context.Context, orgID, projectID string) error {
	f.deleteCalls++
	f.deleteArgs = [2]string{orgID, projectID}
	if f.DeleteByProjectFunc == nil {
		return nil
	}
	return f.DeleteByProjectFunc(ctx, orgID, projectID)
}
func (f *fakeExecs) TryAdmit(context.Context, *models.Execution) (bool, *models.Execution, error) {
	return false, nil, nil
}
func (f *fakeExecs) StartWithRun(context.Context, string, string) (*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) Finish(context.Context, string, string, string) (*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) NoteBuildRetry(context.Context, string, string, string) (*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) GetByIDScoped(context.Context, string, string) (*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) LatestPerKind(context.Context, string, int) (map[string]*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) LatestPerKindScoped(context.Context, string, string, int) (map[string]*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) LatestPerKindForRepo(context.Context, string) (map[int]map[string]*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) LatestPerKindForRepoScoped(context.Context, string, string) (map[int]map[string]*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) ListByIssue(context.Context, string, int) ([]models.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) ListByIssueScoped(context.Context, string, string, int) ([]models.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) ListActive(context.Context) ([]models.Execution, error) { return nil, nil }
func (f *fakeExecs) UpsertReadModel(context.Context, *models.Execution) (*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) GetByWorkflowID(context.Context, string) (*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) ListReadModelByStatus(context.Context, string) ([]models.Execution, error) {
	return nil, nil
}

type fakeSkillsProvisioner struct{ called chan string }

func (f *fakeSkillsProvisioner) EnsureProvisioned(_ context.Context, orgID string) error {
	f.called <- orgID
	return nil
}

// --- translateHTTPError ------------------------------------------------------

func TestTranslateHTTPError(t *testing.T) {
	t.Parallel()
	opaque := errors.New("boom")
	cases := []struct {
		name string
		in   error
		want error // sentinel expected via errors.Is; nil means passthrough of in
	}{
		{"nil is nil", nil, nil},
		{"oc not-found becomes project not-found", openchoreo.ErrNotFound, ErrProjectNotFound},
		{"wrapped oc not-found too", errors.Join(errors.New("ctx"), openchoreo.ErrNotFound), ErrProjectNotFound},
		{"oc unauthorized", openchoreo.ErrUnauthorized, ErrUnauthorized},
		{"oc forbidden", openchoreo.ErrForbidden, ErrForbidden},
		{"opaque passes through", opaque, opaque},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := translateHTTPError(tc.in)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("want nil, got %v", got)
				}
				return
			}
			if !errors.Is(got, tc.want) {
				t.Fatalf("want errors.Is(%v, %v)", got, tc.want)
			}
		})
	}
}

func TestListProjects_TranslatesOCError(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		ListProjectsFunc: func(context.Context, string, int, string) (*models.ProjectList, error) {
			return nil, openchoreo.ErrNotFound
		},
	}
	svc := NewProjectService(oc, nil, nil, nil, nil, nil)
	if _, err := svc.ListProjects(context.Background(), "acme", 100, "", ""); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("want ErrProjectNotFound, got %v", err)
	}
}

func TestListProjects_SearchFiltersPageCaseInsensitive(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		ListProjectsFunc: func(context.Context, string, int, string) (*models.ProjectList, error) {
			return &models.ProjectList{Items: []models.Project{
				{Name: "billing-api", DisplayName: "Billing"},
				{Name: "web-shop", DisplayName: "Shop Front"},
				{Name: "svc-x", DisplayName: "Mobile BILLING helper"},
			}, NextCursor: "tok-2"}, nil
		},
	}
	svc := NewProjectService(oc, nil, nil, nil, nil, nil)
	list, err := svc.ListProjects(context.Background(), "acme", 100, "", "bIlLiNg")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list.Items) != 2 || list.Items[0].Name != "billing-api" || list.Items[1].Name != "svc-x" {
		t.Fatalf("filtered items = %+v; want billing-api (name match) + svc-x (displayName match)", list.Items)
	}
	// The page-scoped filter must NOT eat the continuation token — the caller
	// pages through and filters each page.
	if list.NextCursor != "tok-2" {
		t.Fatalf("NextCursor = %q; want tok-2", list.NextCursor)
	}
}

func TestListProjects_SurfacesNextCursorAndPassesParams(t *testing.T) {
	t.Parallel()
	var gotLimit int
	var gotCursor string
	oc := &ocmocks.ProjectClientMock{
		ListProjectsFunc: func(_ context.Context, _ string, limit int, cursor string) (*models.ProjectList, error) {
			gotLimit, gotCursor = limit, cursor
			return &models.ProjectList{Items: []models.Project{{Name: "a"}}, NextCursor: "next-tok"}, nil
		},
	}
	svc := NewProjectService(oc, nil, nil, nil, nil, nil)
	list, err := svc.ListProjects(context.Background(), "acme", 42, "cur-1", "")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if gotLimit != 42 || gotCursor != "cur-1" {
		t.Fatalf("client got (limit=%d cursor=%q); want (42, cur-1)", gotLimit, gotCursor)
	}
	if list.NextCursor != "next-tok" || len(list.Items) != 1 {
		t.Fatalf("list = %+v; want 1 item + NextCursor next-tok", list)
	}
}

// ListProjects joins each project's git repo URL from the BFF's own rows
// (#108): repo-less projects stay bare, and a failing join degrades to a
// repoUrl-less list rather than a 500 — the listing must never break because
// the annotation source is down.
func TestListProjects_JoinsRepoURLBestEffort(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		ListProjectsFunc: func(context.Context, string, int, string) (*models.ProjectList, error) {
			return &models.ProjectList{Items: []models.Project{
				{Name: "web"},
				{Name: "no-repo"},
			}}, nil
		},
	}
	repoSvc := &fakeRepoSvc{
		ListByOrgFunc: func(_ context.Context, orgID string) ([]models.GitRepository, error) {
			if orgID != "acme" {
				t.Errorf("ListByOrg org = %q, want acme", orgID)
			}
			return []models.GitRepository{
				{OrgID: "acme", ProjectID: "web", RepoURL: "https://github.com/acme/web.git"},
			}, nil
		},
	}
	svc := NewProjectService(oc, repoSvc, nil, nil, nil, nil)

	list, err := svc.ListProjects(context.Background(), "acme", 100, "", "")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if list.Items[0].RepoURL != "https://github.com/acme/web.git" {
		t.Fatalf("web repoUrl = %q, want the joined clone URL", list.Items[0].RepoURL)
	}
	if list.Items[1].RepoURL != "" {
		t.Fatalf("no-repo project got repoUrl %q, want empty", list.Items[1].RepoURL)
	}

	// Join failure → list still returns, just unannotated.
	repoSvc.ListByOrgFunc = func(context.Context, string) ([]models.GitRepository, error) {
		return nil, errors.New("db down")
	}
	list, err = svc.ListProjects(context.Background(), "acme", 100, "", "")
	if err != nil {
		t.Fatalf("ListProjects with failing join: %v", err)
	}
	if list.Items[0].RepoURL != "" {
		t.Fatalf("failing join must not annotate: got %q", list.Items[0].RepoURL)
	}
}

// --- CreateProject -----------------------------------------------------------

func TestCreateProject_HappyPath_ProvisionsRepoWebhookAndSkills(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, org string, req *models.CreateProjectRequest) (*models.Project, error) {
			return &models.Project{Name: req.Name, NamespaceName: org}, nil
		},
	}
	var repoOrg, repoProject, repoProjectName, repoOverride string
	repoSvc := &fakeRepoSvc{
		CreateRepoFunc: func(_ context.Context, orgID, projectID, projectName, repoName string) (*models.GitRepository, error) {
			repoOrg, repoProject, repoProjectName, repoOverride = orgID, projectID, projectName, repoName
			return &models.GitRepository{Status: "ready"}, nil
		},
	}
	webhooks := &fakeWebhookSvc{}
	skills := &fakeSkillsProvisioner{called: make(chan string, 1)}

	svc := NewProjectService(oc, repoSvc, webhooks, nil, nil, nil)
	svc.SetSkillsProvisioner(skills)

	p, err := svc.CreateProject(context.Background(), "acme", &models.CreateProjectRequest{Name: "web"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.Name != "web" {
		t.Fatalf("project name: got %q", p.Name)
	}
	if repoOrg != "acme" || repoProject != "web" || repoProjectName != "web" || repoOverride != "" {
		t.Fatalf("CreateRepo args: got (%q,%q,%q,override=%q)", repoOrg, repoProject, repoProjectName, repoOverride)
	}
	if webhooks.calls != 1 {
		t.Fatalf("webhook Register calls: got %d, want 1", webhooks.calls)
	}
	select {
	case org := <-skills.called:
		if org != "acme" {
			t.Fatalf("skills provisioned for org %q, want acme", org)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("skills provisioner was never called")
	}
}

func TestCreateProject_RepoNameOverridesProvisionedRepoName(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, org string, req *models.CreateProjectRequest) (*models.Project, error) {
			return &models.Project{Name: req.Name, NamespaceName: org}, nil
		},
	}
	var repoProject, repoOverride string
	repoSvc := &fakeRepoSvc{
		CreateRepoFunc: func(_ context.Context, _, projectID, _, repoName string) (*models.GitRepository, error) {
			repoProject, repoOverride = projectID, repoName
			return &models.GitRepository{Status: "ready"}, nil
		},
	}
	svc := NewProjectService(oc, repoSvc, &fakeWebhookSvc{}, nil, nil, nil)

	req := &models.CreateProjectRequest{Name: "gym", RepoName: "gym-repo", Prompt: "a workout tracker"}
	if _, err := svc.CreateProject(context.Background(), "acme", req); err != nil {
		t.Fatalf("create: %v", err)
	}
	// The OC project keeps the project name; only the git repo gets the override.
	if repoProject != "gym" || repoOverride != "gym-repo" {
		t.Fatalf("CreateRepo args: got (project=%q repo=%q), want (gym, gym-repo)", repoProject, repoOverride)
	}
}

// A repo name conflict is a hard failure whether the name was user-chosen or
// derived from the project name: the WHOLE create fails (the just-created OC
// project is compensated away) instead of degrading to the no-repo limbo —
// retrying the same name can never succeed, so the user must be told
// immediately to pick another name.
func TestCreateProject_RepoNameConflictRollsBackProject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		repoName string
	}{
		{"user-chosen repo name", "taken-repo"},
		{"derived repo name", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var deletedProject string
			oc := &ocmocks.ProjectClientMock{
				CreateProjectFunc: func(_ context.Context, org string, req *models.CreateProjectRequest) (*models.Project, error) {
					return &models.Project{Name: req.Name, NamespaceName: org}, nil
				},
				DeleteProjectFunc: func(_ context.Context, _, projectName string) error {
					deletedProject = projectName
					return nil
				},
			}
			repoSvc := &fakeRepoSvc{
				CreateRepoFunc: func(context.Context, string, string, string, string) (*models.GitRepository, error) {
					return nil, fmt.Errorf("create github repo: %w", gitrepo.ErrRepoNameConflict)
				},
			}
			svc := NewProjectService(oc, repoSvc, &fakeWebhookSvc{RegisterFunc: func(context.Context, string, string) (*int64, error) {
				t.Error("webhook must not be registered when the repo conflicts")
				return nil, nil
			}}, nil, nil, nil)

			req := &models.CreateProjectRequest{Name: "gym", RepoName: tc.repoName}
			_, err := svc.CreateProject(context.Background(), "acme", req)
			if !gitrepo.IsRepoNameConflict(err) {
				t.Fatalf("err = %v, want the repo-name-conflict sentinel surfaced", err)
			}
			if deletedProject != "gym" {
				t.Fatalf("OC project compensation: deleted %q, want gym", deletedProject)
			}
		})
	}
}

func TestCreateProject_OCErrorShortCircuits(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(context.Context, string, *models.CreateProjectRequest) (*models.Project, error) {
			return nil, openchoreo.ErrConflict
		},
	}
	// Panicking repo fake + a webhook that fails the test if reached.
	svc := NewProjectService(oc, &fakeRepoSvc{}, &fakeWebhookSvc{RegisterFunc: func(context.Context, string, string) (*int64, error) {
		t.Error("webhook must not be registered when OC create fails")
		return nil, nil
	}}, nil, nil, nil)

	if _, err := svc.CreateProject(context.Background(), "acme", &models.CreateProjectRequest{Name: "web"}); !errors.Is(err, openchoreo.ErrConflict) {
		t.Fatalf("want the OC conflict error surfaced, got %v", err)
	}
}

func TestCreateProject_RepoFailureIsBestEffort(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, _ string, req *models.CreateProjectRequest) (*models.Project, error) {
			return &models.Project{Name: req.Name}, nil
		},
	}
	repoSvc := &fakeRepoSvc{
		CreateRepoFunc: func(context.Context, string, string, string, string) (*models.GitRepository, error) {
			return nil, errors.New("github down")
		},
	}
	webhooks := &fakeWebhookSvc{}
	svc := NewProjectService(oc, repoSvc, webhooks, nil, nil, nil)

	p, err := svc.CreateProject(context.Background(), "acme", &models.CreateProjectRequest{Name: "web"})
	if err != nil || p == nil {
		t.Fatalf("repo provisioning failure must not fail project creation: p=%v err=%v", p, err)
	}
	if webhooks.calls != 0 {
		t.Fatalf("webhook must not be registered when repo creation failed, got %d calls", webhooks.calls)
	}
}

func TestCreateProject_WebhookFailureIsBestEffort(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, _ string, req *models.CreateProjectRequest) (*models.Project, error) {
			return &models.Project{Name: req.Name}, nil
		},
	}
	repoSvc := &fakeRepoSvc{
		CreateRepoFunc: func(context.Context, string, string, string, string) (*models.GitRepository, error) {
			return &models.GitRepository{Status: "ready"}, nil
		},
	}
	webhooks := &fakeWebhookSvc{RegisterFunc: func(context.Context, string, string) (*int64, error) {
		return nil, errors.New("hook API 500")
	}}
	svc := NewProjectService(oc, repoSvc, webhooks, nil, nil, nil)

	if _, err := svc.CreateProject(context.Background(), "acme", &models.CreateProjectRequest{Name: "web"}); err != nil {
		t.Fatalf("webhook failure must not fail project creation: %v", err)
	}
}

// --- DeleteProject -----------------------------------------------------------

func TestDeleteProject_CleansUpRepoAndPurgesExecutions(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		DeleteProjectFunc: func(context.Context, string, string) error { return nil },
	}
	deleted := false
	repoSvc := &fakeRepoSvc{DeleteRepoFunc: func(_ context.Context, orgID, projectID string) error {
		if orgID != "acme" || projectID != "web" {
			t.Errorf("DeleteRepo args: (%q,%q)", orgID, projectID)
		}
		deleted = true
		return nil
	}}
	execs := &fakeExecs{}
	svc := NewProjectService(oc, repoSvc, nil, nil, nil, execs)

	if err := svc.DeleteProject(context.Background(), "acme", "web"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleted {
		t.Error("git repo cleanup was not invoked")
	}
	// The platform-owned executions rows are purged, org+project scoped (§7).
	// The Task issues themselves are GitHub-owned (deleted with the repo) — the
	// service never calls an issue-delete path.
	if execs.deleteCalls != 1 || execs.deleteArgs != [2]string{"acme", "web"} {
		t.Errorf("executions purge: calls=%d args=%v, want 1 (acme,web)", execs.deleteCalls, execs.deleteArgs)
	}
}

func TestDeleteProject_ExecutionsPurgeFailureIsSwallowed(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		DeleteProjectFunc: func(context.Context, string, string) error { return nil },
	}
	execs := &fakeExecs{DeleteByProjectFunc: func(context.Context, string, string) error {
		return errors.New("db down")
	}}
	svc := NewProjectService(oc, nil, nil, nil, nil, execs)
	if err := svc.DeleteProject(context.Background(), "acme", "web"); err != nil {
		t.Fatalf("executions purge failure must be best-effort, got %v", err)
	}
}

func TestDeleteProject_OCErrorSkipsCleanup(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		DeleteProjectFunc: func(context.Context, string, string) error { return openchoreo.ErrNotFound },
	}
	repoSvc := &fakeRepoSvc{DeleteRepoFunc: func(context.Context, string, string) error {
		t.Error("repo cleanup must not run when the OC delete failed")
		return nil
	}}
	execs := &fakeExecs{DeleteByProjectFunc: func(context.Context, string, string) error {
		t.Error("executions purge must not run when the OC delete failed")
		return nil
	}}
	svc := NewProjectService(oc, repoSvc, nil, nil, nil, execs)
	if err := svc.DeleteProject(context.Background(), "acme", "web"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("want ErrProjectNotFound, got %v", err)
	}
	if execs.deleteCalls != 0 {
		t.Errorf("executions purge ran despite OC delete failure (%d calls)", execs.deleteCalls)
	}
}

func TestDeleteProject_RepoCleanupFailureIsSwallowed(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		DeleteProjectFunc: func(context.Context, string, string) error { return nil },
	}
	repoSvc := &fakeRepoSvc{DeleteRepoFunc: func(context.Context, string, string) error {
		return errors.New("fs error")
	}}
	svc := NewProjectService(oc, repoSvc, nil, nil, nil, &fakeExecs{})
	if err := svc.DeleteProject(context.Background(), "acme", "web"); err != nil {
		t.Fatalf("repo cleanup failure must be best-effort, got %v", err)
	}
}

// --- GetProjectStatus phase ladder -------------------------------------------
// Repo-lifecycle short-circuits (no-repo / cloning / error) are proven per branch
// in project_status_test.go against applyRepoToProjectStatus; here the ladder is
// driven end-through with a ready repo. Under tasks-github-native the ladder no
// longer counts tasks — it stops at "tasks" once a design exists (§8).

// statusFixture builds a projectService wired for GetProjectStatus ladder tests.
type statusFixture struct {
	reqFiles       map[string]string
	designFiles    map[string]string
	designFilesErr error
	reqVersions    []artifacts.RequirementsVersionInfo
	desVersions    []artifacts.DesignVersionInfo
}

func (fx statusFixture) service() *projectService {
	fakeArtifacts := &artifactstest.FakeArtifactService{
		ListRequirementFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return fx.reqFiles, nil
		},
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return fx.designFiles, fx.designFilesErr
		},
		ListRequirementsVersionsFunc: func(context.Context, string, string) ([]artifacts.RequirementsVersionInfo, error) {
			return fx.reqVersions, nil
		},
		ListDesignVersionsFunc: func(context.Context, string, string) ([]artifacts.DesignVersionInfo, error) {
			return fx.desVersions, nil
		},
	}
	repoSvc := &fakeRepoSvc{
		GetRepoFunc: func(context.Context, string, string) (*models.GitRepository, error) {
			return &models.GitRepository{Status: "ready", RepoURL: "https://github.com/o/r.git"}, nil
		},
	}
	return NewProjectService(nil, repoSvc, nil, fakeArtifacts, artifacts.NewArtifactStore(fakeArtifacts), nil)
}

func TestGetProjectStatus_NilOrFailingRepoMeansNoRepo(t *testing.T) {
	t.Parallel()

	svc := NewProjectService(nil, nil, nil, nil, nil, nil)
	if st, err := svc.GetProjectStatus(context.Background(), "acme", "web"); err != nil || st.Phase != "no-repo" {
		t.Fatalf("nil repoSvc: want phase no-repo, got %q (err %v)", st.Phase, err)
	}

	failing := &fakeRepoSvc{GetRepoFunc: func(context.Context, string, string) (*models.GitRepository, error) {
		return nil, errors.New("db down")
	}}
	if st, err := NewProjectService(nil, failing, nil, nil, nil, nil).GetProjectStatus(context.Background(), "acme", "web"); err != nil || st.Phase != "no-repo" {
		t.Fatalf("GetRepo error: want phase no-repo, got %q (err %v)", st.Phase, err)
	}

	norow := &fakeRepoSvc{GetRepoFunc: func(context.Context, string, string) (*models.GitRepository, error) {
		return nil, nil
	}}
	if st, err := NewProjectService(nil, norow, nil, nil, nil, nil).GetProjectStatus(context.Background(), "acme", "web"); err != nil || st.Phase != "no-repo" {
		t.Fatalf("GetRepo nil row: want phase no-repo, got %q (err %v)", st.Phase, err)
	}
}

func TestGetProjectStatus_DesignReadErrorPropagates(t *testing.T) {
	t.Parallel()
	fx := statusFixture{
		reqFiles:       map[string]string{"req.md": "# R"},
		designFilesErr: errors.New("git wedged"),
	}
	if _, err := fx.service().GetProjectStatus(context.Background(), "acme", "web"); err == nil {
		t.Fatal("a non-NotFound design read error must propagate")
	}
}

func TestGetProjectStatus_PhaseLadder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		fx         statusFixture
		wantPhase  string
		wantSpec   string
		wantDesign string
	}{
		{
			name:      "no spec files → prompt",
			fx:        statusFixture{},
			wantPhase: "prompt",
		},
		{
			name:      "spec files unversioned → draft, phase spec",
			fx:        statusFixture{reqFiles: map[string]string{"req.md": "# R"}},
			wantPhase: "spec",
			wantSpec:  "draft",
		},
		{
			name: "approved spec + design → phase tasks (no task counting, §8)",
			fx: statusFixture{
				reqFiles:    map[string]string{"req.md": "# R"},
				reqVersions: []artifacts.RequirementsVersionInfo{{Tag: "v1", Version: 1}},
				desVersions: []artifacts.DesignVersionInfo{{Tag: "v1-1"}},
				designFiles: map[string]string{artifacts.DesignRootFile: "# Design"},
			},
			wantPhase:  "tasks",
			wantSpec:   "approved",
			wantDesign: "approved",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, err := tc.fx.service().GetProjectStatus(context.Background(), "acme", "web")
			if err != nil {
				t.Fatalf("status: %v", err)
			}
			if st.Phase != tc.wantPhase {
				t.Errorf("phase = %q, want %q", st.Phase, tc.wantPhase)
			}
			if st.SpecStatus != tc.wantSpec {
				t.Errorf("specStatus = %q, want %q", st.SpecStatus, tc.wantSpec)
			}
			if st.DesignStatus != tc.wantDesign {
				t.Errorf("designStatus = %q, want %q", st.DesignStatus, tc.wantDesign)
			}
			// The tasks-github-native ladder never sets HasTasks (no DB count, §8).
			if st.HasTasks {
				t.Error("HasTasks must stay false — tasks are counted live from GitHub, not here")
			}
		})
	}
}

// fakeCycleFlowReader is a cycleFlowReader test double.
type fakeCycleFlowReader struct {
	state *contract.CycleStateView
	err   error
}

func (f fakeCycleFlowReader) GetFlowState(context.Context, string, string) (*contract.CycleStateView, error) {
	return f.state, f.err
}

// TestGetProjectStatus_CyclePhase covers §R2.2: CyclePhase is layered onto
// the artifact-derived status from a nil-safe cycleFlowReader, on every rung
// of the phase ladder, and never breaks the base status on failure.
func TestGetProjectStatus_CyclePhase(t *testing.T) {
	t.Parallel()

	t.Run("no reader wired → CyclePhase unset", func(t *testing.T) {
		fx := statusFixture{reqFiles: map[string]string{"req.md": "# R"}}
		st, err := fx.service().GetProjectStatus(context.Background(), "acme", "web")
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st.CyclePhase != nil {
			t.Errorf("CyclePhase = %v, want nil (no reader wired)", *st.CyclePhase)
		}
	})

	t.Run("active cycle → CyclePhase set, base Phase unchanged", func(t *testing.T) {
		fx := statusFixture{reqFiles: map[string]string{"req.md": "# R"}}
		svc := fx.service()
		svc.SetCycleFlowReader(fakeCycleFlowReader{state: &contract.CycleStateView{Phase: contract.PhaseImplement}})

		st, err := svc.GetProjectStatus(context.Background(), "acme", "web")
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st.Phase != "spec" {
			t.Errorf("base Phase = %q, want unchanged %q", st.Phase, "spec")
		}
		if st.CyclePhase == nil || *st.CyclePhase != string(contract.PhaseImplement) {
			t.Errorf("CyclePhase = %v, want %q", st.CyclePhase, contract.PhaseImplement)
		}
	})

	t.Run("cycle read failure → CyclePhase unset, no error propagated", func(t *testing.T) {
		fx := statusFixture{reqFiles: map[string]string{"req.md": "# R"}}
		svc := fx.service()
		svc.SetCycleFlowReader(fakeCycleFlowReader{err: cycle.ErrNoActiveCycle})

		st, err := svc.GetProjectStatus(context.Background(), "acme", "web")
		if err != nil {
			t.Fatalf("a cycle-read failure must not fail the whole status read: %v", err)
		}
		if st.CyclePhase != nil {
			t.Errorf("CyclePhase = %v, want nil", *st.CyclePhase)
		}
	})
}
