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

package component

import (
	"context"
	"testing"

	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts/artifactstest"
	"github.com/wso2/aep/aep-api/models"
)

// ensureRepoSvc is a minimal gitrepo.RepoService — only GetRepo is exercised by
// EnsureComponent; the rest panic if reached.
type ensureRepoSvc struct{ repo *models.GitRepository }

func (r ensureRepoSvc) ListByOrg(context.Context, string) ([]models.GitRepository, error) {
	panic("ensureRepoSvc: ListByOrg not expected")
}
func (r ensureRepoSvc) GetRepo(context.Context, string, string) (*models.GitRepository, error) {
	return r.repo, nil
}
func (ensureRepoSvc) CreateRepo(context.Context, string, string, string, string) (*models.GitRepository, error) {
	panic("CreateRepo not expected")
}
func (ensureRepoSvc) EnsureBareRepo(context.Context, string, string, string) (*models.GitRepository, error) {
	panic("EnsureBareRepo not expected")
}
func (ensureRepoSvc) SetWebhookID(context.Context, string, string, int64) error {
	panic("SetWebhookID not expected")
}
func (ensureRepoSvc) DeleteRepo(context.Context, string, string) error {
	panic("DeleteRepo not expected")
}

func TestEnsureComponent_ProvisionsOCComponentFromDesign(t *testing.T) {
	var captured *models.CreateComponentRequest
	oc := &ocmocks.ComponentClientMock{
		CreateComponentFunc: func(_ context.Context, _, _ string, req *models.CreateComponentRequest) (*models.Component, error) {
			captured = req
			return &models.Component{Name: req.Name}, nil
		},
	}
	// A design with one service component named "order-service".
	files := map[string]string{
		artifacts.DesignRootFile: "# Overview\n",
		"components/order-service/design.json": "{\n" +
			"  \"name\": \"order-service\",\n" +
			"  \"type\": \"service\",\n" +
			"  \"description\": \"body\",\n" +
			"  \"dependencies\": []\n" +
			"}\n",
	}
	store := artifacts.NewArtifactStore(&artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
	})
	repo := &models.GitRepository{RepoURL: "https://github.com/acme/widgets", DefaultBranch: "main"}
	svc := NewComponentService(oc, nil, store, ensureRepoSvc{repo: repo}, nil)

	if err := svc.EnsureComponent(context.Background(), "acme", "widgets", "order-service"); err != nil {
		t.Fatalf("EnsureComponent: %v", err)
	}
	if captured == nil {
		t.Fatal("EnsureComponent must call CreateComponent")
	}
	if captured.Name != "order-service" {
		t.Errorf("component name = %q, want the k8s slug order-service", captured.Name)
	}
	if captured.Type != "deployment/service" {
		t.Errorf("component type = %q, want deployment/service", captured.Type)
	}
	// AutoBuild=false (builds are BFF-driven at the merge SHA), AutoDeploy=true.
	if captured.AutoBuild || !captured.AutoDeploy {
		t.Errorf("autoBuild/autoDeploy = %v/%v, want false/true", captured.AutoBuild, captured.AutoDeploy)
	}
	wf := captured.Workflow
	if wf == nil || wf.Name != "dockerfile-builder" || wf.Kind != "ClusterWorkflow" {
		t.Fatalf("workflow wrong: %+v", wf)
	}
	if wf.Parameters == nil || wf.Parameters.Repository == nil ||
		wf.Parameters.Repository.URL != "https://github.com/acme/widgets" ||
		wf.Parameters.Repository.Revision == nil || wf.Parameters.Repository.Revision.Branch != "main" {
		t.Fatalf("workflow repository wrong: %+v", wf.Parameters)
	}
	// No repo secretRef — build credentials are pre-staged per WorkflowRun.
	if wf.Parameters.Repository.SecretRef != "" {
		t.Errorf("repository.secretRef must be empty, got %q", wf.Parameters.Repository.SecretRef)
	}
}

// TestEnsureComponent_WebAppKind_UsesWebApplicationEntrypoint is the
// consumer-level regression for the component-kind vocabulary drift bug: a
// design.json carrying the canonical "web-application" type (OpenChoreo's own
// term, models.ComponentTypeWebApplication) must provision an OC Component
// with the deployment/web-application entrypoint, not silently fall back to a
// plain service (which caused shared-host routing and a missing runtime
// config for the deployed SPA).
func TestEnsureComponent_WebAppKind_UsesWebApplicationEntrypoint(t *testing.T) {
	var captured *models.CreateComponentRequest
	oc := &ocmocks.ComponentClientMock{
		CreateComponentFunc: func(_ context.Context, _, _ string, req *models.CreateComponentRequest) (*models.Component, error) {
			captured = req
			return &models.Component{Name: req.Name}, nil
		},
	}
	files := map[string]string{
		artifacts.DesignRootFile: "# Overview\n",
		"components/web-ui/design.json": "{\n" +
			"  \"name\": \"web-ui\",\n" +
			"  \"type\": \"web-application\",\n" +
			"  \"description\": \"body\",\n" +
			"  \"dependencies\": []\n" +
			"}\n",
	}
	store := artifacts.NewArtifactStore(&artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
	})
	repo := &models.GitRepository{RepoURL: "https://github.com/acme/widgets", DefaultBranch: "main"}
	svc := NewComponentService(oc, nil, store, ensureRepoSvc{repo: repo}, nil)

	if err := svc.EnsureComponent(context.Background(), "acme", "widgets", "web-ui"); err != nil {
		t.Fatalf("EnsureComponent: %v", err)
	}
	if captured == nil {
		t.Fatal("EnsureComponent must call CreateComponent")
	}
	if captured.Type != "deployment/web-application" {
		t.Errorf("component type = %q, want deployment/web-application for a %q-typed design component", captured.Type, "web-application")
	}
}

func TestEnsureComponent_DesignMissingComponent_Errors(t *testing.T) {
	oc := &ocmocks.ComponentClientMock{
		CreateComponentFunc: func(context.Context, string, string, *models.CreateComponentRequest) (*models.Component, error) {
			t.Error("CreateComponent must not be called when the component is absent from the design")
			return nil, nil
		},
	}
	files := map[string]string{artifacts.DesignRootFile: "# Overview\n"} // no component dirs
	store := artifacts.NewArtifactStore(&artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
	})
	svc := NewComponentService(oc, nil, store, ensureRepoSvc{repo: &models.GitRepository{RepoURL: "u"}}, nil)

	if err := svc.EnsureComponent(context.Background(), "acme", "widgets", "ghost"); err == nil {
		t.Fatal("a component absent from the design must error (no CR to build)")
	}
}

func TestEnsureComponent_NoStoreOrRepo_Errors(t *testing.T) {
	oc := &ocmocks.ComponentClientMock{}
	// No artifact store.
	if err := NewComponentService(oc, nil, nil, ensureRepoSvc{}, nil).
		EnsureComponent(context.Background(), "a", "p", "c"); err == nil {
		t.Error("nil artifact store must error")
	}
	// No repo service.
	store := artifacts.NewArtifactStore(&artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return map[string]string{artifacts.DesignRootFile: "# O\n"}, nil
		},
	})
	if err := NewComponentService(oc, nil, store, nil, nil).
		EnsureComponent(context.Background(), "a", "p", "c"); err == nil {
		t.Error("nil repo service must error")
	}
}
