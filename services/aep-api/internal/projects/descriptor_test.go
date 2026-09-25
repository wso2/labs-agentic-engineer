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
	"testing"

	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

type fakeDescriptorWriter struct {
	calls int
	org   string
	proj  string
	name  string
	idea  string
	err   error
}

func (f *fakeDescriptorWriter) WriteDescriptor(_ context.Context, orgID, projectID, name, idea string) error {
	f.calls++
	f.org, f.proj, f.name, f.idea = orgID, projectID, name, idea
	return f.err
}

func createSvcWithDescriptor(t *testing.T, w descriptorWriter) *Service {
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
	svc.SetDescriptorWriter(w)
	return svc
}

// The captured idea is stamped into the repo at create — the ONLY durable copy
// (the console's localStorage copy dies with the browser) and the source the
// /start flow reads it back from.
func TestCreateProject_WritesDescriptorWithPrompt(t *testing.T) {
	t.Parallel()
	w := &fakeDescriptorWriter{}
	svc := createSvcWithDescriptor(t, w)

	_, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{
		Name:   "expense-tracker",
		Prompt: "an expense claim tracker for 200 people",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.calls != 1 {
		t.Fatalf("descriptor writes = %d, want 1", w.calls)
	}
	if w.org != "acme" || w.proj != "expense-tracker" || w.name != "expense-tracker" {
		t.Fatalf("descriptor target = (%q,%q,%q)", w.org, w.proj, w.name)
	}
	if w.idea != "an expense claim tracker for 200 people" {
		t.Fatalf("idea = %q", w.idea)
	}
}

// The descriptor is also the MARKER for "this is an Agentic Engineer project",
// so it is written even when the user supplied no prompt — the idea is simply
// empty and /start asks for it.
func TestCreateProject_WritesDescriptorWithoutPrompt(t *testing.T) {
	t.Parallel()
	w := &fakeDescriptorWriter{}
	svc := createSvcWithDescriptor(t, w)

	if _, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{Name: "web"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.calls != 1 {
		t.Fatalf("descriptor writes = %d, want 1 (the marker is written regardless)", w.calls)
	}
	if w.idea != "" {
		t.Fatalf("idea = %q, want empty", w.idea)
	}
}

// Best-effort, exactly like every other post-create step: a failed descriptor
// write is logged and the project is still returned. Failing here would destroy
// a creation the user already committed to, to protect a value /start recovers
// by asking.
func TestCreateProject_DescriptorWriteFailureIsBestEffort(t *testing.T) {
	t.Parallel()
	w := &fakeDescriptorWriter{err: errors.New("github write blew up")}
	svc := createSvcWithDescriptor(t, w)

	p, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{
		Name:   "web",
		Prompt: "an idea",
	})
	if err != nil {
		t.Fatalf("a failed descriptor write must not fail the create: %v", err)
	}
	if p == nil || p.Name != "web" {
		t.Fatalf("project = %+v, want the created project back", p)
	}
}

// An unwired writer (degraded boot / older composition root) is a documented
// no-op, matching how the other optional collaborators behave.
func TestCreateProject_NilDescriptorWriterIsNoOp(t *testing.T) {
	t.Parallel()
	svc := createSvcWithDescriptor(t, nil)
	if _, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{Name: "web"}); err != nil {
		t.Fatalf("create with no descriptor writer: %v", err)
	}
}

// A project created with no display name of its own is given its NAME as one.
//
// The value becomes the OpenChoreo Project's `openchoreo.dev/display-name`, and
// Agent Manager projects OpenChoreo's Projects into its own catalogue reading
// that annotation. Left empty, AMP's console falls back to the project's
// deployment PIPELINE name, so every AEP project appeared there as "default
// deployment pipeline" — one name in the AEP console and another in AMP's.
func TestCreateProject_DefaultsDisplayNameToTheName(t *testing.T) {
	t.Parallel()
	var got *gen.CreateProjectRequest
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, org string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			got = req
			return &gen.Project{Name: req.Name, NamespaceName: org}, nil
		},
	}
	svc := NewProjectService(oc, &fakeRepoSvc{
		CreateRepoFunc: func(_ context.Context, _, _, _, _ string) (*sourcecontrol.GitRepository, error) {
			return &sourcecontrol.GitRepository{Status: "ready"}, nil
		},
	}, &fakeWebhookSvc{}, nil, nil)

	if _, err := svc.CreateProject(context.Background(), "acme",
		&gen.CreateProjectRequest{Name: "expense-tracker"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got == nil {
		t.Fatal("no create request reached the OpenChoreo client")
	}
	if got.DisplayName != "expense-tracker" {
		t.Errorf("displayName = %q, want the project name", got.DisplayName)
	}
}

// A display name the caller DID supply is never overwritten — the default is a
// fallback, not a policy.
func TestCreateProject_KeepsACallersDisplayName(t *testing.T) {
	t.Parallel()
	var got *gen.CreateProjectRequest
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, org string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			got = req
			return &gen.Project{Name: req.Name, NamespaceName: org}, nil
		},
	}
	svc := NewProjectService(oc, &fakeRepoSvc{
		CreateRepoFunc: func(_ context.Context, _, _, _, _ string) (*sourcecontrol.GitRepository, error) {
			return &sourcecontrol.GitRepository{Status: "ready"}, nil
		},
	}, &fakeWebhookSvc{}, nil, nil)

	if _, err := svc.CreateProject(context.Background(), "acme", &gen.CreateProjectRequest{
		Name:        "expense-tracker",
		DisplayName: "Expense Tracker",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.DisplayName != "Expense Tracker" {
		t.Errorf("displayName = %q, want the caller's own", got.DisplayName)
	}
}
