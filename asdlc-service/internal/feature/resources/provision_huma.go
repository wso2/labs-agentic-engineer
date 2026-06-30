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
	"fmt"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/humakit"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// Sentinel errors for mapping to HTTP status codes.
var (
	// ErrDepNotFound is returned when the named dependency is absent from the
	// component's design or is not a platform-resource kind.
	ErrDepNotFound = errors.New("resources: platform-resource dependency not found")

	// ErrProvisionFailed is returned when the ResourceProvisioner call fails.
	ErrProvisionFailed = errors.New("resources: provisioner failed")
)

// DesignReader is the slice of ArtifactStore the ResourceService reads.
// *artifacts.ArtifactStore satisfies it.
type DesignReader interface {
	ReadDesign(ctx context.Context, orgID, projectID string) (*artifacts.DesignFile, error)
}

// ResourceTaskStore is the slice of the task repo the ResourceService uses to
// mark the resource-provisioning task in-flight. *repositories.TaskRepository
// satisfies it.
type ResourceTaskStore interface {
	ListByProjectID(ctx context.Context, orgID, projectID string) ([]models.ComponentTask, error)
	Update(ctx context.Context, t *models.ComponentTask) error
}

// ResourceService handles the async provision flow for platform-resource
// dependencies:
//  1. Reads the component's design to find the dep's resourceType.
//  2. Calls the ResourceProvisioner to author the OC Resource model.
//  3. Marks the resource-provisioning task in-flight (status = building).
//     It does NOT wait for readiness — the watcher (A5) observes that.
//     It does NOT call redispatch — the watcher cascades on readiness.
type ResourceService struct {
	store    DesignReader
	prov     ResourceProvisioner
	taskRepo ResourceTaskStore
}

func NewResourceService(store DesignReader, prov ResourceProvisioner, taskRepo ResourceTaskStore) *ResourceService {
	return &ResourceService{store: store, prov: prov, taskRepo: taskRepo}
}

// Provision authors the OC Resource model for `depName` on `componentName` and
// marks the matching resource-provisioning task `building`. It is intentionally
// async: it returns as soon as the OC CRs are authored and the task is marked
// in-flight, without waiting for the binding's Ready condition.
//
// SINGLE-PARAMS-MAP NOTE: A2's Provision signature takes a single params map
// applied to all envs. This endpoint therefore accepts one unified params map
// for all envs (not per-env params). If per-env params are needed in the future,
// A2's signature must be updated — this is a documented limitation.
func (s *ResourceService) Provision(
	ctx context.Context,
	orgHandle, projectName, componentName, depName string,
	params map[string]string,
	envs []string,
) error {
	// 1. Read the design and find the platform-resource dep.
	design, err := s.store.ReadDesign(ctx, orgHandle, projectName)
	if err != nil {
		return fmt.Errorf("resources: read design: %w", err)
	}
	if design == nil {
		return fmt.Errorf("%w: no design found for project %q", ErrDepNotFound, projectName)
	}

	resourceType, err := findPlatformResourceType(design, componentName, depName)
	if err != nil {
		return err
	}

	// 2. Call the provisioner (async — authors OC CRs and returns immediately).
	_, err = s.prov.Provision(ctx, orgHandle, projectName, depName, resourceType, params, envs)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrProvisionFailed, err)
	}

	// 3. Mark the resource-provisioning task `building` (the in-flight marker
	// the watcher sweeps). Best-effort: provisioning already succeeded.
	if err := s.markTaskBuilding(ctx, orgHandle, projectName, depName); err != nil {
		slog.WarnContext(ctx, "resources: failed to mark resource-provisioning task building",
			"dep", depName, "project", projectName, "error", err)
	}

	return nil
}

// findPlatformResourceType walks the design to find a platform-resource dep
// named depName on componentName and returns its resourceType.
func findPlatformResourceType(design *artifacts.DesignFile, componentName, depName string) (string, error) {
	for _, comp := range design.Components {
		if comp.Name != componentName {
			continue
		}
		for _, dep := range comp.Dependencies {
			if dep.Name != depName {
				continue
			}
			if dep.Kind != models.DependencyKindPlatformResource {
				return "", fmt.Errorf("%w: dep %q on component %q has kind %q, not %q",
					ErrDepNotFound, depName, componentName, dep.Kind, models.DependencyKindPlatformResource)
			}
			if dep.ResourceType == "" {
				return "", fmt.Errorf("%w: dep %q on component %q has no resourceType",
					ErrDepNotFound, depName, componentName)
			}
			return dep.ResourceType, nil
		}
		// Component found but dep not found.
		return "", fmt.Errorf("%w: dep %q not found on component %q", ErrDepNotFound, depName, componentName)
	}
	return "", fmt.Errorf("%w: component %q not found in design", ErrDepNotFound, componentName)
}

// markTaskBuilding finds the resource-provisioning task for depName and sets
// its status to `building` (the in-flight marker). Does nothing if no matching
// task is found.
func (s *ResourceService) markTaskBuilding(ctx context.Context, orgHandle, projectName, depName string) error {
	if s.taskRepo == nil {
		return nil
	}
	tasks, err := s.taskRepo.ListByProjectID(ctx, orgHandle, projectName)
	if err != nil {
		return err
	}
	for i := range tasks {
		t := &tasks[i]
		if t.Type == models.TaskTypeResourceProvisioning && t.ResourceName == depName &&
			t.Status != string(models.TaskStatusBuilding) {
			t.Status = string(models.TaskStatusBuilding)
			return s.taskRepo.Update(ctx, t)
		}
	}
	return nil
}

// ---- huma routes ------------------------------------------------------------

type provisionResourceInput struct {
	humakit.OrgScopedInput
	ProjectName   string `path:"projectName" doc:"Project name (DNS-label slug)"`
	ComponentName string `path:"componentName" doc:"Component name"`
	DepName       string `path:"depName" doc:"Platform-resource dependency name"`
	Body          struct {
		// Params is the unified provisioning parameters map applied to all envs.
		// A2's Provisioner takes a single params map (not per-env) — see the
		// single-params-map limitation in ResourceService.Provision docs.
		Params map[string]string `json:"params,omitempty" doc:"Provisioning parameters (applied to all envs)"`
		// Environments is the list of environments to provision the resource for.
		Environments []string `json:"environments" doc:"Environment names to bind the resource to (e.g. [\"development\"])"`
	}
}

type provisionResourceOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

type getResourceStatusInput struct {
	humakit.OrgScopedInput
	ProjectName   string `path:"projectName" doc:"Project name (DNS-label slug)"`
	ComponentName string `path:"componentName" doc:"Component name"`
	DepName       string `path:"depName" doc:"Platform-resource dependency name"`
	Environment   string `query:"environment" doc:"Environment name (default: development)" required:"false"`
}

type resourceOutput struct {
	Name string `json:"name"`
}

type getResourceStatusOutput struct {
	Body struct {
		// Status mirrors the resource-provisioning task status (pending, building, deployed, failed…).
		Status string `json:"status"`
		// Ready reports whether the OC binding's native Ready condition is True.
		Ready bool `json:"ready"`
		// Outputs lists binding outputs with secret values MASKED (name only, never value).
		Outputs []resourceOutput `json:"outputs,omitempty"`
	}
}

// RegisterResources registers the async provision + status routes for
// platform-resource dependencies. Call next to RegisterConnections in
// RegisterAllHuma.
func RegisterResources(api huma.API, svc *ResourceService, rc openchoreo.ResourceClient) {
	huma.Register(api, huma.Operation{
		OperationID:   "provision-resource",
		Method:        http.MethodPost,
		Path:          "/api/v1/organizations/{orgHandle}/projects/{projectName}/components/{componentName}/dependencies/{depName}/provision",
		Summary:       "Author the OC Resource model for a platform-resource dep and mark it in-flight (async)",
		Tags:          []string{"Resources"},
		Security:      humakit.SecurityUserJWT,
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, in *provisionResourceInput) (*provisionResourceOutput, error) {
		if svc == nil {
			return nil, huma.Error503ServiceUnavailable("resource provisioning is not configured")
		}
		envs := in.Body.Environments
		if len(envs) == 0 {
			envs = []string{"development"}
		}
		if err := svc.Provision(ctx, in.OrgHandle, in.ProjectName, in.ComponentName, in.DepName,
			in.Body.Params, envs); err != nil {
			switch {
			case errors.Is(err, ErrDepNotFound):
				return nil, huma.Error404NotFound("platform-resource dependency not found", err)
			case errors.Is(err, ErrProvisionFailed):
				return nil, huma.Error502BadGateway("platform provisioner failed", err)
			default:
				return nil, huma.Error500InternalServerError("failed to provision resource", err)
			}
		}
		out := &provisionResourceOutput{}
		out.Body.Status = "provisioning"
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-resource-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/components/{componentName}/dependencies/{depName}/status",
		Summary:     "Get the provisioning status and masked outputs for a platform-resource dependency",
		Tags:        []string{"Resources"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *getResourceStatusInput) (*getResourceStatusOutput, error) {
		if rc == nil {
			return nil, huma.Error503ServiceUnavailable("resource client is not configured")
		}
		if svc == nil {
			return nil, huma.Error503ServiceUnavailable("resource provisioning is not configured")
		}
		env := in.Environment
		if env == "" {
			env = "development"
		}
		// Derive binding name (mirrors BuildPlatformBinding: project-depName-env).
		bindingName := in.ProjectName + "-" + in.DepName + "-" + env

		binding, err := rc.GetBinding(ctx, in.OrgHandle, bindingName)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to read resource binding", err)
		}

		// Derive task status from the resource-provisioning task.
		taskStatus := "pending"
		if svc.taskRepo != nil {
			tasks, terr := svc.taskRepo.ListByProjectID(ctx, in.OrgHandle, in.ProjectName)
			if terr != nil {
				slog.WarnContext(ctx, "resources: failed to list tasks for status; defaulting to pending",
					"org", in.OrgHandle, "project", in.ProjectName, "dep", in.DepName, "error", terr)
			} else {
				for _, t := range tasks {
					if t.Type == models.TaskTypeResourceProvisioning && t.ResourceName == in.DepName {
						taskStatus = t.Status
						break
					}
				}
			}
		}

		out := &getResourceStatusOutput{}
		out.Body.Status = taskStatus
		if binding != nil {
			out.Body.Ready = binding.IsReady()
			if binding.Status != nil {
				// Mask outputs: emit name only, never value (secrets must not
				// enter API responses — they live only in the OC-rendered Secret).
				for _, o := range binding.Status.Outputs {
					out.Body.Outputs = append(out.Body.Outputs, resourceOutput{Name: o.Name})
				}
			}
		}
		return out, nil
	})
}
