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

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// fakeCells records the bindings CreateProject asked for.
type fakeCells struct {
	envs      []string
	envsErr   error
	bindErr   error
	pipelines []string          // pipeline names PipelineEnvironments was called with
	bound     map[string]string // project -> environment, last write wins per env key
}

func (f *fakeCells) PipelineEnvironments(_ context.Context, _, pipelineName string) ([]string, error) {
	f.pipelines = append(f.pipelines, pipelineName)
	return f.envs, f.envsErr
}

func (f *fakeCells) EnsureProjectReleaseBinding(_ context.Context, _, projectName, environment string) error {
	if f.bindErr != nil {
		return f.bindErr
	}
	if f.bound == nil {
		f.bound = map[string]string{}
	}
	f.bound[environment] = projectName
	return nil
}

func createdProjectOC(deploymentPipeline string) *mocks.ProjectClientMock {
	return &mocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, orgName string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			return &gen.Project{
				Name:               req.Name,
				NamespaceName:      orgName,
				DeploymentPipeline: deploymentPipeline,
			}, nil
		},
		DeleteProjectFunc: func(context.Context, string, string) error { return nil },
	}
}

// A created project must get one ProjectReleaseBinding per environment its
// pipeline promotes through — that binding is what materializes the cell
// namespace on OpenChoreo 1.2.0.
func TestCreateProject_BindsEveryPipelineEnvironment(t *testing.T) {
	t.Parallel()
	oc := createdProjectOC("default")
	cells := &fakeCells{envs: []string{"development", "staging"}}
	svc := NewProjectService(oc, nil, nil, nil, nil)
	svc.SetProjectCellProvisioner(cells)

	if _, err := svc.CreateProject(context.Background(), "acme",
		&gen.CreateProjectRequest{Name: "shop"}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	if got := cells.bound["development"]; got != "shop" {
		t.Errorf("development binding: got %q, want %q", got, "shop")
	}
	if got := cells.bound["staging"]; got != "shop" {
		t.Errorf("staging binding: got %q, want %q", got, "shop")
	}
	// The pipeline the project actually resolved to, not a hardcoded name.
	if len(cells.pipelines) != 1 || cells.pipelines[0] != "default" {
		t.Errorf("pipeline lookups: got %v, want [default]", cells.pipelines)
	}
}

// A binding failure leaves an undeployable project behind, and retrying the
// create cannot fix it (OpenChoreo answers 409). So the project is compensated
// away and the error surfaces.
func TestCreateProject_CompensatesWhenBindingFails(t *testing.T) {
	t.Parallel()
	oc := createdProjectOC("default")
	deleted := ""
	oc.DeleteProjectFunc = func(_ context.Context, _, projectName string) error {
		deleted = projectName
		return nil
	}
	cells := &fakeCells{envs: []string{"development"}, bindErr: errors.New("boom")}
	svc := NewProjectService(oc, nil, nil, nil, nil)
	svc.SetProjectCellProvisioner(cells)

	if _, err := svc.CreateProject(context.Background(), "acme",
		&gen.CreateProjectRequest{Name: "shop"}); err == nil {
		t.Fatal("CreateProject: want error, got nil")
	}
	if deleted != "shop" {
		t.Errorf("compensating delete: got %q, want %q", deleted, "shop")
	}
}

// A pipeline that resolves to no environments is a misconfiguration, not an
// empty success: accepting it would produce exactly the undeployable project
// this path exists to prevent.
func TestCreateProject_RejectsPipelineWithNoEnvironments(t *testing.T) {
	t.Parallel()
	oc := createdProjectOC("default")
	cells := &fakeCells{envs: nil}
	svc := NewProjectService(oc, nil, nil, nil, nil)
	svc.SetProjectCellProvisioner(cells)

	if _, err := svc.CreateProject(context.Background(), "acme",
		&gen.CreateProjectRequest{Name: "shop"}); err == nil {
		t.Fatal("CreateProject: want error, got nil")
	}
}

// An unresolvable pipeline name never reaches OpenChoreo — there is nothing to
// look up, and the project cannot be bound to anything.
func TestCreateProject_RejectsProjectWithNoPipeline(t *testing.T) {
	t.Parallel()
	oc := createdProjectOC("")
	cells := &fakeCells{envs: []string{"development"}}
	svc := NewProjectService(oc, nil, nil, nil, nil)
	svc.SetProjectCellProvisioner(cells)

	if _, err := svc.CreateProject(context.Background(), "acme",
		&gen.CreateProjectRequest{Name: "shop"}); err == nil {
		t.Fatal("CreateProject: want error, got nil")
	}
	if len(cells.pipelines) != 0 {
		t.Errorf("pipeline lookups: got %v, want none", cells.pipelines)
	}
}
