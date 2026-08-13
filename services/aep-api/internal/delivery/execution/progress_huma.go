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

package execution

import (
	"context"
	"errors"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// progressSchemaVersion mirrors the runner/observer progress schema version.
const progressSchemaVersion = 1

// ExecutionLookup resolves an execution id within the acting org (the S2S
// fence). Shared by the execution-keyed internal endpoints.
type ExecutionLookup interface {
	GetByIDScoped(ctx context.Context, orgID, id string) (*delivery.Execution, error)
}

// ErrExecutionNotFound is returned when an execution id does not resolve within
// the acting org (mapped to 404).
var ErrExecutionNotFound = errors.New("execution not found")

// OCProgress reads an OpenChoreo WorkflowRun's status (build steps). Wired from
// openchoreo.ComponentClient.
type OCProgress interface {
	GetWorkflowRun(ctx context.Context, orgName, runName string) (*gen.WorkflowRun, error)
}

// CodingProgress serves a coding execution's live activity — the ca-… pod-log
// tail while the agent runs, then the observer archive (or a legacy
// coding_agent_logs snapshot for pre-milestone execution rows) — as progress
// events. Wired from codingagent at the composition root; nil leaves the coding
// branch reporting terminal-ness only.
type CodingProgress interface {
	AgentProgress(ctx context.Context, row *delivery.Execution, sinceMillis int64) (*contracts.ProgressResponse, error)
}

// ProgressService derives one execution's progress lines keyed by execution id:
// the kind selects the source — build reads the WorkflowRun's step deltas;
// coding surfaces the runner's activity via the CodingProgress source (live
// OC pod-log tail, then observer archive / legacy snapshot). A nil
// codingProgress degrades the coding branch to terminal-ness only. It is the
// per-execution line source the task-log SSE stream (task_stream.go) walks
// across every attempt — no longer an HTTP handler of its own (the cursor-poll
// endpoint it once backed is retired).
type ProgressService struct {
	execs          ExecutionLookup
	oc             OCProgress
	codingProgress CodingProgress
}

// NewProgressService wires the progress reader.
func NewProgressService(execs ExecutionLookup, oc OCProgress) *ProgressService {
	return &ProgressService{execs: execs, oc: oc}
}

// WithCodingProgress attaches the coding-activity source (live OC / archive /
// legacy coding_agent_logs reader). nil-safe: an unset source leaves coding
// executions reporting terminal-ness only. Returns the receiver for chaining.
func (s *ProgressService) WithCodingProgress(cp CodingProgress) *ProgressService {
	s.codingProgress = cp
	return s
}

// GetProgress returns the progress for one execution, fenced to orgHandle.
func (s *ProgressService) GetProgress(ctx context.Context, orgHandle, executionID string, sinceMillis int64) (*contracts.ProgressResponse, error) {
	row, err := s.execs.GetByIDScoped(ctx, orgHandle, executionID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrExecutionNotFound
	}
	final := taskmeta.ExecutionStatus(row.Status).IsTerminal()
	resp := &contracts.ProgressResponse{
		SchemaVersion: progressSchemaVersion,
		Lines:         []contracts.ProgressEvent{},
		CursorMillis:  sinceMillis,
		Final:         final,
	}
	switch {
	case row.Kind == string(taskmeta.KindBuild) && row.RunName != "" && s.oc != nil:
		run, gerr := s.oc.GetWorkflowRun(ctx, row.OrgID, row.RunName)
		if gerr == nil && run != nil {
			resp.Lines = buildStepEvents(run)
			resp.Phase = run.Status
		}
	case row.Kind == string(taskmeta.KindCoding) && s.codingProgress != nil:
		// Live pod-log tail while running, captured snapshot once terminal. On
		// any source failure, degrade to the terminal-only resp above — the
		// execution status stays accurate and the console shows "live progress
		// unavailable" rather than erroring the whole page.
		if cp, cerr := s.codingProgress.AgentProgress(ctx, row, sinceMillis); cerr == nil && cp != nil {
			resp = cp
		}
	}
	return resp, nil
}

// buildStepEvents synthesizes build_step progress events from a WorkflowRun's
// per-task status (the same shape the legacy build-progress endpoint produced).
func buildStepEvents(run *gen.WorkflowRun) []contracts.ProgressEvent {
	out := make([]contracts.ProgressEvent, 0, len(run.Tasks))
	for i, t := range run.Tasks {
		out = append(out, contracts.ProgressEvent{
			SchemaVersion: progressSchemaVersion,
			Seq:           int64(i),
			Kind:          "build_step",
			Step:          t.Name,
			Status:        t.Phase,
			StartedAt:     t.StartedAt,
			CompletedAt:   t.CompletedAt,
			Message:       t.Message,
			Ts:            t.CompletedAt,
		})
	}
	return out
}
