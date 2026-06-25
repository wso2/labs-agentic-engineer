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

import (
	"time"

	"github.com/wso2/asdlc/asdlc-service/internal/contracts"
)

// StringSlice is a reusable slice type for JSONB storage in PostgreSQL.
type StringSlice []string

// TaskLifecycleStatus tracks the GitHub issue creation phase for a ComponentTask.
type TaskLifecycleStatus string

const (
	// TaskLifecycleGhIssueWaiting is the default — the task row exists but the
	// GitHub issue has not been created yet (issue creation is in flight).
	TaskLifecycleGhIssueWaiting TaskLifecycleStatus = "gh_issue_waiting"
	// TaskLifecycleGhIssueSyncing is a response-only value — never written to DB.
	// GetBoard returns this when a gh_issue_created task's issue is not yet
	// visible on the GitHub Project board (fallback path, board has 0 items).
	TaskLifecycleGhIssueSyncing TaskLifecycleStatus = "gh_issue_syncing"
	// TaskLifecycleGhIssueCreated is set once the GitHub issue is successfully
	// created and the issue URL + number are persisted.
	TaskLifecycleGhIssueCreated TaskLifecycleStatus = "gh_issue_created"
	// TaskLifecycleGhIssueFailed is set when GitHub issue creation fails after
	// the issue goroutine exhausts its attempts.
	TaskLifecycleGhIssueFailed TaskLifecycleStatus = "gh_issue_failed"
)

// TaskStatus is the single, webhook-driven lifecycle for a ComponentTask.
// Its canonical definition + the transition algebra (ApplyTaskEvent, the
// transition table, IsTerminal) live in internal/contracts — the
// dependency-free layer. These are re-export aliases so the gorm model and
// every models.TaskStatus consumer reuse the contracts state machine. The
// projector in services/webhook is the only writer outside dispatch.
type TaskStatus = contracts.TaskStatus

const (
	TaskStatusPending            = contracts.TaskStatusPending
	TaskStatusInProgress         = contracts.TaskStatusInProgress
	TaskStatusReadyForReview     = contracts.TaskStatusReadyForReview
	TaskStatusMerged             = contracts.TaskStatusMerged
	TaskStatusBuilding           = contracts.TaskStatusBuilding
	TaskStatusDeployed           = contracts.TaskStatusDeployed
	TaskStatusRejected           = contracts.TaskStatusRejected
	TaskStatusFailed             = contracts.TaskStatusFailed
	TaskStatusAbandoned          = contracts.TaskStatusAbandoned
	TaskStatusOnHold             = contracts.TaskStatusOnHold
	TaskStatusVerificationFailed = contracts.TaskStatusVerificationFailed
)

// Task types (the typed task graph, plan §4). "component" is a code task (1:1
// with a GitHub issue + PR). "config-collection" collects a connection's per-env
// values in the console (no GitHub issue; reaching `deployed` means provisioned).
// "resource-provisioning" is P5. Empty Type is treated as "component" (old rows).
const (
	TaskTypeComponent            = "component"
	TaskTypeConfigCollection     = "config-collection"
	TaskTypeResourceProvisioning = "resource-provisioning"
)

// ComponentTask is one implementation task targeting a single component.
// Scoped to an append-only batch (see BatchID, SourceDesignVersion,
// SourceSpecVersion). Maps 1:1:1:1 to a GitHub issue, feature branch, and
// draft PR; state is driven by webhooks (services/webhook/projector.go).
//
// The row does not snapshot component shape (OpenAPI, language, dependencies,
// etc.); dispatch reads the current design from the multi-file `specs/design/`
// tree on every run (docs/design/tech-lead-agent.md).
type ComponentTask struct {
	ID        string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ProjectID string `gorm:"index;not null" json:"projectId"`
	OrgID     string `gorm:"index;not null" json:"-"`

	// Component identity — name only. Full component shape lives under
	// `specs/design/components/<ComponentName>/` (design.md +
	// optional openapi.yaml).
	ComponentName string `gorm:"not null" json:"componentName"`

	// Title is the GitHub issue title. Used as the dependsOn key within a
	// batch and surfaces in the UI / kanban.
	Title string `gorm:"type:text" json:"title"`

	// Rationale is the planner's one-sentence "why this task exists".
	// Surfaced under the title in each card.
	Rationale string `gorm:"type:text" json:"rationale,omitempty"`

	// Body is the GitHub issue body authored by the detail phase, before
	// the platform's "Local Developer Setup" + "Closes #N" suffix is
	// appended at issue-create / edit time. The DB row is canonical for
	// dispatch; if the GH edit fails BodySyncPending is set and a
	// reconciler retries.
	Body string `gorm:"type:text" json:"body,omitempty"`

	// DependsOnComponents lists component names this task's component
	// depends on, sourced directly from the `specs/design/` tree
	// (design.Components[*].DependsOn, parsed from per-component
	// design.md frontmatter). The value is platform-authored,
	// not LLM-authored, so gating cannot silently fail open on a
	// hallucinated identifier. Dispatch (deploy-gated): a task is
	// dispatchable only when, for every entry c in DependsOnComponents,
	// the batch contains a task whose ComponentName == c and Status
	// == deployed. See services/dispatch_service.go::depsAllDeployed.
	DependsOnComponents StringSlice `gorm:"column:depends_on_components;type:jsonb;serializer:json" json:"dependsOnComponents,omitempty"`

	// DependsOnConnections lists the `external` connection names this component
	// binds. A component task gates (in addition to DependsOnComponents) until
	// each connection's config-collection task is Completed — the typed task
	// graph (plan §4). Platform-authored from the design's external dependencies.
	DependsOnConnections StringSlice `gorm:"column:depends_on_connections;type:jsonb;serializer:json" json:"dependsOnConnections,omitempty"`

	// ConnectionName is set only on config-collection tasks (Type ==
	// config-collection): the external connection whose per-env values this task
	// collects. Empty for component tasks.
	ConnectionName string `gorm:"column:connection_name;index" json:"connectionName,omitempty"`

	// Lineage — set at generation time, immutable thereafter.
	BatchID             *string `gorm:"type:uuid;index" json:"batchId,omitempty"`
	SourceDesignVersion string  `gorm:"type:text" json:"sourceDesignVersion,omitempty"`
	SourceSpecVersion   string  `gorm:"type:text" json:"sourceSpecVersion,omitempty"`

	// Type discriminates the task graph. "component" = a code task (1:1 with a
	// GitHub issue + PR; the only type today). Forward-compat for P2:
	// "config-collection" (collect connection / component config values in the
	// console; no GitHub issue) and "resource-provisioning" (P5). Kept an OPEN
	// string, not a fixed switch — gating/cascade branch on "is this blocker
	// satisfied" per type. Empty is treated as "component" for old rows.
	Type string `gorm:"default:component;index" json:"type"`

	// Execution
	Order         int    `json:"order"` // 1-indexed; surfaces as a stable display order
	Status        string `gorm:"default:pending;index" json:"status"`
	WorkspacePath string `json:"workspacePath"`
	ExecType      string `json:"execType"` // "SYSTEM","WORKER"

	// GitHub artifacts (1:1 with this task) — set at dispatch.
	IssueURL          string      `gorm:"type:text;index" json:"issueUrl,omitempty"`
	IssueNumber       int         `json:"issueNumber,omitempty"`
	Labels            StringSlice `gorm:"type:jsonb;serializer:json" json:"labels,omitempty"`
	LifecycleStatus   string      `gorm:"not null;default:gh_issue_waiting" json:"lifecycleStatus"`
	BranchName        string      `gorm:"type:text;index" json:"branchName,omitempty"`
	PullRequestNumber int         `gorm:"index" json:"pullRequestNumber,omitempty"`
	PullRequestURL    string      `gorm:"type:text" json:"pullRequestUrl,omitempty"`

	// State derived from webhooks.
	MergeCommitSHA   string     `gorm:"type:text;index" json:"mergeCommitSha,omitempty"`
	LastEventAt      *time.Time `gorm:"index" json:"lastEventAt,omitempty"`
	LastBuildRunName string     `gorm:"type:text" json:"lastBuildRunName,omitempty"`
	LastBuildSHA     string     `gorm:"type:text" json:"lastBuildSha,omitempty"`
	// LastCodingAgentRunName is the most recent OC WorkflowRun that ran the
	// per-task coding agent. Mirrors LastBuildRunName for the build phase.
	// Set by DispatchService at dispatch time; the coding-agent watcher
	// reads it to poll the run's status.
	LastCodingAgentRunName string `gorm:"type:text" json:"lastCodingAgentRunName,omitempty"`

	// Error tracking
	ErrorMessage string `gorm:"type:text" json:"errorMessage,omitempty"`

	// Cause records the projector event that drove the most recent
	// terminal transition. NULL on non-terminal rows.
	Cause *string `gorm:"type:text;index" json:"cause,omitempty"`

	// BuildAuthRetryCount counts how many times the build watcher has
	// re-minted + recreated this task's WorkflowRun in response to
	// git_clone_failed_auth. Budget enforced in build_watcher.
	BuildAuthRetryCount int `gorm:"not null;default:0" json:"buildAuthRetryCount,omitempty"`

	// DispatchDeferredAt is set the first time dispatchOne reverts a task
	// to on_hold because a dependency's external URL is not yet available
	// in the OC ReleaseBinding status (timing race between build completion
	// and the OC controller resolving the ingress). The on_hold_watcher
	// retries dispatch every 10s; after deferDeadline (2 min) the task is
	// permanently failed. Nil on tasks that were never deferred.
	DispatchDeferredAt *time.Time `gorm:"column:dispatch_deferred_at" json:"dispatchDeferredAt,omitempty"`

	// BodySyncPending is set when the GitHub issue body edit failed after
	// retries; a periodic reconciler re-attempts the edit. The DB row's
	// Body field is canonical for dispatch regardless of GH state.
	BodySyncPending bool `gorm:"not null;default:false" json:"bodySyncPending,omitempty"`

	DispatchedAt *time.Time `json:"dispatchedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}
