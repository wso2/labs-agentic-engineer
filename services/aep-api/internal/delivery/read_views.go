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

import "time"

// The shared task/execution READ DTOs (§10.3.1): the projected shapes every
// reader join produces. They live in the delivery ROOT rather than the taskflow
// sub-package because they cross a feature boundary — the `build` sub-package's
// task-list read consumes TaskView through its TaskReader port, and a build must
// not import taskflow (slice ⊥ sibling). Keeping them here is what lets both the
// task reader (producer) and the build reader (consumer) name the same type
// while each imports only the root. They carry no behaviour and no gorm — the
// task Reads service in the taskflow sub-package builds them from live GitHub
// facts fused with the executions rows.

// The derived-status vocabulary a Task view can carry.
//
// It is DEGRADED, on purpose. The old algebra derived ten values by joining
// GitHub facts with per-issue execution rows; the milestone model writes no
// per-issue execution rows, so the only honest facts left about a Task are the
// ones GitHub holds about its issue. Two values are derivable from those alone:
// the issue is open, or the issue is closed — and an agent closes an issue by
// merging a pull request that references it.
//
// The two strings are deliberately members of the retired ten-value set. The
// console consumes derivedStatus through an UNTYPED contract field (there is no
// enum), so removing values breaks nothing but inventing one would: a chip
// keyed on an unknown string renders as nothing. Anything richer than this
// belongs on the run's cycle timeline, which is where the loop's real position
// lives.
const (
	// DerivedStatusPending is an open issue: planned, not finished.
	DerivedStatusPending = "pending"
	// DerivedStatusMerged is a closed issue: its work landed.
	DerivedStatusMerged = "merged"
)

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

// TaskView is the list-item shape (§9.1): live GitHub facts fused with the
// latest Execution per kind into a derived status.
type TaskView struct {
	IssueNumber int    `json:"issueNumber"`
	Title       string `json:"title"`
	IssueURL    string `json:"issueUrl"`
	// A task carries no pull request of its own: agent work is claimed by a BUILD
	// SESSION's pull request, whose identity lives on the run's cycle record
	// (delivery.RunCycle) because that is what the merge policy decided about.
	ExecutorClass string `json:"executorClass"`
	// Kind is the issue's LABEL KIND, raw (labels.go): development, bug,
	// conflict, validation, provision, or empty when the issue carries none.
	//
	// It exists because ExecutorClass is coarser on purpose — planned work, a
	// bug and a merge conflict are all `coding` there, since all three are
	// dispatched the same way — and the read DTO carries no raw labels, so
	// without this the console cannot tell a defect from planned work. Optional:
	// a row whose kind this console has not learned renders untagged rather than
	// guessing.
	Kind          string                   `json:"kind,omitempty"`
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
	// Comments is the issue's newest comments, oldest first — the version's live
	// narrative, read straight from the host on every request.
	//
	// It is filled only on a MILESTONE-SCOPED read that asked for it. That is the
	// same asymmetry ledger issues already have on this list (see the task Reads
	// service): the comment fetch is anchored on one milestone, so a read
	// spanning versions has no bounded set to ask for. Omitted therefore means
	// "not asked for"; an empty slice would have meant "this issue has none", and
	// keeping the two apart is why this is omitempty rather than always present.
	Comments []IssueComment `json:"comments,omitempty"`
}

// IssueComment is one comment on a Task's issue, projected for the read surface.
//
// It is a delivery type rather than the host's own so the read DTOs stay a
// closed set this domain owns — the same reason ExecutionView exists beside the
// execution row. The projection is total: every field is a passthrough, and the
// platform stores none of it.
type IssueComment struct {
	// ID is the host's node id — stable across reads, and the consumer's list key.
	ID     string `json:"id"`
	Author string `json:"author"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	// CreatedAt is when the comment was posted. Edits are not tracked: the host
	// answers the current body under the original timestamp, which is what a
	// narrative wants — the moment the note entered the story.
	CreatedAt time.Time `json:"createdAt"`
}

// TaskDetail is the Get shape: a TaskView plus the full Execution history.
type TaskDetail struct {
	TaskView
	ExecutionHistory []ExecutionView `json:"executionHistory"`
}
