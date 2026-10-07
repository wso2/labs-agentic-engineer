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

// AgentGuardrailApplicationRepository reads and writes
// agent_guardrail_applications.
type AgentGuardrailApplicationRepository interface {
	// Get returns the recorded application; nil when none is recorded (not an
	// error).
	Get(ctx context.Context, ocOrgID, project, component, environment string) (*AgentGuardrailApplication, error)
	// Put records row as the latest application, replacing any earlier one.
	Put(ctx context.Context, row AgentGuardrailApplication) error
	// ListForComponent returns the component's application in every
	// environment it has one for.
	ListForComponent(ctx context.Context, ocOrgID, project, component string) ([]AgentGuardrailApplication, error)
	// DeleteByProject removes every record of the project — each agent, each
	// environment. Deleting nothing is not an error.
	DeleteByProject(ctx context.Context, ocOrgID, project string) error
}

type agentGuardrailApplicationRepository struct{ db *gorm.DB }

// NewAgentGuardrailApplicationRepository constructs the gorm-backed repository.
func NewAgentGuardrailApplicationRepository(db *gorm.DB) AgentGuardrailApplicationRepository {
	return &agentGuardrailApplicationRepository{db: db}
}

func (r *agentGuardrailApplicationRepository) Get(ctx context.Context, ocOrgID, project, component, environment string) (*AgentGuardrailApplication, error) {
	var row AgentGuardrailApplication
	err := r.db.WithContext(ctx).
		Where("oc_org_id = ? AND project = ? AND component = ? AND environment = ?", ocOrgID, project, component, environment).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *agentGuardrailApplicationRepository) Put(ctx context.Context, row AgentGuardrailApplication) error {
	row.UpdatedAt = time.Now().UTC()
	if row.AppliedNames == nil {
		row.AppliedNames = []string{}
	}
	if len(row.Outcomes) == 0 {
		row.Outcomes = []byte("[]")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "oc_org_id"}, {Name: "project"}, {Name: "component"}, {Name: "environment"}},
		DoUpdates: clause.AssignmentColumns([]string{"applied_names", "outcomes", "updated_at"}),
	}).Create(&row).Error
}

func (r *agentGuardrailApplicationRepository) ListForComponent(ctx context.Context, ocOrgID, project, component string) ([]AgentGuardrailApplication, error) {
	var rows []AgentGuardrailApplication
	err := r.db.WithContext(ctx).
		Where("oc_org_id = ? AND project = ? AND component = ?", ocOrgID, project, component).
		Order("environment").
		Find(&rows).Error
	return rows, err
}

func (r *agentGuardrailApplicationRepository) DeleteByProject(ctx context.Context, ocOrgID, project string) error {
	return r.db.WithContext(ctx).
		Where("oc_org_id = ? AND project = ?", ocOrgID, project).
		Delete(&AgentGuardrailApplication{}).Error
}
