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

package models

import "time"

// CycleStatus is the coarse lifecycle status of a development-cycle read-model
// row. It is NOT the workflow's phase (requirements|design|implement|…, which
// Temporal owns and the BFF reads via a query) — it only records whether the
// cycle's workflow is still running. Stored as a plain string, matching the
// Execution model convention.
type CycleStatus string

const (
	CycleActive    CycleStatus = "active"
	CycleCompleted CycleStatus = "completed"
	CycleFailed    CycleStatus = "failed"
)

// DevelopmentCycle is the BFF's read-model anchor for one Temporal
// DevelopmentFlowWorkflow instance. Temporal owns the durable flow position
// (phase, gates, task states — read via QueryWorkflow); this row only maps a
// project's development cycle to its workflow ID so the BFF can find the right
// workflow to signal/query, and lists a project's cycles for the console.
//
// One row per workflow instance: WorkflowID is unique and deterministic
// (devflow:<org>:<project>:<cycle> — see packages/contracts/orchestration), so
// a retried start trigger resolves to the same row (idempotent Ensure) and a
// retried StartWorkflow hits WorkflowExecutionAlreadyStarted.
type DevelopmentCycle struct {
	ID        string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrgID     string `gorm:"index;not null" json:"-"`
	ProjectID string `gorm:"index;not null" json:"projectId"`

	// RequirementVersion is the requirements tag (e.g. "v3") that started this
	// cycle; empty for the GitHub-issue fast-path. Snapshot for audit/listing.
	RequirementVersion string `gorm:"type:text" json:"requirementVersion,omitempty"`

	// WorkflowID is the Temporal workflow ID this row anchors. Unique — one row
	// per workflow instance; the deterministic shape gives free dedup.
	WorkflowID string `gorm:"uniqueIndex;not null;type:text" json:"workflowId"`

	// Status is active|completed|failed (see CycleStatus). It reflects the
	// workflow's terminal disposition, not its in-flight phase.
	Status string `gorm:"not null;index" json:"status"`

	CreatedAt   time.Time  `json:"createdAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

// TableName pins the table name so a struct rename cannot silently move it.
func (DevelopmentCycle) TableName() string { return "development_cycles" }
