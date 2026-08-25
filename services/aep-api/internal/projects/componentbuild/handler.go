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

package componentbuild

import (
	"context"
	"errors"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/projects"
)

// Handler serves the component build + deploy read feature on the strict
// interface. Every operation is org-scoped: the deny-by-default tenant gate
// bound the token org into the context before these run, and the handler passes
// it to the service as an explicit argument. projectName/componentName/buildName
// path params are validated as DNS-label slugs (400 on malformed) before any
// service (OC client / repo) is touched.
type Handler struct{ comp projects.ComponentService }

// New returns the slice's handler.
func New(comp projects.ComponentService) *Handler { return &Handler{comp: comp} }

// --- Build operations --------------------------------------------------------

func (h *Handler) TriggerBuild(ctx context.Context, request gen.TriggerBuildRequestObject) (gen.TriggerBuildResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if err := projects.RequireComponentSlugs(request.ProjectName, request.ComponentName); err != nil {
		return nil, err
	}
	run, err := h.comp.TriggerBuild(ctx, org, request.ProjectName, request.ComponentName)
	if err != nil {
		return nil, projects.MapComponentError(err, "failed to trigger build")
	}
	return gen.TriggerBuild201JSONResponse(*run), nil
}

func (h *Handler) ListBuilds(ctx context.Context, request gen.ListBuildsRequestObject) (gen.ListBuildsResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if err := projects.RequireComponentSlugs(request.ProjectName, request.ComponentName); err != nil {
		return nil, err
	}
	list, err := h.comp.ListBuilds(ctx, org, request.ProjectName, request.ComponentName, 20, "")
	if err != nil {
		return nil, projects.MapComponentError(err, "failed to list builds")
	}
	return gen.ListBuilds200JSONResponse(*list), nil
}

func (h *Handler) GetBuildLogs(ctx context.Context, request gen.GetBuildLogsRequestObject) (gen.GetBuildLogsResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if err := projects.RequireComponentSlugs(request.ProjectName, request.ComponentName); err != nil {
		return nil, err
	}
	if err := projects.RequireSlug("buildName", request.BuildName); err != nil {
		return nil, err
	}
	// An absent ?since decodes to 0, which is exactly "read from the beginning".
	logs, err := h.comp.GetBuildLogs(ctx, org, request.ProjectName, request.ComponentName, request.BuildName, request.Params.Since)
	if err != nil {
		if errors.Is(err, projects.ErrLogsUnavailable) {
			return nil, apierr.ServiceUnavailable("build logs service not available")
		}
		return nil, projects.MapComponentError(err, "failed to get build logs")
	}
	return gen.GetBuildLogs200JSONResponse(*logs), nil
}

// --- Deploy operations (read-only) --------------------------------------------
// OC's Component controller drives the deploy chain via AutoDeploy. The list
// reflects materialised ReleaseBindings for this component.

func (h *Handler) ListDeployments(ctx context.Context, request gen.ListDeploymentsRequestObject) (gen.ListDeploymentsResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if err := projects.RequireComponentSlugs(request.ProjectName, request.ComponentName); err != nil {
		return nil, err
	}
	list, err := h.comp.ListDeployments(ctx, org, request.ProjectName, request.ComponentName)
	if err != nil {
		return nil, projects.MapComponentError(err, "failed to list deployments")
	}
	return gen.ListDeployments200JSONResponse(*list), nil
}

// --- OpenAPI spec (drives the Test tab) ----------------------------------------
// Read from specs/design/components/<name>/openapi.yaml. Service components
// have a guaranteed OpenAPI 3.0 doc; non-service components return 409 with
// the componentType so the UI can render a typed empty state.

func (h *Handler) GetComponentOpenapi(ctx context.Context, request gen.GetComponentOpenapiRequestObject) (gen.GetComponentOpenapiResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if err := projects.RequireComponentSlugs(request.ProjectName, request.ComponentName); err != nil {
		return nil, err
	}
	spec, err := h.comp.GetComponentOpenAPI(ctx, org, request.ProjectName, request.ComponentName)
	if err != nil {
		if errors.Is(err, projects.ErrComponentNotFound) {
			return nil, apierr.NotFound("no OpenAPI spec for this component")
		}
		if errors.Is(err, projects.ErrComponentNotService) {
			// Hand the type back (409, contract-declared) so the client can
			// say "this is a web-app, not a service". The body still carries
			// componentType. Guard nil: only the concrete service happens to
			// pair the sentinel with a non-nil spec.
			if spec == nil {
				return nil, apierr.Conflict("component does not expose an API")
			}
			return gen.GetComponentOpenapi409JSONResponse(*spec), nil
		}
		return nil, projects.MapComponentError(err, "failed to get OpenAPI spec")
	}
	return gen.GetComponentOpenapi200JSONResponse(*spec), nil
}
