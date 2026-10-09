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

package execution

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Handler serves stream-task-log on the strict interface: GET
// /projects/{projectName}/tasks/{issueNumber}/log → text/event-stream. The
// handler runs the pre-stream fences via the service (a bad path answers a
// normal JSON envelope, never a broken half-stream) and returns a response
// object whose Visit method writes the SSE preamble and hands the body writer
// to the connection loop — the loop itself (frame framing, task/execution/
// line/done frames, keep-alives, settle semantics) is TaskStreamService.run,
// reused verbatim.
type Handler struct{ stream *TaskStreamService }

// NewHandler returns the slice's handler.
func NewHandler(stream *TaskStreamService) *Handler { return &Handler{stream: stream} }

func (h *Handler) StreamTaskLog(ctx context.Context, request gen.StreamTaskLogRequestObject) (gen.StreamTaskLogResponseObject, error) {
	if h.stream == nil {
		return nil, apierr.ServiceUnavailable("task stream not configured")
	}
	org := tenant.BoundOrgFromContext(ctx)
	run, err := h.stream.OpenTaskLogStream(ctx, org, request.ProjectName, int(request.IssueNumber))
	if err != nil {
		return nil, mapTaskStreamError(err)
	}
	return taskLogStreamResponse{run: run}, nil
}

// taskLogStreamResponse adapts the connection loop onto the generated
// ResponseObject: the strict wrapper calls Visit after the handler returns,
// which is where the stream actually runs (the captured request ctx stays
// alive until the loop exits; its cancellation is the client-disconnect
// signal).
type taskLogStreamResponse struct {
	run func(w io.Writer, flush func())
}

func (r taskLogStreamResponse) VisitStreamTaskLogResponse(w http.ResponseWriter) error {
	return sseStream(w, r.run)
}

// mapTaskStreamError translates the pre-stream fence sentinels into the
// envelope, mirroring the retired Huma registration's ladder.
func mapTaskStreamError(err error) error {
	switch {
	case errors.Is(err, ErrTaskStreamRepoNotFound):
		return apierr.NotFound("project repository not found")
	case errors.Is(err, ErrTaskStreamTaskNotFound):
		return apierr.NotFound("task not found")
	case errors.Is(err, ErrTaskStreamSnapshot):
		return apierr.WithCause(apierr.Internal("task snapshot failed"), err)
	default:
		return apierr.WithCause(apierr.Internal("internal error"), err)
	}
}

// sseStream stamps the standard SSE response preamble — the four event-stream
// headers, an explicit 200, and an initial flush so the headers reach the
// client before the first frame — then hands the body writer + a per-chunk
// flush func to run. It is the strict-server re-home of humakit.SSEBody:
// every streaming operation's response type wraps this in its
// VisitXxxResponse(w) method (frame framing and loop logic stay per-feature).
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
