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
	"testing"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// stubRepoBoard returns a fixed GitHub Project board result.
type stubRepoBoard struct{ result *gitrepo.ProjectBoardResult }

func (s *stubRepoBoard) GetBoard(_ context.Context, _, _ string) (*gitrepo.ProjectBoardResult, error) {
	return s.result, nil
}
func (s *stubRepoBoard) MoveIssueToStatus(_ context.Context, _, _, _, _ string) error { return nil }

// TestGetBoard_ReconcilesIssuedTaskMissingFromBoard is the regression guard for
// the two-board race: a component task that HAS a GitHub issue but whose issue
// is absent from the canonical Project board (it landed on an orphaned second
// board, or a project-item add failed) must still be surfaced — matched by
// neither the primary loop nor the unissued path, it would otherwise vanish.
func TestGetBoard_ReconcilesIssuedTaskMissingFromBoard(t *testing.T) {
	const webURL = "https://github.com/o/r/issues/1"
	const apiURL = "https://github.com/o/r/issues/2"

	// Canonical board holds only storefront-web (#1); order-api (#2) is off-board.
	repoBoard := &stubRepoBoard{result: &gitrepo.ProjectBoardResult{
		URL:   "https://github.com/orgs/o/projects/85",
		Items: []gitrepo.BoardItem{{ID: "PVTI_web", Title: "storefront-web", URL: webURL, Status: "On Hold"}},
	}}
	taskRepo := &stubTaskRepo{tasks: []models.ComponentTask{
		{ID: "t-web", ComponentName: "storefront-web", IssueURL: webURL, Status: "on_hold"},
		{ID: "t-api", ComponentName: "order-api", IssueURL: apiURL, Status: "on_hold"}, // issued, off-board
		{ID: "t-db", ComponentName: "orders-db", Type: "resource-provisioning", Status: "pending"}, // unissued
	}}

	board, err := NewBoardService(repoBoard, taskRepo).GetBoard(context.Background(), "org", "proj")
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}

	onHold := map[string]int{}
	for _, tk := range board.OnHold {
		onHold[tk.ComponentName]++
	}
	if onHold["storefront-web"] != 1 {
		t.Errorf("storefront-web: OnHold count = %d, want 1", onHold["storefront-web"])
	}
	if onHold["order-api"] != 1 {
		t.Errorf("order-api (issued but off canonical board): OnHold count = %d, want 1 — reconcile failed", onHold["order-api"])
	}
}

// TestGetBoard_NoDoubleCountForOnBoardTask asserts the reconcile pass does not
// re-add a task the primary loop already rendered.
func TestGetBoard_NoDoubleCountForOnBoardTask(t *testing.T) {
	const url = "https://github.com/o/r/issues/1"
	repoBoard := &stubRepoBoard{result: &gitrepo.ProjectBoardResult{
		Items: []gitrepo.BoardItem{{ID: "PVTI_1", Title: "api", URL: url, Status: "On Hold"}},
	}}
	taskRepo := &stubTaskRepo{tasks: []models.ComponentTask{
		{ID: "t-1", ComponentName: "api", IssueURL: url, Status: "on_hold"},
	}}
	board, err := NewBoardService(repoBoard, taskRepo).GetBoard(context.Background(), "org", "proj")
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(board.OnHold) != 1 {
		t.Fatalf("OnHold has %d tasks, want 1 (no double-count)", len(board.OnHold))
	}
}

// TestGetBoard_SurfacesGateKindsAndSystemTaskFields asserts the payload carries
// the full set of gate kinds (so the On Hold reason is complete) and the type +
// dep name of SYSTEM tasks (so the frontend can deep-link the drawer).
func TestGetBoard_SurfacesGateKindsAndSystemTaskFields(t *testing.T) {
	repoBoard := &stubRepoBoard{result: &gitrepo.ProjectBoardResult{}} // 0 items → whole-DB fallback
	taskRepo := &stubTaskRepo{tasks: []models.ComponentTask{
		{ID: "t-api", ComponentName: "order-api", Type: "component", Status: "on_hold",
			DependsOnResources:   models.StringSlice{"orders-db"},
			DependsOnConnections: models.StringSlice{"exchangerate"},
			DependsOnOrgServices: models.StringSlice{"catalog-product-api"}},
		{ID: "t-db", ComponentName: "orders-db", Type: "resource-provisioning", Status: "pending", ResourceName: "orders-db"},
		{ID: "t-cfg", ComponentName: "exchangerate", Type: "config-collection", Status: "pending", ConnectionName: "exchangerate"},
	}}
	board, err := NewBoardService(repoBoard, taskRepo).GetBoard(context.Background(), "o", "p")
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}

	var api *BoardTask
	for i := range board.OnHold {
		if board.OnHold[i].ComponentName == "order-api" {
			api = &board.OnHold[i]
		}
	}
	if api == nil {
		t.Fatal("order-api not in OnHold")
	}
	if len(api.DependsOnResources) != 1 || len(api.DependsOnConnections) != 1 || len(api.DependsOnOrgServices) != 1 {
		t.Errorf("gate kinds not surfaced: res=%v conn=%v org=%v", api.DependsOnResources, api.DependsOnConnections, api.DependsOnOrgServices)
	}

	find := func(name string) *BoardTask {
		for i := range board.Todo {
			if board.Todo[i].ComponentName == name {
				return &board.Todo[i]
			}
		}
		return nil
	}
	if db := find("orders-db"); db == nil || db.Type != "resource-provisioning" || db.ResourceName != "orders-db" {
		t.Errorf("orders-db SYSTEM fields wrong: %+v", db)
	}
	if cfg := find("exchangerate"); cfg == nil || cfg.Type != "config-collection" || cfg.ConnectionName != "exchangerate" {
		t.Errorf("exchangerate SYSTEM fields wrong: %+v", cfg)
	}
}

// TestGetBoard_UnissuedSystemTaskRoutedByStatus asserts a resource-provisioning /
// config-collection task (no issue → the unissued path) is routed by its own
// status: deployed → Done (not pinned to Todo with a "deployed" label), pending
// → Todo.
func TestGetBoard_UnissuedSystemTaskRoutedByStatus(t *testing.T) {
	// One board item that matches no DB task keeps result.Items > 0 so the
	// whole-DB fallback does not fire; the DB-only tasks flow through unissued.
	repoBoard := &stubRepoBoard{result: &gitrepo.ProjectBoardResult{
		Items: []gitrepo.BoardItem{{ID: "PVTI_x", URL: "https://github.com/o/r/issues/9", Status: "Todo"}},
	}}
	taskRepo := &stubTaskRepo{tasks: []models.ComponentTask{
		{ID: "t-db", ComponentName: "orders-db", Type: "resource-provisioning", ResourceName: "orders-db", Status: "deployed"},
		{ID: "t-cfg", ComponentName: "exchangerate", Type: "config-collection", ConnectionName: "exchangerate", Status: "pending"},
	}}
	board, err := NewBoardService(repoBoard, taskRepo).GetBoard(context.Background(), "o", "p")
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	has := func(list []BoardTask, name string) bool {
		for _, tk := range list {
			if tk.ComponentName == name {
				return true
			}
		}
		return false
	}
	if !has(board.Done, "orders-db") {
		t.Errorf("deployed resource-provisioning task not in Done (Todo=%v)", has(board.Todo, "orders-db"))
	}
	if !has(board.Todo, "exchangerate") {
		t.Errorf("pending config-collection task should be in Todo")
	}
}
