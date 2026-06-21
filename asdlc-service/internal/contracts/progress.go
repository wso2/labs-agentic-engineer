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

import "errors"

// Progress DTOs are the cross-feature wire shapes for the BFF's /progress/*
// endpoints. They live here (the dependency-free leaf) so codingagent can
// PRODUCE them and task's HTTP edge can CONSUME them via a task-local
// ProgressReader port — without task importing the codingagent feature
// concretely (which would re-open the task↔codingagent cycle, §4 / arch_test).
//
// ProgressEvent could not be hosted in models: models already re-exports the
// task-state algebra FROM contracts (models.TaskStatus = contracts.TaskStatus),
// so contracts→models is a cycle. As a pure scalar value type it belongs here
// directly, keeping contracts a stdlib-only leaf. clients/observer aliases this
// type (it owns the wire parser ParseProgressLine that produces it).

// ProgressEvent is the unified shape returned to /progress/agent and
// /progress/build callers. Optional fields use omitempty so JSON payloads
// stay compact. schemaVersion=1 mirrors the TS source-of-truth at
// remote-worker/src/lib/progress/schema.ts.
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
}

// ProgressResponse is the shape returned by both /progress/agent and
// /progress/build. Schema-versioned so the console can branch on future
// envelope changes without flag-flipping.
type ProgressResponse struct {
	SchemaVersion int             `json:"schemaVersion"`
	Lines         []ProgressEvent `json:"lines"`
	CursorMillis  int64           `json:"cursorMillis"`
	Phase         string          `json:"phase,omitempty"`
	Truncated     bool            `json:"truncated,omitempty"`
	Final         bool            `json:"final"`
}

// ErrProgressUnavailable signals that the Observer is not reachable. The
// progress service returns it; the HTTP edge maps it to 503. Hosted here so
// the task controller can errors.Is against it without importing codingagent.
var ErrProgressUnavailable = errors.New("progress unavailable")
