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
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The planning turn runs in the org's AE Studio pod. What its
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

// A different turn running for the project (a browser turn, or the
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
	acts, runs := planWith(fmt.Errorf("plan: %w", sourcecontrol.ErrAEStudioMisconfigured))

	err := acts.PlanMilestone(context.Background(), PlanMilestoneInput{RunID: "run-1"})

	appErr, permanent := nonRetryable(err)
	require.True(t, permanent, "a misconfigured AE-only client must not be retried")
	require.Equal(t, "ae_studio_misconfigured", appErr.Type())
	require.ErrorIs(t, err, sourcecontrol.ErrAEStudioMisconfigured, "the cause stays legible")
	require.Len(t, runs.recorded, 1)
	require.True(t, runs.recorded[0].Permanent, "the record and the retry policy must agree")
}

// An org with no AE Studio (no GitHub token) cannot plan until a person
// connects GitHub: the run fails on its first attempt instead of spinning.
// A permanent answer of the pod (an unknown project, a 4xx) is the same.
func TestPlanMilestone_APermanentAEStudioAnswerFailsFast(t *testing.T) {
	for name, cause := range map[string]error{
		"absent":          sourcecontrol.ErrAEStudioAbsent,
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
	acts, _ := planWith(fmt.Errorf("plan: %w", sourcecontrol.ErrAEStudioUnavailable))

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

// The planning activity heartbeats per turn event, each
// beat carrying how many events the turn has sent: proof the turn is moving,
// not only that the worker is.
func TestPlanMilestone_HeartbeatsPerTurnEvent(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
	var mu sync.Mutex
	var last planBeatDetails
	env.SetOnActivityHeartbeatListener(func(_ *activity.Info, details converter.EncodedValues) {
		var d planBeatDetails
		if details.HasValues() && details.Get(&d) == nil {
			mu.Lock()
			last = d
			mu.Unlock()
		}
	})
	acts := NewActivities(Deps{Planner: progressPlanner{n: 3}})
	env.RegisterActivity(acts.PlanMilestone)

	_, err := env.ExecuteActivity(acts.PlanMilestone, PlanMilestoneInput{})
	require.Error(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 3, last.Events, "the last beat must carry the turn's event count")
	require.Zero(t, last.ProviderLimits, "a turn that failed otherwise is no provider limit")
}

// A turn the pod ends `failed`. Each attempt of the planning activity
// is a new paid model turn, so only a turn that did not run to its own end is
// retried freely; a provider limit is retried a bounded number of times, each
// after planProviderLimitRetryDelay; any other ending is the model's answer
// and fails the run on its first attempt.
func TestPlanMilestone_AFailedTurnIsClassifiedByItsCode(t *testing.T) {
	for _, tc := range []struct {
		code      string
		permanent bool
	}{
		{aestudiotools.TurnCodeShutdown, false},
		{aestudiotools.TurnCodeStreamDied, false},
		{aestudiotools.TurnCodeProviderLimit, false},
		{"agent-error", true},
		{"output_truncated", true},
		{"internal", true},
		{"", true},
	} {
		t.Run(tc.code, func(t *testing.T) {
			acts, runs := planWith(fmt.Errorf("plan: %w", &aestudiotools.TurnFailedError{Code: tc.code}))

			err := acts.PlanMilestone(context.Background(), PlanMilestoneInput{RunID: "run-1"})

			appErr, permanent := nonRetryable(err)
			require.Equal(t, tc.permanent, permanent)
			if permanent {
				require.Equal(t, errTypePermanentPlan, appErr.Type())
			}
			require.True(t, runs.recorded[0].Permanent == tc.permanent, "the record and the retry policy must agree")
		})
	}
}

// provider_limit is bounded by how many attempts the provider's limit
// stopped, counted apart from the Temporal attempt: a shutdown or a
// dead stream before it never spends the bound. Each try waits for the
// provider's reset time when it stated one, clamped to
// [planProviderLimitMinDelay, planProviderLimitMaxDelay], else the fixed
// planProviderLimitRetryDelay.
func TestPlanErr_AProviderLimitIsRetriedABoundedNumberOfTimes(t *testing.T) {
	now := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	limited := func(resetAt time.Time) error {
		return fmt.Errorf("plan: %w", &aestudiotools.TurnFailedError{Code: aestudiotools.TurnCodeProviderLimit, ResetAt: resetAt})
	}

	for limits := 1; limits < planProviderLimitAttempts; limits++ {
		var appErr *temporal.ApplicationError
		require.ErrorAs(t, planErr(limited(time.Time{}), limits, now), &appErr, "provider limit %d", limits)
		require.False(t, appErr.NonRetryable(), "provider limit %d must be retried", limits)
		require.Equal(t, planProviderLimitRetryDelay, appErr.NextRetryDelay())
		f := planFailure(limited(time.Time{}), 9, limits)
		require.False(t, f.Permanent)
		require.Equal(t, limits, f.Attempts, "the record counts provider limits, not Temporal attempts")
		require.Equal(t, planProviderLimitAttempts, f.MaxAttempts)
	}
	appErr, permanent := nonRetryable(planErr(limited(time.Time{}), planProviderLimitAttempts, now))
	require.True(t, permanent, "the last provider_limit try must not be retried")
	require.Equal(t, errTypePermanentPlan, appErr.Type())
	require.True(t, planFailure(limited(time.Time{}), 9, planProviderLimitAttempts).Permanent)

	// Interrupted turns keep the default (unbounded) policy at any attempt,
	// and never count against the provider-limit bound.
	died := &aestudiotools.TurnFailedError{Code: aestudiotools.TurnCodeStreamDied}
	_, permanent = nonRetryable(planErr(died, planProviderLimitAttempts, now))
	require.False(t, permanent)
}

func TestPlanErr_AProviderLimitWaitsForTheStatedReset(t *testing.T) {
	now := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		resetAt time.Time
		want    time.Duration
	}{
		"in 5 min":    {now.Add(5 * time.Minute), 5 * time.Minute},
		"in 2 h":      {now.Add(2 * time.Hour), 30 * time.Minute},
		"in the past": {now.Add(-time.Minute), time.Minute},
		"in 10 s":     {now.Add(10 * time.Second), time.Minute},
		"absent":      {time.Time{}, 10 * time.Minute},
	} {
		t.Run(name, func(t *testing.T) {
			err := fmt.Errorf("plan: %w", &aestudiotools.TurnFailedError{Code: aestudiotools.TurnCodeProviderLimit, ResetAt: tc.resetAt})
			var appErr *temporal.ApplicationError
			require.ErrorAs(t, planErr(err, 1, now), &appErr)
			require.False(t, appErr.NonRetryable())
			require.Equal(t, errTypeProviderLimitedPlan, appErr.Type())
			require.Equal(t, tc.want, appErr.NextRetryDelay())
		})
	}
}

// The provider-limit count rides in the planning activity's heartbeat
// details, which Temporal hands to the next attempt: an attempt that starts
// after three provider limits and meets a fourth fails for good, whatever
// its Temporal attempt number.
func TestPlanMilestone_ReadsTheProviderLimitCountFromTheLastHeartbeat(t *testing.T) {
	limited := fmt.Errorf("plan: %w", &aestudiotools.TurnFailedError{Code: aestudiotools.TurnCodeProviderLimit})
	for name, tc := range map[string]struct {
		prior     any
		permanent bool
	}{
		"first attempt":       {nil, false},
		"after two limits":    {planBeatDetails{ProviderLimits: 2}, false},
		"after three limits":  {planBeatDetails{ProviderLimits: 3}, true},
		"an older beat (int)": {7, false},
	} {
		t.Run(name, func(t *testing.T) {
			env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			if tc.prior != nil {
				env.SetHeartbeatDetails(tc.prior)
			}
			var mu sync.Mutex
			var last planBeatDetails
			env.SetOnActivityHeartbeatListener(func(_ *activity.Info, details converter.EncodedValues) {
				var d planBeatDetails
				if details.HasValues() && details.Get(&d) == nil {
					mu.Lock()
					last = d
					mu.Unlock()
				}
			})
			acts := NewActivities(Deps{Planner: stubPlanner{err: limited}})
			env.RegisterActivity(acts.PlanMilestone)

			_, err := env.ExecuteActivity(acts.PlanMilestone, PlanMilestoneInput{})
			_, permanent := nonRetryable(err)
			require.Equal(t, tc.permanent, permanent)
			mu.Lock()
			defer mu.Unlock()
			want := 1
			if d, ok := tc.prior.(planBeatDetails); ok {
				want = d.ProviderLimits + 1
			}
			require.Equal(t, want, last.ProviderLimits, "the last beat must carry this attempt's provider limit")
		})
	}
}
