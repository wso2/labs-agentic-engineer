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
