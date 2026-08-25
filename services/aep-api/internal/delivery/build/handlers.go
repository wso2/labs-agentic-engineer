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

package build

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Handler serves the build feature on the strict interface: build-project /
// list-project-builds plus the dependency-drawer get-build-preflight. Every
// operation is org-scoped — the tenant gate bound
// the token org before these run, and the handlers pass it to the service
// explicitly.
//
// Error dialect: Run is the same sequence the non-HTTP StartProjectBuild
// trigger uses, whose error sites speak the neutral *EdgeError vocabulary.
// mapBuildRunError translates those onto the flat envelope; the former 422
// spec-gate problem is a 400 validation_failed with details[] now (the
// error-model break).
type Handler struct {
	svc       *Service
	preflight *PreflightService
	activity  SpecPublishedRecorder
	publisher PublisherProvisioner
}

// PublisherProvisioner ensures the org's Thunder publisher client_credentials
// SecretReference exists before a coding-agent build starts. Wired on Handler
// (never on Service) because POST /projects/{name}/build is the sole request
// path that still carries the console JWT ProvisionPublisherForBuild needs —
// Temporal dispatch and the StartProjectBuild auto-kick trigger run with no
// such JWT and must stay read-only with respect to publisher credentials.
type PublisherProvisioner interface {
	ProvisionPublisherForBuild(ctx context.Context, orgID string) error
}

// SpecPublishedRecorder appends the spec_published activity line (issue #239)
// when a build start succeeds: the user published spec v<tag> and kicked off
// the build. Best-effort and optional (nil = no feed): recording never fails
// the request. Satisfied by an app-root adapter that resolves the signed-in
// user's identity from ctx and appends via the projects activity service
// (build must not import projects — projects already imports delivery).
type SpecPublishedRecorder interface {
	RecordSpecPublished(ctx context.Context, orgID, projectName, tag string)
}

// NewHandler returns the slice's handler.
func NewHandler(svc *Service, preflight *PreflightService, activity SpecPublishedRecorder) *Handler {
	return &Handler{svc: svc, preflight: preflight, activity: activity}
}

// WithPublisherProvisioner wires the publisher provisioner. Optional: nil
// skips provisioning (tests that do not care).
func (h *Handler) WithPublisherProvisioner(p PublisherProvisioner) *Handler {
	h.publisher = p
	return h
}

func (h *Handler) BuildProject(ctx context.Context, request gen.BuildProjectRequestObject) (gen.BuildProjectResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.publisher != nil {
		if err := h.publisher.ProvisionPublisherForBuild(ctx, org); err != nil {
			slog.ErrorContext(ctx, "publisher provision for build failed", "error", err)
			return nil, apierr.ServiceUnavailable("publisher credentials unavailable")
		}
	}
	var inputs []BuildInputItem
	if request.Body != nil {
		inputs = toBuildInputItems(request.Body.Inputs)
	}
	tag, failures, err := h.svc.Run(ctx, org, request.ProjectName, inputs)
	if err != nil {
		return nil, mapBuildRunError(err)
	}
	if len(failures) > 0 {
		return gen.BuildProject200JSONResponse(gen.BuildResponse{Failures: toInputFailures(failures)}), nil
	}
	if h.activity != nil && tag != "" {
		h.activity.RecordSpecPublished(ctx, org, request.ProjectName, tag)
	}
	return gen.BuildProject200JSONResponse(gen.BuildResponse{Tag: tag}), nil
}

func (h *Handler) ListProjectBuilds(ctx context.Context, request gen.ListProjectBuildsRequestObject) (gen.ListProjectBuildsResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	list, err := h.svc.List(ctx, org, request.ProjectName)
	if err != nil {
		return nil, apierr.Internal("list builds")
	}
	return gen.ListProjectBuilds200JSONResponse(toBuildList(list)), nil
}

// GetBuildPreflight computes the build dependency-drawer preflight. A nil
// service answers 503, mirroring the retired RegisterPreflight nil guard (the
// surface exists with the feature unwired).
func (h *Handler) GetBuildPreflight(ctx context.Context, request gen.GetBuildPreflightRequestObject) (gen.GetBuildPreflightResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.preflight == nil {
		return nil, apierr.ServiceUnavailable("build preflight is not configured")
	}
	pf, err := h.preflight.Preflight(ctx, org, request.ProjectName)
	if err != nil {
		return nil, apierr.Internal("compute build preflight: " + err.Error())
	}
	return gen.GetBuildPreflight200JSONResponse(toBuildPreflight(pf)), nil
}

// mapBuildRunError translates Run's failures onto the envelope: the
// already-running sentinel is a 409, and every Run *EdgeError carries its
// status, message, and per-file spec-gate details across (a 400 with details is
// the validation_failed dialect).
func mapBuildRunError(err error) error {
	if errors.Is(err, ErrBuildAlreadyRunning) {
		return apierr.Conflict("a build is already running for this project")
	}
	// The same 409 shape for the other refusal: a live validation run on the
	// project. Its own message, because the two send a user to different places —
	// one is "wait", the other is "cancel the validation".
	if errors.Is(err, ErrValidationRunLive) {
		return apierr.Conflict(ErrValidationRunLive.Error())
	}
	var ee *EdgeError
	if !errors.As(err, &ee) {
		return apierr.Internal("internal error")
	}
	switch ee.Status {
	case http.StatusBadRequest:
		return apierr.New(http.StatusBadRequest, "validation_failed", ee.Message, ee.Details)
	case http.StatusServiceUnavailable:
		return apierr.ServiceUnavailable(ee.Message)
	case http.StatusBadGateway:
		return apierr.BadGateway(ee.Message)
	default:
		return errFromStatus(ee.Status, ee.Message)
	}
}

// errFromStatus maps a sentinel-classified HTTP status (e.g. an OpenChoreo
// pass-through classified by ocerr.Status) onto the envelope, mirroring the
// edge's retired humakit.ErrorFromStatus ladder — reproduced here so the build
// slice can map an *EdgeError status without importing the edge.
func errFromStatus(status int, msg string) error {
	switch status {
	case http.StatusBadRequest:
		return apierr.BadRequest(msg)
	case http.StatusUnauthorized:
		return apierr.Unauthorized(msg)
	case http.StatusForbidden:
		return apierr.Forbidden(msg)
	case http.StatusNotFound:
		return apierr.NotFound(msg)
	case http.StatusConflict:
		return apierr.Conflict(msg)
	default:
		return apierr.Internal(msg)
	}
}

// --- schema <-> feature projections ------------------------------------------

func toBuildInputItems(in []gen.BuildInputItem) []BuildInputItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]BuildInputItem, 0, len(in))
	for _, it := range in {
		var values []ConfigValue
		for _, v := range it.Values {
			values = append(values, ConfigValue{Key: v.Key, Value: v.Value})
		}
		out = append(out, BuildInputItem{
			Component:   it.Component,
			Dependency:  it.Dependency,
			Kind:        string(it.Kind),
			Values:      values,
			SpecContent: it.SpecContent,
			SpecURL:     it.SpecURL,
			Parameters:  it.Parameters,
			Approved:    it.Approved,
		})
	}
	return out
}

func toInputFailures(in []InputFailure) []gen.InputFailure {
	out := make([]gen.InputFailure, 0, len(in))
	for _, f := range in {
		out = append(out, gen.InputFailure{
			Component:  f.Component,
			Dependency: f.Dependency,
			Kind:       f.Kind,
			Reason:     f.Reason,
		})
	}
	return out
}

func toBuildList(l BuildList) gen.BuildList {
	// Builds stays non-nil so the JSON body is [] rather than null.
	builds := make([]gen.BuildSummary, 0, len(l.Builds))
	for _, b := range l.Builds {
		s := gen.BuildSummary{
			Tag:             b.Tag,
			MilestoneNumber: int64(b.MilestoneNumber),
			Status:          gen.BuildSummaryStatus(b.Status),
			Reason:          b.Reason,
			StartedAt:       b.StartedAt,
		}
		s.CompletedAt = b.CompletedAt // nil while running — omitted on the wire
		builds = append(builds, s)
	}
	return gen.BuildList{Builds: builds}
}

func toBuildPreflight(pf BuildPreflight) gen.BuildPreflight {
	// Items stays non-nil so the JSON body is [] rather than null.
	items := make([]gen.PreflightItem, 0, len(pf.Items))
	for _, it := range pf.Items {
		var cfg []gen.ConfigKeyView
		for _, k := range it.Config {
			cfg = append(cfg, gen.ConfigKeyView{
				Key:          k.Key,
				Secret:       k.Secret,
				Description:  k.Description,
				DefaultValue: k.DefaultValue,
			})
		}
		items = append(items, gen.PreflightItem{
			Component:    it.Component,
			Dependency:   it.Dependency,
			Kind:         gen.PreflightItemKind(it.Kind),
			Description:  it.Description,
			Config:       cfg,
			ResourceType: it.ResourceType,
			Parameters:   it.Parameters,
		})
	}
	return gen.BuildPreflight{NeedsInput: pf.NeedsInput, Items: items}
}
