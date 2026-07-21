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

package contracts

// Progress DTOs are the cross-feature wire shapes for feature/execution's
// unified GET /projects/{p}/executions/{id}/progress endpoint. They live here
// (the dependency-free leaf) so any package — feature or client — can speak
// the shape without importing a feature package, keeping contracts a
// stdlib-only leaf.

// ProgressEvent is the unified shape returned to progress callers. Optional
// fields use omitempty so JSON payloads stay compact. schemaVersion=1 mirrors
// the TS source-of-truth at remote-worker/src/lib/progress/schema.ts.
type ProgressEvent struct {
	SchemaVersion int    `json:"schemaVersion"`
	Ts            string `json:"ts"`
	Seq           int64  `json:"seq"`
	Kind          string `json:"kind"`

	// Phase events.
	Phase string `json:"phase,omitempty"`

	// Tool-use events.
	Tool string `json:"tool,omitempty"`

	// git_commit / git_push.
	SHA    string `json:"sha,omitempty"`
	Branch string `json:"branch,omitempty"`
	Files  int    `json:"files,omitempty"`

	// gh_action.
	Command string `json:"command,omitempty"`

	// log + result.
	Level   string `json:"level,omitempty"`
	Status  string `json:"status,omitempty"`
	Summary string `json:"summary,omitempty"`
	Error   string `json:"error,omitempty"`

	// build_step (BFF-synthetic, emitted by progress_service from
	// WorkflowRun.Status.Tasks[] deltas).
	Step        string `json:"step,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
	Message     string `json:"message,omitempty"`

	// result: the run's token usage (#249), present once the runner captures
	// it from the SDK terminal message.
	Usage *TokenUsage `json:"usage,omitempty"`
}

// ProgressResponse is the envelope the progress reader returns per execution.
// Schema-versioned so the console can branch on future envelope changes
// without flag-flipping. It is an INTERNAL DTO — the task-log HTTP surface is
// the SSE stream (TimelineEvent frames); this envelope is how the per-kind
// line sources report their slice to the stream assembler.
type ProgressResponse struct {
	SchemaVersion int             `json:"schemaVersion"`
	Lines         []ProgressEvent `json:"lines"`
	CursorMillis  int64           `json:"cursorMillis"`
	Phase         string          `json:"phase,omitempty"`
	Truncated     bool            `json:"truncated,omitempty"`
	Final         bool            `json:"final"`
}

// TimelineEvent is one entry on the unified task-log stream: a ProgressEvent
// (its fields flatten into the JSON — the struct is embedded, not nested) plus
// attribution for WHICH execution attempt produced it. The console renders one
// row per TimelineEvent and groups rows by executionId/executionKind — no
// server-side per-execution filter, so history browsing is a client-side
// group-by over one feed.
type TimelineEvent struct {
	ProgressEvent
	ExecutionID   string `json:"executionId"`
	ExecutionKind string `json:"executionKind"`
}
