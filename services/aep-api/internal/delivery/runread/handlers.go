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

package runread

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Handler serves the run read surface on the strict interface: the version's
// runs, the per-run and version-wide progress streams, and cancel.
type Handler struct {
	reads       *Reads
	progress    *ProgressService
	commands    *Commands
	cycleBuilds *CycleBuilds
}

// NewHandler returns the slice's handler. Any nil service leaves its operations
// answering 503 rather than panicking — the degraded-boot contract every slice
// handler follows.
func NewHandler(reads *Reads, progress *ProgressService, commands *Commands, cycleBuilds *CycleBuilds) *Handler {
	return &Handler{reads: reads, progress: progress, commands: commands, cycleBuilds: cycleBuilds}
}

// ListBuildRuns serves GET /projects/{p}/builds/{tag}/runs.
func (h *Handler) ListBuildRuns(ctx context.Context, request gen.ListBuildRunsRequestObject) (gen.ListBuildRunsResponseObject, error) {
	if h.reads == nil {
		return nil, apierr.ServiceUnavailable("run reads not configured")
	}
	out, err := h.reads.RunsForTag(ctx, tenant.BoundOrgFromContext(ctx), request.ProjectName, request.Tag)
	if err != nil {
		return nil, mapRunError(err)
	}
	return gen.ListBuildRuns200JSONResponse(*out), nil
}

// ListCycleBuilds serves GET /projects/{p}/builds/{tag}/cycles/{cycleId}/builds.
func (h *Handler) ListCycleBuilds(ctx context.Context, request gen.ListCycleBuildsRequestObject) (gen.ListCycleBuildsResponseObject, error) {
	if h.cycleBuilds == nil {
		return nil, apierr.ServiceUnavailable("cycle build reads not configured")
	}
	out, err := h.cycleBuilds.ForCycle(ctx, tenant.BoundOrgFromContext(ctx), request.ProjectName, request.Tag, request.CycleID)
	if err != nil {
		return nil, mapRunError(err)
	}
	return gen.ListCycleBuilds200JSONResponse(*out), nil
}

// StreamRunProgress serves GET /projects/{p}/runs/{runId}/progress.
//
// The fences run HERE, before any byte is written, so a bad path answers a JSON
// envelope; the stream itself runs in Visit, after this returns.
func (h *Handler) StreamRunProgress(ctx context.Context, request gen.StreamRunProgressRequestObject) (gen.StreamRunProgressResponseObject, error) {
	if h.progress == nil {
		return nil, apierr.ServiceUnavailable("run progress stream not configured")
	}
	run, err := h.progress.OpenRunProgressStream(ctx, tenant.BoundOrgFromContext(ctx), request.ProjectName, request.RunID)
	if err != nil {
		return nil, mapRunError(err)
	}
	return runProgressStreamResponse{run: run}, nil
}

// StreamBuildProgress serves GET /projects/{p}/builds/{tag}/progress — the
// VERSION's narrative, stitched across every run on its milestone.
//
// Same fence discipline as StreamRunProgress, and for the same reason: the tag is
// resolved through the org-and-project-scoped run rows HERE, before any byte is
// written, so a version of another org or project answers a JSON 404 envelope
// rather than an empty stream a caller could read as "this version did nothing".
func (h *Handler) StreamBuildProgress(ctx context.Context, request gen.StreamBuildProgressRequestObject) (gen.StreamBuildProgressResponseObject, error) {
	if h.progress == nil {
		return nil, apierr.ServiceUnavailable("build progress stream not configured")
	}
	run, err := h.progress.OpenBuildProgressStream(ctx, tenant.BoundOrgFromContext(ctx), request.ProjectName, request.Tag)
	if err != nil {
		return nil, mapRunError(err)
	}
	return buildProgressStreamResponse{run: run}, nil
}

// CancelRun serves POST /projects/{p}/runs/{runId}/cancel.
func (h *Handler) CancelRun(ctx context.Context, request gen.CancelRunRequestObject) (gen.CancelRunResponseObject, error) {
	if h.commands == nil {
		return nil, apierr.ServiceUnavailable("run cancel not configured")
	}
	if err := h.commands.Cancel(ctx, tenant.BoundOrgFromContext(ctx), request.ProjectName, request.RunID); err != nil {
		return nil, mapRunError(err)
	}
	return gen.CancelRun202Response{}, nil
}

// RevalidateBuild serves POST /projects/{p}/builds/{tag}/revalidate.
//
// The body is optional — an absent one means both budgets take the platform
// default, which is the full repair loop.
func (h *Handler) RevalidateBuild(ctx context.Context, request gen.RevalidateBuildRequestObject) (gen.RevalidateBuildResponseObject, error) {
	if h.commands == nil {
		return nil, apierr.ServiceUnavailable("revalidate not configured")
	}
	var attempts, ceiling int
	if request.Body != nil {
		attempts = int(request.Body.ValidationAttempts)
		ceiling = int(request.Body.CycleCeiling)
	}
	runID, milestone, err := h.commands.Revalidate(
		ctx, tenant.BoundOrgFromContext(ctx), request.ProjectName, request.Tag, attempts, ceiling)
	if err != nil {
		return nil, mapRunError(err)
	}
	return gen.RevalidateBuild202JSONResponse{RunID: runID, MilestoneNumber: int64(milestone)}, nil
}

// runProgressStreamResponse adapts the connection loop onto the generated
// ResponseObject: the strict wrapper calls Visit after the handler returns,
// which is where the stream actually runs (the captured request ctx stays alive
// until the loop exits; its cancellation is the client-disconnect signal).
type runProgressStreamResponse struct {
	run func(w io.Writer, flush func())
}

func (r runProgressStreamResponse) VisitStreamRunProgressResponse(w http.ResponseWriter) error {
	return sseStream(w, r.run)
}

// buildProgressStreamResponse is runProgressStreamResponse's twin for the
// version stream. Two types rather than one generic wrapper because the
// generated ResponseObject interfaces are per-operation: each declares its own
// Visit method, and a shared type would have to satisfy both, which would let
// either handler return the other's stream.
type buildProgressStreamResponse struct {
	run func(w io.Writer, flush func())
}

func (r buildProgressStreamResponse) VisitStreamBuildProgressResponse(w http.ResponseWriter) error {
	return sseStream(w, r.run)
}

// mapRunError translates this slice's sentinels into the error envelope.
// Both not-found sentinels are 404 including the cross-org case: the org-scoped
// read misses, so "belongs to someone else" is indistinguishable from "does not
// exist" and existence never leaks.
func mapRunError(err error) error {
	switch {
	case errors.Is(err, ErrTagNotFound):
		return apierr.NotFound("no run for this version")
	case errors.Is(err, ErrRunNotFound):
		return apierr.NotFound("run not found")
	case errors.Is(err, ErrCycleNotFound):
		return apierr.NotFound("cycle not found")
	case errors.Is(err, delivery.ErrRunAlreadyLive), errors.Is(err, delivery.ErrMilestoneHasOpenWork):
		// Both mean "not in this state" rather than "not allowed", and both clear on
		// their own — the live run settles, the open work gets worked. The message is
		// the sentinel's own, which is written for the human who clicked.
		return apierr.Conflict(err.Error())
	case errors.Is(err, delivery.ErrNoValidationCriteria):
		// The request is well-formed and the version exists; there is simply nothing
		// to validate it against, which is the caller's to fix by authoring criteria.
		// A slice-owned code rather than the shared `validation_failed`, which here
		// would read as "the validation phase failed" — the opposite of what happened.
		return apierr.New(http.StatusUnprocessableEntity, "no_validation_criteria", err.Error(), nil)
	case errors.Is(err, delivery.ErrRunNotStarted):
		// The platform is not ready to work this version — a degraded boot, most
		// likely. Retryable, so 503 rather than a 500 the caller can do nothing with.
		return apierr.ServiceUnavailable(err.Error())
	case errors.Is(err, delivery.ErrTemporalUnavailable):
		// Nothing was cancelled and the caller may retry — a 503, not a 500.
		return apierr.ServiceUnavailable("the workflow engine is unavailable — nothing was cancelled")
	default:
		return apierr.Internal("internal error")
	}
}

// sseStream stamps the standard SSE response preamble — the four event-stream
// headers, an explicit 200, and an initial flush so the headers reach the client
// before the first frame — then hands the body writer + a per-chunk flush func
// to run. A slice-local copy, matching the other streaming slices: framing and
// loop logic stay per-feature, and this preamble is the only shared shape.
func sseStream(w http.ResponseWriter, run func(w io.Writer, flush func())) error {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flush := func() {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	flush()
	run(w, flush)
	return nil
}
