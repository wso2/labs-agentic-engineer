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
	"time"

	"github.com/google/uuid"
)

// CodingAgentLog is a legacy per-run snapshot of a coding-agent pod's
// stdout/stderr, keyed by `(task_id, run_name)` where task_id is an
// execution id. New milestone cycles do not write this table — live and
// archived logs are read from OpenChoreo and the observability plane.
// Rows that already exist remain readable for old execution-keyed progress.
type CodingAgentLog struct {
	TaskID     uuid.UUID `gorm:"type:uuid;primaryKey;column:task_id" json:"taskId"`
	RunName    string    `gorm:"type:text;primaryKey;column:run_name" json:"runName"`
	FinalPhase string    `gorm:"type:text;not null;column:final_phase" json:"finalPhase"`
	CapturedAt time.Time `gorm:"type:timestamptz;not null;default:now();column:captured_at" json:"capturedAt"`
	LogText    string    `gorm:"type:text;not null;column:log_text" json:"-"`
	SizeBytes  int64     `gorm:"type:bigint;not null;column:size_bytes" json:"sizeBytes"`
}

func (CodingAgentLog) TableName() string { return "coding_agent_logs" }
