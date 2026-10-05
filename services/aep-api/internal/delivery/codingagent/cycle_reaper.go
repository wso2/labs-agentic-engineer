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
	"errors"
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// cancelStore is the cycle-record surface a cancel writes. Satisfied by
// delivery.RunCycleRepository.
type cancelStore interface {
	Latest(ctx context.Context, orgID, runID string) (*delivery.RunCycle, error)
	MarkJobSuspended(ctx context.Context, id string) error
	FinishCancelled(ctx context.Context, id string) (*delivery.RunCycle, error)
}

// CycleReaper stops a cancelled run's in-flight agent. It exists for cancel,
// and cancel alone: the console's "Cancel run" button has always stopped the
// RUN, and this is what makes it stop the POD.
//
// It closes the cycle as cancelled, then suspends the cycle's Job binding. A
// suspended Job's pod is terminated (the runner gets its 30 s SIGTERM grace),
// and the Job OpenChoreo re-creates after its TTL is born suspended, so the
// runner never runs again for this cycle. It deletes nothing: the Component,
// and with it the org's billing concurrency slot, goes at settle (Task 5.9's
// settler), at most CODING_AGENT_SETTLE_GRACE plus one sweep tick after that
// SIGTERM grace.
//
// The close comes first because it is the fence a re-dispatch already in
// flight reads: NoteDispatch moves only an open row, and the supervisor resumes
// a binding only after that write moved one. Suspending first would leave a
// window in which the dispatch notes the still-open row and resumes what the
// cancel just suspended. A dispatch whose write landed before the close is the
// supervisor's to settle: it re-reads the run's cancel stamp after its resume
// and suspends again (run.Activities.NoteCycleDispatch).
type CycleReaper struct {
	jobs    jobSuspender
	cycles  cancelStore
	targets writeTargetResolver
}

// NewCycleReaper wires the reaper. targets resolves the environment of a cycle
// that recorded none.
func NewCycleReaper(jobs jobSuspender, cycles cancelStore, targets writeTargetResolver) *CycleReaper {
	return &CycleReaper{jobs: jobs, cycles: cycles, targets: targets}
}

// ReapRunCycle closes the run's newest cycle as cancelled and suspends its Job
// binding.
//
// Idempotent and forgiving about absence: a run that never dispatched, a cycle
// with no Job, or a ref that is not one of our Components (a build
// WorkflowRun-shaped name) are all no-ops. A cycle another path already closed
// is still suspended: the user asked for the pod to stop. Outcomes of the
// suspend:
//   - success: marked (job_suspended_at), then codingagent.job_suspended is
//     logged with cause=cancel — the one event that says it took effect;
//   - ErrNotFound: the binding is gone, nothing left to suspend; marked, not
//     announced;
//   - ErrSuspendUnsupported: a legacy release with no suspend schema; NOT
//     marked, which is how the settler knows suspend did not apply;
//   - anything else: returned, not marked; the settler's backstop suspends the
//     closed cycle later.
func (r *CycleReaper) ReapRunCycle(ctx context.Context, orgID, projectID, runID string) error {
	if r == nil || r.jobs == nil || r.cycles == nil {
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
	// agent component rather than an OpenChoreo build WorkflowRun.
	if !isCodingAgentRun(cycle.JobRef) {
		return nil
	}
	if _, err := r.cycles.FinishCancelled(ctx, cycle.ID); err != nil {
		return fmt.Errorf("reap run cycle %s: close cycle %s as cancelled: %w", runID, cycle.ID, err)
	}
	env, err := cycleEnvironment(ctx, r.targets, cycle)
	if err != nil {
		return fmt.Errorf("reap run cycle %s: resolve environment: %w", runID, err)
	}
	err = r.jobs.SuspendJobBinding(ctx, orgID, projectID, cycle.JobRef, env)
	switch {
	case err == nil:
		if err := r.cycles.MarkJobSuspended(ctx, cycle.ID); err != nil {
			return fmt.Errorf("reap run cycle %s: mark job suspended: %w", runID, err)
		}
		slog.InfoContext(ctx, "codingagent.job_suspended", "cycle", cycle.ID, "component", cycle.JobRef, "cause", "cancel")
	case errors.Is(err, openchoreo.ErrNotFound):
		if err := r.cycles.MarkJobSuspended(ctx, cycle.ID); err != nil {
			return fmt.Errorf("reap run cycle %s: mark job suspended: %w", runID, err)
		}
	case errors.Is(err, openchoreo.ErrSuspendUnsupported):
		slog.WarnContext(ctx, "codingagent.job_suspend_unsupported", "cycle", cycle.ID, "component", cycle.JobRef)
	default:
		return fmt.Errorf("reap run cycle %s: suspend job %q: %w", runID, cycle.JobRef, err)
	}
	return nil
}
