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

// Package activities holds the orchestrator's Temporal activities — all side
// effects live here, never in the workflow packages (determinism boundary,
// ADR-0004). The real work is delegated to injected dependency interfaces
// (contracts.go); the methods nil-guard each dependency so a zero-value
// Activities is a valid no-op (used by the worker until real adapters are wired,
// and by workflow tests that mock specific activities).
package activities

import (
	"context"

	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/types"
)

// Activity names, referenced by the workflows via string so the (deterministic)
// workflows package never imports this one.
const (
	ActivityPlanTasks      = "PlanTasks"
	ActivityRunGateChecks  = "RunGateChecks"
	ActivityDispatchTask   = "DispatchTask"
	ActivityDispatchDeploy = "DispatchDeploy"
	ActivityAutoMerge      = "AutoMerge"
)

// Activities is the receiver for all orchestrator activity methods. Its fields
// are the injected dependencies; any left nil makes the corresponding activity a
// safe no-op. Wire real adapters via the composition root (internal/deps).
type Activities struct {
	Design   DesignReader
	Checker  GateChecker
	Dispatch TaskDispatcher
	Merger   PRMerger
}

// New builds an Activities with the given dependencies. Pass nil for any not yet
// available (no-op until its adapter is ported in O4-real).
func New(design DesignReader, checker GateChecker, dispatch TaskDispatcher, merger PRMerger) *Activities {
	return &Activities{Design: design, Checker: checker, Dispatch: dispatch, Merger: merger}
}

// PlanTasks derives the implement DAG from the approved design.
func (a *Activities) PlanTasks(ctx context.Context, in types.DevelopmentFlowInput) ([]types.TaskSpec, error) {
	if a == nil || a.Design == nil {
		return nil, nil
	}
	components, err := a.Design.Components(ctx, in.Org, in.Project)
	if err != nil {
		return nil, err
	}
	return componentsToTasks(components), nil
}

// RunGateChecks runs the automated gate (auto mode). No-op pass when no checker
// is wired, so an auto cycle still advances in a not-yet-integrated environment.
func (a *Activities) RunGateChecks(ctx context.Context, in types.GateChecksInput) (types.GateChecksResult, error) {
	if a == nil || a.Checker == nil {
		return types.GateChecksResult{Passed: true}, nil
	}
	return a.Checker.RunChecks(ctx, in)
}

// DispatchTask ensures the per-org workspace, then dispatches the coding-agent
// Job for the task. Both steps are idempotent.
func (a *Activities) DispatchTask(ctx context.Context, in types.TaskLifecycleInput) error {
	if a == nil || a.Dispatch == nil {
		return nil
	}
	if err := a.Dispatch.EnsureOrgWorkspace(ctx, in.Org); err != nil {
		return err
	}
	return a.Dispatch.DispatchTask(ctx, in)
}

// DispatchDeploy issues the deploy command for a task whose build succeeded.
func (a *Activities) DispatchDeploy(ctx context.Context, in types.TaskLifecycleInput) error {
	if a == nil || a.Dispatch == nil {
		return nil
	}
	return a.Dispatch.DeployTask(ctx, in)
}

// AutoMerge merges the task's PR in auto code-review mode.
func (a *Activities) AutoMerge(ctx context.Context, in types.TaskLifecycleInput) error {
	if a == nil || a.Merger == nil {
		return nil
	}
	return a.Merger.MergePR(ctx, in)
}
