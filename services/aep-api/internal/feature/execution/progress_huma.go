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
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/platform/humakit"
	"github.com/wso2/aep/aep-api/models"
)

// progressSchemaVersion mirrors the runner/observer progress schema version.
const progressSchemaVersion = 1

// ExecutionLookup resolves an execution id within the acting org (the S2S
// fence). Shared by the execution-keyed internal endpoints.
type ExecutionLookup interface {
	GetByIDScoped(ctx context.Context, orgID, id string) (*models.Execution, error)
}

// ErrExecutionNotFound is returned when an execution id does not resolve within
// the acting org (mapped to 404).
var ErrExecutionNotFound = errors.New("execution not found")

// OCProgress reads an OpenChoreo WorkflowRun's status (build steps). Wired from
// openchoreo.ComponentClient.
type OCProgress interface {
	GetWorkflowRun(ctx context.Context, orgName, runName string) (*models.WorkflowRun, error)
}

// CodingProgress serves a coding execution's live activity — the ca-… pod-log
// tail while the Job runs, or the captured coding_agent_logs snapshot once
// terminal — as progress events. Wired from codingagent at the composition root
// (it owns the cluster-gateway-proxy + DB edge); nil leaves the coding branch
// reporting terminal-ness only.
type CodingProgress interface {
	AgentProgress(ctx context.Context, row *models.Execution, sinceMillis int64) (*contracts.ProgressResponse, error)
}

// ProgressService serves the unified progress endpoint keyed by execution id
// (§9.1): the kind selects the source — build reads the WorkflowRun's step
// deltas; coding surfaces the runner's activity via the CodingProgress source
// (live pod-log tail while running, captured coding_agent_logs snapshot once
// terminal). A nil codingProgress degrades the coding branch to terminal-ness
// only.
type ProgressService struct {
	execs          ExecutionLookup
	oc             OCProgress
	codingProgress CodingProgress
}

// NewProgressService wires the progress reader.
func NewProgressService(execs ExecutionLookup, oc OCProgress) *ProgressService {
	return &ProgressService{execs: execs, oc: oc}
}

// WithCodingProgress attaches the coding-activity source (the ca-… pod-log tail
// + coding_agent_logs snapshot reader). nil-safe: an unset source leaves coding
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
func buildStepEvents(run *models.WorkflowRun) []contracts.ProgressEvent {
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

type progressInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	ExecutionID string `path:"executionId" doc:"Execution id"`
	SinceMillis string `query:"sinceMillis" doc:"Cursor: only lines after this epoch-millis (0 ⇒ initial load)"`
}

type progressOutput struct{ Body *contracts.ProgressResponse }

// RegisterProgress registers the unified progress endpoint on the public Huma API.
func RegisterProgress(api huma.API, svc *ProgressService) {
	huma.Register(api, huma.Operation{
		OperationID: "get-execution-progress",
		Method:      http.MethodGet,
		Path:        "/projects/{projectName}/executions/{executionId}/progress",
		Summary:     "Poll an Execution's progress (build steps / coding status)",
		Tags:        []string{"Tasks"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *progressInput) (*progressOutput, error) {
		if svc == nil {
			return nil, huma.Error503ServiceUnavailable("progress not configured")
		}
		sinceMillis, _ := strconv.ParseInt(in.SinceMillis, 10, 64)
		resp, err := svc.GetProgress(ctx, in.OrgHandle, in.ExecutionID, sinceMillis)
		if err != nil {
			if errors.Is(err, ErrExecutionNotFound) {
				return nil, huma.Error404NotFound("execution not found")
			}
			return nil, huma.Error500InternalServerError("progress failed")
		}
		return &progressOutput{Body: resp}, nil
	})
}
