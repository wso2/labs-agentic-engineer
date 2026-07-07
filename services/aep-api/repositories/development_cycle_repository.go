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
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wso2/aep/aep-api/models"
)

// DevelopmentCycleRepository is the BFF's read-model store mapping a project's
// development cycles to their Temporal workflow IDs (models.DevelopmentCycle).
// Temporal owns the durable flow position; these rows only let the BFF find the
// workflow to signal/query and list a project's cycles. Lookups miss with
// (nil, nil) — never gorm.ErrRecordNotFound — matching the house convention.
type DevelopmentCycleRepository interface {
	// Ensure idempotently records a cycle keyed by its (unique) WorkflowID. On a
	// first call it INSERTs and returns the stored row; a repeated call with the
	// same WorkflowID (a retried start trigger) is a no-op that returns the
	// existing row. Status defaults to active when empty.
	Ensure(ctx context.Context, c *models.DevelopmentCycle) (*models.DevelopmentCycle, error)

	// GetByWorkflowID returns the cycle for a workflow ID, or (nil, nil) if none.
	GetByWorkflowID(ctx context.Context, workflowID string) (*models.DevelopmentCycle, error)

	// ListByProject returns a project's cycles, newest first (org-scoped).
	ListByProject(ctx context.Context, orgID, projectID string) ([]*models.DevelopmentCycle, error)

	// SetStatus records a cycle's terminal disposition (completed|failed),
	// stamping completed_at. Returns the updated row, or (nil, nil) if the
	// workflow ID is unknown. Idempotent — a re-set to the same status re-stamps.
	SetStatus(ctx context.Context, workflowID string, status models.CycleStatus) (*models.DevelopmentCycle, error)
}

type developmentCycleRepository struct {
	db *gorm.DB
}

func NewDevelopmentCycleRepository(db *gorm.DB) DevelopmentCycleRepository {
	return &developmentCycleRepository{db: db}
}

func (r *developmentCycleRepository) Ensure(ctx context.Context, c *models.DevelopmentCycle) (*models.DevelopmentCycle, error) {
	if c.Status == "" {
		c.Status = string(models.CycleActive)
	}
	// ON CONFLICT (workflow_id) DO NOTHING: a retried start trigger inserts zero
	// rows. When that happens, fetch and return the existing row so callers get a
	// consistent result whether they won or lost the race.
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(c)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return r.GetByWorkflowID(ctx, c.WorkflowID)
	}
	return c, nil
}

func (r *developmentCycleRepository) GetByWorkflowID(ctx context.Context, workflowID string) (*models.DevelopmentCycle, error) {
	var c models.DevelopmentCycle
	err := r.db.WithContext(ctx).Where("workflow_id = ?", workflowID).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *developmentCycleRepository) ListByProject(ctx context.Context, orgID, projectID string) ([]*models.DevelopmentCycle, error) {
	var out []*models.DevelopmentCycle
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Order("created_at DESC").
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *developmentCycleRepository) SetStatus(ctx context.Context, workflowID string, status models.CycleStatus) (*models.DevelopmentCycle, error) {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).
		Model(&models.DevelopmentCycle{}).
		Where("workflow_id = ?", workflowID).
		Updates(map[string]any{
			"status":       string(status),
			"completed_at": now,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.GetByWorkflowID(ctx, workflowID)
}
