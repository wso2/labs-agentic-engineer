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

	// Emitter attributes the line to the main agent or to one of the subagents
	// it fans out to with the Task tool ("main" | "subagent"). The runner stamps
	// it only on subagent lines (from the SDK's parent_tool_use_id), so an empty
	// value means main — readers should default rather than treat it as unknown.
	Emitter string `json:"emitter,omitempty"`

	// EmitterID / EmitterLabel say WHICH subagent, for the cycles that fan out
	// to several at once and whose lines therefore interleave. The id is the
	// fan-out tool call (stable for that subagent's life) and the label is the
	// description the main agent gave it. Both empty on main-agent lines.
	EmitterID    string `json:"emitterId,omitempty"`
	EmitterLabel string `json:"emitterLabel,omitempty"`

	// Phase events.
	Phase string `json:"phase,omitempty"`

	// Tool-use events.
	Tool string `json:"tool,omitempty"`

	// tool_use / tool_result: the tool call this line is about. A tool_result
	// carries the id of the tool_use it answers, which is what pairs a call
	// with its outcome once several subagents interleave the feed.
	ToolUseID string `json:"toolUseId,omitempty"`

	// tool_result. OK is a POINTER because `false` is the interesting value —
	// a failed call with `omitempty` on a plain bool would vanish from the wire
	// and read as a success, which is the exact defect this event exists to
	// fix. Nil means "not a tool_result". DurationMs is measured runner-side
	// between the call and its outcome, so it includes model turnaround.
	OK         *bool `json:"ok,omitempty"`
	DurationMs int64 `json:"durationMs,omitempty"`

	// ExitCode is the process status of a failed shell call, as the runner
	// parsed it from the SDK's own `Exit code N` line. A POINTER for the same
	// reason as OK: it is the honest per-step failure signal, and a reader must
	// be able to tell "the SDK reported no code" (nil, for the non-shell tools
	// that report `<tool_use_error>`) from any particular value.
	ExitCode *int `json:"exitCode,omitempty"`

	// The authoritative totals for one fanned-out subagent, present only on its
	// fan-out call's result: the SDK's own duration (in DurationMs above), tool
	// count, and the lines its edits added and removed. They are what a settled
	// subagent section reports and cannot be derived from this feed — a
	// subagent's per-edit line counts never appear in its own events.
	ToolCount    int `json:"toolCount,omitempty"`
	LinesAdded   int `json:"linesAdded,omitempty"`
	LinesRemoved int `json:"linesRemoved,omitempty"`

	// git_commit / git_push.
	SHA    string `json:"sha,omitempty"`
	Branch string `json:"branch,omitempty"`
	Files  int    `json:"files,omitempty"`

	// gh_action.
	Command string `json:"command,omitempty"`

	// progress_item: WHICH named unit of work the line is about — the key a
	// consumer folds on, since many lines describe the same item and a reader
	// wants one row repainted rather than many rows printed. Validation binds
	// items to acceptance criteria ("AC-003-a"); CycleKind on the run line
	// already says that, so this field never repeats it.
	//
	// The status itself rides on Status below rather than a field of its own:
	// Kind scopes what it means, and progress_item's vocabulary (planned |
	// exploring | authoring | running | healing | pass | fail) shares nothing
	// with tool_result's SDK verdict word.
	ItemID string `json:"itemId,omitempty"`

	// log + result + progress_item.
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
	// it from the SDK terminal message. CapturedUsage carries the aggregate
	// plus the per-model split the capture path prices (#291).
	Usage *CapturedUsage `json:"usage,omitempty"`
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
