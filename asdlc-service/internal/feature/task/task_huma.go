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
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/internal/contracts"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/humakit"
	"github.com/wso2/asdlc/asdlc-service/models"
	"github.com/wso2/asdlc/asdlc-service/repositories"
)

// --- Inputs / Outputs ------------------------------------------------------
// Inputs embed humakit.OrgScopedInput, which declares {orgHandle} and applies
// the tenant gate (the IDOR fence) by construction. Extra path params
// (projectName, taskId) are sibling fields; query params via `query:"…"`.

type listOrgTasksInput struct {
	humakit.OrgScopedInput
	Status string `query:"status" doc:"Filter by ComponentTask execution status"`
	Cause  string `query:"cause" doc:"Filter by abandonment cause"`
	Since  string `query:"since" doc:"RFC3339 timestamp or duration shorthand (e.g. 24h, 7d→168h)"`
}

type listTasksInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
}

type taskByIDInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	TaskID      string `path:"taskId" doc:"ComponentTask UUID"`
}

type taskProgressInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	TaskID      string `path:"taskId" doc:"ComponentTask UUID"`
	// SinceMillis/Limit mirror the controller's lenient cursor parsing: taken
	// as strings and parsed with strconv (a non-numeric value degrades to 0,
	// matching the legacy handler rather than 422-ing).
	SinceMillis string `query:"sinceMillis" doc:"Cursor: only return lines after this epoch-millis (0 ⇒ initial load)"`
	Limit       string `query:"limit" doc:"Max lines to return (0 ⇒ server default)"`
}

type generateTasksInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
}

type regenerateTaskBodyInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	TaskID      string `path:"taskId" doc:"ComponentTask UUID"`
}

type taskListOutput struct{ Body []models.ComponentTask }
type taskOutput struct{ Body *models.ComponentTask }
type tasksOutput struct{ Body *models.Tasks }
type taskStatusOutput struct{ Body *TaskStatusResponse }
type dispatchResultsOutput struct{ Body []contracts.DispatchResult }
type dispatchResultOutput struct{ Body contracts.DispatchResult }
type progressOutput struct{ Body *contracts.ProgressResponse }
type execStartedOutput struct{ Body map[string]string }

// RegisterTask registers the task feature's org-scoped, non-streaming HTTP
// operations on the Huma API. It is the code-first replacement for the
// org-scoped half of registerTaskRoutes (api/task_routes.go): same paths,
// methods, params, and auth posture, with the spec generated from the typed
// inputs/outputs.
//
// Deps mirror exactly what these handlers call (read from task_controller.go):
//   - svc         TaskService            — list/get/status/exec
//   - dispatchSvc TaskDispatcher         — dispatch + retry
//   - progressSvc ProgressReader         — agent/build progress polling
//   - ocClient    openchoreo.ComponentClient — build-run steps for GetTaskStatus
//
// SKIPPED here (not registered by this func):
//   - generate-tasks + regenerate-task-body — SSE streams (http.Flusher /
//     text/event-stream); they stay on the legacy mux.
//   - the runner-facing Task-JWT routes (verification-failed, skills,
//     credentials/refresh) — registered on the outer mux in api/app.go, not
//     org-scoped, handled separately.
func RegisterTask(
	api huma.API,
	svc TaskService,
	dispatchSvc TaskDispatcher,
	progressSvc ProgressReader,
	ocClient openchoreo.ComponentClient,
) {
	huma.Register(api, huma.Operation{
		OperationID: "list-org-tasks",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/tasks",
		Summary:     "List all tasks in an organization",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *listOrgTasksInput) (*taskListOutput, error) {
		filter := repositories.ListByOrgFilter{
			Status: in.Status,
			Cause:  in.Cause,
		}
		if rawSince := in.Since; rawSince != "" {
			if dur, err := time.ParseDuration(rawSince); err == nil {
				t := time.Now().Add(-dur)
				filter.Since = &t
			} else if t, err := time.Parse(time.RFC3339, rawSince); err == nil {
				filter.Since = &t
			} else {
				return nil, huma.Error400BadRequest("since must be RFC3339 or duration (e.g. 24h)")
			}
		}
		tasks, err := svc.ListTasksByOrg(ctx, in.OrgHandle, filter)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list org tasks")
		}
		return &taskListOutput{Body: tasks}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-tasks",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/tasks",
		Summary:     "List a project's tasks",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *listTasksInput) (*taskListOutput, error) {
		tasks, err := svc.ListTasks(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list tasks")
		}
		return &taskListOutput{Body: tasks}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-generated-tasks",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/generated",
		Summary:     "Get a project's generated tasks",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *listTasksInput) (*tasksOutput, error) {
		tasks, err := svc.GetTasks(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to get tasks")
		}
		// nil → serialized as null, mirroring the legacy nil-body 200.
		return &tasksOutput{Body: tasks}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-task",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}",
		Summary:     "Get a task",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *taskByIDInput) (*taskOutput, error) {
		task, err := svc.GetTaskScoped(ctx, in.OrgHandle, in.TaskID)
		if err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				return nil, huma.Error404NotFound("task not found")
			}
			return nil, huma.Error500InternalServerError("failed to get task")
		}
		return &taskOutput{Body: task}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-task-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}/status",
		Summary:     "Get a task's status with build-run steps",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *taskByIDInput) (*taskStatusOutput, error) {
		task, err := svc.GetTaskScoped(ctx, in.OrgHandle, in.TaskID)
		if err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				return nil, huma.Error404NotFound("task not found")
			}
			return nil, huma.Error500InternalServerError("failed to get task")
		}
		resp := TaskStatusResponse{Task: task}
		// Only fetch build steps while the task is actively building; once the
		// run is terminal the steps are frozen.
		if task.Status == string(models.TaskStatusBuilding) && task.LastBuildRunName != "" && ocClient != nil {
			if run, rerr := ocClient.GetWorkflowRun(ctx, task.OrgID, task.LastBuildRunName); rerr == nil && run != nil {
				resp.BuildSteps = run.Tasks
			}
		}
		return &taskStatusOutput{Body: &resp}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "dispatch-tasks",
		Method:      http.MethodPost,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/dispatch",
		Summary:     "Dispatch a project's tasks to the coding agent",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *listTasksInput) (*dispatchResultsOutput, error) {
		if dispatchSvc == nil {
			return nil, huma.Error503ServiceUnavailable("dispatch service not configured")
		}
		results, err := dispatchSvc.DispatchTasks(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to dispatch tasks")
		}
		return &dispatchResultsOutput{Body: results}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "exec-task",
		Method:      http.MethodPost,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}/exec",
		Summary:     "Execute a single task now",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *taskByIDInput) (*execStartedOutput, error) {
		// Org-scoped pre-check 404s on a cross-org/unknown {taskId} before work.
		if _, err := svc.GetTaskScoped(ctx, in.OrgHandle, in.TaskID); err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				return nil, huma.Error404NotFound("task not found")
			}
			return nil, huma.Error500InternalServerError("failed to get task")
		}
		if err := svc.ExecTask(ctx, in.TaskID); err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				return nil, huma.Error404NotFound("task not found")
			}
			return nil, huma.Error500InternalServerError("failed to execute task")
		}
		return &execStartedOutput{Body: map[string]string{"status": "task execution started"}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "retry-task",
		Method:      http.MethodPost,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}/retry",
		Summary:     "Retry a verification-failed task",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *taskByIDInput) (*dispatchResultOutput, error) {
		if dispatchSvc == nil {
			return nil, huma.Error503ServiceUnavailable("dispatch service not configured")
		}
		// Org-scoped pre-check 404s on a cross-org/unknown {taskId} before work.
		if _, err := svc.GetTaskScoped(ctx, in.OrgHandle, in.TaskID); err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				return nil, huma.Error404NotFound("task not found")
			}
			return nil, huma.Error500InternalServerError("failed to get task")
		}
		res, err := dispatchSvc.RetryTask(ctx, in.TaskID)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to retry task")
		}
		return &dispatchResultOutput{Body: res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-task-agent-progress",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}/progress/agent",
		Summary:     "Poll a task's coding-agent progress",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *taskProgressInput) (*progressOutput, error) {
		if progressSvc == nil {
			return nil, huma.Error503ServiceUnavailable("progress_unavailable")
		}
		// Org-scoped pre-check 404s on a cross-org/unknown {taskId} before
		// reading another org's coding-agent progress.
		if err := assertTaskInOrgHuma(ctx, svc, in.OrgHandle, in.TaskID); err != nil {
			return nil, err
		}
		sinceMillis, _ := strconv.ParseInt(in.SinceMillis, 10, 64)
		limit, _ := strconv.Atoi(in.Limit)
		resp, err := progressSvc.GetAgentProgress(ctx, in.TaskID, sinceMillis, limit)
		if err != nil {
			return nil, mapProgressError(err)
		}
		return &progressOutput{Body: resp}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-task-build-progress",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}/progress/build",
		Summary:     "Poll a task's build progress",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *taskProgressInput) (*progressOutput, error) {
		if progressSvc == nil {
			return nil, huma.Error503ServiceUnavailable("progress_unavailable")
		}
		if err := assertTaskInOrgHuma(ctx, svc, in.OrgHandle, in.TaskID); err != nil {
			return nil, err
		}
		sinceMillis, _ := strconv.ParseInt(in.SinceMillis, 10, 64)
		resp, err := progressSvc.GetBuildProgress(ctx, in.TaskID, sinceMillis)
		if err != nil {
			return nil, mapProgressError(err)
		}
		return &progressOutput{Body: resp}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "generate-tasks",
		Method:      http.MethodPost,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/generate",
		Summary:     "Generate a project's tasks (tech-lead agent, SSE stream)",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *generateTasksInput) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", "text/event-stream")
			hctx.SetHeader("Cache-Control", "no-cache")
			hctx.SetHeader("Connection", "keep-alive")
			hctx.SetHeader("X-Accel-Buffering", "no")
			hctx.SetHeader("x-vercel-ai-ui-message-stream", "v1")
			hctx.SetStatus(http.StatusOK)
			w := hctx.BodyWriter()
			flush := func() {
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			flush()
			// Stream errors are surfaced as UI message-stream error frames by the
			// service after headers are sent; the HTTP status cannot change here.
			_ = svc.StreamGenerateTasks(hctx.Context(), in.OrgHandle, in.ProjectName, w, flush)
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "regenerate-task-body",
		Method:      http.MethodPost,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}/regenerate-body",
		Summary:     "Regenerate a single task's body (Phase 2 detail, SSE stream)",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *regenerateTaskBodyInput) (*huma.StreamResponse, error) {
		// Org-scoped pre-check 404s on a cross-org/unknown {taskId} before any
		// stream is started (closes the by-UUID IDOR on this operator route).
		if _, err := svc.GetTaskScoped(ctx, in.OrgHandle, in.TaskID); err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				return nil, huma.Error404NotFound("task not found")
			}
			return nil, huma.Error500InternalServerError("failed to get task")
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", "text/event-stream")
			hctx.SetHeader("Cache-Control", "no-cache")
			hctx.SetHeader("Connection", "keep-alive")
			hctx.SetHeader("X-Accel-Buffering", "no")
			hctx.SetHeader("x-vercel-ai-ui-message-stream", "v1")
			hctx.SetStatus(http.StatusOK)
			w := hctx.BodyWriter()
			flush := func() {
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			flush()
			// Stream errors are surfaced as UI message-stream error frames by the
			// service after headers are sent; the HTTP status cannot change here.
			_ = svc.RegenerateTaskBody(hctx.Context(), in.TaskID, w, flush)
		}}, nil
	})
}

// assertTaskInOrgHuma verifies the {orgHandle} on the path actually owns
// {taskId} before a progress handler reads it by bare UUID — the Huma
// counterpart of (*taskController).assertTaskInOrg. Returns nil to proceed; a
// 404 on a cross-org/unknown task (no existence leak), a 500 on a load error.
func assertTaskInOrgHuma(ctx context.Context, svc TaskService, orgHandle, taskID string) error {
	if _, err := svc.GetTaskScoped(ctx, orgHandle, taskID); err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			return huma.Error404NotFound("task not found")
		}
		return huma.Error500InternalServerError("failed to get task")
	}
	return nil
}

// mapProgressError mirrors writeProgressError: ErrTaskNotFound → 404,
// contracts.ErrProgressUnavailable → 503, anything else → 500.
func mapProgressError(err error) error {
	switch {
	case errors.Is(err, ErrTaskNotFound):
		return huma.Error404NotFound("task not found")
	case errors.Is(err, contracts.ErrProgressUnavailable):
		return huma.Error503ServiceUnavailable("progress_unavailable")
	default:
		return huma.Error500InternalServerError("progress failed")
	}
}
