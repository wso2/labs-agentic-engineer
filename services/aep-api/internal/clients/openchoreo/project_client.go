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

package openchoreo

import (
	"context"
	"fmt"
	"net/http"

	ocgen "github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
	"github.com/wso2/aep/aep-api/internal/gen"
)

//go:generate go run github.com/matryer/moq@v0.7.1 -rm -fmt goimports -pkg mocks -out mocks/project_client_mock.go . ProjectClient

// ProjectClient defines operations for managing OpenChoreo projects.
type ProjectClient interface {
	ListProjects(ctx context.Context, orgName string, limit int, cursor string) (*gen.ProjectList, error)
	GetProject(ctx context.Context, orgName, projectName string) (*gen.Project, error)
	CreateProject(ctx context.Context, orgName string, req *gen.CreateProjectRequest) (*gen.Project, error)
	DeleteProject(ctx context.Context, orgName, projectName string) error
}

type projectClient struct {
	oc *ocgen.ClientWithResponses
}

func NewProjectClient(cfg Config) ProjectClient {
	oc, err := newGenClient(cfg)
	if err != nil {
		panic(fmt.Errorf("init openchoreo project client: %w", err))
	}
	return &projectClient{oc: oc}
}

func (c *projectClient) ListProjects(ctx context.Context, orgName string, limit int, cursor string) (*gen.ProjectList, error) {
	params := &ocgen.ListProjectsParams{}
	if limit > 0 {
		l := ocgen.LimitParam(limit)
		params.Limit = &l
	}
	if cursor != "" {
		cur := ocgen.CursorParam(cursor)
		params.Cursor = &cur
	}
	resp, err := c.oc.ListProjectsWithResponse(ctx, orgName, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	items := make([]gen.Project, len(resp.JSON200.Items))
	for i, p := range resp.JSON200.Items {
		items[i] = projectToModel(p)
	}
	out := &gen.ProjectList{Items: items}
	// Surface OC's continuation token verbatim (absent = last page); the
	// console pages on it.
	if nc := resp.JSON200.Pagination.NextCursor; nc != nil {
		out.NextCursor = *nc
	}
	return out, nil
}

func (c *projectClient) GetProject(ctx context.Context, orgName, projectName string) (*gen.Project, error) {
	resp, err := c.oc.GetProjectWithResponse(ctx, orgName, projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	p := projectToModel(*resp.JSON200)
	return &p, nil
}

func (c *projectClient) CreateProject(ctx context.Context, orgName string, body *gen.CreateProjectRequest) (*gen.Project, error) {
	resp, err := c.oc.CreateProjectWithResponse(ctx, orgName, buildCreateProjectBody(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create project: %w", err)
	}
	if resp.StatusCode() == http.StatusPaymentRequired {
		// Entitlement gate (inactive subscription / project quota). Parse the
		// platform body so the console shows the real sentence instead of a
		// generic 500 — gen has no typed JSON402 on this endpoint.
		msg := humanErrorMessage(resp.Body, "Quota limit reached. Upgrade your subscription to continue.")
		return nil, fmt.Errorf("%w: %s", ErrPaymentRequired, msg)
	}
	// OC's POST returns 201 on success; tolerate 200 in case a future build flips to it.
	if (resp.StatusCode() != http.StatusCreated && resp.StatusCode() != http.StatusOK) || resp.JSON201 == nil {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON409: resp.JSON409,
			JSON500: resp.JSON500,
		})
	}
	p := projectToModel(*resp.JSON201)
	return &p, nil
}

func (c *projectClient) DeleteProject(ctx context.Context, orgName, projectName string) error {
	resp, err := c.oc.DeleteProjectWithResponse(ctx, orgName, projectName)
	if err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent && resp.StatusCode() != http.StatusOK {
		return handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	return nil
}

func projectToModel(p ocgen.Project) gen.Project {
	var deploymentPipeline string
	if p.Spec != nil && p.Spec.DeploymentPipelineRef != nil {
		deploymentPipeline = p.Spec.DeploymentPipelineRef.Name
	}
	return gen.Project{
		UID:                derefStr(p.Metadata.Uid),
		Name:               p.Metadata.Name,
		NamespaceName:      derefStr(p.Metadata.Namespace),
		DisplayName:        annotation(p.Metadata.Annotations, AnnotationKeyDisplayName),
		Description:        annotation(p.Metadata.Annotations, AnnotationKeyDescription),
		DeploymentPipeline: deploymentPipeline,
		CreatedAt:          derefTimeRFC3339(p.Metadata.CreationTimestamp),
		Status:             latestProjectStatusReason(p.Status),
	}
}

func buildCreateProjectBody(req *gen.CreateProjectRequest) ocgen.CreateProjectJSONRequestBody {
	body := ocgen.Project{
		Metadata: ocgen.ObjectMeta{Name: req.Name},
	}
	if req.DisplayName != "" || req.Description != "" {
		ann := map[string]string{}
		if req.DisplayName != "" {
			ann[AnnotationKeyDisplayName] = req.DisplayName
		}
		if req.Description != "" {
			ann[AnnotationKeyDescription] = req.Description
		}
		body.Metadata.Annotations = &ann
	}
	if req.DeploymentPipeline != "" {
		body.Spec = &ocgen.ProjectSpec{
			DeploymentPipelineRef: &struct {
				Kind *ocgen.ProjectSpecDeploymentPipelineRefKind `json:"kind,omitempty"`
				Name string                                      `json:"name"`
			}{Name: req.DeploymentPipeline},
		}
	}
	return body
}

func latestProjectStatusReason(status *ocgen.ProjectStatus) string {
	if status == nil {
		return ""
	}
	return latestConditionReason(status.Conditions)
}
