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

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	"github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/activities"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/types"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/workflows"
)

func taskInput(codeReview orchestration.GateMode) types.TaskLifecycleInput {
	return types.TaskLifecycleInput{
		Org: "acme", Project: "web", TaskID: "T1", ComponentName: "api", CodeReview: codeReview,
	}
}

// signalAt schedules a signal to the main workflow at the given ordinal (ms).
func signalAt(env *testsuite.TestWorkflowEnvironment, order int, name string) {
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(name, nil) }, time.Duration(order)*time.Millisecond)
}

func runTask(t *testing.T, in types.TaskLifecycleInput, schedule func(env *testsuite.TestWorkflowEnvironment)) types.TaskStateView {
	t.Helper()
	var s testsuite.WorkflowTestSuite
	env := s.NewTestWorkflowEnvironment()
	env.RegisterActivity(&activities.Activities{})
	schedule(env)
	env.ExecuteWorkflow(workflows.TaskLifecycleWorkflow, in)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var out types.TaskStateView
	require.NoError(t, env.GetWorkflowResult(&out))
	return out
}

func TestTask_HappyPath(t *testing.T) {
	out := runTask(t, taskInput(orchestration.GateHuman), func(env *testsuite.TestWorkflowEnvironment) {
		signalAt(env, 1, orchestration.SignalPRReady)
		signalAt(env, 2, orchestration.SignalPRMerged)
		signalAt(env, 3, orchestration.SignalBuildStarted)
		signalAt(env, 4, orchestration.SignalBuildSucceeded)
		signalAt(env, 5, orchestration.SignalDeployStarted)
		signalAt(env, 6, orchestration.SignalDeploySucceeded)
	})
	require.Equal(t, orchestration.TaskDeployed, out.Status)
}

func TestTask_DeployFailed(t *testing.T) {
	out := runTask(t, taskInput(orchestration.GateHuman), func(env *testsuite.TestWorkflowEnvironment) {
		signalAt(env, 1, orchestration.SignalPRReady)
		signalAt(env, 2, orchestration.SignalPRMerged)
		signalAt(env, 3, orchestration.SignalBuildStarted)
		signalAt(env, 4, orchestration.SignalBuildSucceeded)
		signalAt(env, 5, orchestration.SignalDeployStarted)
		signalAt(env, 6, orchestration.SignalDeployFailed)
	})
	require.Equal(t, orchestration.TaskFailed, out.Status)
}

func TestTask_RejectedInProgress(t *testing.T) {
	out := runTask(t, taskInput(orchestration.GateHuman), func(env *testsuite.TestWorkflowEnvironment) {
		signalAt(env, 1, orchestration.SignalPRRejected)
	})
	require.Equal(t, orchestration.TaskRejected, out.Status)
}

func TestTask_BuildFailed(t *testing.T) {
	out := runTask(t, taskInput(orchestration.GateHuman), func(env *testsuite.TestWorkflowEnvironment) {
		signalAt(env, 1, orchestration.SignalPRReady)
		signalAt(env, 2, orchestration.SignalPRMerged)
		signalAt(env, 3, orchestration.SignalBuildStarted)
		signalAt(env, 4, orchestration.SignalBuildFailed)
	})
	require.Equal(t, orchestration.TaskFailed, out.Status)
}

func TestTask_VerificationFailedThenRetry(t *testing.T) {
	out := runTask(t, taskInput(orchestration.GateHuman), func(env *testsuite.TestWorkflowEnvironment) {
		signalAt(env, 1, orchestration.SignalVerificationFailed)
		signalAt(env, 2, orchestration.SignalRetry)
		signalAt(env, 3, orchestration.SignalPRReady)
		signalAt(env, 4, orchestration.SignalPRMerged)
		signalAt(env, 5, orchestration.SignalBuildStarted)
		signalAt(env, 6, orchestration.SignalBuildSucceeded)
		signalAt(env, 7, orchestration.SignalDeployStarted)
		signalAt(env, 8, orchestration.SignalDeploySucceeded)
	})
	require.Equal(t, orchestration.TaskDeployed, out.Status)
}

func TestTask_AutoMerge(t *testing.T) {
	out := runTask(t, taskInput(orchestration.GateAuto), func(env *testsuite.TestWorkflowEnvironment) {
		// auto code-review fires AutoMerge (stub); the merge still surfaces as PRMerged.
		signalAt(env, 1, orchestration.SignalPRReady)
		signalAt(env, 2, orchestration.SignalPRMerged)
		signalAt(env, 3, orchestration.SignalBuildStarted)
		signalAt(env, 4, orchestration.SignalBuildSucceeded)
		signalAt(env, 5, orchestration.SignalDeployStarted)
		signalAt(env, 6, orchestration.SignalDeploySucceeded)
	})
	require.Equal(t, orchestration.TaskDeployed, out.Status)
}

func TestTask_OrgDisconnectedAbandons(t *testing.T) {
	out := runTask(t, taskInput(orchestration.GateHuman), func(env *testsuite.TestWorkflowEnvironment) {
		signalAt(env, 1, orchestration.SignalPRReady)
		signalAt(env, 2, orchestration.SignalOrgDisconnected)
	})
	require.Equal(t, orchestration.TaskAbandoned, out.Status)
}
