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

// Create's half of the server-side spec interview (#485): every new project
// gets one, and a create the user already committed to is never failed by it.
package projects

import (
	"context"
	"errors"
	"testing"
	"time"

	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func kickoffCreateService(t *testing.T, kickoff *fakeSpecKickoff, repoErr error) *Service {
	t.Helper()
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, org string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			return &gen.Project{Name: req.Name, NamespaceName: org}, nil
		},
		DeleteProjectFunc: func(context.Context, string, string) error { return nil },
	}
	repoSvc := &fakeRepoSvc{
		CreateRepoFunc: func(context.Context, string, string, string, string) (*sourcecontrol.GitRepository, error) {
			if repoErr != nil {
				return nil, repoErr
			}
			return &sourcecontrol.GitRepository{Status: "ready"}, nil
		},
	}
	svc := NewProjectService(oc, repoSvc, &fakeWebhookSvc{}, nil, nil)
	svc.SetSpecKickoff(kickoff)
	return svc
}

// A project created WITHOUT a prompt starts the interview too: with no idea
// captured the start skill opens by asking what the user wants to build, so a
// prompt gate here would leave exactly those users staring at a dead panel.
func TestCreateProject_StartsTheSpecInterview(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		prompt string
	}{
		{"with a create prompt", "a workout tracker"},
		{"without one", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			kickoff := &fakeSpecKickoff{started: make(chan struct{})}
			svc := kickoffCreateService(t, kickoff, nil)

			if _, err := svc.CreateProject(context.Background(), "acme",
				&gen.CreateProjectRequest{Name: "web", Prompt: tc.prompt}); err != nil {
				t.Fatalf("create: %v", err)
			}
			select {
			case <-kickoff.started:
			case <-time.After(2 * time.Second):
				t.Fatal("the spec interview was never started")
			}
		})
	}
}

// The kickoff is best-effort like every other post-create step: a create the
// user already committed to must not be rolled back because an interview could
// not start. The failure reaches them as a card with a Retry instead.
func TestCreateProject_KickoffFailureDoesNotFailTheCreate(t *testing.T) {
	t.Parallel()
	kickoff := &fakeSpecKickoff{started: make(chan struct{}), err: errors.New("agents service down")}
	svc := kickoffCreateService(t, kickoff, nil)

	if _, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{Name: "web"}); err != nil {
		t.Fatalf("create must survive a failed kickoff, got %v", err)
	}
	select {
	case <-kickoff.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the spec interview was never attempted")
	}
}

// No repo, no turn: the interview's snapshot is the project's repo, so there is
// nothing to start against and nothing to record.
func TestCreateProject_NoRepoMeansNoKickoff(t *testing.T) {
	t.Parallel()
	kickoff := &fakeSpecKickoff{started: make(chan struct{})}
	svc := kickoffCreateService(t, kickoff, errors.New("github unreachable"))

	if _, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{Name: "web"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	select {
	case <-kickoff.started:
		t.Fatal("started an interview for a project with no repository")
	case <-time.After(200 * time.Millisecond):
	}
}
