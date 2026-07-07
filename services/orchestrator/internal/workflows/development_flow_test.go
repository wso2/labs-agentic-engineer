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

package workflows_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/activities"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/types"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/workflows"
)

const (
	tOrg     = "acme"
	tProject = "web"
)

func cycleInput(start orchestration.Phase, policy types.GatePolicy) types.DevelopmentFlowInput {
	return types.DevelopmentFlowInput{
		Org: tOrg, Project: tProject, CycleID: "c1",
		Source: orchestration.SourceRequirement, StartPhase: start, GatePolicy: policy,
	}
}

func allHuman() types.GatePolicy {
	return types.GatePolicy{Requirements: orchestration.GateHuman, Design: orchestration.GateHuman, CodeReview: orchestration.GateHuman}
}
func allAuto() types.GatePolicy {
	return types.GatePolicy{Requirements: orchestration.GateAuto, Design: orchestration.GateAuto, CodeReview: orchestration.GateAuto}
}

func childSignalAt(env *testsuite.TestWorkflowEnvironment, order int, wfID, name string) {
	env.RegisterDelayedCallback(func() {
		// Errors (e.g. child not started yet) would fail the test downstream via
		// an unmet final state; the signal-send error itself is not asserted here.
		_ = env.SignalWorkflowByID(wfID, name, nil)
	}, time.Duration(order)*time.Millisecond)
}

// driveTaskToDeployed schedules the signals that take a child task from
// in_progress to deployed (PR → build → deploy), starting at the given ordinal.
func driveTaskToDeployed(env *testsuite.TestWorkflowEnvironment, startOrder int, childID string) {
	childSignalAt(env, startOrder+0, childID, orchestration.SignalPRReady)
	childSignalAt(env, startOrder+1, childID, orchestration.SignalPRMerged)
	childSignalAt(env, startOrder+2, childID, orchestration.SignalBuildStarted)
	childSignalAt(env, startOrder+3, childID, orchestration.SignalBuildSucceeded)
	childSignalAt(env, startOrder+4, childID, orchestration.SignalDeployStarted)
	childSignalAt(env, startOrder+5, childID, orchestration.SignalDeploySucceeded)
}

func newCycleEnv() (*testsuite.TestWorkflowEnvironment, *testsuite.WorkflowTestSuite) {
	s := &testsuite.WorkflowTestSuite{}
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivity(&activities.Activities{})
	env.RegisterWorkflow(workflows.TaskLifecycleWorkflow) // child
	return env, s
}

func TestDevFlow_HumanGates_EmptyTasks(t *testing.T) {
	env, _ := newCycleEnv()
	// PlanTasks stub returns no tasks -> implement completes immediately.
	signalAt(env, 1, orchestration.SignalApproveRequirements)
	signalAt(env, 2, orchestration.SignalApproveDesign)
	signalAt(env, 3, orchestration.SignalMarkComplete)

	env.ExecuteWorkflow(workflows.DevelopmentFlowWorkflow, cycleInput(orchestration.PhaseRequirements, allHuman()))

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var out types.CycleStateView
	require.NoError(t, env.GetWorkflowResult(&out))
	require.Equal(t, orchestration.PhaseComplete, out.Phase)
	require.True(t, out.GatesPassed.Requirements)
	require.True(t, out.GatesPassed.Design)
	require.True(t, out.GatesPassed.Tasks)
}

func TestDevFlow_AutonomousGates_EmptyTasks(t *testing.T) {
	env, _ := newCycleEnv()
	// auto gates advance via the RunGateChecks stub (passes); only MarkComplete needed.
	signalAt(env, 1, orchestration.SignalMarkComplete)

	env.ExecuteWorkflow(workflows.DevelopmentFlowWorkflow, cycleInput(orchestration.PhaseRequirements, allAuto()))

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var out types.CycleStateView
	require.NoError(t, env.GetWorkflowResult(&out))
	require.Equal(t, orchestration.PhaseComplete, out.Phase)
	require.True(t, out.GatesPassed.Requirements)
	require.True(t, out.GatesPassed.Design)
}

func TestDevFlow_BackFromDesignToRequirements(t *testing.T) {
	env, _ := newCycleEnv()
	signalAt(env, 1, orchestration.SignalApproveRequirements) // -> design
	signalAt(env, 2, orchestration.SignalBackToRequirements)  // -> requirements (req gate reset)
	signalAt(env, 3, orchestration.SignalApproveRequirements) // -> design
	signalAt(env, 4, orchestration.SignalApproveDesign)       // -> implement -> merge
	signalAt(env, 5, orchestration.SignalMarkComplete)        // -> complete

	env.ExecuteWorkflow(workflows.DevelopmentFlowWorkflow, cycleInput(orchestration.PhaseRequirements, allHuman()))

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var out types.CycleStateView
	require.NoError(t, env.GetWorkflowResult(&out))
	require.Equal(t, orchestration.PhaseComplete, out.Phase)
}

func TestDevFlow_IssueFastPath(t *testing.T) {
	env, _ := newCycleEnv()
	// startPhase = implement skips requirements/design (issue fast-path).
	signalAt(env, 1, orchestration.SignalMarkComplete)

	in := cycleInput(orchestration.PhaseImplement, allHuman())
	in.Source = orchestration.SourceIssue
	env.ExecuteWorkflow(workflows.DevelopmentFlowWorkflow, in)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var out types.CycleStateView
	require.NoError(t, env.GetWorkflowResult(&out))
	require.Equal(t, orchestration.PhaseComplete, out.Phase)
	require.False(t, out.GatesPassed.Requirements)
	require.False(t, out.GatesPassed.Design)
	require.True(t, out.GatesPassed.Tasks)
}

func TestDevFlow_OneTaskDeploys(t *testing.T) {
	env, _ := newCycleEnv()
	act := &activities.Activities{}
	env.OnActivity(act.PlanTasks, mock.Anything, mock.Anything).Return(
		[]types.TaskSpec{{TaskID: "T1", ComponentName: "api"}}, nil)

	childID := orchestration.TaskWorkflowID(tOrg, tProject, "T1")
	signalAt(env, 1, orchestration.SignalApproveRequirements)
	signalAt(env, 2, orchestration.SignalApproveDesign) // implement starts child T1
	driveTaskToDeployed(env, 3, childID)                // 3..8 -> T1 deployed
	signalAt(env, 10, orchestration.SignalMarkComplete)

	env.ExecuteWorkflow(workflows.DevelopmentFlowWorkflow, cycleInput(orchestration.PhaseRequirements, allHuman()))

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var out types.CycleStateView
	require.NoError(t, env.GetWorkflowResult(&out))
	require.Equal(t, orchestration.PhaseComplete, out.Phase)
	require.Len(t, out.Tasks, 1)
	require.Equal(t, orchestration.TaskDeployed, out.Tasks[0].Status)
}

func TestDevFlow_DependencyOrderDAG(t *testing.T) {
	env, _ := newCycleEnv()
	act := &activities.Activities{}
	// T2 depends on T1 -> T2 must not deploy until T1 is deployed.
	env.OnActivity(act.PlanTasks, mock.Anything, mock.Anything).Return([]types.TaskSpec{
		{TaskID: "T1", ComponentName: "api"},
		{TaskID: "T2", ComponentName: "web", DependsOn: []string{"T1"}},
	}, nil)

	t1 := orchestration.TaskWorkflowID(tOrg, tProject, "T1")
	t2 := orchestration.TaskWorkflowID(tOrg, tProject, "T2")
	signalAt(env, 1, orchestration.SignalApproveRequirements)
	signalAt(env, 2, orchestration.SignalApproveDesign)
	driveTaskToDeployed(env, 3, t1)  // 3..8 -> T1 deployed, which unblocks T2
	driveTaskToDeployed(env, 10, t2) // 10..15 -> T2 deployed
	signalAt(env, 17, orchestration.SignalMarkComplete)

	env.ExecuteWorkflow(workflows.DevelopmentFlowWorkflow, cycleInput(orchestration.PhaseRequirements, allHuman()))

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var out types.CycleStateView
	require.NoError(t, env.GetWorkflowResult(&out))
	require.Equal(t, orchestration.PhaseComplete, out.Phase)
	require.Len(t, out.Tasks, 2)
	for _, ts := range out.Tasks {
		require.Equal(t, orchestration.TaskDeployed, ts.Status, "task %s", ts.TaskID)
	}
}

func TestDevFlow_QueryCycleState(t *testing.T) {
	env, _ := newCycleEnv()
	// Query mid-flow: after requirements approved, before design approved.
	signalAt(env, 1, orchestration.SignalApproveRequirements)
	env.RegisterDelayedCallback(func() {
		var st types.CycleStateView
		val, err := env.QueryWorkflow(orchestration.QueryGetCycleState)
		require.NoError(t, err)
		require.NoError(t, val.Get(&st))
		require.Equal(t, orchestration.PhaseDesign, st.Phase)
		require.True(t, st.GatesPassed.Requirements)
	}, 2*time.Millisecond)
	signalAt(env, 3, orchestration.SignalApproveDesign)
	signalAt(env, 4, orchestration.SignalMarkComplete)

	env.ExecuteWorkflow(workflows.DevelopmentFlowWorkflow, cycleInput(orchestration.PhaseRequirements, allHuman()))

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}
