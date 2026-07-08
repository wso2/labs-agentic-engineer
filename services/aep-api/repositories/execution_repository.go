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

package repositories

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/models"
)

// ExecutionRepository is the platform-owned half of the Task/Execution split
// (docs/design/tasks-github-native.md §7): the executions rows. Lookups miss
// with (nil, nil) — never gorm.ErrRecordNotFound — matching the house
// convention. The org-scoped variants fence reads to a single org for the read
// API and the progress endpoint.
type ExecutionRepository interface {
	// TryAdmit is the admission mutex (§5): it INSERTs a queued Execution unless
	// an active (queued|running) one already exists for the same
	// (repo, issue_number, kind), via INSERT … ON CONFLICT DO NOTHING against
	// the partial unique index. It returns admitted=true and the populated row
	// on success, or admitted=false and nil when another entrant won the race.
	// The passed row's Status defaults to queued when empty; on success its ID
	// (and CreatedAt) are populated.
	TryAdmit(ctx context.Context, e *models.Execution) (admitted bool, row *models.Execution, err error)

	// StartWithRun transitions a queued Execution to running, stamps started_at,
	// and records the dispatched run name (OpenChoreo WorkflowRun) in one guarded
	// write — the executor's single discipline for run-name persistence
	// (docs/design/tasks-github-native.md §10.1: no raw-gorm state writes).
	// Guarded on the queued state, so a double-start returns (nil, nil).
	StartWithRun(ctx context.Context, id, runName string) (*models.Execution, error)

	// Finish transitions an active (queued|running) Execution to a terminal
	// status, recording reason and ended_at. Guarded on the active state, so
	// finishing an already-terminal row returns (nil, nil) and never overwrites
	// a recorded outcome. status should be a terminal taskmeta.ExecutionStatus.
	Finish(ctx context.Context, id, status, reason string) (*models.Execution, error)

	// NoteBuildRetry re-points a still-running build Execution at a freshly
	// re-triggered WorkflowRun and records the retry reason, WITHOUT ending the
	// row — the git-auth build-retry path (tasks-github-native §7 auth budget):
	// on a git-clone-auth build failure within budget the watcher re-mints the
	// clone credential, re-triggers the build, and threads the new run name +
	// retry counter here so the next sweep tracks the retry rather than the dead
	// original. Guarded on running state (a terminal row is never resurrected).
	NoteBuildRetry(ctx context.Context, id, runName, reason string) (*models.Execution, error)

	// GetByIDScoped returns the Execution only when it belongs to orgID — the
	// store fence for token-derived org (e.g. the progress endpoint). Returns
	// (nil, nil) for both "no such id" and "belongs to another org".
	GetByIDScoped(ctx context.Context, orgID, id string) (*models.Execution, error)

	// LatestPerKind returns the most-recent Execution per kind for a Task,
	// keyed by kind — the join the read path and the funnel gates consume.
	LatestPerKind(ctx context.Context, repo string, issueNumber int) (map[string]*models.Execution, error)
	// LatestPerKindScoped is LatestPerKind fenced to orgID.
	LatestPerKindScoped(ctx context.Context, orgID, repo string, issueNumber int) (map[string]*models.Execution, error)

	// LatestPerKindForRepo is the batch form of LatestPerKind for one repo:
	// the most-recent Execution per kind for EVERY Task, keyed by issue number
	// then kind — one query for the task list and the sweep instead of one per
	// issue.
	LatestPerKindForRepo(ctx context.Context, repo string) (map[int]map[string]*models.Execution, error)
	// LatestPerKindForRepoScoped is LatestPerKindForRepo fenced to orgID.
	LatestPerKindForRepoScoped(ctx context.Context, orgID, repo string) (map[int]map[string]*models.Execution, error)

	// ListByIssue returns every Execution for a Task, oldest first (full history
	// for the detail page).
	ListByIssue(ctx context.Context, repo string, issueNumber int) ([]models.Execution, error)
	// ListByIssueScoped is ListByIssue fenced to orgID.
	ListByIssueScoped(ctx context.Context, orgID, repo string, issueNumber int) ([]models.Execution, error)

	// ListActive returns every active (queued|running) Execution across all
	// orgs — the reconciliation sweep's input (§5). Intentionally NOT
	// org-scoped: the sweep spans every org, like RepoRepository.ListAllReady.
	ListActive(ctx context.Context) ([]models.Execution, error)

	// DeleteByProject removes every Execution row for a project — the
	// project-delete orphan purge (executions are platform-owned rows keyed to
	// the project; the Task issues are deleted with the repo). Org-scoped so a
	// per-org project slug reused across orgs cannot cross-delete.
	DeleteByProject(ctx context.Context, orgID, projectID string) error

	// UpsertReadModel upserts a Temporal-driven task snapshot keyed by
	// e.WorkflowID (required — errors if empty), via INSERT … ON CONFLICT
	// (workflow_id) DO UPDATE against ux_executions_readmodel. One row per task
	// workflow; Version increments on every write (including the first, which
	// starts at 1). Distinct from TryAdmit's per-attempt admission-mutex insert
	// — the two paths never collide (disjoint partial indexes).
	UpsertReadModel(ctx context.Context, e *models.Execution) (*models.Execution, error)

	// GetByWorkflowID returns the read-model row for a task workflow, or
	// (nil, nil) if none has been upserted yet.
	GetByWorkflowID(ctx context.Context, workflowID string) (*models.Execution, error)

	// ListReadModelByStatus returns every read-model row (non-empty
	// WorkflowID) at the given status — the deploy-completion poll's input
	// (§R3.2/§R4.1: ExecWatcher polls rows at "deploying" for the component's
	// ReleaseBinding Ready condition, replacing the old synthetic
	// DeploySucceeded signal). Status here is a plain string, not a
	// taskmeta.ExecutionStatus — read-model rows track workflow-position
	// checkpoints, not dispatch-attempt lifecycle.
	ListReadModelByStatus(ctx context.Context, status string) ([]models.Execution, error)
}

type executionRepository struct {
	db *gorm.DB
}

func NewExecutionRepository(db *gorm.DB) ExecutionRepository {
	return &executionRepository{db: db}
}

func (r *executionRepository) TryAdmit(ctx context.Context, e *models.Execution) (bool, *models.Execution, error) {
	if e.Status == "" {
		e.Status = string(taskmeta.ExecQueued)
	}
	// ON CONFLICT DO NOTHING against the partial admission-mutex index: the
	// losing racer inserts zero rows. No conflict target is named — a plain
	// DO NOTHING catches the partial unique violation, and the uuid PK never
	// collides.
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(e)
	if res.Error != nil {
		return false, nil, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil, nil
	}
	return true, e, nil
}

func (r *executionRepository) StartWithRun(ctx context.Context, id, runName string) (*models.Execution, error) {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).
		Model(&models.Execution{}).
		Where("id = ? AND status = ?", id, string(taskmeta.ExecQueued)).
		Updates(map[string]any{
			"status":     string(taskmeta.ExecRunning),
			"started_at": now,
			"run_name":   runName,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.getByID(ctx, id)
}

func (r *executionRepository) Finish(ctx context.Context, id, status, reason string) (*models.Execution, error) {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).
		Model(&models.Execution{}).
		Where("id = ? AND status IN ?", id, []string{
			string(taskmeta.ExecQueued), string(taskmeta.ExecRunning),
		}).
		Updates(map[string]any{
			"status":   status,
			"reason":   reason,
			"ended_at": now,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.getByID(ctx, id)
}

func (r *executionRepository) NoteBuildRetry(ctx context.Context, id, runName, reason string) (*models.Execution, error) {
	res := r.db.WithContext(ctx).
		Model(&models.Execution{}).
		Where("id = ? AND status = ?", id, string(taskmeta.ExecRunning)).
		Updates(map[string]any{
			"run_name": runName,
			"reason":   reason,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.getByID(ctx, id)
}

func (r *executionRepository) GetByIDScoped(ctx context.Context, orgID, id string) (*models.Execution, error) {
	var e models.Execution
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND id = ?", orgID, id).
		First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *executionRepository) LatestPerKind(ctx context.Context, repo string, issueNumber int) (map[string]*models.Execution, error) {
	return r.latestPerKind(ctx, "", repo, issueNumber)
}

func (r *executionRepository) LatestPerKindScoped(ctx context.Context, orgID, repo string, issueNumber int) (map[string]*models.Execution, error) {
	return r.latestPerKind(ctx, orgID, repo, issueNumber)
}

// latestPerKind returns the most-recent row per kind via Postgres DISTINCT ON.
// orgID == "" spans all orgs (the funnel/webhook path); a non-empty orgID
// fences to that org (the read path). Raw SQL keeps the DISTINCT ON explicit
// rather than relying on gorm's Distinct formatting.
func (r *executionRepository) latestPerKind(ctx context.Context, orgID, repo string, issueNumber int) (map[string]*models.Execution, error) {
	sql := `SELECT DISTINCT ON (kind) * FROM executions WHERE repo = ? AND issue_number = ?`
	args := []any{repo, issueNumber}
	if orgID != "" {
		sql += ` AND org_id = ?`
		args = append(args, orgID)
	}
	sql += ` ORDER BY kind, created_at DESC`

	var rows []models.Execution
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]*models.Execution, len(rows))
	for i := range rows {
		out[rows[i].Kind] = &rows[i]
	}
	return out, nil
}

func (r *executionRepository) LatestPerKindForRepo(ctx context.Context, repo string) (map[int]map[string]*models.Execution, error) {
	return r.latestPerKindForRepo(ctx, "", repo)
}

func (r *executionRepository) LatestPerKindForRepoScoped(ctx context.Context, orgID, repo string) (map[int]map[string]*models.Execution, error) {
	return r.latestPerKindForRepo(ctx, orgID, repo)
}

// latestPerKindForRepo is the batch form of latestPerKind: one DISTINCT ON
// query over (issue_number, kind) for the whole repo, indexed per issue. The
// same orgID convention applies ("" spans all orgs).
func (r *executionRepository) latestPerKindForRepo(ctx context.Context, orgID, repo string) (map[int]map[string]*models.Execution, error) {
	sql := `SELECT DISTINCT ON (issue_number, kind) * FROM executions WHERE repo = ?`
	args := []any{repo}
	if orgID != "" {
		sql += ` AND org_id = ?`
		args = append(args, orgID)
	}
	sql += ` ORDER BY issue_number, kind, created_at DESC`

	var rows []models.Execution
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[int]map[string]*models.Execution{}
	for i := range rows {
		byKind := out[rows[i].IssueNumber]
		if byKind == nil {
			byKind = map[string]*models.Execution{}
			out[rows[i].IssueNumber] = byKind
		}
		byKind[rows[i].Kind] = &rows[i]
	}
	return out, nil
}

func (r *executionRepository) ListByIssue(ctx context.Context, repo string, issueNumber int) ([]models.Execution, error) {
	return r.listByIssue(ctx, "", repo, issueNumber)
}

func (r *executionRepository) ListByIssueScoped(ctx context.Context, orgID, repo string, issueNumber int) ([]models.Execution, error) {
	return r.listByIssue(ctx, orgID, repo, issueNumber)
}

func (r *executionRepository) listByIssue(ctx context.Context, orgID, repo string, issueNumber int) ([]models.Execution, error) {
	q := r.db.WithContext(ctx).
		Where("repo = ? AND issue_number = ?", repo, issueNumber).
		Order("created_at ASC")
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	var rows []models.Execution
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *executionRepository) ListActive(ctx context.Context) ([]models.Execution, error) {
	var rows []models.Execution
	err := r.db.WithContext(ctx).
		Where("status IN ?", []string{string(taskmeta.ExecQueued), string(taskmeta.ExecRunning)}).
		Order("created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *executionRepository) DeleteByProject(ctx context.Context, orgID, projectID string) error {
	return r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Delete(&models.Execution{}).Error
}

func (r *executionRepository) UpsertReadModel(ctx context.Context, e *models.Execution) (*models.Execution, error) {
	if e.WorkflowID == "" {
		return nil, fmt.Errorf("upsert read model: workflow id required")
	}
	if e.Status == "" {
		e.Status = string(taskmeta.ExecQueued)
	}
	if e.Version == 0 {
		e.Version = 1
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "workflow_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"status":     e.Status,
			"reason":     e.Reason,
			"run_name":   e.RunName,
			"component":  e.Component,
			"design_tag": e.DesignTag,
			"version":    gorm.Expr("executions.version + 1"),
		}),
	}).Create(e)
	if res.Error != nil {
		return nil, res.Error
	}
	return r.GetByWorkflowID(ctx, e.WorkflowID)
}

func (r *executionRepository) ListReadModelByStatus(ctx context.Context, status string) ([]models.Execution, error) {
	var rows []models.Execution
	err := r.db.WithContext(ctx).
		Where("workflow_id <> '' AND status = ?", status).
		Order("created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *executionRepository) GetByWorkflowID(ctx context.Context, workflowID string) (*models.Execution, error) {
	var e models.Execution
	err := r.db.WithContext(ctx).Where("workflow_id = ?", workflowID).First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ExecutionFacts projects latest-per-kind Execution rows into the minimal
// facts the taskmeta derive algebra consumes (Derive / PRStateFromFacts). It
// lives with the executions rows — the shared kernel — so both halves of the
// Task/Execution split project ONE way without importing each other.
func ExecutionFacts(execs map[string]*models.Execution) []taskmeta.ExecutionFact {
	out := make([]taskmeta.ExecutionFact, 0, len(execs))
	for _, e := range execs {
		if e == nil {
			continue
		}
		out = append(out, taskmeta.ExecutionFact{
			Kind:      taskmeta.ExecutionKind(e.Kind),
			Status:    taskmeta.ExecutionStatus(e.Status),
			Reason:    e.Reason,
			CreatedAt: e.CreatedAt,
		})
	}
	return out
}

func (r *executionRepository) getByID(ctx context.Context, id string) (*models.Execution, error) {
	var e models.Execution
	err := r.db.WithContext(ctx).First(&e, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}
