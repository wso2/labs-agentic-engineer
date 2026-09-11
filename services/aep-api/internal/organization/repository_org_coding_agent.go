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

package organization

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrgCodingAgentRepository persists the per-org coding-agent setting.
//
// Every method is keyed by the org handle — never a broadenable predicate — so
// a dropped filter is a missing method rather than a cross-org write, the same
// rule the other repositories in this package follow.
type OrgCodingAgentRepository interface {
	// GetByOrg returns the org's setting, or nil when there is none.
	//
	// A missing row is NOT an error: it is the ordinary state of an org that has
	// never opened the setting, and it means the platform defaults.
	GetByOrg(ctx context.Context, ocOrgID string) (*OrgCodingAgentSetting, error)
	// Upsert writes the whole row, creating it or replacing every column.
	//
	// Whole-row rather than column-wise because the service always resolves both
	// values before it writes: a patch that names only a model still carries the
	// runtime the org already had, resolved against the row this same call is
	// about to replace. Two columns and one writer is not a place to invent a
	// partial-update path.
	Upsert(ctx context.Context, row *OrgCodingAgentSetting) error
	// DeleteByOrg removes the org's setting, putting it back on the platform
	// defaults. Idempotent: deleting a setting that is not there is not an error,
	// because "reset me" is satisfied either way.
	DeleteByOrg(ctx context.Context, ocOrgID string) error
}

type orgCodingAgentRepository struct{ db *gorm.DB }

// NewOrgCodingAgentRepository constructs the gorm-backed repository.
func NewOrgCodingAgentRepository(db *gorm.DB) OrgCodingAgentRepository {
	return &orgCodingAgentRepository{db: db}
}

func (r *orgCodingAgentRepository) GetByOrg(ctx context.Context, ocOrgID string) (*OrgCodingAgentSetting, error) {
	var row OrgCodingAgentSetting
	err := r.db.WithContext(ctx).Where("oc_org_id = ?", ocOrgID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *orgCodingAgentRepository) Upsert(ctx context.Context, row *OrgCodingAgentSetting) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "oc_org_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"runtime", "model", "updated_by", "updated_at"}),
	}).Create(row).Error
}

func (r *orgCodingAgentRepository) DeleteByOrg(ctx context.Context, ocOrgID string) error {
	return r.db.WithContext(ctx).Where("oc_org_id = ?", ocOrgID).Delete(&OrgCodingAgentSetting{}).Error
}
