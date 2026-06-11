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

package api

import (
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/controllers"
	"github.com/wso2/asdlc/asdlc-service/middleware"
)

func registerTaskRoutes(mux *http.ServeMux, c controllers.TaskController) {
	// Org-scoped tasks list (Phase 2 PR D — used by ReachReconciliationBanner).
	// Supports ?status=, ?cause=, ?since= filters.
	mux.HandleFunc("GET /api/v1/organizations/{orgHandle}/tasks", c.ListOrgTasks)
	mux.HandleFunc("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/tasks", c.ListTasks)
	mux.HandleFunc("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/generated", c.GetTasks)
	mux.HandleFunc("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}", c.GetTask)
	mux.HandleFunc("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}/status", c.GetTaskStatus)
	mux.HandleFunc("POST /api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/dispatch", c.DispatchTasks)
	mux.HandleFunc("POST /api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/generate", c.GenerateTasks)
	mux.HandleFunc("POST /api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}/exec", c.ExecTask)
	mux.HandleFunc("POST /api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}/regenerate-body", c.RegenerateTaskBody)
	// F3c — operator retry for verification_failed tasks. Uses standard
	// user auth (org/project-scoped).
	mux.HandleFunc("POST /api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}/retry", c.Retry)

	// Progress endpoints — task-execution-progress.md §5.2. Per-org rate
	// limited (token bucket, 100 req/s burst 200) so a single tenant can't
	// starve Observer for others.
	progressLimiter := middleware.ProgressRateLimit(100, 200)
	mux.Handle("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}/progress/agent",
		progressLimiter(http.HandlerFunc(c.GetTaskAgentProgress)))
	mux.Handle("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/tasks/{taskId}/progress/build",
		progressLimiter(http.HandlerFunc(c.GetTaskBuildProgress)))
}
