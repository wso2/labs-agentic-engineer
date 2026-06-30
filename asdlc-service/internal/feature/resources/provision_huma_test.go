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

package resources

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// ---- fakes ------------------------------------------------------------------

type fakeDesignStore struct {
	design *artifacts.DesignFile
	err    error
}

func (f *fakeDesignStore) ReadDesign(_ context.Context, _, _ string) (*artifacts.DesignFile, error) {
	return f.design, f.err
}

type fakeProvisioner struct {
	called int
	result *ProvisionResult
	err    error
}

func (f *fakeProvisioner) Provision(_ context.Context, _, _, _, _ string, _ map[string]string, _ []string) (*ProvisionResult, error) {
	f.called++
	return f.result, f.err
}

type fakeTaskRepo struct {
	tasks   []models.ComponentTask
	updated *models.ComponentTask
}

func (f *fakeTaskRepo) ListByProjectID(_ context.Context, _, _ string) ([]models.ComponentTask, error) {
	return f.tasks, nil
}
func (f *fakeTaskRepo) Update(_ context.Context, t *models.ComponentTask) error {
	cp := *t
	f.updated = &cp
	return nil
}

// ---- helpers ----------------------------------------------------------------

func designWithPlatformResource(compName, depName, resourceType string) *artifacts.DesignFile {
	return &artifacts.DesignFile{
		Components: []models.DesignComponent{
			{
				Name: compName,
				Dependencies: []models.Dependency{
					{
						Kind:         models.DependencyKindPlatformResource,
						Name:         depName,
						ResourceType: resourceType,
					},
				},
			},
		},
	}
}

// ---- tests ------------------------------------------------------------------

// TestResourceService_Provision_SetsBuildingNotDeployed is the TDD RED test:
// calling Provision marks the matching resource-provisioning task to `building`
// (the in-flight marker the watcher sweeps), NOT `deployed`. It calls
// ResourceProvisioner.Provision exactly once and does NOT call redispatch.
func TestResourceService_Provision_SetsBuildingNotDeployed(t *testing.T) {
	prov := &fakeProvisioner{result: &ProvisionResult{ResourceName: "proj-db", BindingByEnv: map[string]string{"development": "proj-db-development"}}}
	store := &fakeDesignStore{
		design: designWithPlatformResource("api", "db", "postgres-cnpg"),
	}
	taskRepo := &fakeTaskRepo{
		tasks: []models.ComponentTask{
			{Type: models.TaskTypeResourceProvisioning, ResourceName: "db", Status: string(models.TaskStatusPending)},
			{Type: models.TaskTypeComponent, ComponentName: "api", Status: string(models.TaskStatusOnHold)},
		},
	}

	svc := NewResourceService(store, prov, taskRepo)

	err := svc.Provision(context.Background(), "default", "proj", "api", "db",
		map[string]string{"storage": "10Gi"}, []string{"development"})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	// Provisioner must be called exactly once.
	if prov.called != 1 {
		t.Errorf("Provisioner.Provision called %d times, want 1", prov.called)
	}

	// Task must be set to `building` (the in-flight marker), NOT `deployed`.
	if taskRepo.updated == nil {
		t.Fatal("task was not updated")
	}
	if taskRepo.updated.Status != string(models.TaskStatusBuilding) {
		t.Errorf("task status = %q, want %q", taskRepo.updated.Status, models.TaskStatusBuilding)
	}
	if taskRepo.updated.Status == string(models.TaskStatusDeployed) {
		t.Error("task must NOT be set to deployed — that is the watcher's job (async)")
	}
}

// TestResourceService_Provision_MissingDep returns an error when the dependency
// is not found in the component's design.
func TestResourceService_Provision_MissingDep(t *testing.T) {
	store := &fakeDesignStore{
		design: designWithPlatformResource("api", "cache", "redis"),
	}
	svc := NewResourceService(store, &fakeProvisioner{}, &fakeTaskRepo{})

	err := svc.Provision(context.Background(), "default", "proj", "api", "db",
		nil, []string{"development"})
	if err == nil {
		t.Fatal("expected error for missing dep, got nil")
	}
	if !errors.Is(err, ErrDepNotFound) {
		t.Errorf("expected ErrDepNotFound, got %v", err)
	}
}

// TestResourceService_Provision_WrongKind returns an error when the dep exists
// but is not a platform-resource kind.
func TestResourceService_Provision_WrongKind(t *testing.T) {
	store := &fakeDesignStore{
		design: &artifacts.DesignFile{
			Components: []models.DesignComponent{
				{
					Name: "api",
					Dependencies: []models.Dependency{
						{Kind: models.DependencyKindExternal, Name: "db"},
					},
				},
			},
		},
	}
	svc := NewResourceService(store, &fakeProvisioner{}, &fakeTaskRepo{})

	err := svc.Provision(context.Background(), "default", "proj", "api", "db",
		nil, []string{"development"})
	if err == nil {
		t.Fatal("expected error for wrong dep kind, got nil")
	}
	if !errors.Is(err, ErrDepNotFound) {
		t.Errorf("expected ErrDepNotFound, got %v", err)
	}
}

// TestResourceService_Provision_ProvisionerFail returns ErrProvisionFailed when
// the provisioner returns an error.
func TestResourceService_Provision_ProvisionerFail(t *testing.T) {
	store := &fakeDesignStore{
		design: designWithPlatformResource("api", "db", "postgres-cnpg"),
	}
	prov := &fakeProvisioner{err: errors.New("OC 503")}
	svc := NewResourceService(store, prov, &fakeTaskRepo{})

	err := svc.Provision(context.Background(), "default", "proj", "api", "db",
		nil, []string{"development"})
	if err == nil {
		t.Fatal("expected ErrProvisionFailed, got nil")
	}
	if !errors.Is(err, ErrProvisionFailed) {
		t.Errorf("expected ErrProvisionFailed, got %v", err)
	}
}
