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

package build

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// This file is the strict-server (contract-first) entry surface of the build
// service: exported, org-explicit methods the internal/api handlers call
// (handlers_build.go). Run IS the build sequence (build_service.go), shared
// with the non-HTTP StartProjectBuild trigger; its edge-mapped failures are
// *EdgeError values the api-layer mapper copies onto the flat envelope.

// ErrBuildNotFound reports an unknown build tag under the caller's org. The
// workflow_runs row is the org fence, so a cross-org read lands here too.
var ErrBuildNotFound = errors.New("build not found")

// Status maps the dev workflow's live status (or its workflow_runs row when
// the live query is unavailable) onto the contract's BuildStatus — the strict
// port of the Huma get handler. Unknown tag (or a cross-org read: the
// workflow_runs row is the org fence) returns ErrBuildNotFound.
func (s *Service) Status(ctx context.Context, orgID, projectID, tag string) (BuildStatus, error) {
	workflowID := delivery.DevWorkflowID(orgID, projectID, tag)
	row, err := s.store.GetByWorkflowID(ctx, orgID, workflowID)
	if err != nil {
		return BuildStatus{}, fmt.Errorf("lookup build: %w", err)
	}
	if row == nil {
		return BuildStatus{}, ErrBuildNotFound
	}

	st, qerr := s.runner.BuildStatus(ctx, workflowID)
	if qerr != nil {
		// Live query unavailable (Temporal down, run archived) — degrade the
		// overall status to the indexed terminal row status, but STILL list the
		// build's tasks from the durable lineage read (an archived run no longer
		// answers a query, yet its planned issues are permanent).
		return BuildStatus{
			Status:         statusFromRow(row.Status),
			WorkflowStatus: row.Status,
			Reason:         row.Reason,
			Tasks:          s.taskStatuses(ctx, orgID, projectID, tag, nil),
		}, nil
	}
	return BuildStatus{
		Status:         statusFromPhase(st.Phase),
		WorkflowStatus: st.Phase,
		Reason:         row.Reason,
		Tasks:          s.taskStatuses(ctx, orgID, projectID, tag, st.Tasks),
	}, nil
}

// List enumerates the project's builds from the workflow_runs index alone (no
// live Temporal queries — the list must stay one cheap read). Rows arrive
// newest-first; a same-tag rebuild writes a second (workflowID, runID) row, so
// the first row seen per tag — its newest run — represents that build. The
// strict port of the Huma list handler; Builds is always non-nil so the JSON
// body is [] rather than null.
func (s *Service) List(ctx context.Context, orgID, projectID string) (BuildList, error) {
	rows, err := s.store.ListByProject(ctx, orgID, projectID, delivery.WorkflowKindDev)
	if err != nil {
		return BuildList{}, fmt.Errorf("list builds: %w", err)
	}
	usageByTag := s.usageByTag(ctx, orgID, projectID)
	seen := make(map[string]bool, len(rows))
	builds := make([]BuildSummary, 0, len(rows))
	for _, row := range rows {
		if row.Tag == "" || seen[row.Tag] {
			continue
		}
		seen[row.Tag] = true
		b := BuildSummary{
			Tag:       row.Tag,
			Status:    statusFromRow(row.Status),
			Reason:    row.Reason,
			StartedAt: row.CreatedAt,
			Tasks: BuildTally{
				Total:  int64(row.TasksTotal),
				Done:   int64(row.TasksDone),
				Failed: int64(row.TasksFailed),
			},
		}
		// Active is computed, clamped so a lost total write can never render
		// negative (same rule as the overview's build stage).
		if active := row.TasksTotal - row.TasksDone - row.TasksFailed; active > 0 {
			b.Tasks.Active = int64(active)
		}
		if row.Status != delivery.WorkflowStatusRunning {
			completed := row.UpdatedAt
			b.CompletedAt = &completed
		}
		// The tag lineage's captured spend (#245): accrues on this same read
		// while the build runs; absent when nothing was captured.
		if u, ok := usageByTag[row.Tag]; ok && !u.IsZero() {
			b.Usage = delivery.NewUsageView(u, s.pricer.Cost(u))
		}
		builds = append(builds, b)
	}
	return BuildList{Builds: builds}, nil
}

// usageByTag fetches the project's per-spec-tag usage rollup; unwired deps or
// a load failure degrade to no usage figures (never a failed list).
func (s *Service) usageByTag(ctx context.Context, orgID, projectID string) map[string]contracts.TokenUsage {
	if s.usage == nil || s.pricer == nil {
		return nil
	}
	usage, err := s.usage.SumUsageBySpecTag(ctx, orgID, projectID)
	if err != nil {
		slog.WarnContext(ctx, "build: load usage rollup failed", "project", projectID, "error", err)
		return nil
	}
	return usage
}
