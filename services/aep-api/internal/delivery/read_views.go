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

	"github.com/wso2/aep/aep-api/internal/contracts"
)

// The shared task/execution READ DTOs (§10.3.1): the projected shapes every
// reader join produces. They live in the delivery ROOT rather than the taskflow
// sub-package because they cross a feature boundary — the `build` sub-package's
// task-list read consumes TaskView through its TaskReader port, and a build must
// not import taskflow (slice ⊥ sibling). Keeping them here is what lets both the
// task reader (producer) and the build reader (consumer) name the same type
// while each imports only the root. They carry no behaviour and no gorm — the
// task Reads service in the taskflow sub-package builds them from live GitHub
// facts fused with the executions rows.

// Lineage is the spec+design versions a Task was planned from (§2 lineage).
type Lineage struct {
	SpecTag   string `json:"specTag,omitempty"`
	DesignTag string `json:"designTag,omitempty"`
}

// ExecutionView is one Execution row projected for the API.
type ExecutionView struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Status    string     `json:"status"`
	RunName   string     `json:"runName,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
}

// UsageView is the Usage wire shape (#245) attached to task/build read
// models: the persisted token counts plus the read-time-derived costUsd
// (ADR-0011 — CostUsd nil when no rate is configured for the model).
type UsageView struct {
	InputTokens         int64    `json:"inputTokens"`
	OutputTokens        int64    `json:"outputTokens"`
	CacheReadTokens     int64    `json:"cacheReadTokens"`
	CacheCreationTokens int64    `json:"cacheCreationTokens"`
	Model               string   `json:"model"`
	CostUsd             *float64 `json:"costUsd"`
}

// NewUsageView projects an aggregate + its derived cost onto the wire shape.
func NewUsageView(u contracts.TokenUsage, costUSD *float64) *UsageView {
	return &UsageView{
		InputTokens:         u.InputTokens,
		OutputTokens:        u.OutputTokens,
		CacheReadTokens:     u.CacheReadTokens,
		CacheCreationTokens: u.CacheCreationTokens,
		Model:               u.Model,
		CostUsd:             costUSD,
	}
}

// TaskView is the list-item shape (§9.1): live GitHub facts fused with the
// latest Execution per kind into a derived status.
type TaskView struct {
	IssueNumber int    `json:"issueNumber"`
	Title       string `json:"title"`
	IssueURL    string `json:"issueUrl"`
	// PRURL links the task's pull request, recovered from the succeeded coding
	// Execution's "pr#N" reason (no live PR query); empty before a PR opens.
	PRURL         string                   `json:"prUrl,omitempty"`
	ExecutorClass string                   `json:"executorClass,omitempty"`
	Origin        string                   `json:"origin,omitempty"`
	Component     string                   `json:"component,omitempty"`
	Operation     string                   `json:"operation,omitempty"`
	DependsOn     []string                 `json:"dependsOn"`
	Rationale     string                   `json:"rationale,omitempty"`
	Body          string                   `json:"body,omitempty"`
	Lineage       Lineage                  `json:"lineage"`
	DerivedStatus string                   `json:"derivedStatus"`
	Hold          bool                     `json:"hold"`
	Attention     []string                 `json:"attention"`
	Executions    map[string]ExecutionView `json:"executions"`
	// BlockedBy lists the dependency display names a not-yet-started coding Task
	// is waiting on when its DerivedStatus was reconciled to on_hold (issue #164
	// follow-up). Empty/omitted when the Task is not dependency-gated — the board
	// reads it to render "On hold — Waiting for X".
	BlockedBy []string `json:"blockedBy,omitempty"`
	// Usage is the task's aggregate captured usage across all its executions
	// (#245); absent for tasks with none recorded.
	Usage *UsageView `json:"usage,omitempty"`
}

// TaskDetail is the Get shape: a TaskView plus the full Execution history.
type TaskDetail struct {
	TaskView
	ExecutionHistory []ExecutionView `json:"executionHistory"`
}
