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
)

// OrgAgentSettingsRepository reads the per-org agent setting
// (`org_agent_settings`). It has no write method on purpose: the row is
// written only by the AI agents card's unit of work (AgentsCardTx), together
// with the credentials the same save touches.
type OrgAgentSettingsRepository interface {
	// GetByOrg returns the org's setting, or nil when there is none.
	//
	// A missing row is NOT an error: it is the ordinary state of an org that has
	// never opened the setting, and it means the platform defaults.
	GetByOrg(ctx context.Context, ocOrgID string) (*OrgAgentSettings, error)
}

type orgAgentSettingsRepository struct{ db *gorm.DB }

// NewOrgAgentSettingsRepository constructs the gorm-backed repository.
func NewOrgAgentSettingsRepository(db *gorm.DB) OrgAgentSettingsRepository {
	return &orgAgentSettingsRepository{db: db}
}

func (r *orgAgentSettingsRepository) GetByOrg(ctx context.Context, ocOrgID string) (*OrgAgentSettings, error) {
	return getAgentSettings(r.db.WithContext(ctx), ocOrgID)
}

// getAgentSettings is the one read of the row, shared by the pool reader and
// the card's transaction so the two cannot disagree on what "absent" means.
func getAgentSettings(db *gorm.DB, ocOrgID string) (*OrgAgentSettings, error) {
	var row OrgAgentSettings
	err := db.Where("oc_org_id = ?", ocOrgID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
