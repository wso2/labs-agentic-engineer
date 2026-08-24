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

package provisioning

import (
	"context"
	"errors"

	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Handler is the dependency-provisioning slice of the strict interface: the
// external-resource catalog, value collection, platform-resource provisioning/
// status, and the cross-project org-service access-request surface. Every
// operation is org-scoped; the org from the gate serves as both the OC
// namespace/issues org and the SM-API org id. A nil service answers 503 (the
// surface exists with the feature unwired) — mirroring the pre-migration edge's
// RegisterResources/registerAccess nil guards.
type Handler struct {
	svc *Service
}

// NewHandler wires the slice over the provisioning service. A nil svc is a
// supported configuration: every op degrades to 503.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// errProvisioningUnavailable is the nil-service guard's 503.
func errProvisioningUnavailable() error {
	return apierr.ServiceUnavailable("provisioning is not configured")
}

func (h *Handler) ListWorkloadDependencies(ctx context.Context, request gen.ListWorkloadDependenciesRequestObject) (gen.ListWorkloadDependenciesResponseObject, error) {
	if h.svc == nil {
		return nil, errProvisioningUnavailable()
	}
	org := tenant.BoundOrgFromContext(ctx)
	views, err := h.svc.ListWorkloadDependencies(ctx, org, request.ProjectName)
	if err != nil {
		return nil, mapProvisionError(err)
	}
	return gen.ListWorkloadDependencies200JSONResponse(toWorkloadDependencyDTOs(views)), nil
}

func (h *Handler) ListExternalResources(ctx context.Context, _ gen.ListExternalResourcesRequestObject) (gen.ListExternalResourcesResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.svc == nil {
		return nil, errProvisioningUnavailable()
	}
	views, err := h.svc.ListExternalResources(ctx, org)
	if err != nil {
		return nil, mapProvisionError(err)
	}
	return gen.ListExternalResources200JSONResponse(toExternalResourceDTOs(views)), nil
}

func (h *Handler) DeleteExternalResource(ctx context.Context, request gen.DeleteExternalResourceRequestObject) (gen.DeleteExternalResourceResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.svc == nil {
		return nil, errProvisioningUnavailable()
	}
	if err := h.svc.DeleteExternalResource(ctx, org, request.Name); err != nil {
		return nil, mapProvisionError(err)
	}
	return gen.DeleteExternalResource204Response{}, nil
}

func (h *Handler) CollectExternalResourceValues(ctx context.Context, request gen.CollectExternalResourceValuesRequestObject) (gen.CollectExternalResourceValuesResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.svc == nil {
		return nil, errProvisioningUnavailable()
	}
	var envs map[string]map[string]string
	if request.Body != nil {
		envs = request.Body.Environments
	}
	// org serves as both the OC namespace/issues org and the SM-API org id; the
	// ctx carries the user JWT the SM-API writer reads for the vault path.
	if err := h.svc.SaveValues(ctx, org, org, request.ProjectName, request.Name, envs); err != nil {
		return nil, mapProvisionError(err)
	}
	return gen.CollectExternalResourceValues200JSONResponse(gen.StatusMsg{Status: "provisioned"}), nil
}

func (h *Handler) ProvisionPlatformResource(ctx context.Context, request gen.ProvisionPlatformResourceRequestObject) (gen.ProvisionPlatformResourceResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.svc == nil {
		return nil, errProvisioningUnavailable()
	}
	var params map[string]any
	var envs []string
	if request.Body != nil {
		// ProvisionBody.params is free-form in the contract (mixed scalars —
		// string, number, boolean — exactly what the service accepts).
		params = request.Body.Params
		envs = request.Body.Environments
	}
	if err := h.svc.Provision(ctx, org, request.ProjectName, request.DepName, params, envs); err != nil {
		return nil, mapProvisionError(err)
	}
	return gen.ProvisionPlatformResource202JSONResponse(gen.StatusMsg{Status: "provisioning"}), nil
}

func (h *Handler) GetDependencyStatus(ctx context.Context, request gen.GetDependencyStatusRequestObject) (gen.GetDependencyStatusResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.svc == nil {
		return nil, errProvisioningUnavailable()
	}
	env := ""
	if request.Params.Environment != "" {
		env = request.Params.Environment
	}
	st, err := h.svc.Status(ctx, org, request.ProjectName, request.DepName, env)
	if err != nil {
		return nil, mapProvisionError(err)
	}
	response := gen.DependencyStatus{
		Status:  st.Status,
		Ready:   st.Ready,
		Outputs: st.Outputs,
	}
	if st.ValueState != "" {
		response.ValueState = gen.ExternalDependencyValueState(st.ValueState)
	}
	return gen.GetDependencyStatus200JSONResponse(response), nil
}

func (h *Handler) GetProjectDependencyReadiness(ctx context.Context, request gen.GetProjectDependencyReadinessRequestObject) (gen.GetProjectDependencyReadinessResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.svc == nil {
		return nil, errProvisioningUnavailable()
	}
	readiness, err := h.svc.ConfigurationReadiness(ctx, org, request.ProjectName, request.Params.Environment)
	if err != nil {
		return nil, mapProvisionError(err)
	}
	deps := make([]gen.ExternalDependencyReadiness, 0, len(readiness.Dependencies))
	for _, dep := range readiness.Dependencies {
		deps = append(deps, gen.ExternalDependencyReadiness{
			Name: dep.Name, State: gen.ExternalDependencyValueState(dep.State), MissingKeys: dep.MissingKeys,
		})
	}
	return gen.GetProjectDependencyReadiness200JSONResponse(gen.ProjectDependencyReadiness{
		Configured: readiness.Configured, Dependencies: deps,
	}), nil
}

func (h *Handler) RequestOrgServiceAccess(ctx context.Context, request gen.RequestOrgServiceAccessRequestObject) (gen.RequestOrgServiceAccessResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.svc == nil {
		return nil, errProvisioningUnavailable()
	}
	ar, err := h.svc.RequestAccess(ctx, org, request.ProjectName, request.ComponentName, request.DepName)
	if err != nil {
		return nil, mapProvisionError(err)
	}
	return gen.RequestOrgServiceAccess201JSONResponse(accessRequestToWire(*ar)), nil
}

func (h *Handler) ListAccessRequests(ctx context.Context, request gen.ListAccessRequestsRequestObject) (gen.ListAccessRequestsResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.svc == nil {
		return nil, errProvisioningUnavailable()
	}
	reqs, err := h.svc.ListAccessRequests(ctx, org, request.ProjectName)
	if err != nil {
		return nil, mapProvisionError(err)
	}
	if reqs == nil {
		reqs = []dependencies.AccessRequest{}
	}
	return gen.ListAccessRequests200JSONResponse(accessRequestsToWire(reqs)), nil
}

// accessRequestToWire projects the dependencies.AccessRequest domain entity
// onto the generated wire type. Field-for-field identical except the entity's
// ProviderIssueNumber is a plain int (the schema types it int64) — a widening
// cast that leaves the JSON unchanged.
func accessRequestToWire(ar dependencies.AccessRequest) gen.AccessRequest {
	return gen.AccessRequest{
		ID:                    ar.ID,
		ConsumerProjectID:     ar.ConsumerProjectID,
		ConsumerComponentName: ar.ConsumerComponentName,
		OrgServiceName:        ar.OrgServiceName,
		ProviderProjectID:     ar.ProviderProjectID,
		ProviderComponentName: ar.ProviderComponentName,
		ProviderTaskID:        ar.ProviderTaskID,
		ProviderIssueNumber:   int64(ar.ProviderIssueNumber),
		ProviderIssueURL:      ar.ProviderIssueURL,
		Status:                ar.Status,
		CreatedAt:             ar.CreatedAt,
		UpdatedAt:             ar.UpdatedAt,
	}
}

func accessRequestsToWire(reqs []dependencies.AccessRequest) []gen.AccessRequest {
	out := make([]gen.AccessRequest, 0, len(reqs))
	for _, ar := range reqs {
		out = append(out, accessRequestToWire(ar))
	}
	return out
}

// mapProvisionError translates the provisioning sentinels into the envelope:
// wrong kind → 400, not-found / not-registered → 404, in-use → 409, provision
// failure → 502, else an opaque 500. It names both the domain-root resource
// sentinels (dependencies.Err*) and this slice's own (ErrOrgServiceNotFound /
// ErrExternalResourceInUse).
func mapProvisionError(err error) error {
	switch {
	case errors.Is(err, dependencies.ErrDepWrongKind):
		return apierr.BadRequest(err.Error())
	case errors.Is(err, dependencies.ErrDepNotFound),
		errors.Is(err, dependencies.ErrNotRegistered),
		errors.Is(err, ErrOrgServiceNotFound):
		return apierr.NotFound(err.Error())
	case errors.Is(err, ErrExternalResourceInUse):
		return apierr.Conflict(err.Error())
	case errors.Is(err, dependencies.ErrProvisionFailed):
		return apierr.BadGateway(err.Error())
	}
	return apierr.Internal("provisioning failed")
}

func toExternalResourceDTOs(views []ExternalResourceView) []gen.ExternalResourceDTO {
	out := make([]gen.ExternalResourceDTO, 0, len(views))
	for _, v := range views {
		keys := make([]gen.ConfigKeyDTO, 0, len(v.Config))
		for _, k := range v.Config {
			keys = append(keys, gen.ConfigKeyDTO{Key: k.Key, Secret: k.Secret, Description: k.Description, DefaultValue: k.DefaultValue})
		}
		consumers := make([]gen.ConsumerDTO, 0, len(v.Consumers))
		for _, c := range v.Consumers {
			consumers = append(consumers, gen.ConsumerDTO{ProjectID: c.ProjectID, ComponentName: c.ComponentName})
		}
		out = append(out, gen.ExternalResourceDTO{
			Name:        v.Name,
			Description: v.Description,
			Config:      keys,
			Consumers:   consumers,
		})
	}
	return out
}

func toWorkloadDependencyDTOs(views []WorkloadDependencyView) []gen.WorkloadDependencyDTO {
	out := make([]gen.WorkloadDependencyDTO, 0, len(views))
	for _, v := range views {
		out = append(out, gen.WorkloadDependencyDTO{
			Kind:      gen.WorkloadDependencyDTOKind(v.Kind),
			Ref:       v.Ref,
			Tag:       gen.WorkloadDependencyDTOTag(v.Tag),
			Name:      v.Name,
			Project:   v.Project,
			Component: v.Component,
		})
	}
	return out
}
