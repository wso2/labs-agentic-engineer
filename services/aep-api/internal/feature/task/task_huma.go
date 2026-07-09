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
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/aep/aep-api/internal/platform/humakit"
)

// planInProgressError renders the 409 body as {"code":"plan_in_progress"} (§9.1).
type planInProgressError struct {
	Code string `json:"code"`
}

func (e *planInProgressError) Error() string {
	return "a plan turn is already running for this project"
}
func (e *planInProgressError) GetStatus() int { return http.StatusConflict }

// --- inputs / outputs -------------------------------------------------------

type planInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
}

type listInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	State       string `query:"state" enum:"open,closed,all" doc:"Which Tasks to return (default open)"`
}

type listOutput struct {
	Body []TaskView
}

type getInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	IssueNumber int    `path:"issueNumber" doc:"GitHub issue number of the Task"`
}

type getOutput struct {
	Body *TaskDetail
}

type issueActionInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	IssueNumber int    `path:"issueNumber" doc:"GitHub issue number of the Task"`
}

type emptyOutput struct{}

// RegisterTask registers the Task surface (plan / list / get / execute / hold)
// on the code-first Huma API. Org is derived from the verified token
// (humakit.OrgScopedInput) — a cross-org request is unrepresentable.
func RegisterTask(api huma.API, reads *Reads, commands *Commands, plan *PlanService) {
	huma.Register(api, huma.Operation{
		OperationID: "plan-tasks",
		Method:      http.MethodPost,
		Path:        "/projects/{projectName}/tasks/plan",
		Summary:     "Plan implementation Tasks (SSE — raw StreamPart frames + [DONE])",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *planInput) (*huma.StreamResponse, error) {
		if plan == nil {
			return nil, huma.Error503ServiceUnavailable("planning not configured")
		}
		session, err := plan.StartPlan(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			return nil, mapPlanError(err)
		}
		return &huma.StreamResponse{Body: humakit.SSEBody(session.Stream)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-tasks",
		Method:      http.MethodGet,
		Path:        "/projects/{projectName}/tasks",
		Summary:     "List a project's Tasks (live GitHub ⋈ executions → derived status)",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *listInput) (*listOutput, error) {
		if reads == nil {
			return nil, huma.Error503ServiceUnavailable("tasks not configured")
		}
		views, err := reads.List(ctx, in.OrgHandle, in.ProjectName, in.State)
		if err != nil {
			return nil, mapReadError(err)
		}
		return &listOutput{Body: views}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-task",
		Method:      http.MethodGet,
		Path:        "/projects/{projectName}/tasks/{issueNumber}",
		Summary:     "Get one Task with its full Execution history",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *getInput) (*getOutput, error) {
		if reads == nil {
			return nil, huma.Error503ServiceUnavailable("tasks not configured")
		}
		detail, err := reads.Get(ctx, in.OrgHandle, in.ProjectName, in.IssueNumber)
		if err != nil {
			return nil, mapReadError(err)
		}
		return &getOutput{Body: detail}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "execute-task",
		Method:        http.MethodPost,
		Path:          "/projects/{projectName}/tasks/{issueNumber}/execute",
		Summary:       "Stamp aep:execute and dispatch through the funnel (async)",
		Tags:          []string{"Tasks"},
		Security:      humakit.SecurityUserJWT,
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, in *issueActionInput) (*emptyOutput, error) {
		if commands == nil {
			return nil, huma.Error503ServiceUnavailable("tasks not configured")
		}
		if err := commands.Execute(ctx, in.OrgHandle, in.ProjectName, in.IssueNumber); err != nil {
			return nil, mapCommandError(err)
		}
		return &emptyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "hold-task",
		Method:        http.MethodPost,
		Path:          "/projects/{projectName}/tasks/{issueNumber}/hold",
		Summary:       "Set the aep:hold label (level-triggered; idempotent)",
		Tags:          []string{"Tasks"},
		Security:      humakit.SecurityUserJWT,
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *issueActionInput) (*emptyOutput, error) {
		if commands == nil {
			return nil, huma.Error503ServiceUnavailable("tasks not configured")
		}
		if err := commands.Hold(ctx, in.OrgHandle, in.ProjectName, in.IssueNumber); err != nil {
			return nil, mapCommandError(err)
		}
		return &emptyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "unhold-task",
		Method:        http.MethodDelete,
		Path:          "/projects/{projectName}/tasks/{issueNumber}/hold",
		Summary:       "Remove the aep:hold label and re-evaluate (idempotent)",
		Tags:          []string{"Tasks"},
		Security:      humakit.SecurityUserJWT,
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *issueActionInput) (*emptyOutput, error) {
		if commands == nil {
			return nil, huma.Error503ServiceUnavailable("tasks not configured")
		}
		if err := commands.Unhold(ctx, in.OrgHandle, in.ProjectName, in.IssueNumber); err != nil {
			return nil, mapCommandError(err)
		}
		return &emptyOutput{}, nil
	})
}

func mapPlanError(err error) error {
	if mapped, ok := humakit.MapAgentsUpstreamError(err, map[int]error{
		http.StatusConflict: &planInProgressError{Code: "plan_in_progress"},
	}); ok {
		return mapped
	}
	switch {
	case errors.Is(err, ErrPlanInProgress):
		return &planInProgressError{Code: "plan_in_progress"}
	case errors.Is(err, ErrNoApprovedDesign):
		return huma.Error400BadRequest(ErrNoApprovedDesign.Error())
	case errors.Is(err, ErrNoAnthropicKey):
		return huma.Error400BadRequest(ErrNoAnthropicKey.Error())
	case errors.Is(err, ErrProjectRepoNotFound):
		return huma.Error404NotFound(ErrProjectRepoNotFound.Error())
	case errors.Is(err, ErrSkillsRepoUnavailable):
		// Platform-side dependency down (see ErrSkillsRepoUnavailable) — a clear
		// 503 with the cause logged. (This mapper is not ctx-threaded yet; a
		// broader mapper ctx-threading + default-500-logging pass is a follow-up.)
		slog.ErrorContext(context.Background(), "task plan: org skills repository unavailable", "error", err)
		return huma.Error503ServiceUnavailable("org skills repository unavailable — contact your platform admin")
	default:
		return huma.Error500InternalServerError("internal error")
	}
}

func mapReadError(err error) error {
	switch {
	case errors.Is(err, ErrTaskNotFound):
		return huma.Error404NotFound("task not found")
	case errors.Is(err, ErrProjectRepoNotFound):
		return huma.Error404NotFound(ErrProjectRepoNotFound.Error())
	default:
		return huma.Error500InternalServerError("internal error")
	}
}

func mapCommandError(err error) error {
	switch {
	case errors.Is(err, ErrTaskNotFound):
		return huma.Error404NotFound("task not found")
	case errors.Is(err, ErrProjectRepoNotFound):
		return huma.Error404NotFound(ErrProjectRepoNotFound.Error())
	case errors.Is(err, ErrIssueClosed):
		return huma.Error409Conflict("issue is closed")
	default:
		return huma.Error500InternalServerError("internal error")
	}
}
