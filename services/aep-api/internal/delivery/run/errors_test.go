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

// What the retry classification is worth is measured in ATTEMPTS, so these
// tests run the REAL activity — not a mocked one — over a scripted port and
// count how many times the port was asked. A mocked activity would prove
// nothing here: the whole behaviour under test lives in the error the activity
// returns and in what Temporal's retry machinery does with it.
//
// The pair is the point. The first test pins that an answer is asked once; the
// second pins that a blip is still asked again, so a future "just stop
// retrying" cannot pass by making the supervisor give up on everything.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// scriptedMilestones answers the boundary poll from a queued script — one entry
// per call, the last repeating — and counts how often it was asked. A nil entry
// answers an empty milestone, which parks the run.
type scriptedMilestones struct {
	mu     sync.Mutex
	script []error
	calls  int
}

func (s *scriptedMilestones) MilestoneIssueCounts(context.Context, string, string, int) (*sourcecontrol.MilestoneIssueCounts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	err := s.script[min(s.calls-1, len(s.script)-1)]
	if err != nil {
		return nil, err
	}
	return &sourcecontrol.MilestoneIssueCounts{}, nil
}

func (s *scriptedMilestones) CloseMilestone(context.Context, string, string, int) error { return nil }

func (s *scriptedMilestones) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// pollEnv wires the real PollMilestone over a scripted port. The run row's
// writes stay mocked: they are not what is being measured, and a fake store
// would only add a second thing that could fail.
func pollEnv(t *testing.T, script ...error) (*testsuite.TestWorkflowEnvironment, *scriptedMilestones) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	port := &scriptedMilestones{script: script}
	acts := NewActivities(Deps{Milestones: port})
	env.RegisterActivity(acts)
	env.OnActivity(acts.SetRunState, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(acts.SettleRun, mock.Anything, mock.Anything).Return(nil)
	// A run that plans nothing SETTLES rather than parking, and records `skipped`
	// on the way out: it landed nothing, so nothing will ever judge it and an empty
	// verdict would read as "any moment now" forever.
	env.OnActivity(acts.SetValidationVerdict, mock.Anything, mock.Anything).Return(nil)
	// The boundary asks whether the run was cancelled before it polls. Nobody
	// cancelled these runs; what is being measured is the poll's retry shape.
	env.OnActivity(acts.ReadCycleFacts, mock.Anything, mock.Anything).Return(CycleFacts{}, nil)
	return env, port
}

func executePoll(env *testsuite.TestWorkflowEnvironment) {
	env.ExecuteWorkflow(DevRunWorkflow, RunInput{
		RunID:           testRunID,
		OrgID:           testOrg,
		ProjectID:       testProject,
		MilestoneNumber: testMilepost,
		MilestoneTitle:  "v3",
		Kind:            delivery.RunKindDev,
		Origin:          delivery.RunOriginSpecBuild,
		// A spec build always carries the tag it claimed — it is what tells the loop
		// this run OWNS the version and therefore plans its own milestone. Omitting
		// it would describe a state the click cannot produce, and the run would park
		// waiting for work nobody was going to file.
		Tag: "v3",
	})
}

// A deleted project takes its repo row with it, and every later boundary poll
// fails identically. The run must fail on the FIRST one: this is the defect
// that ran an activity to attempt 325 against a project that no longer existed.
func TestPollMilestoneStopsAtTheFirstPermanentFailure(t *testing.T) {
	env, port := pollEnv(t, sourcecontrol.ErrRepoNotFound)

	executePoll(env)

	require.True(t, env.IsWorkflowCompleted())
	require.Equal(t, 1, port.count(), "a permanent failure must be asked exactly once")

	err := env.GetWorkflowError()
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, errTypePermanentSourceControl, appErr.Type())
	require.True(t, appErr.NonRetryable())
	// The cause survives the trip, so the failed workflow still names what went
	// wrong rather than only that something did.
	require.Contains(t, err.Error(), sourcecontrol.ErrRepoNotFound.Error())
}

// The GraphQL surface answers 200 with an errors[] entry, so a repository
// deleted on GitHub reaches the supervisor as NOT_FOUND rather than as a 404.
// It is the same answer and must cost the same one attempt.
func TestPollMilestoneStopsOnGraphQLNotFound(t *testing.T) {
	env, port := pollEnv(t, &sourcecontrol.GraphQLError{Errors: []sourcecontrol.GraphQLErrorDetail{{
		Type:    sourcecontrol.GraphQLTypeNotFound,
		Message: "Could not resolve to a Repository with the name 'org1/proj1'.",
	}}})

	executePoll(env)

	require.True(t, env.IsWorkflowCompleted())
	require.Equal(t, 1, port.count())
	require.Error(t, env.GetWorkflowError())
}

// The other half of the contract: a blip is still retried, unbounded, exactly
// as before. Two 500s then an answer, and the run carries on.
func TestPollMilestoneKeepsRetryingATransientFailure(t *testing.T) {
	blip := &sourcecontrol.HTTPStatusError{StatusCode: http.StatusInternalServerError, Body: "server error"}
	env, port := pollEnv(t, blip, blip, nil)

	executePoll(env)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, port.count(), "a transient failure must be retried until it answers")

	// The poll answers an empty milestone with no cycles behind it. That used to
	// park the run in an unbounded wait, because the click admitted the row
	// before planning and "empty" could mean "not planned yet". Planning is the
	// workflow's own first phase now, so empty is unambiguous: delivered.
	var res RunResult
	require.NoError(t, env.GetWorkflowResult(&res))
	require.Equal(t, delivery.RunStateSucceeded, res.State)
}

// sourceControlErr is the whole seam, so its pass-through half is pinned
// directly: an error nothing can classify must reach Temporal untouched, or a
// blip would silently become terminal.
func TestSourceControlErrLeavesUnclassifiedErrorsAlone(t *testing.T) {
	require.NoError(t, sourceControlErr(nil))

	transient := errors.New("dial tcp: connection refused")
	require.Same(t, transient, sourceControlErr(transient))
}

// scriptedPlanner answers the planning turn from a queued script — one entry per
// call, the last repeating — and counts how often it was asked.
type scriptedPlanner struct {
	mu     sync.Mutex
	script []error
	calls  int
}

func (s *scriptedPlanner) PlanIntoMilestone(context.Context, string, string, int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.script[min(s.calls-1, len(s.script)-1)]
}

func (s *scriptedPlanner) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// planEnv wires the real PlanMilestone over a scripted planner. The milestone
// poll answers empty, so a run that gets past planning settles immediately and
// the test measures planning alone.
func planEnv(t *testing.T, script ...error) (*testsuite.TestWorkflowEnvironment, *scriptedPlanner) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	planner := &scriptedPlanner{script: script}
	acts := NewActivities(Deps{Milestones: &scriptedMilestones{script: []error{nil}}, Planner: planner})
	env.RegisterActivity(acts)
	env.OnActivity(acts.SetRunState, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(acts.SettleRun, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(acts.SetValidationVerdict, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(acts.ReadCycleFacts, mock.Anything, mock.Anything).Return(CycleFacts{}, nil)
	return env, planner
}

func executePlan(env *testsuite.TestWorkflowEnvironment) {
	env.ExecuteWorkflow(DevRunWorkflow, RunInput{
		RunID:           testRunID,
		OrgID:           testOrg,
		ProjectID:       testProject,
		MilestoneNumber: testMilepost,
		MilestoneTitle:  "v3",
		Kind:            delivery.RunKindDev,
		Origin:          delivery.RunOriginSpecBuild,
		Tag:             "v3",
	})
}

// THE regression this whole change exists for. A version died `plan-failed`
// because a seven-second TCP connect timeout to GitHub reached a plan path with
// no retry at all. Under the workflow the blip is asked again and the version
// survives it.
func TestPlanMilestoneRetriesABlip(t *testing.T) {
	env, planner := planEnv(t, errors.New("connect github.com:443: connection timed out"), nil)

	executePlan(env)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, planner.count(), "a transient planning failure must be asked again")
}

// The other half: an ANSWER is asked exactly once. A repository that is gone
// will not come back on attempt 300, and retrying it would hide the one failure
// that mattered behind a thousand copies.
func TestPlanMilestoneStopsAtTheFirstPermanentFailure(t *testing.T) {
	env, planner := planEnv(t, sourcecontrol.ErrRepoNotFound)

	executePlan(env)

	require.True(t, env.IsWorkflowCompleted())
	require.Equal(t, 1, planner.count(), "a permanent planning failure must be asked exactly once")
}

// scriptedGates answers ProvisionForBuild from a queued script — one entry per
// call, the last repeating — and counts how often it was asked.
type scriptedGates struct {
	mu     sync.Mutex
	script []error
	calls  int
}

func (s *scriptedGates) ProvisionForBuild(context.Context, string, string, string, int, []delivery.ProvisionInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.script[min(s.calls-1, len(s.script)-1)]
}

func (s *scriptedGates) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// gatesEnv wires the real ProvisionGates over a scripted port. Planner
// succeeds so a provision blip-then-nil still completes the workflow; the
// test measures ProvisionGates attempts, not planning.
func gatesEnv(t *testing.T, script ...error) (*testsuite.TestWorkflowEnvironment, *scriptedGates) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	gates := &scriptedGates{script: script}
	acts := NewActivities(Deps{
		Milestones: &scriptedMilestones{script: []error{nil}},
		Gates:      gates,
		Planner:    &scriptedPlanner{script: []error{nil}},
	})
	env.RegisterActivity(acts)
	env.OnActivity(acts.SetRunState, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(acts.SettleRun, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(acts.SetValidationVerdict, mock.Anything, mock.Anything).Return(nil)
	env.OnActivity(acts.ReadCycleFacts, mock.Anything, mock.Anything).Return(CycleFacts{}, nil)
	return env, gates
}

func TestProvisionGatesStopsAtTheFirstPermanentFailure(t *testing.T) {
	env, gates := gatesEnv(t, fmt.Errorf("%w: object-storage missing", delivery.ErrProvisionPermanent))
	executePlan(env)
	require.True(t, env.IsWorkflowCompleted())
	require.Equal(t, 1, gates.count(), "a permanent provision failure must be asked exactly once")
}

func TestProvisionGatesRetriesABlip(t *testing.T) {
	env, gates := gatesEnv(t, errors.New("openchoreo unreachable"), nil)
	executePlan(env)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, gates.count(), "a transient provision failure must be asked again")
}

// The third case, and the one the bounded policy exists for: a fault that is
// NOT one of the permanent kinds we can name, and that never clears.
//
// The two tests above cover the answers we recognise (asked once) and the blips
// that pass (asked again). Neither would notice the mode that caused the
// incident: an error the classifier does not know, repeating forever under
// Temporal's unbounded default while the version never fails. So the cap is
// asserted over the REAL activity, error classification and all, rather than
// over a mocked one — that is what makes it evidence about the two guards
// TOGETHER rather than about the retry policy alone.
func TestProvisionGatesGivesUpOnABlipThatNeverClears(t *testing.T) {
	env, gates := gatesEnv(t, errors.New("openchoreo unreachable"))

	executePlan(env)

	require.True(t, env.IsWorkflowCompleted())
	require.Equal(t, gateActivityAttempts, gates.count(),
		"an unrecognised fault is retried a bounded number of times and then the version fails")
	// The workflow itself completes rather than erroring: giving up on the gates
	// settles the run `plan-failed` on the ordinary path, which is the whole
	// reason cancel and failure here are signals rather than workflow faults.
	// That the reason is plan-failed is asserted in workflow_test.go, over the
	// harness that can read it.
}
