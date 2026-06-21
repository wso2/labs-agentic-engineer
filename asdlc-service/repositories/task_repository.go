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

	"github.com/wso2/asdlc/asdlc-service/models"
)

// ListByOrgFilter narrows an org-scoped list query to tasks matching the
// given status, cause, and lower-bound on the last update. Empty/nil
// fields are unconstrained.
type ListByOrgFilter struct {
	Status string
	Cause  string
	Since  *time.Time
}

type TaskRepository interface {
	GetByID(ctx context.Context, id string) (*models.ComponentTask, error)
	// GetByIDScoped returns the task only when it belongs to orgID. Returns
	// (nil, nil) when there is no such (orgID, id) row — the SAME result for
	// "no such task" and "task belongs to another org" (no existence leak).
	GetByIDScoped(ctx context.Context, orgID, id string) (*models.ComponentTask, error)
	GetByComponentName(ctx context.Context, orgID, projectID, componentName string) (*models.ComponentTask, error)
	ListByProjectID(ctx context.Context, orgID, projectID string) ([]models.ComponentTask, error)
	// ListNonTerminalByOrgID returns every task under orgID whose status
	// is non-terminal. Used by the Phase 2 PR B disconnect cascade
	// (services/org_disconnect_service.go).
	ListNonTerminalByOrgID(ctx context.Context, orgID string) ([]models.ComponentTask, error)
	// ListByOrgID returns every task under orgID matching the optional
	// status / cause / since filter. Used by the PR D
	// ReachReconciliationBanner (status='abandoned', cause='repo.unselected',
	// since=now-24h) and by future audit UIs.
	ListByOrgID(ctx context.Context, orgID string, f ListByOrgFilter) ([]models.ComponentTask, error)
	// GetBaselineBatch returns the most-recent active batch's (batchID,
	// specVersion, designVersion) for an org+project, or zero values if none.
	// "Active" means the batch has at least one task in a live status
	// (pending, in_progress, ready_for_review, merged, building, deployed);
	// rejected / failed / abandoned do not qualify a batch as a baseline.
	// SourceSpecVersion / SourceDesignVersion are git tag strings (e.g. "v1",
	// "v1-2"), not integers. Scoped by org_id AND project_id.
	GetBaselineBatch(ctx context.Context, orgID, projectID string) (batchID, specVersion, designVersion string, err error)
	Create(ctx context.Context, task *models.ComponentTask) error
	Update(ctx context.Context, task *models.ComponentTask) error
	DeleteByProjectID(ctx context.Context, orgID, projectID string) error
	DeleteAll(ctx context.Context) error
}

type taskRepository struct {
	db *gorm.DB
}

func NewTaskRepository(db *gorm.DB) TaskRepository {
	return &taskRepository{db: db}
}

func (r *taskRepository) GetByID(ctx context.Context, id string) (*models.ComponentTask, error) {
	var task models.ComponentTask
	err := r.db.WithContext(ctx).First(&task, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *taskRepository) GetByIDScoped(ctx context.Context, orgID, id string) (*models.ComponentTask, error) {
	var task models.ComponentTask
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND id = ?", orgID, id).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *taskRepository) GetByComponentName(ctx context.Context, orgID, projectID, componentName string) (*models.ComponentTask, error) {
	var task models.ComponentTask
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND component_name = ?", orgID, projectID, componentName).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}


func (r *taskRepository) ListByProjectID(ctx context.Context, orgID, projectID string) ([]models.ComponentTask, error) {
	var tasks []models.ComponentTask
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Order(`"order" ASC, component_name ASC`).
		Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *taskRepository) ListNonTerminalByOrgID(ctx context.Context, orgID string) ([]models.ComponentTask, error) {
	var tasks []models.ComponentTask
	terminal := []string{
		string(models.TaskStatusDeployed),
		string(models.TaskStatusRejected),
		string(models.TaskStatusFailed),
		string(models.TaskStatusAbandoned),
	}
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND status NOT IN ?", orgID, terminal).
		Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *taskRepository) ListByOrgID(ctx context.Context, orgID string, f ListByOrgFilter) ([]models.ComponentTask, error) {
	q := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Cause != "" {
		q = q.Where("cause = ?", f.Cause)
	}
	if f.Since != nil {
		q = q.Where("last_event_at >= ?", *f.Since)
	}
	var tasks []models.ComponentTask
	if err := q.Order("last_event_at DESC").Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *taskRepository) GetBaselineBatch(ctx context.Context, orgID, projectID string) (batchID, specVersion, designVersion string, err error) {
	type row struct {
		BatchID             *string
		SourceSpecVersion   *string
		SourceDesignVersion *string
	}
	live := []string{
		string(models.TaskStatusPending),
		string(models.TaskStatusInProgress),
		string(models.TaskStatusReadyForReview),
		string(models.TaskStatusMerged),
		string(models.TaskStatusBuilding),
		string(models.TaskStatusDeployed),
	}
	var r0 row
	res := r.db.WithContext(ctx).
		Model(&models.ComponentTask{}).
		Select("batch_id", "source_spec_version", "source_design_version").
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Where("batch_id IS NOT NULL").
		Where("status IN ?", live).
		Group("batch_id, source_spec_version, source_design_version").
		Order("MAX(created_at) DESC").
		Limit(1).
		Scan(&r0)
	if res.Error != nil {
		return "", "", "", fmt.Errorf("baseline batch query: %w", res.Error)
	}
	if res.RowsAffected == 0 || r0.BatchID == nil {
		return "", "", "", nil
	}
	batchID = *r0.BatchID
	if r0.SourceSpecVersion != nil {
		specVersion = *r0.SourceSpecVersion
	}
	if r0.SourceDesignVersion != nil {
		designVersion = *r0.SourceDesignVersion
	}
	return batchID, specVersion, designVersion, nil
}

func (r *taskRepository) Create(ctx context.Context, task *models.ComponentTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *taskRepository) Update(ctx context.Context, task *models.ComponentTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *taskRepository) DeleteByProjectID(ctx context.Context, orgID, projectID string) error {
	return r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Delete(&models.ComponentTask{}).Error
}

func (r *taskRepository) DeleteAll(ctx context.Context) error {
	return r.db.WithContext(ctx).Where("1=1").Delete(&models.ComponentTask{}).Error
}
