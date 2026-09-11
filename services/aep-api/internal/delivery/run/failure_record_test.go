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
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// failureRuns is a RunStore that records the failure writes and nothing else.
type failureRuns struct {
	RunStore
	recorded []delivery.RunFailure
	cleared  int
}

func (r *failureRuns) RecordFailure(_ context.Context, _ string, f delivery.RunFailure) error {
	r.recorded = append(r.recorded, f)
	return nil
}

func (r *failureRuns) ClearFailure(context.Context, string) error {
	r.cleared++
	return nil
}

// stubGates answers ProvisionForBuild with a fixed error.
type stubGates struct{ err error }

func (g stubGates) ProvisionForBuild(context.Context, string, string, string, int, []delivery.ProvisionInput) error {
	return g.err
}

// stubPlanner answers PlanIntoMilestone with a fixed error.
type stubPlanner struct{ err error }

func (p stubPlanner) PlanIntoMilestone(context.Context, string, string, int) error { return p.err }

// TestProvisionGates_RecordsThePermanentFaultAndFailsFast is the sendgrid
// incident: a dependency whose schema the ResourceType builder refuses. The
// record has to name the dependency and the component, say repeating cannot
// help, and the activity has to fail non-retryable — one attempt, not three.
func TestProvisionGates_RecordsThePermanentFaultAndFailsFast(t *testing.T) {
	runs := &failureRuns{}
	perr := &delivery.ProvisionFailedError{Faults: []delivery.ProvisionFault{{
		Component:  "allocation-api",
		Dependency: "sendgrid",
		Reason:     `platform provisioner failed: platform provisioner failed permanently: external resources: build resourcetype: external resourcetype "sendgrid": at least one config key required`,
		Permanent:  true,
	}}}
	acts := NewActivities(Deps{Runs: runs, Gates: stubGates{err: perr}})

	err := acts.ProvisionGates(context.Background(), PlanMilestoneInput{RunID: "run-1", OrgID: "acme", ProjectID: "shop", Tag: "v1"})

	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable(), "a permanent answer must not be retried")

	require.Len(t, runs.recorded, 1)
	got := runs.recorded[0]
	require.Equal(t, delivery.RunFailureCodeDependencyUnprovisionable, got.Code)
	require.Equal(t, delivery.RunPhasePlanning, got.Phase)
	require.Equal(t, "sendgrid", got.Dependency)
	require.Equal(t, "allocation-api", got.Component)
	require.True(t, got.Permanent)
	require.Equal(t, 1, got.Attempts)
	require.Equal(t, gateActivityAttempts, got.MaxAttempts)
	require.Equal(t, `sendgrid: external resources: build resourcetype: external resourcetype "sendgrid": at least one config key required`, got.Detail,
		"the detail is the provisioner's words per dependency, without the sentinel wraps")
	require.Zero(t, runs.cleared)
}

// TestProvisionGates_RecordsATransientFaultAsRetryable: an error the platform
// cannot call permanent keeps the bounded retry, and the record says so — code
// `dependency-provision-failed`, not permanent, the bound on it.
func TestProvisionGates_RecordsATransientFaultAsRetryable(t *testing.T) {
	runs := &failureRuns{}
	perr := &delivery.ProvisionFailedError{Faults: []delivery.ProvisionFault{{
		Component: "api", Dependency: "orders-db", Reason: "resources: apply resource: 503",
	}}}
	acts := NewActivities(Deps{Runs: runs, Gates: stubGates{err: perr}})

	err := acts.ProvisionGates(context.Background(), PlanMilestoneInput{RunID: "run-1"})

	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.False(t, errors.As(err, &appErr) && appErr.NonRetryable(), "a blip keeps its retries")
	require.Len(t, runs.recorded, 1)
	require.Equal(t, delivery.RunFailureCodeDependencyProvisionFailed, runs.recorded[0].Code)
	require.False(t, runs.recorded[0].Permanent)
	require.Equal(t, "orders-db", runs.recorded[0].Dependency)
}

// TestProvisionGates_SuccessOnFirstAttemptWritesNothing: the ordinary path
// costs the row nothing.
func TestProvisionGates_SuccessOnFirstAttemptWritesNothing(t *testing.T) {
	runs := &failureRuns{}
	acts := NewActivities(Deps{Runs: runs, Gates: stubGates{}})

	require.NoError(t, acts.ProvisionGates(context.Background(), PlanMilestoneInput{RunID: "run-1"}))
	require.Empty(t, runs.recorded)
	require.Zero(t, runs.cleared)
}

// TestProvisionGates_NoRunIDRecordsNothing: an input from before the record
// existed (a replay of old history) still provisions, and records nothing.
func TestProvisionGates_NoRunIDRecordsNothing(t *testing.T) {
	runs := &failureRuns{}
	acts := NewActivities(Deps{Runs: runs, Gates: stubGates{err: errors.New("boom")}})

	require.Error(t, acts.ProvisionGates(context.Background(), PlanMilestoneInput{}))
	require.Empty(t, runs.recorded)
}

// TestPlanMilestone_RecordsTheTurnFault: a planning-turn error is recorded
// unbounded (MaxAttempts 0) and retryable; a permanent source-control answer is
// `repository-unavailable` and non-retryable.
func TestPlanMilestone_RecordsTheTurnFault(t *testing.T) {
	t.Run("transient", func(t *testing.T) {
		runs := &failureRuns{}
		acts := NewActivities(Deps{Runs: runs, Planner: stubPlanner{err: errors.New("anthropic: 529 overloaded")}})

		err := acts.PlanMilestone(context.Background(), PlanMilestoneInput{RunID: "run-1"})

		require.Error(t, err)
		require.Len(t, runs.recorded, 1)
		require.Equal(t, delivery.RunFailureCodePlanTurnFailed, runs.recorded[0].Code)
		require.False(t, runs.recorded[0].Permanent)
		require.Zero(t, runs.recorded[0].MaxAttempts, "the planning turn retries unbounded")
	})
	t.Run("repository gone", func(t *testing.T) {
		runs := &failureRuns{}
		gone := fmt.Errorf("plan: %w", sourcecontrol.ErrRepoNotFound)
		acts := NewActivities(Deps{Runs: runs, Planner: stubPlanner{err: gone}})

		err := acts.PlanMilestone(context.Background(), PlanMilestoneInput{RunID: "run-1"})

		var appErr *temporal.ApplicationError
		require.True(t, errors.As(err, &appErr) && appErr.NonRetryable())
		require.Len(t, runs.recorded, 1)
		require.Equal(t, delivery.RunFailureCodeRepositoryUnavailable, runs.recorded[0].Code)
		require.True(t, runs.recorded[0].Permanent)
	})
}

// TestRecordPlanningFault_ClearsOnlyAfterAnEarlierAttempt pins the clear rule
// at the helper: a healed fault is one that an EARLIER attempt recorded, so a
// nil failure on attempt 1 writes nothing while one on attempt 2 clears.
func TestRecordPlanningFault_ClearsOnlyAfterAnEarlierAttempt(t *testing.T) {
	runs := &failureRuns{}
	acts := NewActivities(Deps{Runs: runs})

	// Success on the first attempt: nothing was ever recorded, nothing to clear.
	acts.recordPlanningFault(context.Background(), "run-1", nil, 1)
	require.Zero(t, runs.cleared)

	// A fault on attempt 1, then success on attempt 2: the record is cleared.
	acts.recordPlanningFault(context.Background(), "run-1", &delivery.RunFailure{Code: delivery.RunFailureCodePlanTurnFailed}, 1)
	require.Len(t, runs.recorded, 1)
	acts.recordPlanningFault(context.Background(), "run-1", nil, 2)
	require.Equal(t, 1, runs.cleared, "a healed fault is cleared")

	// No run id (a pre-record history): nothing is written or cleared.
	acts.recordPlanningFault(context.Background(), "", nil, 2)
	require.Equal(t, 1, runs.cleared)
}
