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

// UNIT tier: project create's ordering against the org's AE Studio:
// the Ready check comes before the OpenChoreo project, and a repository the
// pod could not create fails the create and compensates the OC project. The
// repo half runs the REAL sourcecontrol RepoService over the in-memory pod
// (aestudiotest.Fake), so the failure travels the path production takes.
package projects

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ocSpy counts the OC project creates and deletes (the compensations).
type ocSpy struct {
	created, deleted int
	trace            *[]string
}

func (o *ocSpy) client() *ocmocks.ProjectClientMock {
	return &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, org string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			o.created++
			if o.trace != nil {
				*o.trace = append(*o.trace, "oc-create")
			}
			return &gen.Project{Name: req.Name, NamespaceName: org}, nil
		},
		DeleteProjectFunc: func(context.Context, string, string) error {
			o.deleted++
			return nil
		},
	}
}

// readyStub is the aeStudioReady port.
type readyStub struct {
	err   error
	trace *[]string
}

func (r readyStub) RequireReady(context.Context, string) error {
	if r.trace != nil {
		*r.trace = append(*r.trace, "ready")
	}
	return r.err
}

func readyErr(err error) readyStub { return readyStub{err: err} }

// emptyRepoRows is a git_repositories table with no rows: it serves the
// lookup a create makes first and accepts the row it inserts.
type emptyRepoRows struct{ sourcecontrol.RepoRepository }

func (emptyRepoRows) GetByOrgAndProjectID(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}

func (emptyRepoRows) Create(context.Context, *sourcecontrol.GitRepository) error { return nil }

type acmeOwner struct{}

func (acmeOwner) GitHubOwner(context.Context, string) (string, error) { return "acme", nil }

func newProjectService(t *testing.T, oc *ocSpy, pod *aestudiotest.Fake, ready readyStub) *Service {
	t.Helper()
	repos := sourcecontrol.NewRepoService(emptyRepoRows{}, pod, pod, acmeOwner{}, "private")
	svc := NewProjectService(oc.client(), repos, &fakeWebhookSvc{}, nil, nil)
	svc.SetAEStudioReady(ready)
	return svc
}

func TestCreateProject_ChecksReadyBeforeOC(t *testing.T) {
	t.Parallel()
	for _, want := range []error{sourcecontrol.ErrAEStudioAbsent, sourcecontrol.ErrAEStudioUnavailable} {
		oc := &ocSpy{}
		svc := newProjectService(t, oc, aestudiotest.New(), readyErr(want))
		_, err := svc.CreateProject(context.Background(), "default", &gen.CreateProjectRequest{Name: "p"})
		if !errors.Is(err, want) || oc.created != 0 {
			t.Fatalf("err=%v created=%d, want %v and no OC project", err, oc.created, want)
		}
	}
}

func TestCreateProject_ReadyCheckRunsFirst(t *testing.T) {
	t.Parallel()
	var trace []string
	oc := &ocSpy{trace: &trace}
	svc := newProjectService(t, oc, aestudiotest.New(), readyStub{trace: &trace})
	if _, err := svc.CreateProject(context.Background(), "default", &gen.CreateProjectRequest{Name: "p"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(trace) < 2 || trace[0] != "ready" || trace[1] != "oc-create" {
		t.Fatalf("order = %v, want ready before oc-create", trace)
	}
}

func TestCreateProject_RepoFailureCompensates(t *testing.T) {
	t.Parallel()
	oc := &ocSpy{}
	f := aestudiotest.New()
	f.FailOp(aestudiotest.OpCreateRepo, &sourcecontrol.HTTPStatusError{StatusCode: 502})
	svc := newProjectService(t, oc, f, readyErr(nil))
	if _, err := svc.CreateProject(context.Background(), "default", &gen.CreateProjectRequest{Name: "p"}); err == nil {
		t.Fatal("a failed repo create must fail the project create")
	}
	if oc.deleted != 1 {
		t.Fatalf("compensations = %d, want 1", oc.deleted)
	}
}

// A service with no Ready check wired creates as before (the port is
// optional, like the other setters).
func TestCreateProject_NoReadyCheckWiredStillCreates(t *testing.T) {
	t.Parallel()
	oc := &ocSpy{}
	repos := &fakeRepoSvc{CreateRepoFunc: func(context.Context, string, string, string, string) (*sourcecontrol.GitRepository, error) {
		return &sourcecontrol.GitRepository{Status: "ready"}, nil
	}}
	svc := NewProjectService(oc.client(), repos, &fakeWebhookSvc{}, nil, nil)
	if _, err := svc.CreateProject(context.Background(), "default", &gen.CreateProjectRequest{Name: "p"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if oc.created != 1 {
		t.Fatalf("created = %d, want 1", oc.created)
	}
}

// A create cut short by its client (or a gateway timeout) still compensates:
// the OC delete runs on the request's values, not its cancellation.
func TestCreateProject_CompensatesOnACancelledRequest(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	deleted := 0
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, org string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			return &gen.Project{Name: req.Name, NamespaceName: org}, nil
		},
		DeleteProjectFunc: func(c context.Context, _, _ string) error {
			if c.Err() != nil {
				return c.Err()
			}
			if _, ok := c.Deadline(); !ok {
				t.Error("the compensation must be bounded")
			}
			deleted++
			return nil
		},
	}
	repos := &fakeRepoSvc{CreateRepoFunc: func(context.Context, string, string, string, string) (*sourcecontrol.GitRepository, error) {
		cancel() // the client went away while the pod was creating the repo
		return nil, context.Canceled
	}}
	svc := NewProjectService(oc, repos, &fakeWebhookSvc{}, nil, nil)
	if _, err := svc.CreateProject(ctx, "default", &gen.CreateProjectRequest{Name: "p"}); err == nil {
		t.Fatal("want the create to fail")
	}
	if deleted != 1 {
		t.Fatalf("compensations = %d, want 1", deleted)
	}
}

// The delete marks the repo row before the OC delete, so no sweep lists the
// project and no hook id can land on its row while it is torn down; a delete
// OpenChoreo refuses puts the mark back.
func TestDeleteProject_MarksTheRowFirstAndUnmarksOnARefusedDelete(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		ocErr error
		want  []string
	}{
		{"deleted", nil, []string{"begin"}},
		{"refused", errors.New("openchoreo down"), []string{"begin", "abort"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var order []string
			repos := &fakeRepoSvc{DeleteRepoFunc: func(context.Context, string, string) error { return nil }}
			oc := &ocmocks.ProjectClientMock{DeleteProjectFunc: func(context.Context, string, string) error {
				order = append(order, "oc:"+strings.Join(repos.marks, ","))
				return tc.ocErr
			}}
			svc := NewProjectService(oc, repos, nil, nil, &fakeExecs{})
			_ = svc.DeleteProject(context.Background(), "default", "p")
			if len(order) != 1 || order[0] != "oc:begin" {
				t.Fatalf("OC delete saw marks %v, want the row marked first", order)
			}
			if strings.Join(repos.marks, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("marks = %v, want %v", repos.marks, tc.want)
			}
		})
	}
}

// A create that meets the repo row of a delete which stopped after the OC
// project went finishes that delete's teardown (the post-OC half: runs,
// webhook, repo row, executions, run ledger) and then creates. The old delete
// cannot be re-run from the console: its OC project is gone and the new one
// is being created under the same name.
func TestCreateProject_FinishesALeftoverDeleteThenCreates(t *testing.T) {
	t.Parallel()
	trace := &deleteTrace{}
	oc := &ocSpy{}
	creates := 0
	repos := &fakeRepoSvc{
		CreateRepoFunc: func(context.Context, string, string, string, string) (*sourcecontrol.GitRepository, error) {
			creates++
			trace.steps = append(trace.steps, "create-repo")
			if creates == 1 {
				return nil, sourcecontrol.ErrRepoDeletePending
			}
			return &sourcecontrol.GitRepository{Status: sourcecontrol.RepoStatusReady}, nil
		},
		DeleteRepoFunc: func(context.Context, string, string) error {
			trace.steps = append(trace.steps, "repo")
			return nil
		},
	}
	webhooks := &fakeWebhookSvc{trace: trace}
	execs := &fakeExecs{DeleteByProjectFunc: func(context.Context, string, string) error {
		trace.steps = append(trace.steps, "execs")
		return nil
	}}
	abandoner := &fakeRunAbandoner{trace: trace}
	svc := NewProjectService(oc.client(), repos, webhooks, nil, execs)
	svc.SetStageSources(tracingRunRows{trace: trace}, fakeBindingsReader{})
	svc.SetRunAbandoner(abandoner)

	got, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{Name: "web"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got == nil || got.Name != "web" {
		t.Fatalf("project = %+v, want the new project web", got)
	}
	if oc.created != 1 || oc.deleted != 0 {
		t.Fatalf("oc created=%d deleted=%d, want 1 and no compensation", oc.created, oc.deleted)
	}
	if abandoner.args != [2]string{"acme", "web"} || webhooks.unregisterArgs != [2]string{"acme", "web"} ||
		execs.deleteArgs != [2]string{"acme", "web"} {
		t.Fatalf("teardown addressed abandon=%v webhook=%v execs=%v, want (acme,web)",
			abandoner.args, webhooks.unregisterArgs, execs.deleteArgs)
	}
	assertTeardownOrder(t, trace,
		"create-repo", "abandon", "webhook", "repo", "execs", "purge", "create-repo")
}

// A leftover delete the teardown cannot clear (the row is still `deleting`
// after it) is not retried again: the create compensates its OC project and
// returns ErrRepoDeletePending, as it did before the teardown existed.
func TestCreateProject_LeftoverDeleteThatPersistsCompensates(t *testing.T) {
	t.Parallel()
	oc := &ocSpy{}
	creates := 0
	repos := &fakeRepoSvc{
		CreateRepoFunc: func(context.Context, string, string, string, string) (*sourcecontrol.GitRepository, error) {
			creates++
			return nil, sourcecontrol.ErrRepoDeletePending
		},
		DeleteRepoFunc: func(context.Context, string, string) error { return errors.New("pod refused the trash") },
	}
	svc := NewProjectService(oc.client(), repos, &fakeWebhookSvc{}, nil, &fakeExecs{})

	_, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{Name: "web"})
	if !errors.Is(err, sourcecontrol.ErrRepoDeletePending) {
		t.Fatalf("err = %v, want ErrRepoDeletePending", err)
	}
	if creates != 2 {
		t.Fatalf("CreateRepo calls = %d, want 2 (one retry after the teardown)", creates)
	}
	if oc.deleted != 1 {
		t.Fatalf("compensations = %d, want 1", oc.deleted)
	}
}
