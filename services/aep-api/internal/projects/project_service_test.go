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

// UNIT tier: the REAL Service with every
// port mocked — no HTTP, no DB. Proves the service's logic branches under the
// tasks-github-native model: sentinel translation, CreateProject's best-effort
// side-effect chain, the delete cascade (repo cleanup + executions purge — NO
// component_tasks table any more), and the GetProjectStatus phase ladder (which
// no longer counts tasks: it stops at "tasks" once a design exists, §8). The
// HTTP contract lives in project_component_test.go; the DeleteProject executions
// purge over real Postgres lives in project_dbtest_test.go; the
// applyRepoToProjectStatus repo-lifecycle table lives in project_status_test.go.
package projects

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
	"github.com/wso2/aep/aep-api/internal/spec/artifactstest"
)

// --- port fakes --------------------------------------------------------------

// fakeRepoSvc fakes sourcecontrol.RepoService. Unset funcs panic loudly.
type fakeRepoSvc struct {
	CreateRepoFunc func(ctx context.Context, orgID, projectID, projectName, repoName string) (*sourcecontrol.GitRepository, error)
	GetRepoFunc    func(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error)
	DeleteRepoFunc func(ctx context.Context, orgID, projectID string) error
	ListByOrgFunc  func(ctx context.Context, orgID string) ([]sourcecontrol.GitRepository, error)
}

func (f *fakeRepoSvc) CreateRepo(ctx context.Context, orgID, projectID, projectName, repoName string) (*sourcecontrol.GitRepository, error) {
	if f.CreateRepoFunc == nil {
		panic("fakeRepoSvc: CreateRepo not set")
	}
	return f.CreateRepoFunc(ctx, orgID, projectID, projectName, repoName)
}
func (f *fakeRepoSvc) ListByOrg(ctx context.Context, orgID string) ([]sourcecontrol.GitRepository, error) {
	if f.ListByOrgFunc == nil {
		panic("fakeRepoSvc: ListByOrg not set")
	}
	return f.ListByOrgFunc(ctx, orgID)
}
func (f *fakeRepoSvc) EnsureBareRepo(context.Context, string, string, string) (*sourcecontrol.GitRepository, error) {
	panic("fakeRepoSvc: EnsureBareRepo not expected in project tests")
}
func (f *fakeRepoSvc) GetRepo(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error) {
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
	RegisterFunc   func(ctx context.Context, orgID, projectID string) (*int64, error)
	UnregisterFunc func(ctx context.Context, orgID, projectID string) error
	calls          int
	// trace, when set, records the unregister step in the delete cascade so its
	// ORDER relative to the repo-row delete can be asserted.
	trace           *deleteTrace
	unregisterArgs  [2]string
	unregisterCalls int
}

func (f *fakeWebhookSvc) Register(ctx context.Context, orgID, projectID string) (*int64, error) {
	f.calls++
	if f.RegisterFunc == nil {
		return nil, nil
	}
	return f.RegisterFunc(ctx, orgID, projectID)
}

func (f *fakeWebhookSvc) Unregister(ctx context.Context, orgID, projectID string) error {
	f.unregisterCalls++
	f.unregisterArgs = [2]string{orgID, projectID}
	if f.trace != nil {
		f.trace.steps = append(f.trace.steps, "webhook")
	}
	if f.UnregisterFunc == nil {
		return nil
	}
	return f.UnregisterFunc(ctx, orgID, projectID)
}

// fakeExecs fakes the slice of delivery.ExecutionRepository the project
// feature drives: DeleteByProject (the orphan purge). Every other verb is
// unreachable from the project feature and returns zero.
type fakeExecs struct {
	DeleteByProjectFunc     func(ctx context.Context, orgID, projectID string) error
	LatestPerKindScopedFunc func(ctx context.Context, orgID, repo string, issue int) (map[string]*delivery.Execution, error)
	deleteArgs              [2]string
	deleteCalls             int
}

func (f *fakeExecs) DistinctDeployedProjects(context.Context) ([]delivery.DeployedProjectRef, error) {
	return nil, nil
}

func (f *fakeExecs) RecordUsage(context.Context, string, contracts.CapturedUsage) error { return nil }

func (f *fakeExecs) SumUsageByProjectPhase(context.Context, string) (map[string]contracts.StampedUsage, map[string]contracts.StampedUsage, error) {
	return nil, nil, nil
}

func (f *fakeExecs) DeleteByProject(ctx context.Context, orgID, projectID string) error {
	f.deleteCalls++
	f.deleteArgs = [2]string{orgID, projectID}
	if f.DeleteByProjectFunc == nil {
		return nil
	}
	return f.DeleteByProjectFunc(ctx, orgID, projectID)
}
func (f *fakeExecs) TryAdmit(context.Context, *delivery.Execution) (bool, *delivery.Execution, error) {
	return false, nil, nil
}
func (f *fakeExecs) StartWithRun(context.Context, string, string) (*delivery.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) Finish(context.Context, string, string, string) (*delivery.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) NoteBuildRetry(context.Context, string, string, string) (*delivery.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) GetByIDScoped(context.Context, string, string) (*delivery.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) LatestPerKind(context.Context, string, int) (map[string]*delivery.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) LatestPerKindScoped(ctx context.Context, orgID, repo string, issue int) (map[string]*delivery.Execution, error) {
	if f.LatestPerKindScopedFunc != nil {
		return f.LatestPerKindScopedFunc(ctx, orgID, repo, issue)
	}
	return nil, nil
}
func (f *fakeExecs) LatestPerKindForRepo(context.Context, string) (map[int]map[string]*delivery.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) LatestPerKindForRepoScoped(context.Context, string, string) (map[int]map[string]*delivery.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) ListByIssue(context.Context, string, int) ([]delivery.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) ListByIssueScoped(context.Context, string, string, int) ([]delivery.Execution, error) {
	return nil, nil
}
func (f *fakeExecs) ListActive(context.Context) ([]delivery.Execution, error) { return nil, nil }

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
		ListProjectsFunc: func(context.Context, string, int, string) (*gen.ProjectList, error) {
			return nil, openchoreo.ErrNotFound
		},
	}
	svc := NewProjectService(oc, nil, nil, nil, nil)
	if _, err := svc.ListProjects(context.Background(), "acme", 100, "", ""); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("want ErrProjectNotFound, got %v", err)
	}
}

func TestListProjects_SearchFiltersPageCaseInsensitive(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		ListProjectsFunc: func(context.Context, string, int, string) (*gen.ProjectList, error) {
			return &gen.ProjectList{Items: []gen.Project{
				{Name: "billing-api", DisplayName: "Billing"},
				{Name: "web-shop", DisplayName: "Shop Front"},
				{Name: "svc-x", DisplayName: "Mobile BILLING helper"},
			}, NextCursor: "tok-2"}, nil
		},
	}
	svc := NewProjectService(oc, nil, nil, nil, nil)
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
		ListProjectsFunc: func(_ context.Context, _ string, limit int, cursor string) (*gen.ProjectList, error) {
			gotLimit, gotCursor = limit, cursor
			return &gen.ProjectList{Items: []gen.Project{{Name: "a"}}, NextCursor: "next-tok"}, nil
		},
	}
	svc := NewProjectService(oc, nil, nil, nil, nil)
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
		ListProjectsFunc: func(context.Context, string, int, string) (*gen.ProjectList, error) {
			return &gen.ProjectList{Items: []gen.Project{
				{Name: "web"},
				{Name: "no-repo"},
			}}, nil
		},
	}
	repoSvc := &fakeRepoSvc{
		ListByOrgFunc: func(_ context.Context, orgID string) ([]sourcecontrol.GitRepository, error) {
			if orgID != "acme" {
				t.Errorf("ListByOrg org = %q, want acme", orgID)
			}
			return []sourcecontrol.GitRepository{
				{OrgID: "acme", ProjectID: "web", RepoURL: "https://github.com/acme/web.git"},
			}, nil
		},
	}
	svc := NewProjectService(oc, repoSvc, nil, nil, nil)

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
	repoSvc.ListByOrgFunc = func(context.Context, string) ([]sourcecontrol.GitRepository, error) {
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
		CreateProjectFunc: func(_ context.Context, org string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			return &gen.Project{Name: req.Name, NamespaceName: org}, nil
		},
	}
	var repoOrg, repoProject, repoProjectName, repoOverride string
	repoSvc := &fakeRepoSvc{
		CreateRepoFunc: func(_ context.Context, orgID, projectID, projectName, repoName string) (*sourcecontrol.GitRepository, error) {
			repoOrg, repoProject, repoProjectName, repoOverride = orgID, projectID, projectName, repoName
			return &sourcecontrol.GitRepository{Status: "ready"}, nil
		},
	}
	webhooks := &fakeWebhookSvc{}
	skills := &fakeSkillsProvisioner{called: make(chan string, 1)}

	svc := NewProjectService(oc, repoSvc, webhooks, nil, nil)
	svc.SetSkillsProvisioner(skills)

	p, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{Name: "web"})
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
		CreateProjectFunc: func(_ context.Context, org string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			return &gen.Project{Name: req.Name, NamespaceName: org}, nil
		},
	}
	var repoProject, repoOverride string
	repoSvc := &fakeRepoSvc{
		CreateRepoFunc: func(_ context.Context, _, projectID, _, repoName string) (*sourcecontrol.GitRepository, error) {
			repoProject, repoOverride = projectID, repoName
			return &sourcecontrol.GitRepository{Status: "ready"}, nil
		},
	}
	svc := NewProjectService(oc, repoSvc, &fakeWebhookSvc{}, nil, nil)

	req := &gen.CreateProjectRequest{Name: "gym", RepoName: "gym-repo", Prompt: "a workout tracker"}
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
				CreateProjectFunc: func(_ context.Context, org string, req *gen.CreateProjectRequest) (*gen.Project, error) {
					return &gen.Project{Name: req.Name, NamespaceName: org}, nil
				},
				DeleteProjectFunc: func(_ context.Context, _, projectName string) error {
					deletedProject = projectName
					return nil
				},
			}
			repoSvc := &fakeRepoSvc{
				CreateRepoFunc: func(context.Context, string, string, string, string) (*sourcecontrol.GitRepository, error) {
					return nil, fmt.Errorf("create github repo: %w", sourcecontrol.ErrRepoNameConflict)
				},
			}
			svc := NewProjectService(oc, repoSvc, &fakeWebhookSvc{RegisterFunc: func(context.Context, string, string) (*int64, error) {
				t.Error("webhook must not be registered when the repo conflicts")
				return nil, nil
			}}, nil, nil)

			req := &gen.CreateProjectRequest{Name: "gym", RepoName: tc.repoName}
			_, err := svc.CreateProject(context.Background(), "acme", req)
			if !sourcecontrol.IsRepoNameConflict(err) {
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
		CreateProjectFunc: func(context.Context, string, *gen.CreateProjectRequest) (*gen.Project, error) {
			return nil, openchoreo.ErrConflict
		},
	}
	// Panicking repo fake + a webhook that fails the test if reached.
	svc := NewProjectService(oc, &fakeRepoSvc{}, &fakeWebhookSvc{RegisterFunc: func(context.Context, string, string) (*int64, error) {
		t.Error("webhook must not be registered when OC create fails")
		return nil, nil
	}}, nil, nil)

	if _, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{Name: "web"}); !errors.Is(err, openchoreo.ErrConflict) {
		t.Fatalf("want the OC conflict error surfaced, got %v", err)
	}
}

func TestCreateProject_RepoFailureIsBestEffort(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, _ string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			return &gen.Project{Name: req.Name}, nil
		},
	}
	repoSvc := &fakeRepoSvc{
		CreateRepoFunc: func(context.Context, string, string, string, string) (*sourcecontrol.GitRepository, error) {
			return nil, errors.New("github down")
		},
	}
	webhooks := &fakeWebhookSvc{}
	svc := NewProjectService(oc, repoSvc, webhooks, nil, nil)

	p, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{Name: "web"})
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
		CreateProjectFunc: func(_ context.Context, _ string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			return &gen.Project{Name: req.Name}, nil
		},
	}
	repoSvc := &fakeRepoSvc{
		CreateRepoFunc: func(context.Context, string, string, string, string) (*sourcecontrol.GitRepository, error) {
			return &sourcecontrol.GitRepository{Status: "ready"}, nil
		},
	}
	webhooks := &fakeWebhookSvc{RegisterFunc: func(context.Context, string, string) (*int64, error) {
		return nil, errors.New("hook API 500")
	}}
	svc := NewProjectService(oc, repoSvc, webhooks, nil, nil)

	if _, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{Name: "web"}); err != nil {
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
	svc := NewProjectService(oc, repoSvc, nil, nil, execs)

	if err := svc.DeleteProject(context.Background(), "acme", "web"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleted {
		t.Error("git repo cleanup was not invoked")
	}
	// The platform-owned executions rows are purged, org+project scoped (§7).
	// The Task issues themselves are GitHub-owned and SURVIVE: the delete leaves
	// the remote repository standing, and the service never calls an issue-delete
	// path.
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
	svc := NewProjectService(oc, nil, nil, nil, execs)
	if err := svc.DeleteProject(context.Background(), "acme", "web"); err != nil {
		t.Fatalf("executions purge failure must be best-effort, got %v", err)
	}
}

// TestDeleteProject_OCErrorSkipsCleanup: a delete that could not REACH
// OpenChoreo has established nothing about the project, so the platform's half
// of it stays put. Tearing down the repo and the rows underneath a project that
// still exists in OC would strand it.
//
// The one OC failure that does NOT stop the cascade is not-found — see
// TestDeleteProject_AlreadyGoneOCProjectStillTearsDownPlatformState.
func TestDeleteProject_OCErrorSkipsCleanup(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		DeleteProjectFunc: func(context.Context, string, string) error { return openchoreo.ErrForbidden },
	}
	repoSvc := &fakeRepoSvc{DeleteRepoFunc: func(context.Context, string, string) error {
		t.Error("repo cleanup must not run when the OC delete failed")
		return nil
	}}
	execs := &fakeExecs{DeleteByProjectFunc: func(context.Context, string, string) error {
		t.Error("executions purge must not run when the OC delete failed")
		return nil
	}}
	svc := NewProjectService(oc, repoSvc, nil, nil, execs)
	if err := svc.DeleteProject(context.Background(), "acme", "web"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
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
	svc := NewProjectService(oc, repoSvc, nil, nil, &fakeExecs{})
	if err := svc.DeleteProject(context.Background(), "acme", "web"); err != nil {
		t.Fatalf("repo cleanup failure must be best-effort, got %v", err)
	}
}

// --- the run-supervisor half of the delete cascade ---------------------------

// deleteTrace records the teardown steps in the order the service ran them. The
// ORDER is the contract, not just the calls: a supervisor abandoned after its
// repository is gone has already spent a poll on a repository that no longer
// exists, and one abandoned after its rows are purged has nothing left to
// identify.
type deleteTrace struct {
	steps []string
}

// fakeRunAbandoner is the run-supervisor teardown port.
type fakeRunAbandoner struct {
	trace *deleteTrace
	args  [2]string
	calls int
	err   error
}

func (f *fakeRunAbandoner) AbandonProjectRuns(_ context.Context, orgID, projectID string) error {
	f.calls++
	f.args = [2]string{orgID, projectID}
	f.trace.steps = append(f.trace.steps, "abandon")
	return f.err
}

// tracingRunRows is the milestoneRunRows port with its purge traced.
type tracingRunRows struct {
	trace *deleteTrace
	err   error
}

func (tracingRunRows) ListByProject(context.Context, string, string) ([]delivery.MilestoneRun, error) {
	return nil, nil
}

func (tracingRunRows) LatestCycle(context.Context, string, string) (*delivery.RunCycle, error) {
	return nil, nil
}

func (t tracingRunRows) DeleteByProject(context.Context, string, string) error {
	t.trace.steps = append(t.trace.steps, "purge")
	return t.err
}

// TestDeleteProject_AbandonsRunSupervisorsBeforeTheirRepoAndRows: purging a
// project's run rows does not stop the workflows that write them. Left running,
// a supervisor retries its milestone poll forever against a deleted repository,
// and its workflow id — keyed on (org, project, milestone) alone — collides with
// any project later created under the same name, whose first run is then refused
// as AlreadyStarted and never supervised.
func TestDeleteProject_AbandonsRunSupervisorsBeforeTheirRepoAndRows(t *testing.T) {
	t.Parallel()
	trace := &deleteTrace{}
	oc := &ocmocks.ProjectClientMock{
		DeleteProjectFunc: func(context.Context, string, string) error { return nil },
	}
	repoSvc := &fakeRepoSvc{
		GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
			return nil, nil
		},
		DeleteRepoFunc: func(context.Context, string, string) error {
			trace.steps = append(trace.steps, "repo")
			return nil
		},
	}
	abandoner := &fakeRunAbandoner{trace: trace}
	svc := NewProjectService(oc, repoSvc, nil, nil, &fakeExecs{})
	svc.SetStageSources(tracingRunRows{trace: trace}, fakeBindingsReader{})
	svc.SetRunAbandoner(abandoner)

	if err := svc.DeleteProject(context.Background(), "acme", "web"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if abandoner.calls != 1 || abandoner.args != [2]string{"acme", "web"} {
		t.Fatalf("abandon: calls=%d args=%v, want 1 (acme,web)", abandoner.calls, abandoner.args)
	}
	want := []string{"abandon", "repo", "purge"}
	if len(trace.steps) != len(want) {
		t.Fatalf("teardown order = %v, want %v", trace.steps, want)
	}
	for i := range want {
		if trace.steps[i] != want[i] {
			t.Fatalf("teardown order = %v, want %v", trace.steps, want)
		}
	}
}

// TestDeleteProject_AbandonFailureIsSwallowed: the OC delete has already been
// committed by the time the teardown runs, so a Temporal outage must not leave
// the caller a half-deleted project it cannot retry — the rest of the cascade
// still runs.
func TestDeleteProject_AbandonFailureIsSwallowed(t *testing.T) {
	t.Parallel()
	trace := &deleteTrace{}
	oc := &ocmocks.ProjectClientMock{
		DeleteProjectFunc: func(context.Context, string, string) error { return nil },
	}
	abandoner := &fakeRunAbandoner{trace: trace, err: errors.New("temporal down")}
	svc := NewProjectService(oc, nil, nil, nil, &fakeExecs{})
	svc.SetStageSources(tracingRunRows{trace: trace}, fakeBindingsReader{})
	svc.SetRunAbandoner(abandoner)

	if err := svc.DeleteProject(context.Background(), "acme", "web"); err != nil {
		t.Fatalf("abandon failure must be best-effort, got %v", err)
	}
	if len(trace.steps) != 2 || trace.steps[1] != "purge" {
		t.Errorf("the purge must still run after a failed abandon: %v", trace.steps)
	}
}

// TestDeleteProject_UnreachableOCSkipsTheRunTeardown: a delete that could not
// REACH OpenChoreo has established nothing, so the project still exists there —
// and killing the supervisors of a project that is still alive would strand it
// mid-run with no way to resume.
func TestDeleteProject_UnreachableOCSkipsTheRunTeardown(t *testing.T) {
	t.Parallel()
	trace := &deleteTrace{}
	unreachable := errors.New("openchoreo unreachable")
	oc := &ocmocks.ProjectClientMock{
		DeleteProjectFunc: func(context.Context, string, string) error { return unreachable },
	}
	abandoner := &fakeRunAbandoner{trace: trace}
	svc := NewProjectService(oc, nil, nil, nil, &fakeExecs{})
	svc.SetStageSources(tracingRunRows{trace: trace}, fakeBindingsReader{})
	svc.SetRunAbandoner(abandoner)

	if err := svc.DeleteProject(context.Background(), "acme", "web"); !errors.Is(err, unreachable) {
		t.Fatalf("want the OC error, got %v", err)
	}
	if abandoner.calls != 0 {
		t.Errorf("run supervisors abandoned despite a failed OC delete (%d calls)", abandoner.calls)
	}
}

// --- GetProjectStatus phase ladder -------------------------------------------
// Repo-lifecycle short-circuits (no-repo / cloning / error) are proven per branch
// in project_status_test.go against applyRepoToProjectStatus; here the ladder is
// driven end-through with a ready repo. Under tasks-github-native the ladder no
// longer counts tasks — it stops at "tasks" once a design exists (§8).

// fakeRunReader / fakeBindingsReader fake the stage-source ports
// (status_stages.go) — the build/deploy inputs of the status poll.
type fakeRunReader struct {
	rows []delivery.MilestoneRun
	err  error
	// cycle is the run's latest cycle record, consulted only when the newest run
	// is live with no verdict yet — the one case where "is a validation cycle in
	// flight?" cannot be answered from the run row.
	cycle    *delivery.RunCycle
	cycleErr error
}

func (f fakeRunReader) ListByProject(context.Context, string, string) ([]delivery.MilestoneRun, error) {
	return f.rows, f.err
}

func (f fakeRunReader) LatestCycle(context.Context, string, string) (*delivery.RunCycle, error) {
	return f.cycle, f.cycleErr
}

func (f fakeRunReader) DeleteByProject(context.Context, string, string) error { return nil }

type fakeBindingsReader struct {
	items []openchoreo.ReleaseBindingSummary
	err   error
}

func (f fakeBindingsReader) ListProjectReleaseBindings(context.Context, string, string) ([]openchoreo.ReleaseBindingSummary, error) {
	return f.items, f.err
}

// statusFixture builds a Service wired for GetProjectStatus tests: a ready repo
// row + the three poll sources (git snapshot, milestone run rows, dev bindings)
// as fakes.
type statusFixture struct {
	snap     spec.StatusSnapshot
	snapErr  error
	counts   map[string]int // ComponentCountAtTag fixture, keyed by tag
	countErr error
	runs     []delivery.MilestoneRun
	runsErr  error
	// cycle is the newest run's latest cycle record. Only consulted when that run
	// is live with no verdict yet, which is the sole case where the run row cannot
	// say whether a validation cycle is in flight.
	cycle       *delivery.RunCycle
	bindings    []openchoreo.ReleaseBindingSummary
	bindingsErr error
}

func (fx statusFixture) service() *Service {
	fakeArtifacts := &artifactstest.FakeArtifactService{
		StatusSnapshotFunc: func(context.Context, string, string) (*spec.StatusSnapshot, error) {
			if fx.snapErr != nil {
				return nil, fx.snapErr
			}
			snap := fx.snap
			return &snap, nil
		},
		ComponentCountAtTagFunc: func(_ context.Context, _, _, tag string) (int, error) {
			if fx.countErr != nil {
				return 0, fx.countErr
			}
			n, ok := fx.counts[tag]
			if !ok {
				return 0, fmt.Errorf("no component-count fixture for tag %q", tag)
			}
			return n, nil
		},
	}
	repoSvc := &fakeRepoSvc{
		GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
			return &sourcecontrol.GitRepository{Status: "ready", RepoURL: "https://github.com/o/r.git"}, nil
		},
	}
	svc := NewProjectService(nil, repoSvc, nil, fakeArtifacts, nil)
	svc.SetStageSources(
		fakeRunReader{rows: fx.runs, err: fx.runsErr, cycle: fx.cycle},
		fakeBindingsReader{items: fx.bindings, err: fx.bindingsErr})
	return svc
}

func TestGetProjectStatus_NilOrFailingRepoMeansNoRepo(t *testing.T) {
	t.Parallel()

	svc := NewProjectService(nil, nil, nil, nil, nil)
	if st, err := svc.GetProjectStatus(context.Background(), "acme", "web"); err != nil || st.Phase != "no-repo" {
		t.Fatalf("nil repoSvc: want phase no-repo, got %q (err %v)", st.Phase, err)
	}

	failing := &fakeRepoSvc{GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
		return nil, errors.New("db down")
	}}
	if st, err := NewProjectService(nil, failing, nil, nil, nil).GetProjectStatus(context.Background(), "acme", "web"); err != nil || st.Phase != "no-repo" {
		t.Fatalf("GetRepo error: want phase no-repo, got %q (err %v)", st.Phase, err)
	}

	norow := &fakeRepoSvc{GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
		return nil, nil
	}}
	if st, err := NewProjectService(nil, norow, nil, nil, nil).GetProjectStatus(context.Background(), "acme", "web"); err != nil || st.Phase != "no-repo" {
		t.Fatalf("GetRepo nil row: want phase no-repo, got %q (err %v)", st.Phase, err)
	}
}

// TestGetProjectStatus_StrictSourceFailures pins the strict failure mode:
// any stage source failing fails the whole read — the endpoint never
// fabricates emptiness (the console keeps last-good data and repolls).
func TestGetProjectStatus_StrictSourceFailures(t *testing.T) {
	t.Parallel()
	base := statusFixture{snap: spec.StatusSnapshot{HasSpec: true}}

	git := base
	git.snapErr = errors.New("git wedged")
	if _, err := git.service().GetProjectStatus(context.Background(), "acme", "web"); err == nil {
		t.Fatal("git snapshot failure must fail the status read")
	}

	db := base
	db.runsErr = errors.New("db down")
	if _, err := db.service().GetProjectStatus(context.Background(), "acme", "web"); err == nil {
		t.Fatal("run-row failure must fail the status read")
	}

	oc := base
	oc.bindingsErr = errors.New("oc 503")
	if _, err := oc.service().GetProjectStatus(context.Background(), "acme", "web"); err == nil {
		t.Fatal("release-binding failure must fail the status read")
	}

	// The deploy denominator read joins strictly too.
	cnt := base
	cnt.runs = []delivery.MilestoneRun{devRun("v1", delivery.RunStateSucceeded)}
	cnt.countErr = errors.New("tag missing from mirror")
	if _, err := cnt.service().GetProjectStatus(context.Background(), "acme", "web"); err == nil {
		t.Fatal("component-count failure must fail the status read")
	}
}

// TestGetProjectStatus_SourcesNotWired: a ready repo with unwired stage
// sources is a composition bug, surfaced loudly — never a zero-valued lie.
func TestGetProjectStatus_SourcesNotWired(t *testing.T) {
	t.Parallel()
	repoSvc := &fakeRepoSvc{
		GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
			return &sourcecontrol.GitRepository{Status: "ready", RepoURL: "https://github.com/o/r.git"}, nil
		},
	}
	svc := NewProjectService(nil, repoSvc, nil, &artifactstest.FakeArtifactService{}, nil)
	if _, err := svc.GetProjectStatus(context.Background(), "acme", "web"); err == nil {
		t.Fatal("unwired stage sources must error on a ready repo")
	}
}

func TestGetProjectStatus_PhaseLadder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		fx            statusFixture
		wantPhase     string
		wantSpec      string
		wantDesign    string
		wantHasDesign bool
	}{
		{
			name:      "no spec files → prompt",
			fx:        statusFixture{},
			wantPhase: "prompt",
		},
		{
			name:      "spec files unversioned → draft, phase spec",
			fx:        statusFixture{snap: spec.StatusSnapshot{HasSpec: true}},
			wantPhase: "spec",
			wantSpec:  "draft",
		},
		{
			// Design files without any spec: the flat flag stays false (the
			// old ladder never read the design past "prompt").
			name:      "design without spec → prompt, flat hasDesign stays false",
			fx:        statusFixture{snap: spec.StatusSnapshot{HasDesign: true}},
			wantPhase: "prompt",
		},
		{
			name: "approved spec + design → phase tasks (no task counting, §8)",
			fx: statusFixture{
				snap: spec.StatusSnapshot{
					HasSpec:      true,
					HasDesign:    true,
					SpecVersion:  "v1",
					HasDesignTag: true,
				},
			},
			wantPhase:     "tasks",
			wantSpec:      "approved",
			wantDesign:    "approved",
			wantHasDesign: true,
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
			// The flat flags mirror the snapshot; hasDesign stays gated on a
			// spec existing (the old ladder's early return).
			if st.HasSpec != tc.fx.snap.HasSpec {
				t.Errorf("hasSpec = %v, want %v", st.HasSpec, tc.fx.snap.HasSpec)
			}
			if st.HasDesign != tc.wantHasDesign {
				t.Errorf("hasDesign = %v, want %v", st.HasDesign, tc.wantHasDesign)
			}
			// The nested spec stage mirrors the snapshot ungated.
			want := gen.SpecStage{
				Exists:  tc.fx.snap.HasSpec,
				Version: tc.fx.snap.SpecVersion,
				Dirty:   tc.fx.snap.SpecDirty,
				Design:  tc.fx.snap.HasDesign,
			}
			if st.Spec != want {
				t.Errorf("spec stage = %+v, want %+v", st.Spec, want)
			}
			// The tasks-github-native ladder never sets HasTasks (no DB count, §8).
			if st.HasTasks {
				t.Error("HasTasks must stay false — tasks are counted live from GitHub, not here")
			}
		})
	}
}
