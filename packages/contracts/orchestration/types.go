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

// DTOs crossing the workflow boundary. They live in the shared contract module
// (not the orchestrator's internal package) so aep-api can build workflow
// inputs and decode query results without importing the orchestrator's
// internals. The orchestrator's internal/types package re-exports these as
// aliases so its workflow/activity code keeps its short local names.

// GatePolicy is the per-stage gate mode for a cycle (human | auto).
type GatePolicy struct {
	Requirements GateMode
	Design       GateMode
	CodeReview   GateMode
}

// DevelopmentFlowInput starts a cycle. StartPhase lets an issue fast-path to
// implement (skipping requirements/design).
type DevelopmentFlowInput struct {
	Org        string
	Project    string
	CycleID    string
	Source     CycleSource
	StartPhase Phase
	GatePolicy GatePolicy
}

// TaskSpec is a unit of work in the implement DAG. DependsOn lists task IDs that
// must reach `deployed` before this task is dispatched.
type TaskSpec struct {
	TaskID        string
	ComponentName string
	DependsOn     []string
}

// TaskLifecycleInput starts a per-task child workflow.
type TaskLifecycleInput struct {
	Org           string
	Project       string
	TaskID        string
	ComponentName string
	CodeReview    GateMode
}

// GateChecksInput is the input to the RunGateChecks activity (auto gate).
type GateChecksInput struct {
	Org     string
	Project string
	Stage   string
}

// GateChecksResult is returned by the RunGateChecks activity (auto gate).
type GateChecksResult struct {
	Passed bool
	Detail string
}

// GateStatus mirrors which approval gates a cycle has passed.
type GateStatus struct {
	Requirements bool
	Design       bool
	Tasks        bool
}

// TaskStateView is a task's durable position (GetTaskState query result; also a
// TaskLifecycleWorkflow's return value).
type TaskStateView struct {
	TaskID string
	Status TaskStatus
}

// CycleStateView is a cycle's durable position (GetCycleState query result; also
// a DevelopmentFlowWorkflow's return value).
type CycleStateView struct {
	CycleID     string
	Phase       Phase
	GatesPassed GateStatus
	Tasks       []TaskStateView
}
