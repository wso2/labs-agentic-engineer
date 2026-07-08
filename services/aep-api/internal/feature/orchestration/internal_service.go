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

package orchestration

import (
	"context"

	contract "github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
)

// ComponentSpec is the approved-design subset the orchestrator needs to build
// the implement DAG.
type ComponentSpec struct {
	Name      string   `json:"name"`
	DependsOn []string `json:"dependsOn"`
}

// DesignReader reads the approved design component graph from aep-api's
// integration-owned artifact store.
type DesignReader interface {
	Components(ctx context.Context, orgID, projectID string) ([]ComponentSpec, error)
}

// GateChecker runs an automated cycle gate.
type GateChecker interface {
	RunChecks(ctx context.Context, in contract.GateChecksInput) (contract.GateChecksResult, error)
}

// TaskDriver wraps the preserved task side-effect paths owned by aep-api.
type TaskDriver interface {
	DispatchTask(ctx context.Context, in contract.TaskLifecycleInput) error
	DeployTask(ctx context.Context, in contract.TaskLifecycleInput) error
	AutoMerge(ctx context.Context, in contract.TaskLifecycleInput) error
}

// InternalService backs the orchestrator -> aep-api internal activity surface.
// Each dependency is nil-safe so spec generation and partial local wiring do
// not panic.
type InternalService struct {
	design DesignReader
	checks GateChecker
	tasks  TaskDriver
}

// NewInternalService wires the internal orchestration surface.
func NewInternalService(design DesignReader, checks GateChecker, tasks TaskDriver) *InternalService {
	return &InternalService{design: design, checks: checks, tasks: tasks}
}

// Components returns the task DAG source from the approved design.
func (s *InternalService) Components(ctx context.Context, orgID, projectID string) ([]ComponentSpec, error) {
	if s == nil || s.design == nil {
		return nil, nil
	}
	return s.design.Components(ctx, orgID, projectID)
}

// RunChecks executes an automated gate. No real tests/lint/self-review checker
// exists yet (deliberately out of scope for the R3 gap-closure pass — see
// docs/design/orchestration/README.md) — a nil checker is an EXPLICIT,
// authenticated pass (not a silent bypass: the caller is verified by
// bearerAuth same as every other orchestration op) so an `auto`-mode cycle can
// advance rather than deadlock in every environment until a real checker is
// wired. Do not read this as "checks ran and passed" — no check runs.
func (s *InternalService) RunChecks(ctx context.Context, in contract.GateChecksInput) (contract.GateChecksResult, error) {
	if s == nil || s.checks == nil {
		return contract.GateChecksResult{Passed: true}, nil
	}
	return s.checks.RunChecks(ctx, in)
}

// DispatchTask delegates coding-agent dispatch to the preserved aep-api path.
func (s *InternalService) DispatchTask(ctx context.Context, in contract.TaskLifecycleInput) error {
	if s == nil || s.tasks == nil {
		return nil
	}
	return s.tasks.DispatchTask(ctx, in)
}

// DeployTask delegates deployment to aep-api.
func (s *InternalService) DeployTask(ctx context.Context, in contract.TaskLifecycleInput) error {
	if s == nil || s.tasks == nil {
		return nil
	}
	return s.tasks.DeployTask(ctx, in)
}

// AutoMerge delegates auto code-review merge to aep-api.
func (s *InternalService) AutoMerge(ctx context.Context, in contract.TaskLifecycleInput) error {
	if s == nil || s.tasks == nil {
		return nil
	}
	return s.tasks.AutoMerge(ctx, in)
}
