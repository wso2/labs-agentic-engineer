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

package project

import (
	"context"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/humakit"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/tenant"
	"github.com/wso2/asdlc/asdlc-service/models"
)

type fakeProjectService struct{}

func (fakeProjectService) ListProjects(_ context.Context, _ string, _ int, _ string) (*models.ProjectList, error) {
	return &models.ProjectList{Items: []models.Project{{Name: "demo"}}}, nil
}
func (fakeProjectService) GetProject(_ context.Context, _, name string) (*models.Project, error) {
	return &models.Project{Name: name}, nil
}
func (fakeProjectService) CreateProject(_ context.Context, _ string, req *models.CreateProjectRequest) (*models.Project, error) {
	return &models.Project{Name: req.Name}, nil
}
func (fakeProjectService) DeleteProject(_ context.Context, _, _ string) error { return nil }
func (fakeProjectService) GetProjectStatus(_ context.Context, _, _ string) (*models.ProjectStatus, error) {
	return &models.ProjectStatus{Phase: "prompt"}, nil
}

// TestRegisterProject_SpecAndWiring proves the Huma registration does not panic,
// the OpenAPI spec carries the project operations, and a request flows through
// the typed handler. Gate is set to log mode so the no-claims test request is
// not rejected by the tenant resolver (the gate itself is unit-tested in
// humakit).
func TestRegisterProject_SpecAndWiring(t *testing.T) {
	humakit.SetGateMode(tenant.GateModeLog)
	t.Cleanup(func() { humakit.SetGateMode(tenant.GateModeEnforce) })

	_, api := humatest.New(t)
	RegisterProject(api, fakeProjectService{})

	spec, err := api.OpenAPI().YAML()
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	for _, want := range []string{"list-projects", "create-project", "get-project", "delete-project", "get-project-status", "/api/v1/organizations/{orgHandle}/projects"} {
		if !strings.Contains(string(spec), want) {
			t.Fatalf("spec missing %q", want)
		}
	}

	resp := api.Get("/api/v1/organizations/acme/projects")
	if resp.Code != 200 {
		t.Fatalf("list projects: got %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "demo") {
		t.Fatalf("list projects body missing item: %s", resp.Body.String())
	}
}
