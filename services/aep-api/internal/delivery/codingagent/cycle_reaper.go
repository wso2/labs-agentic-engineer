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

package codingagent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// LatestCycleReader resolves a run's newest cycle. Satisfied by
// delivery.RunCycleRepository.
type LatestCycleReader interface {
	Latest(ctx context.Context, orgID, runID string) (*delivery.RunCycle, error)
}

// CycleReaper deletes a run's in-flight agent Component. It exists for cancel,
// and cancel alone: the console's "Cancel run" button has always stopped the
// RUN, and this is what makes it stop the POD.
//
// Deletion is immediate and unconditional on cancel — no retention window —
// because the user has said the work is abandoned, and because deleting the
// Component through the OC API is the only thing that frees the org's billing
// concurrency slot. A natural finish is NOT reaped here: those are retained
// until the next pre-create LRU pass (retention.go), which is what keeps recent
// cycles inspectable.
type CycleReaper struct {
	oc     openchoreo.ComponentClient
	cycles LatestCycleReader
}

// NewCycleReaper wires the reaper.
func NewCycleReaper(oc openchoreo.ComponentClient, cycles LatestCycleReader) *CycleReaper {
	return &CycleReaper{oc: oc, cycles: cycles}
}

// ReapRunCycle deletes the Component of the run's newest cycle.
//
// Idempotent and forgiving about absence: a run that never dispatched, a cycle
// with no Job, or a ref that is not one of our Components (a legacy
// WorkflowRun-shaped name) are all no-ops, and DeleteComponent treats 404 as
// success. Only a real OC failure is returned.
func (r *CycleReaper) ReapRunCycle(ctx context.Context, orgID, projectID, runID string) error {
	if r == nil || r.oc == nil || r.cycles == nil {
		return nil
	}
	cycle, err := r.cycles.Latest(ctx, orgID, runID)
	if err != nil {
		return fmt.Errorf("reap run cycle %s: read latest cycle: %w", runID, err)
	}
	if cycle == nil || cycle.JobRef == "" {
		return nil
	}
	// The `ca-` prefix is the ONE discriminator that says this ref names an
	// agent component rather than an OpenChoreo build WorkflowRun. Deleting a
	// component by a build's run name would be a different object entirely.
	if !isCodingAgentRun(cycle.JobRef) {
		return nil
	}
	if err := r.oc.DeleteComponent(ctx, orgID, projectID, cycle.JobRef); err != nil {
		return fmt.Errorf("reap run cycle %s: delete component %q: %w", runID, cycle.JobRef, err)
	}
	slog.InfoContext(ctx, "cancel: deleted the cycle's agent component",
		"org", orgID, "project", projectID, "run", runID, "cycle", cycle.ID, "component", cycle.JobRef)
	return nil
}
