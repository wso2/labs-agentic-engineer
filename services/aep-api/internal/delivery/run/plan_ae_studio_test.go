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
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// The planning turn runs in the org's AE Studio pod (Task 3.17). What its
// failures mean to Temporal: a turn already running is a wait (retry), an
// operator fault or a permanent answer is not (fail on the first attempt).

func planWith(err error) (*Activities, *failureRuns) {
	runs := &failureRuns{}
	return NewActivities(Deps{Runs: runs, Planner: stubPlanner{err: err}}), runs
}

func nonRetryable(err error) (*temporal.ApplicationError, bool) {
	var appErr *temporal.ApplicationError
	return appErr, errors.As(err, &appErr) && appErr.NonRetryable()
}

// 05 §5: a different turn running for the project (a browser turn, or the
// kickoff) is a wait, not a failure. The activity returns a RETRYABLE error,
// so Temporal asks again with backoff until the turn ends.
func TestPlanMilestone_ATurnInProgressIsRetried(t *testing.T) {
	acts, runs := planWith(fmt.Errorf("plan: %w (active turn t-1)", aestudiotools.ErrTurnInProgress))

	err := acts.PlanMilestone(context.Background(), PlanMilestoneInput{RunID: "run-1"})

	require.ErrorIs(t, err, aestudiotools.ErrTurnInProgress)
	_, permanent := nonRetryable(err)
	require.False(t, permanent, "a turn in progress must be retried")
	require.Len(t, runs.recorded, 1)
	require.False(t, runs.recorded[0].Permanent)
}

// C3: aep-api's AE-only client is misconfigured. No retry fixes that, an
// operator must, so the activity fails non-retryable under its own type.
func TestPlanMilestone_AMisconfiguredAEStudioFailsFast(t *testing.T) {
	acts, runs := planWith(fmt.Errorf("plan: %w", aestudiotools.ErrAEStudioMisconfigured))

	err := acts.PlanMilestone(context.Background(), PlanMilestoneInput{RunID: "run-1"})

	appErr, permanent := nonRetryable(err)
	require.True(t, permanent, "a misconfigured AE-only client must not be retried")
	require.Equal(t, "ae_studio_misconfigured", appErr.Type())
	require.ErrorIs(t, err, aestudiotools.ErrAEStudioMisconfigured, "the cause stays legible")
	require.Len(t, runs.recorded, 1)
	require.True(t, runs.recorded[0].Permanent, "the record and the retry policy must agree")
}

// An org with no AE Studio (no GitHub token) cannot plan until a person
// connects GitHub: the run fails on its first attempt instead of spinning.
// A permanent answer of the pod (an unknown project, a 4xx) is the same.
func TestPlanMilestone_APermanentAEStudioAnswerFailsFast(t *testing.T) {
	for name, cause := range map[string]error{
		"absent":          aestudiotools.ErrAEStudioAbsent,
		"unknown project": &aestudiotools.StatusError{Op: "start-repo-turn", Status: 404, Code: "project_unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			acts, runs := planWith(fmt.Errorf("plan: %w", cause))

			err := acts.PlanMilestone(context.Background(), PlanMilestoneInput{RunID: "run-1"})

			appErr, permanent := nonRetryable(err)
			require.True(t, permanent)
			require.Equal(t, errTypePermanentPlan, appErr.Type())
			require.True(t, runs.recorded[0].Permanent)
		})
	}
}

// An unreachable pod is a blip, retried like any other.
func TestPlanMilestone_AnUnavailableAEStudioIsRetried(t *testing.T) {
	acts, _ := planWith(fmt.Errorf("plan: %w", aestudiotools.ErrAEStudioUnavailable))

	_, permanent := nonRetryable(acts.PlanMilestone(context.Background(), PlanMilestoneInput{RunID: "run-1"}))
	require.False(t, permanent)
}

// progressPlanner reports progress n times — what the plan tap does once per
// turn event — then fails, which makes the test environment flush the last
// heartbeat to the listener.
type progressPlanner struct{ n int }

func (p progressPlanner) PlanIntoMilestone(ctx context.Context, _, _ string, _ int) error {
	for range p.n {
		delivery.ReportProgress(ctx)
	}
	return errors.New("turn failed")
}

// The planning activity heartbeats per turn event (04 §5, D-2/Q-8), each
// beat carrying how many events the turn has sent: proof the turn is moving,
// not only that the worker is.
func TestPlanMilestone_HeartbeatsPerTurnEvent(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
	var mu sync.Mutex
	var last int
	env.SetOnActivityHeartbeatListener(func(_ *activity.Info, details converter.EncodedValues) {
		var n int
		if details.HasValues() && details.Get(&n) == nil {
			mu.Lock()
			last = n
			mu.Unlock()
		}
	})
	acts := NewActivities(Deps{Planner: progressPlanner{n: 3}})
	env.RegisterActivity(acts.PlanMilestone)

	_, err := env.ExecuteActivity(acts.PlanMilestone, PlanMilestoneInput{})
	require.Error(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 3, last, "the last beat must carry the turn's event count")
}
