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

package projects

import (
	"context"
	"errors"
	"sync"
	"testing"

	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// fakeKickoff records the create path's kickoff dispatch. Kickoff is
// fire-and-forget with no return, so what a test can observe is exactly what
// production observes: whether it was asked, and for which project.
type fakeKickoff struct {
	mu    sync.Mutex
	calls int
	org   string
	proj  string
}

func (f *fakeKickoff) Kickoff(_ context.Context, orgID, projectID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.org, f.proj = orgID, projectID
}

func (f *fakeKickoff) seen() (int, string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.org, f.proj
}

func createSvcWithKickoff(t *testing.T, k kickoffStarter, w descriptorWriter, repoErr error) *Service {
	t.Helper()
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, org string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			return &gen.Project{Name: req.Name, NamespaceName: org}, nil
		},
		DeleteProjectFunc: func(context.Context, string, string) error { return nil },
	}
	repoSvc := &fakeRepoSvc{
		CreateRepoFunc: func(_ context.Context, _, _, _, _ string) (*sourcecontrol.GitRepository, error) {
			if repoErr != nil {
				return nil, repoErr
			}
			return &sourcecontrol.GitRepository{Status: "ready"}, nil
		},
	}
	svc := NewProjectService(oc, repoSvc, &fakeWebhookSvc{}, nil, nil)
	svc.SetDescriptorWriter(w)
	svc.SetKickoffStarter(k)
	return svc
}

// The journey starts itself (#562): creating a project fires `/start` with no
// client trigger and no user click.
func TestCreateProject_FiresTheKickoff(t *testing.T) {
	t.Parallel()
	k := &fakeKickoff{}
	svc := createSvcWithKickoff(t, k, &fakeDescriptorWriter{}, nil)

	if _, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{
		Name:   "expense-approval",
		Prompt: "employees submit expense claims",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	calls, org, proj := k.seen()
	if calls != 1 {
		t.Fatalf("kickoffs = %d, want 1", calls)
	}
	if org != "acme" || proj != "expense-approval" {
		t.Fatalf("kickoff target = (%q,%q), want (acme,expense-approval)", org, proj)
	}
}

// Reference documents are the primary brief, so a create that says they are
// still coming HOLDS the kickoff — an interview started before they land is
// conducted blind. The references upload fires it instead.
func TestCreateProject_HoldsTheKickoffWhileReferencesAreComing(t *testing.T) {
	t.Parallel()
	k := &fakeKickoff{}
	svc := createSvcWithKickoff(t, k, &fakeDescriptorWriter{}, nil)

	if _, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{
		Name:              "expense-approval",
		Prompt:            "employees submit expense claims",
		ReferencesPending: true,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if calls, _, _ := k.seen(); calls != 0 {
		t.Fatalf("kickoffs = %d, want 0 while the documents are still coming", calls)
	}
}

// The kickoff hangs off the repo branch, after the descriptor commit that
// carries the idea — so a project whose repo never provisioned has nothing to
// read the idea from and nothing to interview against.
func TestCreateProject_NoKickoffWithoutARepo(t *testing.T) {
	t.Parallel()
	k := &fakeKickoff{}
	svc := createSvcWithKickoff(t, k, &fakeDescriptorWriter{}, errors.New("github is down"))

	if _, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{Name: "web"}); err != nil {
		t.Fatalf("a failed repo provision must not fail the create: %v", err)
	}
	if calls, _, _ := k.seen(); calls != 0 {
		t.Fatalf("kickoffs = %d, want 0 when the repo never provisioned", calls)
	}
}

// An unwired starter is a documented no-op, matching every other optional
// collaborator on the create path.
func TestCreateProject_NilKickoffStarterIsNoOp(t *testing.T) {
	t.Parallel()
	svc := createSvcWithKickoff(t, nil, &fakeDescriptorWriter{}, nil)
	if _, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{Name: "web"}); err != nil {
		t.Fatalf("create with no kickoff starter: %v", err)
	}
}

// --- ordering ---------------------------------------------------------------

// Two collaborators sharing one tape, so the ASSERTION is about sequence rather
// than about each having run.
type tape struct {
	mu    sync.Mutex
	steps []string
}

func (t *tape) mark(step string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.steps = append(t.steps, step)
}

func (t *tape) read() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.steps...)
}

type tapedDescriptorWriter struct{ tape *tape }

func (w tapedDescriptorWriter) WriteDescriptor(_ context.Context, _, _, _, _ string) error {
	w.tape.mark("descriptor")
	return nil
}

type tapedKickoff struct{ tape *tape }

func (k tapedKickoff) Kickoff(_ context.Context, _, _ string) { k.tape.mark("kickoff") }

// `/start` reads the captured idea out of the descriptor, so the write has to
// land FIRST. Asserting only that both ran — which is all
// TestCreateProject_FiresTheKickoff does — would pass a reordered create that
// interviews against a descriptor which is not there yet.
func TestCreateProject_WritesTheDescriptorBeforeFiringTheKickoff(t *testing.T) {
	t.Parallel()
	tp := &tape{}
	svc := createSvcWithKickoff(t, tapedKickoff{tape: tp}, tapedDescriptorWriter{tape: tp}, nil)

	if _, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{
		Name:   "expense-approval",
		Prompt: "employees submit expense claims",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	got := tp.read()
	want := []string{"descriptor", "kickoff"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
