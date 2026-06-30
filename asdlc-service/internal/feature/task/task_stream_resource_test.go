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

// Package task — resource-provisioning task generation tests.
//
// These tests assert the A3 invariants for persistAndIssue:
//   - A component task with a platform-resource dep has DependsOnResources
//     populated (platform-authored, not LLM-authored).
//   - Exactly one resource-provisioning task is created per distinct
//     platform-resource dep, with ResourceName set and status pending.
//   - Re-generation does not duplicate existing resource-provisioning tasks
//     (dedup against existing tasks by ResourceName).
package task

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
	"github.com/wso2/asdlc/asdlc-service/models"
	"github.com/wso2/asdlc/asdlc-service/repositories"
)

// stubTaskRepo is an in-memory task repository for unit tests.
// Only Create and ListByProjectID are wired — the test assertions query the
// stored slice directly.
type stubTaskRepo struct {
	tasks []models.ComponentTask
}

func (r *stubTaskRepo) Create(_ context.Context, task *models.ComponentTask) error {
	r.tasks = append(r.tasks, *task)
	return nil
}

func (r *stubTaskRepo) ListByProjectID(_ context.Context, _, _ string) ([]models.ComponentTask, error) {
	return r.tasks, nil
}

// Implement the full TaskRepository interface with panicking stubs.
func (r *stubTaskRepo) GetByID(_ context.Context, _ string) (*models.ComponentTask, error) {
	panic("not implemented")
}
func (r *stubTaskRepo) GetByIDScoped(_ context.Context, _, _ string) (*models.ComponentTask, error) {
	panic("not implemented")
}
func (r *stubTaskRepo) GetByComponentName(_ context.Context, _, _, _ string) (*models.ComponentTask, error) {
	panic("not implemented")
}
func (r *stubTaskRepo) ListNonTerminalByOrgID(_ context.Context, _ string) ([]models.ComponentTask, error) {
	panic("not implemented")
}
func (r *stubTaskRepo) ListByOrgID(_ context.Context, _ string, _ repositories.ListByOrgFilter) ([]models.ComponentTask, error) {
	panic("not implemented")
}
func (r *stubTaskRepo) GetBaselineBatch(_ context.Context, _, _ string) (string, string, string, error) {
	panic("not implemented")
}
func (r *stubTaskRepo) Update(_ context.Context, _ *models.ComponentTask) error {
	// issue creation updates task status to failed — ignore in tests.
	return nil
}
func (r *stubTaskRepo) DeleteByProjectID(_ context.Context, _, _ string) error {
	panic("not implemented")
}
func (r *stubTaskRepo) DeleteAll(_ context.Context) error {
	panic("not implemented")
}

// newTestTaskService builds a minimal taskService for unit testing persistAndIssue.
// issueSvc is nil — issue creation will fail for component tasks (expected in these
// tests; we assert on the task rows, not issue outcomes).
func newTestTaskService(repo *stubTaskRepo) *taskService {
	return &taskService{
		taskRepo: repo,
		store:    nil,
	}
}

// sseDiscard is an io.Writer that discards all SSE frames during testing.
type sseDiscard struct{}

func (sseDiscard) Write(p []byte) (int, error) { return len(p), nil }

// TestPersistAndIssue_PlatformResourceDep asserts that a design with a
// platform-resource dependency:
//
//	(a) produces a component task with DependsOnResources populated, and
//	(b) produces exactly one resource-provisioning task with ResourceName
//	    set and Status == pending.
func TestPersistAndIssue_PlatformResourceDep(t *testing.T) {
	repo := &stubTaskRepo{}
	svc := newTestTaskService(repo)

	design := &artifacts.DesignFile{
		Components: []models.DesignComponent{
			{
				Name:          "api",
				ComponentType: "service",
				Language:      "Go",
				Dependencies: []models.Dependency{
					{
						Kind:         models.DependencyKindPlatformResource,
						Name:         "maindb",
						ResourceType: "postgres-cnpg",
					},
				},
			},
		},
	}

	plan := []planItemFrame{
		{TempID: "t1", ComponentName: "api", Title: "Implement api"},
	}

	w := &sseWriter{out: sseDiscard{}, flush: func() {}}

	// persistAndIssue will fail at issue creation (issueSvc == nil), but all
	// task rows are persisted before issue creation begins — ignore the error.
	//nolint:errcheck
	_, _ = svc.persistAndIssue(context.Background(), w,
		"org1", "proj1", "batch1",
		"v1", "v1-1",
		plan, design,
		"https://github.com/org/repo", "org/repo",
	)

	// (a) component task must have DependsOnResources = ["maindb"]
	var compTask *models.ComponentTask
	for i := range repo.tasks {
		if repo.tasks[i].Type == models.TaskTypeComponent || repo.tasks[i].Type == "" {
			compTask = &repo.tasks[i]
			break
		}
	}
	if compTask == nil {
		t.Fatal("no component task created")
	}
	if len(compTask.DependsOnResources) != 1 || compTask.DependsOnResources[0] != "maindb" {
		t.Fatalf("want DependsOnResources=[maindb], got %v", compTask.DependsOnResources)
	}

	// (b) exactly one resource-provisioning task for "maindb", status pending
	var resTasks []models.ComponentTask
	for _, task := range repo.tasks {
		if task.Type == models.TaskTypeResourceProvisioning {
			resTasks = append(resTasks, task)
		}
	}
	if len(resTasks) != 1 {
		t.Fatalf("want exactly 1 resource-provisioning task, got %d", len(resTasks))
	}
	rt := resTasks[0]
	if rt.ResourceName != "maindb" {
		t.Errorf("want ResourceName=maindb, got %q", rt.ResourceName)
	}
	if rt.Status != string(models.TaskStatusPending) {
		t.Errorf("want Status=pending, got %q", rt.Status)
	}
	if rt.LifecycleStatus != string(models.TaskLifecycleGhIssueCreated) {
		t.Errorf("want LifecycleStatus=gh_issue_created, got %q", rt.LifecycleStatus)
	}
}

// TestPersistAndIssue_PlatformResourceDep_Dedup asserts that a second
// generation call does not duplicate a resource-provisioning task that already
// exists for the same resource name (mirroring config-collection dedup).
func TestPersistAndIssue_PlatformResourceDep_Dedup(t *testing.T) {
	// Pre-seed with an existing resource-provisioning task for "maindb".
	existing := models.ComponentTask{
		Type:            models.TaskTypeResourceProvisioning,
		ResourceName:    "maindb",
		ComponentName:   "maindb",
		Title:           "Provision resource: maindb",
		Status:          string(models.TaskStatusPending),
		LifecycleStatus: string(models.TaskLifecycleGhIssueCreated),
		CreatedAt:       time.Now(),
	}
	repo := &stubTaskRepo{tasks: []models.ComponentTask{existing}}
	svc := newTestTaskService(repo)

	design := &artifacts.DesignFile{
		Components: []models.DesignComponent{
			{
				Name:          "api",
				ComponentType: "service",
				Language:      "Go",
				Dependencies: []models.Dependency{
					{Kind: models.DependencyKindPlatformResource, Name: "maindb", ResourceType: "postgres-cnpg"},
				},
			},
		},
	}
	plan := []planItemFrame{{TempID: "t1", ComponentName: "api", Title: "Implement api"}}
	w := &sseWriter{out: sseDiscard{}, flush: func() {}}

	//nolint:errcheck
	_, _ = svc.persistAndIssue(context.Background(), w,
		"org1", "proj1", "batch2",
		"v1", "v1-2",
		plan, design,
		"https://github.com/org/repo", "org/repo",
	)

	// Only the original resource-provisioning task should exist (no duplicate).
	var resCount int
	for _, task := range repo.tasks {
		if task.Type == models.TaskTypeResourceProvisioning {
			resCount++
		}
	}
	if resCount != 1 {
		t.Fatalf("dedup: want exactly 1 resource-provisioning task after re-generation, got %d", resCount)
	}
}

// sseWriter duplicate for test — the real sseWriter is unexported so we can't
// import it; since we're in the same package this is fine. Confirm the type
// exists by compiling.
var _ *sseWriter = (*sseWriter)(nil)

// Ensure sseDiscard implements io.Writer (compile-time assertion).
var _ interface{ Write([]byte) (int, error) } = sseDiscard{}

// Ensure bytes.Buffer satisfies write — just to keep the bytes import used.
var _ = (*bytes.Buffer)(nil)
