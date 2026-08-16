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
type SpecKickoff struct {
	OrgID     string    `gorm:"column:org_id;primaryKey"`
	ProjectID string    `gorm:"column:project_id;primaryKey"`
	CreatedAt time.Time `json:"createdAt"`
}

// TableName pins the table name (house convention: explicit, not inflected).
func (SpecKickoff) TableName() string { return "spec_kickoffs" }

// KickoffRepository is the spec_kickoffs claim store.
type KickoffRepository interface {
	// TryClaim takes the project's one auto-kickoff claim. True means this
	// caller won it; false means it was already taken (by an earlier create or
	// a racing one) and the caller must not start a turn.
	TryClaim(ctx context.Context, orgID, projectID string) (bool, error)
}

type kickoffRepository struct{ db *gorm.DB }

// NewKickoffRepository builds the spec_kickoffs store.
func NewKickoffRepository(db *gorm.DB) KickoffRepository {
	return &kickoffRepository{db: db}
}

func (r *kickoffRepository) TryClaim(ctx context.Context, orgID, projectID string) (bool, error) {
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&SpecKickoff{OrgID: orgID, ProjectID: projectID})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}
