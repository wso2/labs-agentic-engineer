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

package spec

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SpecKickoff is the claim row behind "one server-side `/start` per project"
// (#485). The composite primary key IS the exactly-once guard: the claim is an
// INSERT ... ON CONFLICT DO NOTHING, so replicas racing a fresh project's
// creation resolve to exactly one winner without a lock.
//
// It also carries what became of the attempt, because the alternative — the
// console deriving "did the interview start" from the running turn, the thread
// and the spec files — is blind for the seconds between the claim and the turn
// row, and blind forever to an attempt that never produced one.
type SpecKickoff struct {
	OrgID     string `gorm:"primaryKey"`
	ProjectID string `gorm:"primaryKey"`

	// Status is pending | failed | started (KickoffStatus*). `none` is never
	// stored — it is what the read reports for a project with no row.
	Status string `gorm:"not null;default:'pending'"`
	// Reason is the user-facing failure sentence; empty unless Status is failed.
	Reason string `gorm:"type:text;not null;default:''"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName pins the table so a struct rename cannot silently move it.
func (SpecKickoff) TableName() string { return "spec_kickoffs" }

// KickoffRepository is the spec_kickoffs row store. Lookups miss with
// (nil, nil), matching the house convention.
type KickoffRepository interface {
	// TryClaim inserts the project's pending claim, reporting whether THIS
	// caller won it. A spent claim answers false and is never overwritten —
	// that is what makes the create-time kickoff fire once per project, ever.
	TryClaim(ctx context.Context, orgID, projectID string) (bool, error)

	// Get returns the claim row, or (nil, nil) when the project has none.
	Get(ctx context.Context, orgID, projectID string) (*SpecKickoff, error)

	// SetOutcome stamps an attempt's result onto the claim.
	SetOutcome(ctx context.Context, orgID, projectID, status, reason string) error

	// Rearm returns a spent claim to pending and clears its reason — the
	// user's Retry, which is the only thing that re-opens a claim.
	Rearm(ctx context.Context, orgID, projectID string) error
}

type kickoffRepository struct{ db *gorm.DB }

// NewKickoffRepository builds the spec_kickoffs store.
func NewKickoffRepository(db *gorm.DB) KickoffRepository { return &kickoffRepository{db: db} }

func (r *kickoffRepository) TryClaim(ctx context.Context, orgID, projectID string) (bool, error) {
	row := &SpecKickoff{OrgID: orgID, ProjectID: projectID, Status: KickoffStatusPending}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *kickoffRepository) Get(ctx context.Context, orgID, projectID string) (*SpecKickoff, error) {
	var row SpecKickoff
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *kickoffRepository) SetOutcome(ctx context.Context, orgID, projectID, status, reason string) error {
	return r.db.WithContext(ctx).
		Model(&SpecKickoff{}).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Updates(map[string]any{
			"status":     status,
			"reason":     reason,
			"updated_at": time.Now().UTC(),
		}).Error
}

func (r *kickoffRepository) Rearm(ctx context.Context, orgID, projectID string) error {
	return r.SetOutcome(ctx, orgID, projectID, KickoffStatusPending, "")
}
