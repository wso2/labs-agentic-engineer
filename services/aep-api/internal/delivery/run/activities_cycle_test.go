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

package run

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// AppendCycle's PROJECTION — what the activity actually writes onto the row.
//
// The workflow tests next door mock this activity out, so they pin what the LOOP
// passes and nothing about what the activity does with it: drop
// `ValidationIssue: in.ValidationIssue` from the struct literal and every one of
// them still passes, while the console loses the number it needs to reach a
// running validation. That gap is the whole reason this file exists.

// stubCycles captures the row AppendCycle builds. Only Append is exercised; the
// rest of CycleStore is present to satisfy the port and must never be called
// here — a test that reached them would be testing the loop, not the projection
// — except NoteLaunch, which the dispatch activity's own tests read back.
type stubCycles struct {
	appended []delivery.RunCycle
	hosts    map[string]string // cycle id → model host, from NoteLaunch
	envs     map[string]string // cycle id → environment, from NoteLaunch
	hostErr  error

	// dispatched is the row NoteDispatch returns (nil = the cycle was closed,
	// so the fenced write changed nothing).
	dispatched     *delivery.RunCycle
	dispatchErr    error
	noteDispatches int
	order          *[]string
}

func (s *stubCycles) Append(_ context.Context, cycle *delivery.RunCycle) (string, error) {
	s.appended = append(s.appended, *cycle)
	return "cycle-1", nil
}

func (s *stubCycles) NoteDispatch(_ context.Context, cycleID, jobRef string) (*delivery.RunCycle, error) {
	s.noteDispatches++
	if s.order != nil {
		*s.order = append(*s.order, "note-dispatch")
	}
	if s.dispatched == nil {
		return nil, s.dispatchErr
	}
	row := *s.dispatched
	row.ID, row.JobRef = cycleID, jobRef
	return &row, s.dispatchErr
}

func (s *stubCycles) NoteLaunch(_ context.Context, cycleID, host, environment, _ string) error {
	if s.hostErr != nil {
		return s.hostErr
	}
	if s.hosts == nil {
		s.hosts, s.envs = map[string]string{}, map[string]string{}
	}
	s.hosts[cycleID] = host
	s.envs[cycleID] = environment
	return nil
}

func (s *stubCycles) Finish(context.Context, string, string) error { return nil }
func (s *stubCycles) SetValidationVerdict(context.Context, string, string, int, string) error {
	return nil
}

func (s *stubCycles) LatestValidationDigest(context.Context, string, []string) (string, error) {
	return "", nil
}

func (s *stubCycles) Latest(context.Context, string, string) (*delivery.RunCycle, error) {
	return nil, nil
}

// A validation cycle's row carries the issue it was dispatched at, from the open.
//
// This is what the console reads while a run is still going — the issue link, and
// the agent's status line on that issue's newest comment. The run's own copy is
// not written until the verdict, so if this projection is dropped the number
// exists nowhere until the run has ended, which is after the only window those
// two render in.
func TestAppendCycle_ValidationCycleCarriesItsIssue(t *testing.T) {
	cycles := &stubCycles{}
	acts := NewActivities(Deps{Cycles: cycles})

	id, err := acts.AppendCycle(context.Background(), AppendCycleInput{
		RunID: "run-1", OrgID: "acme", ProjectID: "shop",
		Kind: delivery.CycleKindValidation, ValidationIssue: 77,
	})
	require.NoError(t, err)
	require.Equal(t, "cycle-1", id)

	require.Len(t, cycles.appended, 1)
	got := cycles.appended[0]
	require.Equal(t, delivery.CycleKindValidation, got.Kind)
	require.Equal(t, 77, got.ValidationIssue,
		"the row must carry the issue at OPEN — the run's copy does not exist until the verdict")
	require.Equal(t, "run-1", got.RunID)
	require.Equal(t, "acme", got.OrgID)
	require.Equal(t, "shop", got.ProjectID)
}

// A cycle over a whole WORKING SET carries no issue, whatever it was passed.
//
// Guarded here as well as at the call site (noAnchorIssue) because the two
// protect different things: the call site decides what a coding cycle is
// dispatched with, and this pins that the column stays a validation fact even if
// something upstream ever hands one a number.
func TestAppendCycle_CodingCycleCarriesNoIssue(t *testing.T) {
	cycles := &stubCycles{}
	acts := NewActivities(Deps{Cycles: cycles})

	_, err := acts.AppendCycle(context.Background(), AppendCycleInput{
		RunID: "run-1", OrgID: "acme", ProjectID: "shop",
		Kind: delivery.CycleKindCoding,
	})
	require.NoError(t, err)

	require.Len(t, cycles.appended, 1)
	require.Zero(t, cycles.appended[0].ValidationIssue,
		"a coding cycle is anchored to no issue — it re-lists the milestone and picks its own")
}

// An unwired store is an ERROR, not a silent success. AppendCycle opens the
// record every later write keys on, so a supervisor that could not open one must
// not proceed as though it had.
func TestAppendCycle_UnwiredStoreFails(t *testing.T) {
	_, err := NewActivities(Deps{}).AppendCycle(context.Background(), AppendCycleInput{
		RunID: "run-1", Kind: delivery.CycleKindValidation, ValidationIssue: 77,
	})
	require.ErrorIs(t, err, errNotConfigured)
}

// launchingDispatcher stands in for the coding agent: it reports a launch on
// host into env, or fails with err.
type launchingDispatcher struct {
	host string
	env  string
	err  error
}

func (d launchingDispatcher) Dispatch(context.Context, delivery.MilestoneDispatch) (delivery.AgentLaunch, error) {
	if d.err != nil {
		return delivery.AgentLaunch{}, d.err
	}
	return delivery.AgentLaunch{JobRef: "ca-job-1", ModelHost: d.host, Environment: d.env}, nil
}

// The dispatch activity copies the launch's model host onto the cycle — the
// host its usage is later priced on — and still returns the bare Job reference,
// the value workflow history has always recorded for it.
func TestDispatchAgent_RecordsTheLaunchHostOnTheCycle(t *testing.T) {
	cycles := &stubCycles{}
	acts := NewActivities(Deps{Cycles: cycles, Dispatcher: launchingDispatcher{host: "api.anthropic.com"}})

	jobRef, err := acts.DispatchAgent(context.Background(), delivery.MilestoneDispatch{
		OrgID: "acme", ProjectID: "shop", Kind: delivery.CycleKindCoding, CycleID: "cycle-1",
	})
	require.NoError(t, err)
	require.Equal(t, "ca-job-1", jobRef)
	require.Equal(t, "api.anthropic.com", cycles.hosts["cycle-1"])
}

// The environment the Job was bound into is copied onto the cycle beside the
// host, so its readers never re-resolve a write target that may have moved.
// The activity's result is still the bare Job reference.
func TestDispatchAgent_RecordsTheLaunchEnvironmentOnTheCycle(t *testing.T) {
	cycles := &stubCycles{}
	acts := NewActivities(Deps{Cycles: cycles, Dispatcher: launchingDispatcher{host: "api.anthropic.com", env: "dev-b"}})

	jobRef, err := acts.DispatchAgent(context.Background(), delivery.MilestoneDispatch{
		OrgID: "acme", ProjectID: "shop", Kind: delivery.CycleKindCoding, CycleID: "cycle-1",
	})
	require.NoError(t, err)
	require.Equal(t, "ca-job-1", jobRef)
	require.Equal(t, "dev-b", cycles.envs["cycle-1"])
}

// A project whose pipeline names no write target cannot be dispatched into.
// That is a configuration fact, not agent death: the activity stamps its own
// non-retryable type, so the workflow settles the run instead of spending the
// re-dispatch budget, and the run's failure record carries the cause.
func TestDispatchAgent_NoWriteTargetIsTypedAndRecorded(t *testing.T) {
	cycles := &stubCycles{}
	runs := &failureRuns{}
	cause := fmt.Errorf("%w: no write target for acme/shop", delivery.ErrNoWriteTarget)
	acts := NewActivities(Deps{Cycles: cycles, Runs: runs, Dispatcher: launchingDispatcher{err: cause}})

	_, err := acts.DispatchAgent(context.Background(), delivery.MilestoneDispatch{
		OrgID: "acme", ProjectID: "shop", RunID: "run-1", CycleID: "cycle-1",
	})

	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr), "want an ApplicationError, got %v", err)
	require.True(t, appErr.NonRetryable())
	require.Equal(t, delivery.ErrTypeNoWriteTarget, appErr.Type())
	require.Empty(t, cycles.hosts, "nothing launched, so nothing is recorded on the cycle")
	require.Len(t, runs.recorded, 1)
	require.Equal(t, delivery.RunFailureCodeNoWriteTarget, runs.recorded[0].Code)
	require.Equal(t, delivery.RunPhaseCoding, runs.recorded[0].Phase)
	require.Contains(t, runs.recorded[0].Detail, "no write target for acme/shop")
}

// A launch that failed records nothing: there is no Job, so no host ran it.
func TestDispatchAgent_FailedLaunchRecordsNoHost(t *testing.T) {
	cycles := &stubCycles{}
	acts := NewActivities(Deps{Cycles: cycles, Dispatcher: launchingDispatcher{err: errors.New("boom")}})

	_, err := acts.DispatchAgent(context.Background(), delivery.MilestoneDispatch{CycleID: "cycle-1"})
	require.Error(t, err)
	require.Empty(t, cycles.hosts)
}

// A host write that fails after the Job launched does NOT fail the activity:
// retries are off for dispatch and a failure reads as agent death, so returning
// it would launch a second agent beside the running one. The cycle is left
// unpriced instead.
func TestDispatchAgent_HostWriteFailureDoesNotFailTheLaunch(t *testing.T) {
	cycles := &stubCycles{hostErr: errors.New("db down")}
	acts := NewActivities(Deps{Cycles: cycles, Dispatcher: launchingDispatcher{host: "api.anthropic.com"}})

	jobRef, err := acts.DispatchAgent(context.Background(), delivery.MilestoneDispatch{CycleID: "cycle-1"})
	require.NoError(t, err)
	require.Equal(t, "ca-job-1", jobRef)
}

// stubResumer records each un-suspend of a re-dispatched cycle's Job binding.
type stubResumer struct {
	calls []string // org/project/component@environment
	err   error
	order *[]string
}

func (r *stubResumer) ResumeJobBinding(_ context.Context, org, project, component, environment string) error {
	r.calls = append(r.calls, org+"/"+project+"/"+component+"@"+environment)
	if r.order != nil {
		*r.order = append(*r.order, "resume")
	}
	return r.err
}

// A re-dispatch reuses the cycle's Component, whose binding may still carry the
// suspend the watcher set at the previous attempt's terminal pod. The un-suspend
// runs only AFTER the fenced NoteDispatch moved an OPEN row, in the environment
// the launch recorded.
func TestNoteCycleDispatch_ResumesTheOpenCyclesJobAfterTheFencedWrite(t *testing.T) {
	var order []string
	cycles := &stubCycles{order: &order, dispatched: &delivery.RunCycle{
		OrgID: "acme", ProjectID: "shop", Environment: "development",
	}}
	jobs := &stubResumer{order: &order}
	acts := NewActivities(Deps{Cycles: cycles, Jobs: jobs})

	require.NoError(t, acts.NoteCycleDispatch(context.Background(), NoteCycleDispatchInput{CycleID: "c1", JobRef: "ca-c1"}))
	require.Equal(t, []string{"acme/shop/ca-c1@development"}, jobs.calls)
	require.Equal(t, []string{"note-dispatch", "resume"}, order)
}

// A cycle closed (or cancelled) between the launch and the fenced write: the
// write changes nothing, and nothing is un-suspended, so a closed cycle's Job
// stays inert.
func TestNoteCycleDispatch_ClosedCycleIsNeverResumed(t *testing.T) {
	cycles := &stubCycles{}
	jobs := &stubResumer{}
	acts := NewActivities(Deps{Cycles: cycles, Jobs: jobs})

	require.NoError(t, acts.NoteCycleDispatch(context.Background(), NoteCycleDispatchInput{CycleID: "c1", JobRef: "ca-c1"}))
	require.Empty(t, jobs.calls)
}

// A failed un-suspend fails the LAUNCH, not the activity: returning it would
// retry NoteDispatch (a second attempt counted for one launch). The Job stays
// suspended and runs no pod, and the watcher's startup grace reports the
// attempt as it would any pod that never appeared.
func TestNoteCycleDispatch_ResumeFailureIsNotRetried(t *testing.T) {
	cycles := &stubCycles{dispatched: &delivery.RunCycle{OrgID: "acme", ProjectID: "shop", Environment: "development"}}
	jobs := &stubResumer{err: errors.New("oc 500")}
	acts := NewActivities(Deps{Cycles: cycles, Jobs: jobs})

	require.NoError(t, acts.NoteCycleDispatch(context.Background(), NoteCycleDispatchInput{CycleID: "c1", JobRef: "ca-c1"}))
	require.Len(t, jobs.calls, 1)
	require.Equal(t, 1, cycles.noteDispatches)
}

// The fenced write failing is the activity's error (retried), and nothing is
// resumed on its strength.
func TestNoteCycleDispatch_WriteFailureResumesNothing(t *testing.T) {
	cycles := &stubCycles{dispatchErr: errors.New("db down")}
	jobs := &stubResumer{}
	acts := NewActivities(Deps{Cycles: cycles, Jobs: jobs})

	require.Error(t, acts.NoteCycleDispatch(context.Background(), NoteCycleDispatchInput{CycleID: "c1", JobRef: "ca-c1"}))
	require.Empty(t, jobs.calls)
}
