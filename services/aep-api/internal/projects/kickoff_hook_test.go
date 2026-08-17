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

// The BE-side /start kickoff hook (#485): create fires the specKickoff port —
// async, for every project, best-effort. Exactly-once and every turn-side
// guard live in the spec domain; these tests pin only what THIS domain
// decides: when the port fires and that its failure never surfaces.

import (
	"context"
	"errors"
	"testing"
	"time"

	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

type fakeSpecKickoff struct {
	calls chan [2]string // (org, project) per call
	err   error
	// The status read's side of the port: what Kickoff answers, and how many
	// times it was asked (the status tests assert it is skipped once a spec
	// exists).
	state      spec.KickoffState
	stateErr   error
	stateCalls int
}

func newFakeSpecKickoff(err error) *fakeSpecKickoff {
	return &fakeSpecKickoff{calls: make(chan [2]string, 4), err: err}
}

func (f *fakeSpecKickoff) KickoffSpec(_ context.Context, orgID, projectID string) error {
	f.calls <- [2]string{orgID, projectID}
	return f.err
}

func (f *fakeSpecKickoff) Kickoff(context.Context, string, string) (spec.KickoffState, error) {
	f.stateCalls++
	return f.state, f.stateErr
}

func createSvcWithKickoff(t *testing.T, k specKickoff) *Service {
	t.Helper()
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, org string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			return &gen.Project{Name: req.Name, NamespaceName: org}, nil
		},
	}
	repoSvc := &fakeRepoSvc{
		CreateRepoFunc: func(_ context.Context, _, _, _, _ string) (*sourcecontrol.GitRepository, error) {
			return &sourcecontrol.GitRepository{Status: "ready"}, nil
		},
	}
	svc := NewProjectService(oc, repoSvc, &fakeWebhookSvc{}, nil, nil)
	svc.SetSpecKickoff(k)
	return svc
}

func TestCreateProject_KicksOffSpecWhenPromptExists(t *testing.T) {
	t.Parallel()
	kickoff := newFakeSpecKickoff(nil)
	svc := createSvcWithKickoff(t, kickoff)

	_, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{
		Name:   "recipe-share",
		Prompt: "a simple recipe sharing site",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// The kickoff is fired on a detached goroutine (it outlives the request
	// while the repo provisions) — wait for the call, bounded.
	select {
	case got := <-kickoff.calls:
		if got != [2]string{"acme", "recipe-share"} {
			t.Fatalf("kickoff target = %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("kickoff never fired for a prompt-ful create")
	}
}

// A prompt-less project ALSO gets its turn: `/start` with no captured idea
// opens by asking what the user is building (skills/start), which is the first
// beat of the same conversation — not a project that sits inert behind a CTA.
func TestCreateProject_KicksOffSpecWithoutPrompt(t *testing.T) {
	t.Parallel()
	for _, prompt := range []string{"", "   \n\t"} {
		kickoff := newFakeSpecKickoff(nil)
		svc := createSvcWithKickoff(t, kickoff)

		_, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{
			Name:   "empty-shell",
			Prompt: prompt,
		})
		if err != nil {
			t.Fatalf("create(prompt=%q): %v", prompt, err)
		}
		select {
		case got := <-kickoff.calls:
			if got != [2]string{"acme", "empty-shell"} {
				t.Fatalf("kickoff target = %v", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("kickoff never fired for a create with prompt=%q", prompt)
		}
	}
}

// Best-effort per the domain invariant: a failing kickoff must never fail (or
// delay) a creation the user already committed to.
func TestCreateProject_KickoffFailureDoesNotFailCreate(t *testing.T) {
	t.Parallel()
	kickoff := newFakeSpecKickoff(errors.New("agents service down"))
	svc := createSvcWithKickoff(t, kickoff)

	project, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{
		Name:   "resilient",
		Prompt: "an idea",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if project == nil || project.Name != "resilient" {
		t.Fatalf("project = %+v", project)
	}
	select {
	case <-kickoff.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("kickoff never fired")
	}
}

// A nil port is the documented no-op — creation must not depend on the wire.
func TestCreateProject_NilKickoffIsANoOp(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, org string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			return &gen.Project{Name: req.Name, NamespaceName: org}, nil
		},
	}
	repoSvc := &fakeRepoSvc{
		CreateRepoFunc: func(_ context.Context, _, _, _, _ string) (*sourcecontrol.GitRepository, error) {
			return &sourcecontrol.GitRepository{Status: "ready"}, nil
		},
	}
	svc := NewProjectService(oc, repoSvc, &fakeWebhookSvc{}, nil, nil)
	if _, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{
		Name:   "unwired",
		Prompt: "an idea",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
}
