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

// Execution is one platform attempt at one kind of work for a Task (§7 of
// docs/design/tasks-github-native.md). A Task itself is a GitHub issue and has
// no row here — executions reference it by (repo, issue_number). No row spans a
// human gate: a merged PR ends nothing, it spawns a build Execution; a retry is
// a new row, never a mutation of an old one.
//
// Kind is coding|build|ops and Status is queued|running|succeeded|failed|
// canceled — the canonical values live in internal/contracts/taskmeta
// (ExecutionKind / ExecutionStatus). They are stored as plain strings here to
// keep the gorm mapping simple, matching the existing model convention.
//
// The admission mutex (§5) is a partial unique index on
// (repo, issue_number, kind) WHERE status IN ('queued','running'), created by
// the executions migration (AutoMigrate cannot express a partial index).
type Execution struct {
	ID        string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrgID     string `gorm:"index;not null" json:"-"`
	ProjectID string `gorm:"index;not null" json:"projectId"`

	// Repo + IssueNumber identify the Task this Execution attempts. Repo is the
	// full "owner/name" the webhook/read path resolves against.
	Repo        string `gorm:"index;not null" json:"repo"`
	IssueNumber int    `gorm:"index;not null" json:"issueNumber"`

	Kind   string `gorm:"not null;index" json:"kind"`   // coding | build | ops
	Status string `gorm:"not null;index" json:"status"` // queued | running | succeeded | failed | canceled

	// Component is the Task's target component, snapshotted at dispatch. It makes
	// the row self-describing for a build retry (which must re-trigger the build
	// for the same component without re-reading the issue) and for progress/audit.
	// Empty for ops Executions (which carry an operation, not a component).
	Component string `gorm:"type:text" json:"component,omitempty"`

	// RunName is the OpenChoreo WorkflowRun / build run name for coding/build
	// kinds (empty until dispatch). DesignTag snapshots the Task's lineage at
	// dispatch time (§7). Reason holds a queued-gating reason or an error.
	RunName   string `gorm:"type:text" json:"runName,omitempty"`
	DesignTag string `gorm:"type:text" json:"designTag,omitempty"`
	Reason    string `gorm:"type:text" json:"reason,omitempty"`

	// CommitSHA pins a build Execution to the merge commit it builds (set when a
	// merged PR spawns the build, §7). It is the one fact a git-auth build retry
	// needs to re-trigger the same build after re-minting the clone credential
	// (the merge SHA arrived on the webhook and is unavailable later), so it is
	// stored on the row rather than recovered from GitHub. Empty for coding/ops.
	CommitSHA string `gorm:"type:text" json:"commitSha,omitempty"`

	CreatedAt time.Time  `json:"createdAt"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`

	// WorkflowID + Version make this row a Temporal-driven read-model snapshot
	// in addition to a per-attempt dispatch record (§R3.3): the orchestration
	// TaskDriver upserts one row per task workflow (contract.TaskWorkflowID),
	// keyed on WorkflowID via a partial unique index (empty WorkflowID rows —
	// the pre-existing per-kind dispatch attempts — are excluded from that
	// constraint). Version increments on every upsert so a caller can detect a
	// stale read without a second round trip. Empty/0 for rows written only
	// through TryAdmit (no workflow attached).
	WorkflowID string `gorm:"index" json:"workflowId,omitempty"`
	Version    int64  `gorm:"not null;default:1" json:"version,omitempty"`
}

// TableName pins the table name (the default pluralization would already give
// "executions", but pin it so a struct rename cannot silently move the table).
func (Execution) TableName() string { return "executions" }
