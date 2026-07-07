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

// Package orchestration is the single source of truth for the Temporal
// workflow boundary shared by services/orchestrator (which executes the
// workflows) and services/aep-api (which starts/signals/queries them).
//
// Keeping signal/query names, the task-queue name, and workflow-ID builders
// here means there is exactly one definition of each in the repo — neither
// service can drift to a stringly-typed mismatch. This package has no Temporal
// SDK dependency on purpose: it is pure constants + helpers, importable from
// anywhere.
package orchestration

import "fmt"

// TaskQueue is the Temporal task queue the orchestrator worker polls and that
// aep-api targets when starting workflows. Same value in every environment.
const TaskQueue = "aep-orchestrator"

// Signal names for DevelopmentFlowWorkflow (the per-cycle workflow).
const (
	SignalApproveRequirements = "ApproveRequirements"
	SignalReviseRequirements  = "ReviseRequirements"
	SignalApproveDesign       = "ApproveDesign"
	SignalReviseDesign        = "ReviseDesign"
	SignalBackToRequirements  = "BackToRequirements"
	SignalBackToDesign        = "BackToDesign"
	SignalMarkComplete        = "MarkComplete"
	SignalSetGatePolicy       = "SetGatePolicy"
)

// Signal names for TaskLifecycleWorkflow (the per-task child workflow).
// These are the GitHub-webhook-driven and operator-driven transitions.
const (
	SignalPRReady            = "PRReady"
	SignalPRMerged           = "PRMerged"
	SignalPRRejected         = "PRRejected"
	SignalCodingAgentFailed  = "CodingAgentFailed"
	SignalBuildStarted       = "BuildStarted"
	SignalBuildSucceeded     = "BuildSucceeded"
	SignalBuildFailed        = "BuildFailed"
	SignalDeployStarted      = "DeployStarted"
	SignalDeploySucceeded    = "DeploySucceeded"
	SignalDeployFailed       = "DeployFailed"
	SignalVerificationFailed = "VerificationFailed"
	SignalRetry              = "Retry"
	SignalOrgDisconnected    = "OrgDisconnected"
)

// Query names. Read-only; return the current durable position.
const (
	QueryGetCycleState = "GetCycleState"
	QueryGetTaskState  = "GetTaskState"
)

// Phase is the DevelopmentFlowWorkflow position.
type Phase string

const (
	PhaseRequirements Phase = "requirements"
	PhaseDesign       Phase = "design"
	PhaseImplement    Phase = "implement"
	PhaseMerge        Phase = "merge"
	PhaseComplete     Phase = "complete"
)

// TaskStatus is the TaskLifecycleWorkflow position.
type TaskStatus string

const (
	TaskInProgress         TaskStatus = "in_progress"
	TaskReadyForReview     TaskStatus = "ready_for_review"
	TaskMerged             TaskStatus = "merged"
	TaskBuilding           TaskStatus = "building"
	TaskBuilt              TaskStatus = "built"     // build done; deploy command issued, awaiting deploy start
	TaskDeploying          TaskStatus = "deploying" // deploy started, in progress
	TaskDeployed           TaskStatus = "deployed"
	TaskRejected           TaskStatus = "rejected"
	TaskFailed             TaskStatus = "failed"
	TaskAbandoned          TaskStatus = "abandoned"
	TaskVerificationFailed TaskStatus = "verification_failed"
)

// GateMode controls how a DevelopmentFlow stage gate is satisfied.
type GateMode string

const (
	// GateHuman waits for the user to send the stage's Approve signal.
	GateHuman GateMode = "human"
	// GateAuto advances after an automated checks activity passes
	// (tests/lint/agent self-review); never a blind skip.
	GateAuto GateMode = "auto"
)

// CycleSource records how a cycle was started.
type CycleSource string

const (
	SourceRequirement CycleSource = "requirement"
	SourceIssue       CycleSource = "issue"
)

// DevFlowWorkflowID is the canonical Temporal workflow ID for a development
// cycle. Org is encoded for tenant isolation; the deterministic shape gives
// free dedup (a retried trigger hits WorkflowExecutionAlreadyStarted).
func DevFlowWorkflowID(org, project, cycle string) string {
	return fmt.Sprintf("devflow:%s:%s:%s", org, project, cycle)
}

// TaskWorkflowID is the canonical Temporal workflow ID for a task's lifecycle
// child workflow. aep-api routes a GitHub webhook to the exact task by building
// this ID from the org/project/task carried on the PR/branch.
func TaskWorkflowID(org, project, task string) string {
	return fmt.Sprintf("task:%s:%s:%s", org, project, task)
}
