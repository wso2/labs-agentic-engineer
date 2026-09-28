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

// OrgAnthropicRepository reads the per-org, per-role Anthropic credential
// metadata rows (`org_anthropic_credentials`, PK (oc_org_id, role) — the
// non-secret projection fields; the encrypted bytes live in org_secrets) and
// stamps their secret-ref columns. Every accessor is keyed by BOTH oc_org_id and
// role, so a dropped filter is a missing method, not a cross-org write or a
// cross-role one.
//
// It has no insert or delete: a credential row is written only by the AI agents
// card's unit of work (AgentsCardTx), together with its bytes and the setting
// the same save changes.
type OrgAnthropicRepository interface {
	// GetByOrg returns the row for (ocOrgID, role), or nil when absent (not an
	// error).
	GetByOrg(ctx context.Context, ocOrgID string, role AnthropicRole) (*OrgAnthropicCredential, error)
	// UpdateColumns writes the given columns onto the row scoped to
	// (oc_org_id, role) (a map so nil values are written as NULL, not skipped).
	UpdateColumns(ctx context.Context, ocOrgID string, role AnthropicRole, updates map[string]any) error
}

type orgAnthropicRepository struct {
	db *gorm.DB
}

// NewOrgAnthropicRepository constructs the gorm-backed OrgAnthropicRepository.
func NewOrgAnthropicRepository(db *gorm.DB) OrgAnthropicRepository {
	return &orgAnthropicRepository{db: db}
}

func (r *orgAnthropicRepository) GetByOrg(ctx context.Context, ocOrgID string, role AnthropicRole) (*OrgAnthropicCredential, error) {
	var row OrgAnthropicCredential
	err := r.db.WithContext(ctx).Where("oc_org_id = ? AND role = ?", ocOrgID, role).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *orgAnthropicRepository) UpdateColumns(ctx context.Context, ocOrgID string, role AnthropicRole, updates map[string]any) error {
	return r.db.WithContext(ctx).
		Model(&OrgAnthropicCredential{}).
		Where("oc_org_id = ? AND role = ?", ocOrgID, role).
		Updates(updates).Error
}
