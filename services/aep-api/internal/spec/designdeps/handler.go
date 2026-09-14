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

// Package designdeps is the spec domain's slice for the dependency definition view's two
// writes: the user provides an external dependency's contract, or accepts the
// contract the design agent wrote. Both land in the dependency's own directory
// (specs/design/dependencies/<name>/) through the design service.
package designdeps

import (
	"context"
	"errors"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Handler serves provide-dependency-contract and accept-dependency-assumption.
type Handler struct {
	design spec.DependencyContractService
}

// New returns the slice's handler. A nil service 503s every op.
func New(design spec.DependencyContractService) *Handler { return &Handler{design: design} }

// ProvideDependencyContract commits the document the user supplied — fetched
// from a URL or given inline — into the dependency's own directory and records
// it in dependency.json, which is what takes the dependency from
// needs-contract (or needs-acceptance) to resolved on the next read.
func (h *Handler) ProvideDependencyContract(ctx context.Context, request gen.ProvideDependencyContractRequestObject) (gen.ProvideDependencyContractResponseObject, error) {
	if h.design == nil {
		return nil, apierr.ServiceUnavailable("design service is not configured")
	}
	if request.Body == nil {
		return nil, apierr.BadRequest("provide url or content")
	}
	org := tenant.BoundOrgFromContext(ctx)
	var raw []byte
	if request.Body.Content != "" {
		raw = []byte(request.Body.Content)
	}
	path, err := h.design.CollectDependencyContract(ctx, org, request.ProjectName, request.DepName, raw, request.Body.URL)
	if err != nil {
		return nil, mapError(err, "failed to collect the dependency contract")
	}
	return gen.ProvideDependencyContract200JSONResponse{Contract: path}, nil
}

// AcceptDependencyAssumption records the signed-in user's permission to build
// against the contract the design agent wrote for this dependency.
func (h *Handler) AcceptDependencyAssumption(ctx context.Context, request gen.AcceptDependencyAssumptionRequestObject) (gen.AcceptDependencyAssumptionResponseObject, error) {
	if h.design == nil {
		return nil, apierr.ServiceUnavailable("design service is not configured")
	}
	org := tenant.BoundOrgFromContext(ctx)
	note := ""
	if request.Body != nil {
		note = request.Body.Note
	}
	if err := h.design.AcceptDependencyAssumption(ctx, org, request.ProjectName, request.DepName, auth.ActorFromContext(ctx), note); err != nil {
		return nil, mapError(err, "failed to accept the dependency assumption")
	}
	return gen.AcceptDependencyAssumption200JSONResponse{Status: "accepted"}, nil
}

// mapError maps the design service's typed errors onto the envelope.
func mapError(err error, fallback string) error {
	switch {
	case errors.Is(err, spec.ErrDependencyNotFound):
		return apierr.NotFound(err.Error())
	case errors.Is(err, spec.ErrDependencyWrongKind), errors.Is(err, spec.ErrInvalidSpec), errors.Is(err, spec.ErrSpecFetchFailed):
		return apierr.BadRequest(err.Error())
	case errors.Is(err, spec.ErrDependencyNotAssumed), errors.Is(err, spec.ErrDependencyNotChosen), errors.Is(err, spec.ErrSpecCommitConflict):
		return apierr.Conflict(err.Error())
	default:
		return apierr.Internal(fallback)
	}
}
