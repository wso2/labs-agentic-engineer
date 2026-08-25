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

package task

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Handler serves the task read surface (list-tasks / get-task) plus the
// promote-task-from-issue dispatch leg on the strict interface. Org comes from
// the gate-bound context and is passed to the service explicitly. The command
// and plan operations the retired Huma surface also carried (plan-tasks,
// execute-task, hold-task, unhold-task) are NOT in the committed contract and
// were deliberately dropped from the HTTP edge (parked proposal in
// packages/contracts/workflows). promote-task-from-issue STAYS: it is the
// dispatch leg of the SRE/RCA alert handoff, called by the deployed
// aep-mcp-server (AE-HANDOFF-DESIGN.md).
type Handler struct {
	reads    *Reads
	commands *Commands
}

// NewHandler returns the slice's handler.
func NewHandler(reads *Reads, commands *Commands) *Handler {
	return &Handler{reads: reads, commands: commands}
}

func (h *Handler) ListTasks(ctx context.Context, request gen.ListTasksRequestObject) (gen.ListTasksResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.reads == nil {
		return nil, errTasksNotConfigured()
	}
	state, tag := "", ""
	if request.Params.State != "" {
		state = string(request.Params.State)
	}
	if request.Params.Tag != "" {
		tag = request.Params.Tag
	}
	// Comments ride the read by default. The default is true rather than false
	// because the version-scoped read is the one surface that wants them, and
	// every other caller of this list is tag-less — where the read ignores the
	// flag anyway, so defaulting on costs those callers nothing. An explicit
	// false is the way out for a caller that wants the Tasks without the GitHub
	// round trip.
	comments := true
	if request.Params.Comments != nil {
		comments = *request.Params.Comments
	}
	views, err := h.reads.ListByTag(ctx, org, request.ProjectName, state, tag, comments)
	if err != nil {
		return nil, mapTaskReadError(err)
	}
	return listTasksJSONResponse(views), nil
}

func (h *Handler) GetTask(ctx context.Context, request gen.GetTaskRequestObject) (gen.GetTaskResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.reads == nil {
		return nil, errTasksNotConfigured()
	}
	detail, err := h.reads.Get(ctx, org, request.ProjectName, int(request.IssueNumber))
	if err != nil {
		return nil, mapTaskReadError(err)
	}
	return getTaskJSONResponse(*detail), nil
}

// PromoteTaskFromIssue turns an ad-hoc GitHub issue into a coding Task and
// dispatches it through the funnel (async 202, empty body). The second half
// of the SRE/RCA handoff: aep-mcp-server calls this right after create-issue.
func (h *Handler) PromoteTaskFromIssue(ctx context.Context, request gen.PromoteTaskFromIssueRequestObject) (gen.PromoteTaskFromIssueResponseObject, error) {
	if h.commands == nil {
		return nil, apierr.ServiceUnavailable("tasks not configured")
	}
	org := tenant.BoundOrgFromContext(ctx)
	if err := h.commands.PromoteAndExecute(ctx, org, request.ProjectName, request.Body.ComponentName, int(request.IssueNumber)); err != nil {
		return nil, mapTaskCommandError(err)
	}
	return gen.PromoteTaskFromIssue202Response{}, nil
}

// The 200 bodies are served from the delivery read DTOs (delivery.TaskView /
// delivery.TaskDetail) instead of the generated ListTasks200JSONResponse /
// GetTask200JSONResponse: the models generator's prefer-skip-optional-pointer
// renders the contract's OPTIONAL startedAt/endedAt (ExecutionView) as value
// time.Time fields whose `omitempty` never fires, so converting would stamp
// "0001-01-01T00:00:00Z" onto every absent timestamp — a wire regression the
// contract does not require. Marshaling the feature views verbatim keeps the
// wire identical to the retired Huma edge (generated-type defect noted in the
// migration report).

type listTasksJSONResponse []delivery.TaskView

func (r listTasksJSONResponse) VisitListTasksResponse(w http.ResponseWriter) error {
	return writeJSONBody(w, http.StatusOK, r)
}

type getTaskJSONResponse delivery.TaskDetail

func (r getTaskJSONResponse) VisitGetTaskResponse(w http.ResponseWriter) error {
	return writeJSONBody(w, http.StatusOK, delivery.TaskDetail(r))
}

// errTasksNotConfigured is the nil-service guard the Huma registration carried
// (503 "tasks not configured") — kept verbatim on the strict edge.
func errTasksNotConfigured() error {
	return apierr.ServiceUnavailable("tasks not configured")
}

// mapTaskReadError translates the read-path sentinels into the envelope,
// mirroring the retired mapReadError ladder.
func mapTaskReadError(err error) error {
	switch {
	case errors.Is(err, ErrTaskNotFound):
		return apierr.NotFound("task not found")
	case errors.Is(err, ErrProjectRepoNotFound):
		return apierr.NotFound(ErrProjectRepoNotFound.Error())
	default:
		return apierr.Internal("internal error")
	}
}

// mapTaskCommandError mirrors the retired mapCommandError ladder.
func mapTaskCommandError(err error) error {
	switch {
	case errors.Is(err, ErrTaskNotFound):
		return apierr.NotFound("task not found")
	case errors.Is(err, ErrProjectRepoNotFound):
		return apierr.NotFound(ErrProjectRepoNotFound.Error())
	case errors.Is(err, ErrIssueClosed):
		return apierr.Conflict("issue is closed")
	case errors.Is(err, ErrComponentNameRequired):
		return apierr.BadRequest(ErrComponentNameRequired.Error())
	default:
		return apierr.Internal("internal error")
	}
}

// writeJSONBody is the slice-local JSON response writer (copied from the edge's
// response_helpers.go). Buffered: an encode failure surfaces as an error BEFORE
// headers commit (the strict wrapper then serves its 500 envelope) instead of a
// half-written body.
func writeJSONBody(w http.ResponseWriter, status int, body any) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}
