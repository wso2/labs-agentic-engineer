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

// SpecKickoff is the one-auto-`/start`-per-project claim row (#485): project
// create kicks the spec interview off server-side, and this row is what makes
// that exactly-once. The claim is the row's existence — INSERT ... ON CONFLICT
// DO NOTHING against the composite primary key (the #420 admission pattern,
// sans partial index: every row is the constraint) — so racing kickoffs
// resolve to one winner and a re-fired create can never inject a second
// `/start`. Deliberately NOT a uniqueness rule on `/start` turns themselves:
// a user re-running `/start` later is an amendment interview, not a bug.
//
// The row also carries the attempt's OUTCOME. A bare claim says "somebody
// tried" and reads identically whether the turn was created or the attempt
// died with the agents service unreachable — so the console could not tell
// "starting…" from "never started", and the user was left with a project that
// looked busy forever. Status is what makes a failure sayable, and Retry
// reachable.
type SpecKickoff struct {
	OrgID     string `gorm:"column:org_id;primaryKey"`
	ProjectID string `gorm:"column:project_id;primaryKey"`
	// Status is KickoffStatus*: pending while the attempt runs, failed when it
	// ended without a turn, started once one exists. Rows written before the
	// column existed carry "" and are read as pending (see Service.Kickoff).
	Status string `gorm:"column:status"`
	// Reason is the user-facing failure sentence; empty unless Status is failed.
	Reason    string    `gorm:"column:reason"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TableName pins the table name (house convention: explicit, not inflected).
func (SpecKickoff) TableName() string { return "spec_kickoffs" }

// KickoffRepository is the spec_kickoffs claim store.
type KickoffRepository interface {
	// TryClaim takes the project's one auto-kickoff claim. True means this
	// caller won it; false means it was already taken (by an earlier create or
	// a racing one) and the caller must not start a turn.
	TryClaim(ctx context.Context, orgID, projectID string) (bool, error)

	// Get returns the claim row, or (nil, nil) on a miss. Read-only, and the
	// only evidence that a kickoff is COMING before its turn row lands:
	// between the claim and the turn (repo provisioning: seconds) every other
	// signal reads "nothing here", which is what made the overview offer to
	// generate a spec that was already being generated (#485).
	Get(ctx context.Context, orgID, projectID string) (*SpecKickoff, error)

	// SetOutcome stamps the attempt's result onto the claim (status + reason,
	// touching updated_at). Never re-claims: a missing row means the claim was
	// never taken, and inventing one here would let a lost race record an
	// outcome for somebody else's kickoff.
	SetOutcome(ctx context.Context, orgID, projectID, status, reason string) error

	// Rearm puts a spent claim back into pending for a user-triggered retry —
	// the ONE way a project gets a second kickoff, and it takes a click.
	Rearm(ctx context.Context, orgID, projectID string) error
}

type kickoffRepository struct{ db *gorm.DB }

// NewKickoffRepository builds the spec_kickoffs store.
func NewKickoffRepository(db *gorm.DB) KickoffRepository {
	return &kickoffRepository{db: db}
}

func (r *kickoffRepository) TryClaim(ctx context.Context, orgID, projectID string) (bool, error) {
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&SpecKickoff{
			OrgID:     orgID,
			ProjectID: projectID,
			Status:    KickoffStatusPending,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *kickoffRepository) Get(ctx context.Context, orgID, projectID string) (*SpecKickoff, error) {
	var row SpecKickoff
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *kickoffRepository) SetOutcome(ctx context.Context, orgID, projectID, status, reason string) error {
	return r.db.WithContext(ctx).Model(&SpecKickoff{}).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Updates(map[string]any{
			"status":     status,
			"reason":     reason,
			"updated_at": time.Now(),
		}).Error
}

func (r *kickoffRepository) Rearm(ctx context.Context, orgID, projectID string) error {
	return r.SetOutcome(ctx, orgID, projectID, KickoffStatusPending, "")
}
