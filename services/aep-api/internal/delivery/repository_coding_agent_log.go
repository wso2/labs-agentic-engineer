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

package delivery

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CodingAgentLogRepository is the read-only coding_agent_logs store for legacy
// execution-keyed rows written before milestone cycles moved log reading onto
// OpenChoreo + the observability plane. Nothing writes this table any more —
// AgentProgressReader.GetByRun is the only consumer. Lookups miss with
// (nil, nil), matching the house convention.
type CodingAgentLogRepository interface {
	// GetByRun returns a previously captured log for (executionID, runName), or
	// (nil, nil) when no row exists.
	GetByRun(ctx context.Context, executionID uuid.UUID, runName string) (*CodingAgentLog, error)
}

type codingAgentLogRepository struct {
	db *gorm.DB
}

// NewCodingAgentLogRepository builds the coding_agent_logs store.
func NewCodingAgentLogRepository(db *gorm.DB) CodingAgentLogRepository {
	return &codingAgentLogRepository{db: db}
}

func (r *codingAgentLogRepository) GetByRun(ctx context.Context, executionID uuid.UUID, runName string) (*CodingAgentLog, error) {
	var row CodingAgentLog
	err := r.db.WithContext(ctx).
		Where("task_id = ? AND run_name = ?", executionID, runName).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
