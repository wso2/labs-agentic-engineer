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
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AIAgentModelEndpointRepository reads and writes ai_agent_model_endpoints:
// the endpoint each governed ai-agent's key was last stored beside.
type AIAgentModelEndpointRepository interface {
	// Get returns the recorded endpoint; ok is false when none is recorded
	// (not an error).
	Get(ctx context.Context, ocOrgID, component, environment string) (endpoint string, ok bool, err error)
	// Put records endpoint as the one stored beside the agent's key now,
	// replacing whatever was recorded before.
	Put(ctx context.Context, ocOrgID, component, environment, endpoint string) error
}

type aiAgentModelEndpointRepository struct{ db *gorm.DB }

// NewAIAgentModelEndpointRepository constructs the gorm-backed repository.
func NewAIAgentModelEndpointRepository(db *gorm.DB) AIAgentModelEndpointRepository {
	return &aiAgentModelEndpointRepository{db: db}
}

func (r *aiAgentModelEndpointRepository) Get(ctx context.Context, ocOrgID, component, environment string) (string, bool, error) {
	var row AIAgentModelEndpoint
	err := r.db.WithContext(ctx).
		Where("oc_org_id = ? AND component = ? AND environment = ?", ocOrgID, component, environment).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return row.Endpoint, true, nil
}

func (r *aiAgentModelEndpointRepository) Put(ctx context.Context, ocOrgID, component, environment, endpoint string) error {
	row := AIAgentModelEndpoint{
		OcOrgID: ocOrgID, Component: component, Environment: environment,
		Endpoint: endpoint, UpdatedAt: time.Now().UTC(),
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "oc_org_id"}, {Name: "component"}, {Name: "environment"}},
		DoUpdates: clause.AssignmentColumns([]string{"endpoint", "updated_at"}),
	}).Create(&row).Error
}
