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

package activities_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/activities"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/activities/fake"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/types"
)

func TestPlanTasks_DerivesDAGFromDesign(t *testing.T) {
	design := &fake.Design{Default: []activities.ComponentSpec{
		{Name: "api"},
		{Name: "web", DependsOn: []string{"api"}},
	}}
	a := activities.New(design, nil, nil, nil)

	tasks, err := a.PlanTasks(context.Background(), types.DevelopmentFlowInput{Org: "acme", Project: "web"})
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	require.Equal(t, "api", tasks[0].TaskID)
	require.Empty(t, tasks[0].DependsOn)
	require.Equal(t, "web", tasks[1].TaskID)
	require.Equal(t, []string{"api"}, tasks[1].DependsOn)
}

func TestPlanTasks_NilDesignReturnsEmpty(t *testing.T) {
	a := activities.New(nil, nil, nil, nil)
	tasks, err := a.PlanTasks(context.Background(), types.DevelopmentFlowInput{})
	require.NoError(t, err)
	require.Empty(t, tasks)
}

func TestDispatchTask_EnsuresWorkspaceThenDispatches(t *testing.T) {
	d := fake.NewDispatcher()
	a := activities.New(nil, nil, d, nil)
	in := types.TaskLifecycleInput{Org: "acme", Project: "web", TaskID: "T1"}

	require.NoError(t, a.DispatchTask(context.Background(), in))
	require.Equal(t, 1, d.Workspaces["acme"], "should ensure org workspace")
	require.Equal(t, 1, d.Dispatched["T1"], "should dispatch the task")
}

func TestDispatchTask_NilDispatchIsNoOp(t *testing.T) {
	a := activities.New(nil, nil, nil, nil)
	require.NoError(t, a.DispatchTask(context.Background(), types.TaskLifecycleInput{TaskID: "T1"}))
}

func TestRunGateChecks_DelegatesToChecker(t *testing.T) {
	a := activities.New(nil, &fake.Checker{Result: types.GateChecksResult{Passed: false, Detail: "tests failed"}}, nil, nil)
	res, err := a.RunGateChecks(context.Background(), types.GateChecksInput{Org: "acme", Project: "web", Stage: "design"})
	require.NoError(t, err)
	require.False(t, res.Passed)

	// No checker wired -> passes (so an auto cycle still advances pre-integration).
	noChecker := activities.New(nil, nil, nil, nil)
	res, err = noChecker.RunGateChecks(context.Background(), types.GateChecksInput{})
	require.NoError(t, err)
	require.True(t, res.Passed)
}

func TestAutoMerge_DelegatesToMerger(t *testing.T) {
	m := fake.NewMerger()
	a := activities.New(nil, nil, nil, m)
	require.NoError(t, a.AutoMerge(context.Background(), types.TaskLifecycleInput{TaskID: "T1"}))
	require.Equal(t, 1, m.Merged["T1"])
}

// Sanity that the activity name constants match what the workflows reference.
func TestActivityNameConstants(t *testing.T) {
	require.Equal(t, "PlanTasks", activities.ActivityPlanTasks)
	require.Equal(t, "RunGateChecks", activities.ActivityRunGateChecks)
	require.Equal(t, "DispatchTask", activities.ActivityDispatchTask)
	require.Equal(t, "AutoMerge", activities.ActivityAutoMerge)
	require.Equal(t, orchestration.GateAuto, orchestration.GateMode("auto"))
}
