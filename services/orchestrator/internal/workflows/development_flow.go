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

const (
	activityPlanTasks     = "PlanTasks"
	activityRunGateChecks = "RunGateChecks"
)

// devFlowSignals bundles the cycle-level signal channels.
type devFlowSignals struct {
	approveReq, reviseReq       workflow.ReceiveChannel
	approveDesign, reviseDesign workflow.ReceiveChannel
	backToReq, backToDesign     workflow.ReceiveChannel
	markComplete, setPolicy     workflow.ReceiveChannel
}

// DevelopmentFlowWorkflow is the durable per-cycle workflow:
//
//	requirements -> design -> implement -> merge -> complete
//
// with iterate-back edges (BackToRequirements/BackToDesign) and per-stage gate
// modes (human waits for an Approve signal; auto runs a checks activity then
// advances). IMPLEMENT spawns one TaskLifecycleWorkflow child per task, ordered
// by the dependency DAG (a dependent starts once all its deps reach deployed).
func DevelopmentFlowWorkflow(ctx workflow.Context, in types.DevelopmentFlowInput) (types.CycleStateView, error) {
	logger := workflow.GetLogger(ctx)

	state := types.CycleStateView{CycleID: in.CycleID, Phase: in.StartPhase}
	if state.Phase == "" {
		state.Phase = orchestration.PhaseRequirements
	}
	policy := in.GatePolicy

	if err := workflow.SetQueryHandler(ctx, orchestration.QueryGetCycleState, func() (types.CycleStateView, error) {
		return state, nil
	}); err != nil {
		return state, err
	}

	sig := devFlowSignals{
		approveReq:    workflow.GetSignalChannel(ctx, orchestration.SignalApproveRequirements),
		reviseReq:     workflow.GetSignalChannel(ctx, orchestration.SignalReviseRequirements),
		approveDesign: workflow.GetSignalChannel(ctx, orchestration.SignalApproveDesign),
		reviseDesign:  workflow.GetSignalChannel(ctx, orchestration.SignalReviseDesign),
		backToReq:     workflow.GetSignalChannel(ctx, orchestration.SignalBackToRequirements),
		backToDesign:  workflow.GetSignalChannel(ctx, orchestration.SignalBackToDesign),
		markComplete:  workflow.GetSignalChannel(ctx, orchestration.SignalMarkComplete),
		setPolicy:     workflow.GetSignalChannel(ctx, orchestration.SignalSetGatePolicy),
	}
	drainPolicy := func(c workflow.ReceiveChannel, _ bool) {
		var p types.GatePolicy
		c.Receive(ctx, &p)
		policy = p
	}

	for {
		switch state.Phase {
		case orchestration.PhaseRequirements:
			if policy.Requirements == orchestration.GateAuto && runGateChecks(ctx, in, "requirements") {
				state.GatesPassed.Requirements = true
				state.Phase = orchestration.PhaseDesign
				continue
			}
			sel := workflow.NewSelector(ctx)
			sel.AddReceive(sig.approveReq, func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, nil)
				state.GatesPassed.Requirements = true
				state.Phase = orchestration.PhaseDesign
			})
			sel.AddReceive(sig.reviseReq, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil) })
			sel.AddReceive(sig.setPolicy, drainPolicy)
			sel.Select(ctx)

		case orchestration.PhaseDesign:
			if policy.Design == orchestration.GateAuto && runGateChecks(ctx, in, "design") {
				state.GatesPassed.Design = true
				state.Phase = orchestration.PhaseImplement
				continue
			}
			sel := workflow.NewSelector(ctx)
			sel.AddReceive(sig.approveDesign, func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, nil)
				state.GatesPassed.Design = true
				state.Phase = orchestration.PhaseImplement
			})
			sel.AddReceive(sig.reviseDesign, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil) })
			sel.AddReceive(sig.backToReq, func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, nil)
				state.GatesPassed.Requirements = false
				state.Phase = orchestration.PhaseRequirements
			})
			sel.AddReceive(sig.setPolicy, drainPolicy)
			sel.Select(ctx)

		case orchestration.PhaseImplement:
			next := runImplement(ctx, &state, in, policy, sig, drainPolicy)
			switch next {
			case orchestration.PhaseRequirements:
				state.GatesPassed = types.GateStatus{}
			case orchestration.PhaseDesign:
				state.GatesPassed.Design = false
				state.GatesPassed.Tasks = false
			}
			state.Phase = next

		case orchestration.PhaseMerge:
			sel := workflow.NewSelector(ctx)
			sel.AddReceive(sig.markComplete, func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, nil)
				state.Phase = orchestration.PhaseComplete
			})
			sel.AddReceive(sig.backToReq, func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, nil)
				state.GatesPassed = types.GateStatus{}
				state.Phase = orchestration.PhaseRequirements
			})
			sel.AddReceive(sig.backToDesign, func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, nil)
				state.GatesPassed.Design = false
				state.GatesPassed.Tasks = false
				state.Phase = orchestration.PhaseDesign
			})
			sel.AddReceive(sig.setPolicy, drainPolicy)
			sel.Select(ctx)

		case orchestration.PhaseComplete:
			logger.Info("cycle complete", "cycle", in.CycleID)
			return state, nil
		}
	}
}

// runGateChecks runs the auto-mode gate activity for a stage; returns whether it
// passed (a failure falls back to the human wait — stop-and-surface).
func runGateChecks(ctx workflow.Context, in types.DevelopmentFlowInput, stage string) bool {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	}
	checks := types.GateChecksInput{Org: in.Org, Project: in.Project, Stage: stage}
	var res types.GateChecksResult
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, ao), activityRunGateChecks, checks).Get(ctx, &res); err != nil {
		return false
	}
	return res.Passed
}

// runImplement plans the task DAG and schedules one child workflow per task:
// independent tasks run in parallel, a dependent starts once all its deps reach
// deployed. Returns the next phase — merge on completion, or an earlier phase if
// a Back signal interrupts (cancelling in-flight children).
func runImplement(
	ctx workflow.Context,
	state *types.CycleStateView,
	in types.DevelopmentFlowInput,
	policy types.GatePolicy,
	sig devFlowSignals,
	drainPolicy func(workflow.ReceiveChannel, bool),
) orchestration.Phase {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	}
	var tasks []types.TaskSpec
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, ao), activityPlanTasks, in).Get(ctx, &tasks); err != nil {
		// Planning failed — nothing to schedule; surface as an empty implement.
		state.GatesPassed.Tasks = true
		return orchestration.PhaseMerge
	}

	pending := make(map[string]types.TaskSpec, len(tasks))
	state.Tasks = make([]types.TaskStateView, 0, len(tasks))
	for _, t := range tasks {
		pending[t.TaskID] = t
	}

	deployed := make(map[string]bool)
	type running struct {
		cancel workflow.CancelFunc
		fut    workflow.ChildWorkflowFuture
	}
	runs := make(map[string]*running)

	setStatus := func(id string, s orchestration.TaskStatus) {
		for i := range state.Tasks {
			if state.Tasks[i].TaskID == id {
				state.Tasks[i].Status = s
				return
			}
		}
		state.Tasks = append(state.Tasks, types.TaskStateView{TaskID: id, Status: s})
	}

	var back orchestration.Phase
	for back == "" && (len(runs) > 0 || canStart(pending, deployed)) {
		// Start every task whose deps are all deployed.
		for id, t := range pending {
			if depsSatisfied(t.DependsOn, deployed) {
				cctx, cancel := workflow.WithCancel(ctx)
				cwo := workflow.ChildWorkflowOptions{
					WorkflowID: orchestration.TaskWorkflowID(in.Org, in.Project, t.TaskID),
				}
				fut := workflow.ExecuteChildWorkflow(
					workflow.WithChildOptions(cctx, cwo),
					TaskLifecycleWorkflow,
					types.TaskLifecycleInput{
						Org: in.Org, Project: in.Project, TaskID: t.TaskID,
						ComponentName: t.ComponentName, CodeReview: policy.CodeReview,
					},
				)
				runs[id] = &running{cancel: cancel, fut: fut}
				setStatus(id, orchestration.TaskInProgress)
				delete(pending, id)
			}
		}

		sel := workflow.NewSelector(ctx)
		for id, r := range runs {
			id, r := id, r
			sel.AddFuture(r.fut, func(workflow.Future) {
				var res types.TaskStateView
				err := r.fut.Get(ctx, &res)
				delete(runs, id)
				if err != nil {
					setStatus(id, orchestration.TaskFailed)
					return
				}
				setStatus(id, res.Status)
				if res.Status == orchestration.TaskDeployed {
					deployed[id] = true
				}
			})
		}
		sel.AddReceive(sig.backToReq, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); back = orchestration.PhaseRequirements })
		sel.AddReceive(sig.backToDesign, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil); back = orchestration.PhaseDesign })
		sel.AddReceive(sig.setPolicy, drainPolicy)
		sel.Select(ctx)
	}

	if back != "" {
		for _, r := range runs {
			r.cancel()
		}
		return back
	}

	state.GatesPassed.Tasks = true
	return orchestration.PhaseMerge
}

// canStart reports whether any pending task's deps are all deployed.
func canStart(pending map[string]types.TaskSpec, deployed map[string]bool) bool {
	for _, t := range pending {
		if depsSatisfied(t.DependsOn, deployed) {
			return true
		}
	}
	return false
}

func depsSatisfied(deps []string, deployed map[string]bool) bool {
	for _, d := range deps {
		if !deployed[d] {
			return false
		}
	}
	return true
}
