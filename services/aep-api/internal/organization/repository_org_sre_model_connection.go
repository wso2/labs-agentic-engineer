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

// OrgSreModelConnectionRepository reads the org's SRE model connection row
// (`org_sre_model_connections`). It has no insert or delete: the row is
// written only by the AI agents card's unit of work (AgentsCardTx), together
// with the key's bytes in the same transaction.
type OrgSreModelConnectionRepository interface {
	// GetByOrg returns the org's SRE model connection, or nil when it has
	// none (not an error).
	GetByOrg(ctx context.Context, ocOrgID string) (*OrgSreModelConnection, error)
}

type orgSreModelConnectionRepository struct{ db *gorm.DB }

// NewOrgSreModelConnectionRepository constructs the gorm-backed repository.
func NewOrgSreModelConnectionRepository(db *gorm.DB) OrgSreModelConnectionRepository {
	return &orgSreModelConnectionRepository{db: db}
}

func (r *orgSreModelConnectionRepository) GetByOrg(ctx context.Context, ocOrgID string) (*OrgSreModelConnection, error) {
	return getSreModelConnection(r.db.WithContext(ctx), ocOrgID)
}

// getSreModelConnection is the one read of the row, shared by the pool reader
// and the card's transaction so the two cannot disagree on what "absent"
// means.
func getSreModelConnection(db *gorm.DB, ocOrgID string) (*OrgSreModelConnection, error) {
	var row OrgSreModelConnection
	err := db.Where("oc_org_id = ?", ocOrgID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
