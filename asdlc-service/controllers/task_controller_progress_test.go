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

package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/codingagent"
	taskfeature "github.com/wso2/asdlc/asdlc-service/internal/feature/task"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// fakeScopedTaskService embeds the full TaskService interface (nil — any
// unexpected method call panics) and only implements GetTaskScoped, which is
// the authorization seam the progress handlers must consult.
type fakeScopedTaskService struct {
	taskfeature.TaskService
	scoped func(orgHandle, taskID string) (*models.ComponentTask, error)
}

func (f *fakeScopedTaskService) GetTaskScoped(_ context.Context, orgHandle, taskID string) (*models.ComponentTask, error) {
	return f.scoped(orgHandle, taskID)
}

// spyProgressService records whether the progress methods were reached. If a
// handler authorizes correctly, these are never called for a cross-org task.
type spyProgressService struct {
	codingagent.ProgressService
	called bool
}

func (s *spyProgressService) GetAgentProgress(_ context.Context, _ string, _ int64, _ int) (*codingagent.ProgressResponse, error) {
	s.called = true
	return &codingagent.ProgressResponse{}, nil
}

func (s *spyProgressService) GetBuildProgress(_ context.Context, _ string, _ int64) (*codingagent.ProgressResponse, error) {
	s.called = true
	return &codingagent.ProgressResponse{}, nil
}

// TestProgressRoutes_CrossOrgDenied locks the IDOR fix: a caller whose JWT org
// equals the path org (so the OrgScoped gate passes) must NOT be able to read
// another org's coding-agent/build progress by passing that org's taskId. The
// scoped lookup returns ErrTaskNotFound for the cross-org task, so the handler
// must 404 and never reach the progress service.
func TestProgressRoutes_CrossOrgDenied(t *testing.T) {
	const (
		callerOrg    = "org-a"
		crossOrgTask = "11111111-1111-1111-1111-111111111111" // a real UUID owned by org-b
	)

	cases := []struct {
		name string
		path string
		call func(c *taskController, w http.ResponseWriter, r *http.Request)
	}{
		{"agent", "/api/v1/organizations/org-a/projects/p/tasks/" + crossOrgTask + "/progress/agent",
			(*taskController).GetTaskAgentProgress},
		{"build", "/api/v1/organizations/org-a/projects/p/tasks/" + crossOrgTask + "/progress/build",
			(*taskController).GetTaskBuildProgress},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := &fakeScopedTaskService{scoped: func(orgHandle, taskID string) (*models.ComponentTask, error) {
				// org-a does not own this task → not found (no existence leak).
				return nil, taskfeature.ErrTaskNotFound
			}}
			spy := &spyProgressService{}
			c := &taskController{service: ts, progressSvc: spy}

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.SetPathValue("orgHandle", callerOrg)
			req.SetPathValue("taskId", crossOrgTask)
			rec := httptest.NewRecorder()

			tc.call(c, rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("cross-org %s progress: got status %d, want 404", tc.name, rec.Code)
			}
			if spy.called {
				t.Fatalf("cross-org %s progress: progress service was invoked — authz bypassed", tc.name)
			}
		})
	}
}

// TestProgressRoutes_SameOrgAllowed confirms the pre-check does not block the
// legitimate owner: when the scoped lookup resolves, the handler proceeds to
// the progress service.
func TestProgressRoutes_SameOrgAllowed(t *testing.T) {
	ts := &fakeScopedTaskService{scoped: func(orgHandle, taskID string) (*models.ComponentTask, error) {
		return &models.ComponentTask{OrgID: orgHandle, ID: taskID}, nil
	}}
	spy := &spyProgressService{}
	c := &taskController{service: ts, progressSvc: spy}

	const taskID = "22222222-2222-2222-2222-222222222222"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organizations/org-a/projects/p/tasks/"+taskID+"/progress/agent", nil)
	req.SetPathValue("orgHandle", "org-a")
	req.SetPathValue("taskId", taskID)
	rec := httptest.NewRecorder()

	c.GetTaskAgentProgress(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("same-org progress: got status %d, want 200", rec.Code)
	}
	if !spy.called {
		t.Fatalf("same-org progress: progress service was NOT invoked — owner wrongly blocked")
	}
}
