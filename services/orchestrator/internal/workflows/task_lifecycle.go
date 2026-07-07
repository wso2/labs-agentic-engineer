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

package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/types"
)

// Activity names (duplicated as local consts so this deterministic package does
// not import the activities package; values must match activities.Activity*).
const (
	activityDispatchTask   = "DispatchTask"
	activityDispatchDeploy = "DispatchDeploy"
	activityAutoMerge      = "AutoMerge"
)

// TaskLifecycleWorkflow is the durable state machine for one task. GitHub
// webhooks arrive (via aep-api) as signals that drive the transitions:
//
//	in_progress -> ready_for_review -> merged -> building -> deployed
//
// plus terminal rejected/failed/abandoned, and verification_failed which awaits
// a Retry signal back to in_progress. In `auto` code-review mode it fires an
// auto-merge activity on ready_for_review (the merge returns as PRMerged).
func TaskLifecycleWorkflow(ctx workflow.Context, in types.TaskLifecycleInput) (types.TaskStateView, error) {
	logger := workflow.GetLogger(ctx)
	state := types.TaskStateView{TaskID: in.TaskID, Status: orchestration.TaskInProgress}

	if err := workflow.SetQueryHandler(ctx, orchestration.QueryGetTaskState, func() (types.TaskStateView, error) {
		return state, nil
	}); err != nil {
		return state, err
	}

	dispatch := func() {
		ao := workflow.ActivityOptions{
			StartToCloseTimeout: 10 * time.Minute,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
		}
		// Best-effort: a dispatch failure surfaces via signals (CodingAgentFailed).
		_ = workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, ao), activityDispatchTask, in).Get(ctx, nil)
	}

	dispatch() // kick off the coding-agent run

	prReady := workflow.GetSignalChannel(ctx, orchestration.SignalPRReady)
	prMerged := workflow.GetSignalChannel(ctx, orchestration.SignalPRMerged)
	prRejected := workflow.GetSignalChannel(ctx, orchestration.SignalPRRejected)
	codingAgentFailed := workflow.GetSignalChannel(ctx, orchestration.SignalCodingAgentFailed)
	buildStarted := workflow.GetSignalChannel(ctx, orchestration.SignalBuildStarted)
	buildSucceeded := workflow.GetSignalChannel(ctx, orchestration.SignalBuildSucceeded)
	buildFailed := workflow.GetSignalChannel(ctx, orchestration.SignalBuildFailed)
	deployStarted := workflow.GetSignalChannel(ctx, orchestration.SignalDeployStarted)
	deploySucceeded := workflow.GetSignalChannel(ctx, orchestration.SignalDeploySucceeded)
	deployFailed := workflow.GetSignalChannel(ctx, orchestration.SignalDeployFailed)
	verificationFailed := workflow.GetSignalChannel(ctx, orchestration.SignalVerificationFailed)
	retry := workflow.GetSignalChannel(ctx, orchestration.SignalRetry)
	orgDisconnected := workflow.GetSignalChannel(ctx, orchestration.SignalOrgDisconnected)

	set := func(s orchestration.TaskStatus) { state.Status = s }

	for {
		switch state.Status {
		case orchestration.TaskInProgress:
			sel := workflow.NewSelector(ctx)
			sel.AddReceive(prReady, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskReadyForReview) })
			sel.AddReceive(prRejected, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskRejected) })
			sel.AddReceive(codingAgentFailed, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskFailed) })
			sel.AddReceive(verificationFailed, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskVerificationFailed) })
			sel.AddReceive(orgDisconnected, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskAbandoned) })
			sel.Select(ctx)

		case orchestration.TaskVerificationFailed:
			retry.Receive(ctx, nil)
			logger.Info("task retried after verification failure", "taskID", in.TaskID)
			dispatch()
			set(orchestration.TaskInProgress)

		case orchestration.TaskReadyForReview:
			if in.CodeReview == orchestration.GateAuto {
				ao := workflow.ActivityOptions{
					StartToCloseTimeout: 5 * time.Minute,
					RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
				}
				_ = workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, ao), activityAutoMerge, in).Get(ctx, nil)
			}
			sel := workflow.NewSelector(ctx)
			sel.AddReceive(prMerged, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskMerged) })
			sel.AddReceive(prRejected, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskRejected) })
			sel.AddReceive(orgDisconnected, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskAbandoned) })
			sel.Select(ctx)

		case orchestration.TaskMerged:
			// Build runs on Argo (dispatched in O4); here we await its progress.
			buildStarted.Receive(ctx, nil)
			set(orchestration.TaskBuilding)

		case orchestration.TaskBuilding:
			sel := workflow.NewSelector(ctx)
			sel.AddReceive(buildSucceeded, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskBuilt) })
			sel.AddReceive(buildFailed, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskFailed) })
			sel.Select(ctx)

		case orchestration.TaskBuilt:
			// Build succeeded — issue the deploy command, then await deploy start.
			ao := workflow.ActivityOptions{
				StartToCloseTimeout: 10 * time.Minute,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
			}
			_ = workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, ao), activityDispatchDeploy, in).Get(ctx, nil)
			sel := workflow.NewSelector(ctx)
			sel.AddReceive(deployStarted, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskDeploying) })
			sel.AddReceive(deployFailed, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskFailed) })
			sel.Select(ctx)

		case orchestration.TaskDeploying:
			sel := workflow.NewSelector(ctx)
			sel.AddReceive(deploySucceeded, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskDeployed) })
			sel.AddReceive(deployFailed, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); set(orchestration.TaskFailed) })
			sel.Select(ctx)

		case orchestration.TaskDeployed:
			logger.Info("task deployed", "taskID", in.TaskID)
			return state, nil

		case orchestration.TaskRejected, orchestration.TaskFailed, orchestration.TaskAbandoned:
			logger.Info("task terminal", "taskID", in.TaskID, "status", state.Status)
			return state, nil
		}
	}
}
