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
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/run"
)

// raceWorld is the state a cancel and a re-dispatch already in flight share:
// one cycle row (with the repository's open-row fence), its run's cancel stamp
// and the cycle's Job binding. The real CycleReaper and the real
// run.Activities.NoteCycleDispatch both act on it, so these tests pin the
// cross-slice ordering, not either half alone.
type raceWorld struct {
	cycle           delivery.RunCycle
	cancelRequested bool
	suspended       bool
	// onResume runs inside the dispatch's un-suspend, BEFORE it writes: the
	// hook a test uses to land the cancel in the middle of the dispatch.
	onResume func()
}

// cancel is what Commands.Cancel does: the durable stamp on the run row,
// then the reap.
func (w *raceWorld) cancel(t *testing.T) {
	t.Helper()
	w.cancelRequested = true
	if err := NewCycleReaper(raceJobs{w}, raceCancelStore{w}, testWriteTargets()).
		ReapRunCycle(context.Background(), w.cycle.OrgID, w.cycle.ProjectID, w.cycle.RunID); err != nil {
		t.Fatalf("ReapRunCycle: %v", err)
	}
}

func (w *raceWorld) dispatch(t *testing.T) {
	t.Helper()
	acts := run.NewActivities(run.Deps{Runs: raceRuns{w: w}, Cycles: raceCycles{w: w}, Jobs: raceJobs{w}})
	if err := acts.NoteCycleDispatch(context.Background(), run.NoteCycleDispatchInput{CycleID: w.cycle.ID, JobRef: w.cycle.JobRef}); err != nil {
		t.Fatalf("NoteCycleDispatch: %v", err)
	}
}

// raceCancelStore is the reaper's view of the row.
type raceCancelStore struct{ w *raceWorld }

func (s raceCancelStore) Latest(context.Context, string, string) (*delivery.RunCycle, error) {
	row := s.w.cycle
	return &row, nil
}

func (s raceCancelStore) MarkJobSuspended(context.Context, string) (bool, error) {
	if s.w.cycle.JobSuspendedAt != nil {
		return false, nil
	}
	now := time.Now()
	s.w.cycle.JobSuspendedAt = &now
	return true, nil
}

func (s raceCancelStore) FinishCancelled(context.Context, string) (*delivery.RunCycle, error) {
	if s.w.cycle.EndedAt != nil {
		return nil, nil
	}
	now := time.Now()
	s.w.cycle.EndedAt, s.w.cycle.AgentReason = &now, delivery.CycleReasonCancelled
	row := s.w.cycle
	return &row, nil
}

// raceCycles is the supervisor's view of the row. Only the methods the
// dispatch activity calls are implemented; the embedded nil port panics on
// anything else.
type raceCycles struct {
	run.CycleStore
	w *raceWorld
}

func (s raceCycles) NoteDispatch(_ context.Context, _, jobRef string) (*delivery.RunCycle, error) {
	if s.w.cycle.EndedAt != nil {
		return nil, nil // the repository's fence: a closed row is never moved
	}
	now := time.Now()
	s.w.cycle.Attempts++
	s.w.cycle.JobRef, s.w.cycle.DispatchedAt, s.w.cycle.JobSuspendedAt = jobRef, &now, nil
	row := s.w.cycle
	return &row, nil
}

type raceRuns struct {
	run.RunStore
	w *raceWorld
}

func (s raceRuns) CancelRequested(context.Context, string, string) (bool, error) {
	return s.w.cancelRequested, nil
}

// raceJobs is the cycle's one Job binding, as both slices reach it.
type raceJobs struct{ w *raceWorld }

func (j raceJobs) SuspendJobBinding(context.Context, string, string, string, string) error {
	j.w.suspended = true
	return nil
}

func (j raceJobs) ResumeJobBinding(context.Context, string, string, string, string) error {
	if j.w.onResume != nil {
		hook := j.w.onResume
		j.w.onResume = nil
		hook()
	}
	j.w.suspended = false
	return nil
}

func raceCycle() delivery.RunCycle {
	return delivery.RunCycle{
		ID: "c1", OrgID: "acme", ProjectID: "shop", RunID: "run-1",
		Kind: delivery.CycleKindCoding, JobRef: "ca-c1-x", Environment: "development", Attempts: 1,
	}
}

// Cancel, then the dispatch that was already in flight reaches its fenced
// write: the closed row is not moved, so nothing resumes the binding.
func TestCancelThenALateDispatch_BindingStaysSuspended(t *testing.T) {
	w := &raceWorld{cycle: raceCycle()}

	w.cancel(t)
	w.dispatch(t)

	if !w.suspended {
		t.Fatal("a late dispatch un-suspended a cancelled cycle's Job")
	}
	if w.cycle.AgentReason != delivery.CycleReasonCancelled || w.cycle.JobSuspendedAt == nil || w.cycle.Attempts != 1 {
		t.Fatalf("cycle = %+v, want closed cancelled, marked, no new attempt", w.cycle)
	}
}

// The dispatch noted the row while it was still open, and the cancel closed
// and suspended it before the dispatch's un-suspend landed. The un-suspend
// alone would leave a cancelled cycle's Job live; the dispatch re-reads the
// cancel stamp after it and suspends again.
func TestCancelBetweenTheFencedWriteAndTheResume_BindingStaysSuspended(t *testing.T) {
	w := &raceWorld{cycle: raceCycle(), suspended: true}
	w.onResume = func() { w.cancel(t) }

	w.dispatch(t)

	if !w.suspended {
		t.Fatal("the dispatch's un-suspend outlived the cancel's suspend")
	}
	if w.cycle.AgentReason != delivery.CycleReasonCancelled || w.cycle.JobSuspendedAt == nil {
		t.Fatalf("cycle = %+v, want closed cancelled and marked", w.cycle)
	}
}

// Without a cancel the re-dispatch resumes as before.
func TestReDispatchWithoutACancel_ResumesTheBinding(t *testing.T) {
	w := &raceWorld{cycle: raceCycle(), suspended: true}

	w.dispatch(t)

	if w.suspended {
		t.Fatal("an un-cancelled re-dispatch left its Job suspended")
	}
}
