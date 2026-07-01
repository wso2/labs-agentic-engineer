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
	"fmt"
	"strings"
	"time"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
	"github.com/wso2/asdlc/asdlc-service/models"
	"github.com/wso2/asdlc/asdlc-service/repositories"
)

// BoardTask is a single task item on the project board.
type BoardTask struct {
	ID              string              `json:"id"`
	Title           string              `json:"title"`
	URL             string              `json:"url"`
	Description     string              `json:"description,omitempty"`
	Assignee        string              `json:"assignee,omitempty"`
	ComponentTaskID string              `json:"componentTaskId,omitempty"`
	Labels          []gitrepo.LabelInfo `json:"labels,omitempty"`
	LifecycleStatus string              `json:"lifecycleStatus,omitempty"`
	// Status is the ComponentTask execution status (pending, on_hold,
	// in_progress, verification_failed, ready_for_review, merged, building,
	// deployed, rejected, failed, abandoned). Empty when the row has no
	// backing ComponentTask.
	Status string `json:"status,omitempty"`
	// DispatchedAt is the time the task was dispatched for execution.
	// Nil for never-dispatched tasks; the frontend uses it to render
	// "started Xm ago" and to gate the Live progress affordance.
	DispatchedAt *time.Time `json:"dispatchedAt,omitempty"`
	// ExecType mirrors ComponentTask.ExecType ("SYSTEM","WORKER"). The
	// frontend gates the per-task "Execute Now" button on this — the
	// endpoint behind that button (/tasks/{id}/exec) only does meaningful
	// work for SYSTEM tasks; WORKER (coding-agent) tasks must go through
	// the batch /tasks/dispatch path via "Execute all → Remote Agents".
	ExecType string `json:"execType,omitempty"`
	// DependsOnComponents mirrors ComponentTask.DependsOnComponents — the
	// list of component names this task is waiting to be deployed before
	// it can dispatch. Populated for every task; the On Hold column
	// reads it to show "Waiting for: …".
	DependsOnComponents []string `json:"dependsOnComponents,omitempty"`
	// DependsOnResources / DependsOnOrgServices / DependsOnConnections mirror
	// the same-named ComponentTask fields — the resource (platform-provisioned
	// db/cache), cross-project org-service, and external-connection gates a
	// component task waits on. Surfaced so the On Hold column can explain the
	// full reason a task is held (not just component gates).
	DependsOnResources   []string `json:"dependsOnResources,omitempty"`
	DependsOnOrgServices []string `json:"dependsOnOrgServices,omitempty"`
	DependsOnConnections []string `json:"dependsOnConnections,omitempty"`
	// Type mirrors ComponentTask.Type (component, resource-provisioning,
	// config-collection). The frontend uses it to route the row's action —
	// resource-provisioning/config-collection tasks are resolved in the
	// architecture-page drawer, not via the (no-op) per-task exec endpoint.
	Type string `json:"type,omitempty"`
	// ResourceName / ConnectionName mirror the same-named ComponentTask fields
	// (the dependency this SYSTEM task resolves). The frontend deep-links the
	// architecture drawer to this dep name (?dep=<name>).
	ResourceName   string `json:"resourceName,omitempty"`
	ConnectionName string `json:"connectionName,omitempty"`
	// ComponentName mirrors ComponentTask.ComponentName so the frontend
	// can resolve dep -> task lookups (e.g. "what is component `todo-api`'s
	// task currently doing while we wait?").
	ComponentName string `json:"componentName,omitempty"`
	// ErrorMessage mirrors ComponentTask.ErrorMessage. For a
	// verification_failed task it's the diagnostic the agent reported, shown
	// on the card so the operator can decide whether to retry.
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// ProjectBoard holds tasks grouped by their kanban column.
type ProjectBoard struct {
	URL        string      `json:"url"`
	Todo       []BoardTask `json:"todo"`
	InProgress []BoardTask `json:"inProgress"`
	Done       []BoardTask `json:"done"`
	OnHold     []BoardTask `json:"onHold"`
	Failed     []BoardTask `json:"failed"`
}

// BoardService fetches the kanban board for a project.
type BoardService interface {
	GetBoard(ctx context.Context, orgID, projectID string) (*ProjectBoard, error)
}

type boardService struct {
	repoBoardSvc gitrepo.RepoBoardService
	taskRepo     repositories.TaskRepository
}

func NewBoardService(repoBoardSvc gitrepo.RepoBoardService, taskRepo repositories.TaskRepository) BoardService {
	return &boardService{repoBoardSvc: repoBoardSvc, taskRepo: taskRepo}
}

func (s *boardService) GetBoard(ctx context.Context, orgID, projectID string) (*ProjectBoard, error) {
	board := &ProjectBoard{
		URL:        "",
		Todo:       []BoardTask{},
		InProgress: []BoardTask{},
		Done:       []BoardTask{},
		OnHold:     []BoardTask{},
		Failed:     []BoardTask{},
	}

	if s.repoBoardSvc == nil {
		return board, nil
	}

	result, err := s.repoBoardSvc.GetBoard(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get board: %w", err)
	}

	// Load DB tasks for enrichment, keyed by issue URL.
	issueURLToCT := map[string]models.ComponentTask{}
	var allComponentTasks []models.ComponentTask
	// unissuedTasks are tasks with no IssueURL (gh_issue_waiting or gh_issue_failed).
	// They never appear on the GitHub Project board and must be surfaced separately.
	var unissuedTasks []models.ComponentTask
	if s.taskRepo != nil {
		if componentTasks, err := s.taskRepo.ListByProjectID(ctx, orgID, projectID); err == nil {
			allComponentTasks = componentTasks
			for _, ct := range componentTasks {
				if ct.IssueURL != "" {
					issueURLToCT[ct.IssueURL] = ct
				} else {
					unissuedTasks = append(unissuedTasks, ct)
				}
			}
		}
	}

	// Track which DB tasks (by issue URL) the canonical board actually renders,
	// so the reconcile pass below can surface any issued task the board omits.
	renderedIssueURLs := map[string]bool{}
	for _, item := range result.Items {
		renderedIssueURLs[item.URL] = true
		task := BoardTask{
			ID:              item.ID,
			Title:           item.Title,
			URL:             item.URL,
			Description:     item.Body,
			Assignee:        item.Assignee,
			Labels:          item.Labels,
			LifecycleStatus: string(models.TaskLifecycleGhIssueCreated),
		}
		if ct, ok := issueURLToCT[item.URL]; ok {
			applyCTFields(&task, ct)
		}
		// The BFF's ComponentTask.Status is authoritative for kanban routing
		// of `on_hold` (dep-gated), terminal failure states, and
		// `verification_failed` tasks. Terminal failure states must never be
		// overridden by the GitHub board column (which may not have been
		// updated yet, e.g. when markFailed's MoveIssueToStatus call fails).
		switch task.Status {
		case string(models.TaskStatusOnHold):
			board.OnHold = append(board.OnHold, task)
			continue
		case string(models.TaskStatusFailed),
			string(models.TaskStatusRejected),
			string(models.TaskStatusAbandoned),
			string(models.TaskStatusVerificationFailed):
			board.Failed = append(board.Failed, task)
			continue
		}
		switch normalizeStatus(item.Status) {
		case "in progress":
			board.InProgress = append(board.InProgress, task)
		case "done":
			board.Done = append(board.Done, task)
		case "on hold":
			board.OnHold = append(board.OnHold, task)
		case "failed":
			board.Failed = append(board.Failed, task)
		default:
			board.Todo = append(board.Todo, task)
		}
	}
	board.URL = result.URL

	// Fallback: when the GitHub board has no items, show all component tasks from DB.
	if len(result.Items) == 0 && len(allComponentTasks) > 0 {
		for _, ct := range allComponentTasks {
			task := boardTaskFromCT(ct)
			// Board has 0 items — GitHub Project hasn't synced yet. Override
			// gh_issue_created → gh_issue_syncing so the frontend shows a skeleton
			// instead of a labelless task card. Never written to DB.
			if task.LifecycleStatus == string(models.TaskLifecycleGhIssueCreated) {
				task.LifecycleStatus = string(models.TaskLifecycleGhIssueSyncing)
			}
			switch ct.Status {
			case "on_hold":
				board.OnHold = append(board.OnHold, task)
			case "in_progress":
				board.InProgress = append(board.InProgress, task)
			case "ready_for_review", "merged", "building", "deployed":
				board.Done = append(board.Done, task)
			case "failed", "rejected", "verification_failed":
				board.Failed = append(board.Failed, task)
			default:
				board.Todo = append(board.Todo, task)
			}
		}
		return board, nil
	}

	// Always surface unissued tasks (no IssueURL — either a coding task still
	// awaiting its GitHub issue, or a SYSTEM resource-provisioning /
	// config-collection task that never gets an issue by design). These are
	// invisible to the GitHub Project board, so route them by their own status:
	// a SYSTEM task moves Todo → In Progress → Done as it provisions, rather than
	// sitting in Todo with a "deployed" label forever.
	for _, ct := range unissuedTasks {
		task := boardTaskFromCT(ct)
		switch {
		case ct.LifecycleStatus == string(models.TaskLifecycleGhIssueFailed):
			board.Failed = append(board.Failed, task)
		case ct.Status == "deployed":
			board.Done = append(board.Done, task)
		case ct.Status == "building" || ct.Status == "in_progress":
			board.InProgress = append(board.InProgress, task)
		case ct.Status == "failed" || ct.Status == "rejected" || ct.Status == "verification_failed":
			board.Failed = append(board.Failed, task)
		case ct.Status == "on_hold":
			board.OnHold = append(board.OnHold, task)
		default: // pending / empty — needs action, or a coding task awaiting its issue
			board.Todo = append(board.Todo, task)
		}
	}

	// Reconcile: a task may carry an IssueURL yet be absent from the canonical
	// GitHub Project board — e.g. a board-provisioning race split the project's
	// issues across two boards, or a project-item add failed. Such a task is
	// matched by neither the primary loop (its issue is not in result.Items) nor
	// the unissued path (it HAS a URL), so it would silently vanish. Surface any
	// issued task the board omitted, routed by its authoritative ComponentTask
	// status (the same mapping the zero-items fallback uses).
	for _, ct := range allComponentTasks {
		if ct.IssueURL == "" || renderedIssueURLs[ct.IssueURL] {
			continue
		}
		task := boardTaskFromCT(ct)
		switch ct.Status {
		case "on_hold":
			board.OnHold = append(board.OnHold, task)
		case "in_progress":
			board.InProgress = append(board.InProgress, task)
		case "ready_for_review", "merged", "building", "deployed":
			board.Done = append(board.Done, task)
		case "failed", "rejected", "verification_failed", "abandoned":
			board.Failed = append(board.Failed, task)
		default:
			board.Todo = append(board.Todo, task)
		}
	}

	return board, nil
}

func normalizeStatus(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// applyCTFields copies the ComponentTask-derived status / dependency / type
// fields onto a BoardTask, leaving any GitHub-sourced fields (id / title / url /
// body / labels / assignee) intact so the primary path keeps the board item's copy.
func applyCTFields(task *BoardTask, ct models.ComponentTask) {
	task.ComponentTaskID = ct.ID
	task.LifecycleStatus = ct.LifecycleStatus
	task.Status = ct.Status
	task.DispatchedAt = ct.DispatchedAt
	task.ExecType = ct.ExecType
	task.Type = ct.Type
	task.ResourceName = ct.ResourceName
	task.ConnectionName = ct.ConnectionName
	task.DependsOnComponents = []string(ct.DependsOnComponents)
	task.DependsOnResources = []string(ct.DependsOnResources)
	task.DependsOnOrgServices = []string(ct.DependsOnOrgServices)
	task.DependsOnConnections = []string(ct.DependsOnConnections)
	task.ComponentName = ct.ComponentName
	task.ErrorMessage = ct.ErrorMessage
}

// boardTaskFromCT builds a BoardTask entirely from a ComponentTask — used by the
// zero-items fallback, the reconcile pass, and the unissued pass, none of which
// have a GitHub board item to source from.
func boardTaskFromCT(ct models.ComponentTask) BoardTask {
	task := BoardTask{
		ID:          ct.ID,
		Title:       ct.Title,
		URL:         ct.IssueURL,
		Description: ct.Body,
	}
	for _, l := range ct.Labels {
		task.Labels = append(task.Labels, gitrepo.LabelInfo{Name: l})
	}
	applyCTFields(&task, ct)
	return task
}
