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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/internal/contracts"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/orgcreds"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/auth"
	"github.com/wso2/asdlc/asdlc-service/models"
	"github.com/wso2/asdlc/asdlc-service/repositories"
	"github.com/wso2/asdlc/asdlc-service/utils"
)

type TaskController interface {
	ListTasks(w http.ResponseWriter, r *http.Request)
	ListOrgTasks(w http.ResponseWriter, r *http.Request)
	GetTask(w http.ResponseWriter, r *http.Request)
	GetTaskStatus(w http.ResponseWriter, r *http.Request)
	GetTasks(w http.ResponseWriter, r *http.Request)
	DispatchTasks(w http.ResponseWriter, r *http.Request)
	GenerateTasks(w http.ResponseWriter, r *http.Request)
	RegenerateTaskBody(w http.ResponseWriter, r *http.Request)
	ExecTask(w http.ResponseWriter, r *http.Request)

	// Agent-driven verification failure + operator retry.
	// VerificationFailed authenticates the caller with the per-task JWT
	// minted at dispatch (verified locally by TaskTokenManager.Verify
	// against the BFF's own signing key). Retry is operator-only and
	// uses the standard auth middleware.
	VerificationFailed(w http.ResponseWriter, r *http.Request)
	Retry(w http.ResponseWriter, r *http.Request)

	// Skills is called by the dispatched agent inside the runner pod at
	// init. Returns the snapshotted skill bodies for the task's
	// (project_id, design_version). Authenticated via the per-task JWT.
	Skills(w http.ResponseWriter, r *http.Request)

	// RefreshCredentials is the path-scoped credential refresh endpoint.
	// Accepts both the BFF-signed TaskJWT and Thunder-issued publisher cc
	// tokens via authorizeRunnerCallback. Used by the runner's credhelper.
	RefreshCredentials(w http.ResponseWriter, r *http.Request)

	// Progress endpoints — task-execution-progress.md §5.2.
	GetTaskAgentProgress(w http.ResponseWriter, r *http.Request)
	GetTaskBuildProgress(w http.ResponseWriter, r *http.Request)
}

type taskController struct {
	service           TaskService
	dispatchSvc       TaskDispatcher
	progressSvc       ProgressReader
	ocClient          openchoreo.ComponentClient
	taskTokens        *auth.TaskTokenManager
	publisherVerifier *auth.PublisherTokenVerifier
	skillsSvc         *TaskSkillsService
	credsRefreshSvc   orgcreds.CredentialsRefreshService
}

func NewTaskController(
	service TaskService,
	dispatchSvc TaskDispatcher,
	progressSvc ProgressReader,
	ocClient openchoreo.ComponentClient,
	taskTokens *auth.TaskTokenManager,
) TaskController {
	return &taskController{
		service:     service,
		dispatchSvc: dispatchSvc,
		progressSvc: progressSvc,
		ocClient:    ocClient,
		taskTokens:  taskTokens,
	}
}

// SetSkillsService wires the per-task skills pull endpoint. Optional —
// when nil, the handler returns 503.
func (c *taskController) SetSkillsService(s *TaskSkillsService) {
	c.skillsSvc = s
}

// SetPublisherVerifier wires the publisher cc token verifier so the
// runner-callback handlers accept Thunder-issued cc tokens alongside the
// BFF-signed TaskJWTs. Optional — when nil, only TaskJWTs work.
func (c *taskController) SetPublisherVerifier(v *auth.PublisherTokenVerifier) {
	c.publisherVerifier = v
}

// SetCredentialsRefreshService wires the credentials-refresh service so
// the path-scoped /api/v1/tasks/{taskId}/credentials/refresh endpoint can
// delegate. Optional — when nil, the handler returns 503 and the runner
// must fall back to the /api/v1/credentials/refresh route (TaskJWT path).
func (c *taskController) SetCredentialsRefreshService(s orgcreds.CredentialsRefreshService) {
	c.credsRefreshSvc = s
}

// The composition root wires the optional setters above via type-assertion
// (NewTaskController returns the TaskController interface). Naming them and
// asserting *taskController satisfies each turns a setter-signature drift into
// a build failure rather than a wire silently skipped at boot.
type (
	SkillsServiceSetter interface {
		SetSkillsService(*TaskSkillsService)
	}
	PublisherVerifierSetter interface {
		SetPublisherVerifier(*auth.PublisherTokenVerifier)
	}
	CredentialsRefreshSetter interface {
		SetCredentialsRefreshService(orgcreds.CredentialsRefreshService)
	}
)

var (
	_ SkillsServiceSetter      = (*taskController)(nil)
	_ PublisherVerifierSetter  = (*taskController)(nil)
	_ CredentialsRefreshSetter = (*taskController)(nil)
)

// authorizeRunnerCallback validates the inbound Authorization header for
// runner-facing routes (Skills, VerificationFailed, the per-task
// /credentials/refresh). Tries the BFF TaskJWT first; on failure tries
// the publisher cc verifier. Returns the canonical org handle the caller
// may need for downstream lookups.
//
// On error, writes the HTTP error response and returns ok=false.
func (c *taskController) authorizeRunnerCallback(w http.ResponseWriter, r *http.Request, taskID string) (orgHandle string, ok bool) {
	authz := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(authz) <= len(prefix) || authz[:len(prefix)] != prefix {
		utils.WriteErrorResponse(w, http.StatusUnauthorized, "bearer token required")
		return "", false
	}
	tok := authz[len(prefix):]

	if c.taskTokens != nil {
		if claims, err := c.taskTokens.Verify(tok); err == nil {
			if claims.TaskID != taskID {
				slog.WarnContext(r.Context(), "runner callback: task bearer subject mismatch",
					"task", taskID, "claimTaskId", claims.TaskID)
				utils.WriteErrorResponse(w, http.StatusForbidden, "task bearer does not match path")
				return "", false
			}
			return claims.OcOrgID, true
		}
	}

	if c.publisherVerifier != nil {
		if claims, err := c.publisherVerifier.Verify(tok); err == nil {
			task, terr := c.service.GetTask(r.Context(), taskID)
			if terr != nil || task == nil {
				slog.WarnContext(r.Context(), "runner callback: task lookup failed",
					"task", taskID, "error", terr)
				utils.WriteErrorResponse(w, http.StatusForbidden, "task not found")
				return "", false
			}
			if task.OrgID != claims.OrgHandle {
				slog.WarnContext(r.Context(), "runner callback: publisher org mismatch",
					"task", taskID, "taskOrg", task.OrgID, "publisherOrg", claims.OrgHandle)
				utils.WriteErrorResponse(w, http.StatusForbidden, "publisher token does not match task org")
				return "", false
			}
			return claims.OrgHandle, true
		}
	}

	slog.WarnContext(r.Context(), "runner callback: bearer rejected by all verifiers", "task", taskID)
	utils.WriteErrorResponse(w, http.StatusUnauthorized, "invalid bearer")
	return "", false
}

func (c *taskController) ListTasks(w http.ResponseWriter, r *http.Request) {
	org := r.PathValue("orgHandle")
	project := r.PathValue("projectName")
	if !requireOrgHandle(w, org) || !requireProjectName(w, project) {
		return
	}

	tasks, err := c.service.ListTasks(r.Context(), org, project)
	if err != nil {
		slog.ErrorContext(r.Context(), "list tasks failed", "error", err, "org", org, "project", project)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}

	utils.WriteSuccessResponse(w, http.StatusOK, tasks)
}

// ListOrgTasks lists every task under the org with optional ?status, ?cause,
// and ?since filters. since accepts either an RFC3339 timestamp or a relative
// "24h" / "7d" shorthand. Used by the ReachReconciliationBanner.
func (c *taskController) ListOrgTasks(w http.ResponseWriter, r *http.Request) {
	org := r.PathValue("orgHandle")
	if !requireOrgHandle(w, org) {
		return
	}
	q := r.URL.Query()
	filter := repositories.ListByOrgFilter{
		Status: q.Get("status"),
		Cause:  q.Get("cause"),
	}
	if rawSince := q.Get("since"); rawSince != "" {
		if dur, err := time.ParseDuration(rawSince); err == nil {
			t := time.Now().Add(-dur)
			filter.Since = &t
		} else if t, err := time.Parse(time.RFC3339, rawSince); err == nil {
			filter.Since = &t
		} else {
			utils.WriteErrorResponse(w, http.StatusBadRequest, "since must be RFC3339 or duration (e.g. 24h)")
			return
		}
	}
	tasks, err := c.service.ListTasksByOrg(r.Context(), org, filter)
	if err != nil {
		slog.ErrorContext(r.Context(), "list org tasks failed", "error", err, "org", org)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to list org tasks")
		return
	}
	utils.WriteSuccessResponse(w, http.StatusOK, tasks)
}

func (c *taskController) GetTask(w http.ResponseWriter, r *http.Request) {
	orgHandle := r.PathValue("orgHandle")
	taskID := r.PathValue("taskId")
	if !requireOrgHandle(w, orgHandle) || !requireTaskID(w, taskID) {
		return
	}

	// Load org-scoped so a cross-org {taskId} cannot leak another org's task
	// (closes the by-UUID IDOR on this operator route).
	task, err := c.service.GetTaskScoped(r.Context(), orgHandle, taskID)
	if err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			utils.WriteErrorResponse(w, http.StatusNotFound, "task not found")
			return
		}
		slog.ErrorContext(r.Context(), "get task failed", "error", err, "taskId", taskID)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to get task")
		return
	}

	utils.WriteSuccessResponse(w, http.StatusOK, task)
}

func (c *taskController) GetTasks(w http.ResponseWriter, r *http.Request) {
	org := r.PathValue("orgHandle")
	project := r.PathValue("projectName")
	if !requireOrgHandle(w, org) || !requireProjectName(w, project) {
		return
	}

	tasks, err := c.service.GetTasks(r.Context(), org, project)
	if err != nil {
		slog.ErrorContext(r.Context(), "get tasks failed", "error", err, "org", org, "project", project)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to get tasks")
		return
	}

	if tasks == nil {
		utils.WriteSuccessResponse(w, http.StatusOK, nil)
		return
	}
	utils.WriteSuccessResponse(w, http.StatusOK, tasks)
}

func (c *taskController) DispatchTasks(w http.ResponseWriter, r *http.Request) {
	if c.dispatchSvc == nil {
		utils.WriteErrorResponse(w, http.StatusServiceUnavailable, "dispatch service not configured")
		return
	}

	org := r.PathValue("orgHandle")
	project := r.PathValue("projectName")
	if !requireOrgHandle(w, org) || !requireProjectName(w, project) {
		return
	}

	results, err := c.dispatchSvc.DispatchTasks(r.Context(), org, project)
	if err != nil {
		slog.ErrorContext(r.Context(), "dispatch tasks failed", "error", err, "org", org, "project", project)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to dispatch tasks")
		return
	}

	utils.WriteSuccessResponse(w, http.StatusOK, results)
}

// GenerateTasks streams the two-phase tech-lead orchestration as SSE.
// Mirrors design_controller.GenerateDesign.
func (c *taskController) GenerateTasks(w http.ResponseWriter, r *http.Request) {
	org := r.PathValue("orgHandle")
	project := r.PathValue("projectName")
	if !requireOrgHandle(w, org) || !requireProjectName(w, project) {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("x-vercel-ai-ui-message-stream", "v1")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	if err := c.service.StreamGenerateTasks(r.Context(), org, project, w, flusher.Flush); err != nil {
		slog.ErrorContext(r.Context(), "generate tasks failed", "error", err, "org", org, "project", project)
		errText := err.Error()
		switch {
		case errors.Is(err, artifacts.ErrDesignNotFound):
			errText = "design not found"
		case errors.Is(err, artifacts.ErrSpecNotFound):
			errText = "spec not found"
		}
		errFrame, _ := json.Marshal(map[string]any{"type": "error", "data": map[string]any{"scope": "plan", "errorText": errText}})
		fmt.Fprintf(w, "data: %s\n\n", errFrame)
		flusher.Flush()
	}
}

// RegenerateTaskBody re-runs Phase 2 detail for a single task.
func (c *taskController) RegenerateTaskBody(w http.ResponseWriter, r *http.Request) {
	orgHandle := r.PathValue("orgHandle")
	taskID := r.PathValue("taskId")
	if !requireOrgHandle(w, orgHandle) || !requireTaskID(w, taskID) {
		return
	}
	// Org-scoped pre-check 404s on a cross-org/unknown {taskId} before any
	// work (closes the by-UUID IDOR on this operator route).
	if _, err := c.service.GetTaskScoped(r.Context(), orgHandle, taskID); err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			utils.WriteErrorResponse(w, http.StatusNotFound, "task not found")
			return
		}
		slog.ErrorContext(r.Context(), "regenerate task body: load task failed", "error", err, "taskId", taskID)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to get task")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("x-vercel-ai-ui-message-stream", "v1")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	if err := c.service.RegenerateTaskBody(r.Context(), taskID, w, flusher.Flush); err != nil {
		slog.ErrorContext(r.Context(), "regenerate task body failed", "taskId", taskID, "error", err)
		errFrame, _ := json.Marshal(map[string]any{"type": "error", "data": map[string]any{"scope": "detail", "taskId": taskID, "errorText": err.Error()}})
		fmt.Fprintf(w, "data: %s\n\n", errFrame)
		flusher.Flush()
	}
}

// TaskStatusResponse extends the per-task GET payload with the build
// run's per-step task list, so the console's pipeline strip can render
// without an extra round-trip. The task fields are inlined alongside a
// separate buildSteps slice — design §5.2.
type TaskStatusResponse struct {
	Task       *models.ComponentTask    `json:"task"`
	BuildSteps []models.WorkflowRunTask `json:"buildSteps,omitempty"`
}

// GetTaskStatus combines ComponentTask + WorkflowRun.Status.Tasks[] for
// the build run (when present). No new persisted state.
func (c *taskController) GetTaskStatus(w http.ResponseWriter, r *http.Request) {
	orgHandle := r.PathValue("orgHandle")
	taskID := r.PathValue("taskId")
	if !requireOrgHandle(w, orgHandle) || !requireTaskID(w, taskID) {
		return
	}

	// Org-scoped load 404s on a cross-org/unknown {taskId} before any work
	// (closes the by-UUID IDOR that would otherwise expose another org's
	// task / OC run).
	task, err := c.service.GetTaskScoped(r.Context(), orgHandle, taskID)
	if err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			utils.WriteErrorResponse(w, http.StatusNotFound, "task not found")
			return
		}
		slog.ErrorContext(r.Context(), "get task status: load task failed", "error", err, "taskId", taskID)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to get task")
		return
	}

	resp := TaskStatusResponse{Task: task}
	// Only fetch build steps while the task is actively building. Once the
	// run is terminal the steps are frozen — fetching on every poll for a
	// `deployed`/`failed` task is wasted OC calls.
	if task.Status == string(models.TaskStatusBuilding) && task.LastBuildRunName != "" && c.ocClient != nil {
		run, err := c.ocClient.GetWorkflowRun(r.Context(), task.OrgID, task.LastBuildRunName)
		if err != nil {
			slog.WarnContext(r.Context(), "get task status: load build run failed",
				"error", err, "run", task.LastBuildRunName)
		} else if run != nil {
			resp.BuildSteps = run.Tasks
		}
	}
	utils.WriteSuccessResponse(w, http.StatusOK, resp)
}

// GetTaskAgentProgress returns coding-agent NDJSON lines pulled from
// Observer for the per-task WorkflowRun's pod stdout. Cursor-driven —
// pass ?sinceMillis=N (0 ⇒ initial load anchored to task.DispatchedAt).
func (c *taskController) GetTaskAgentProgress(w http.ResponseWriter, r *http.Request) {
	orgHandle := r.PathValue("orgHandle")
	taskID := r.PathValue("taskId")
	if !requireOrgHandle(w, orgHandle) || !requireTaskID(w, taskID) {
		return
	}
	if c.progressSvc == nil {
		utils.WriteErrorResponse(w, http.StatusServiceUnavailable, "progress_unavailable")
		return
	}
	// Org-scoped pre-check 404s on a cross-org/unknown {taskId} before reading
	// another org's coding-agent progress. The OrgScoped gate only matches the
	// path org against the JWT, not task ownership — without this a caller could
	// read any org's agent logs by passing that org's taskId under their own path.
	if !c.assertTaskInOrg(w, r, orgHandle, taskID) {
		return
	}
	sinceMillis, _ := strconv.ParseInt(r.URL.Query().Get("sinceMillis"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	resp, err := c.progressSvc.GetAgentProgress(r.Context(), taskID, sinceMillis, limit)
	if err != nil {
		writeProgressError(w, r, err, "get agent progress")
		return
	}
	utils.WriteSuccessResponse(w, http.StatusOK, resp)
}

// GetTaskBuildProgress returns synthetic build_step lines derived from
// the build WorkflowRun's per-step Phase/Message/timestamps. Cursor
// driven — same shape as /progress/agent.
func (c *taskController) GetTaskBuildProgress(w http.ResponseWriter, r *http.Request) {
	orgHandle := r.PathValue("orgHandle")
	taskID := r.PathValue("taskId")
	if !requireOrgHandle(w, orgHandle) || !requireTaskID(w, taskID) {
		return
	}
	if c.progressSvc == nil {
		utils.WriteErrorResponse(w, http.StatusServiceUnavailable, "progress_unavailable")
		return
	}
	// Org-scoped pre-check 404s on a cross-org/unknown {taskId} before reading
	// another org's build progress (same by-UUID IDOR class as the agent route).
	if !c.assertTaskInOrg(w, r, orgHandle, taskID) {
		return
	}
	sinceMillis, _ := strconv.ParseInt(r.URL.Query().Get("sinceMillis"), 10, 64)

	resp, err := c.progressSvc.GetBuildProgress(r.Context(), taskID, sinceMillis)
	if err != nil {
		writeProgressError(w, r, err, "get build progress")
		return
	}
	utils.WriteSuccessResponse(w, http.StatusOK, resp)
}

// assertTaskInOrg verifies the {orgHandle} on the path actually owns {taskId}
// before a progress handler reads it by bare UUID. Returns true to proceed;
// on a cross-org/unknown task it writes 404 (no existence leak) and returns
// false, on a load error it writes 500 and returns false.
func (c *taskController) assertTaskInOrg(w http.ResponseWriter, r *http.Request, orgHandle, taskID string) bool {
	if _, err := c.service.GetTaskScoped(r.Context(), orgHandle, taskID); err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			utils.WriteErrorResponse(w, http.StatusNotFound, "task not found")
			return false
		}
		slog.ErrorContext(r.Context(), "progress: load task failed", "error", err, "taskId", taskID)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to get task")
		return false
	}
	return true
}

func writeProgressError(w http.ResponseWriter, r *http.Request, err error, op string) {
	if errors.Is(err, ErrTaskNotFound) {
		utils.WriteErrorResponse(w, http.StatusNotFound, "task not found")
		return
	}
	if errors.Is(err, contracts.ErrProgressUnavailable) {
		utils.WriteErrorResponse(w, http.StatusServiceUnavailable, "progress_unavailable")
		return
	}
	slog.ErrorContext(r.Context(), op+" failed", "error", err)
	utils.WriteErrorResponse(w, http.StatusInternalServerError, op+" failed")
}

// verificationFailedRequest is the per-task-bearer-authed JSON body for
// POST /api/v1/tasks/{taskId}/verification-failed. The diagnostic field
// is optional but strongly encouraged so the operator can see what the
// agent observed.
type verificationFailedRequest struct {
	Diagnostic string `json:"diagnostic"`
}

// VerificationFailed is called by the dispatched agent inside the
// runner pod when it detects that a dependency endpoint is not behaving
// as the spec describes. Authenticated via the per-task JWT the runner
// already holds. The handler verifies the JWT, asserts the subject
// matches the URL's taskId, then drives in_progress → verification_failed.
func (c *taskController) VerificationFailed(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	if !requireTaskID(w, taskID) {
		return
	}
	if c.dispatchSvc == nil || c.taskTokens == nil {
		utils.WriteErrorResponse(w, http.StatusServiceUnavailable, "verification-failed not configured")
		return
	}

	if _, ok := c.authorizeRunnerCallback(w, r, taskID); !ok {
		return
	}

	var req verificationFailedRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // diagnostic is optional
	}
	if err := c.dispatchSvc.MarkVerificationFailed(r.Context(), taskID, req.Diagnostic); err != nil {
		slog.ErrorContext(r.Context(), "verification-failed: apply transition failed", "task", taskID, "error", err)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to apply verification_failed")
		return
	}
	utils.WriteSuccessResponse(w, http.StatusAccepted, map[string]string{"status": "verification_failed"})
}

// Retry is the operator-driven retry path: transitions
// verification_failed → in_progress and re-dispatches with a fresh
// WorkflowRun + freshly minted per-task bearer. Standard user auth
// applies (mounted on the org/project-scoped task path).
func (c *taskController) Retry(w http.ResponseWriter, r *http.Request) {
	orgHandle := r.PathValue("orgHandle")
	taskID := r.PathValue("taskId")
	if !requireOrgHandle(w, orgHandle) || !requireTaskID(w, taskID) {
		return
	}
	if c.dispatchSvc == nil {
		utils.WriteErrorResponse(w, http.StatusServiceUnavailable, "dispatch service not configured")
		return
	}
	// Org-scoped pre-check 404s on a cross-org/unknown {taskId} before any
	// dispatch work (closes the by-UUID IDOR on this operator route).
	if _, err := c.service.GetTaskScoped(r.Context(), orgHandle, taskID); err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			utils.WriteErrorResponse(w, http.StatusNotFound, "task not found")
			return
		}
		slog.ErrorContext(r.Context(), "retry: load task failed", "task", taskID, "error", err)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to get task")
		return
	}
	res, err := c.dispatchSvc.RetryTask(r.Context(), taskID)
	if err != nil {
		slog.ErrorContext(r.Context(), "retry: failed", "task", taskID, "error", err)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to retry task")
		return
	}
	utils.WriteSuccessResponse(w, http.StatusOK, res)
}

func (c *taskController) ExecTask(w http.ResponseWriter, r *http.Request) {
	orgHandle := r.PathValue("orgHandle")
	taskID := r.PathValue("taskId")
	if !requireOrgHandle(w, orgHandle) || !requireTaskID(w, taskID) {
		return
	}

	// Org-scoped pre-check 404s on a cross-org/unknown {taskId} before any
	// work (closes the by-UUID IDOR on this operator route).
	if _, err := c.service.GetTaskScoped(r.Context(), orgHandle, taskID); err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			utils.WriteErrorResponse(w, http.StatusNotFound, "task not found")
			return
		}
		slog.ErrorContext(r.Context(), "exec task: load task failed", "error", err, "taskId", taskID)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to get task")
		return
	}

	if err := c.service.ExecTask(r.Context(), taskID); err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			utils.WriteErrorResponse(w, http.StatusNotFound, "task not found")
			return
		}
		slog.ErrorContext(r.Context(), "exec task failed", "error", err, "taskId", taskID)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to execute task")
		return
	}

	utils.WriteSuccessResponse(w, http.StatusOK, map[string]string{"status": "task execution started"})
}

// Skills is called by the runner pod at init time to fetch the
// snapshotted SKILL.md bodies for this task's (project_id,
// design_version). Authenticated via the same per-task bearer used by
// VerificationFailed.
//
// Response shape: {"skills": [{ id, materializedName, kind, skillMd, references }]}
// Empty list (NOT 404) when the task has no snapshot — e.g. designs with
// no attached skills.
func (c *taskController) Skills(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	if !requireTaskID(w, taskID) {
		return
	}
	if c.skillsSvc == nil || c.taskTokens == nil {
		utils.WriteErrorResponse(w, http.StatusServiceUnavailable, "skills endpoint not configured")
		return
	}

	if _, ok := c.authorizeRunnerCallback(w, r, taskID); !ok {
		return
	}

	resp, err := c.skillsSvc.SkillsForTask(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			utils.WriteErrorResponse(w, http.StatusNotFound, "task not found")
			return
		}
		slog.ErrorContext(r.Context(), "skills: lookup failed", "task", taskID, "error", err)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to read skills")
		return
	}

	utils.WriteSuccessResponse(w, http.StatusOK, resp)
}

// RefreshCredentials is the path-scoped credential refresh endpoint that
// accepts either a TaskJWT or a Thunder-issued publisher cc token, so the
// same auth-mode the runner uses for everything else also covers
// /credentials/refresh. The route `POST /api/v1/credentials/refresh`
// (TaskJWT only) also exists.
func (c *taskController) RefreshCredentials(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	if !requireTaskID(w, taskID) {
		return
	}
	if c.credsRefreshSvc == nil {
		utils.WriteErrorResponse(w, http.StatusServiceUnavailable, "credentials refresh not configured")
		return
	}
	orgHandle, ok := c.authorizeRunnerCallback(w, r, taskID)
	if !ok {
		return
	}
	resp, err := c.credsRefreshSvc.Refresh(r.Context(), taskID, orgHandle)
	if err != nil {
		slog.ErrorContext(r.Context(), "refresh credentials failed",
			"taskId", taskID, "ocOrgId", orgHandle, "error", err)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to refresh credentials")
		return
	}
	utils.WriteSuccessResponse(w, http.StatusOK, resp)
}
