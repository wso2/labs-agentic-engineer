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

// Package contracts holds the dependency-free algebra and cross-feature hook
// interfaces shared across the service. It is a pure leaf: it imports only the
// standard library (no models, no platform, no feature, no client, no gorm).
// models.TaskStatus and services' task-state symbols re-export from here, so
// the canonical task lifecycle lives in one acyclic place (§4.0, §6.9).
package contracts

import "errors"

// TaskStatus is the lifecycle state of a ComponentTask. Canonical definition;
// models.TaskStatus is a re-export alias of this type so the gorm model and
// every existing consumer keep working while the algebra is owned here.
type TaskStatus string

const (
	TaskStatusPending        TaskStatus = "pending"
	TaskStatusInProgress     TaskStatus = "in_progress"
	TaskStatusReadyForReview TaskStatus = "ready_for_review"
	TaskStatusMerged         TaskStatus = "merged"
	TaskStatusBuilding       TaskStatus = "building"
	TaskStatusDeployed       TaskStatus = "deployed"
	TaskStatusRejected       TaskStatus = "rejected"
	TaskStatusFailed         TaskStatus = "failed"
	// TaskStatusAbandoned is the cascade target when the org's GitHub credential
	// is disconnected, or when reach reconciliation drops the task's repo from
	// the App install. Terminal.
	TaskStatusAbandoned TaskStatus = "abandoned"
	// TaskStatusOnHold gates dispatch on un-deployed dependencies (deploy-gating).
	// Mirrors the "on_hold" GitHub Project board column.
	TaskStatusOnHold TaskStatus = "on_hold"
	// TaskStatusVerificationFailed is the state when the dispatched agent
	// reports integration verification against a dependency endpoint failed.
	// The PR stays a draft; the operator clicks Retry to re-dispatch.
	TaskStatusVerificationFailed TaskStatus = "verification_failed"
)

// IsTerminal reports whether the status is a terminal state. Terminal states
// absorb late events and reject further transitions.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskStatusDeployed, TaskStatusRejected, TaskStatusFailed, TaskStatusAbandoned:
		return true
	}
	return false
}

// TaskEvent identifies a transition trigger. Mirrors the names used by the
// webhook projector and build watcher.
type TaskEvent string

const (
	TaskEventDispatchSuccess TaskEvent = "dispatch.success"
	TaskEventPRReady         TaskEvent = "pr.ready_for_review"
	TaskEventPRMerged        TaskEvent = "pr.merged"
	TaskEventPRRejected      TaskEvent = "pr.rejected"
	TaskEventPushMatched     TaskEvent = "push.matched"
	TaskEventBuildSucceeded  TaskEvent = "build.succeeded"
	TaskEventBuildFailed     TaskEvent = "build.failed"
	// Org-disconnect and reach-reconciliation cascades. Both targets are
	// TaskStatusAbandoned; the events differ so the projector can record a
	// distinct cause for audit.
	TaskEventOrgDisconnected TaskEvent = "org.disconnected"
	TaskEventRepoUnselected  TaskEvent = "repo.unselected"
	// Git-clone auth-failure retry budget exhausted (§9.3). Drives building →
	// failed; cause column gets "build.auth_retry_exceeded".
	TaskEventBuildAuthRetryExhausted TaskEvent = "build.auth_retry_exceeded"
	// Coding-agent WorkflowRun terminated Failed/Error. Drives in_progress →
	// failed. Emitted by services/webhook/coding_agent_watcher.go.
	TaskEventCodingAgentFailed TaskEvent = "coding_agent.failed"
	// Build dispatch was skipped because the task's own merge push contained no
	// file under its design-declared appPath. Drives merged → failed.
	TaskEventBuildPathMismatch TaskEvent = "build.path_mismatch"
	// TaskEventVerificationFailed — the dispatched agent could not verify the
	// live api dependency before opening its PR. Drives in_progress →
	// verification_failed.
	TaskEventVerificationFailed TaskEvent = "agent.verification_failed"
	// TaskEventRetry — operator clicked "Retry" on a verification_failed task.
	// Drives verification_failed → in_progress.
	TaskEventRetry TaskEvent = "operator.retry"
)

// EventCause maps a TaskEvent to the value written into ComponentTask.Cause on
// a successful terminal transition. Single-source so callers don't fork the
// lookup. Empty string for events that are not terminal.
func EventCause(event TaskEvent) string {
	switch event {
	case TaskEventBuildSucceeded:
		return "build.deployed"
	case TaskEventBuildFailed:
		return "build.failed"
	case TaskEventBuildAuthRetryExhausted:
		return "build.auth_retry_exceeded"
	case TaskEventCodingAgentFailed:
		return "coding_agent.failed"
	case TaskEventPRRejected:
		return "pr.rejected"
	case TaskEventOrgDisconnected:
		// Generic; specific reasons (manual.disconnect, validator.unauthorized,
		// installation.deleted) are passed through OrgDisconnectService's
		// `cause` parameter and override this default at the call site.
		return "org.disconnected"
	case TaskEventRepoUnselected:
		return "repo.unselected"
	case TaskEventBuildPathMismatch:
		return "build.component_path_mismatch"
	case TaskEventVerificationFailed:
		// verification_failed is not terminal (retry transitions back to
		// in_progress) so this cause is recorded for audit but cleared when the
		// next dispatch lands.
		return "agent.verification_failed"
	default:
		return ""
	}
}

// stateTransition is one row of the transition table.
type stateTransition struct {
	From  TaskStatus
	To    TaskStatus
	Event TaskEvent
}

// allowedTransitions is the source of truth for the lifecycle.
//
//	pending → in_progress → ready_for_review → merged → building → deployed
//	                                         ↘ rejected
//	                    (any) ↘ failed (build)
var allowedTransitions = []stateTransition{
	{TaskStatusPending, TaskStatusInProgress, TaskEventDispatchSuccess},
	{TaskStatusInProgress, TaskStatusReadyForReview, TaskEventPRReady},
	{TaskStatusReadyForReview, TaskStatusMerged, TaskEventPRMerged},
	{TaskStatusReadyForReview, TaskStatusRejected, TaskEventPRRejected},
	{TaskStatusInProgress, TaskStatusRejected, TaskEventPRRejected},
	{TaskStatusMerged, TaskStatusBuilding, TaskEventPushMatched},
	{TaskStatusBuilding, TaskStatusDeployed, TaskEventBuildSucceeded},
	{TaskStatusBuilding, TaskStatusFailed, TaskEventBuildFailed},
	// Org disconnect cascade. All non-terminal statuses transition to abandoned.
	// (Once a task is in 'building', the build runs to completion under whatever
	// credential it captured.)
	{TaskStatusPending, TaskStatusAbandoned, TaskEventOrgDisconnected},
	{TaskStatusInProgress, TaskStatusAbandoned, TaskEventOrgDisconnected},
	{TaskStatusReadyForReview, TaskStatusAbandoned, TaskEventOrgDisconnected},
	// Reach-reconciliation cascade (App mode only).
	{TaskStatusPending, TaskStatusAbandoned, TaskEventRepoUnselected},
	{TaskStatusInProgress, TaskStatusAbandoned, TaskEventRepoUnselected},
	{TaskStatusReadyForReview, TaskStatusAbandoned, TaskEventRepoUnselected},
	// Build watcher auth-retry budget exhausted (§9.3).
	{TaskStatusBuilding, TaskStatusFailed, TaskEventBuildAuthRetryExhausted},
	// Coding-agent WorkflowRun terminated Failed/Error.
	{TaskStatusInProgress, TaskStatusFailed, TaskEventCodingAgentFailed},
	// Push containing this task's own merge SHA arrived but matched no file
	// under the design-declared appPath — build was never dispatched.
	{TaskStatusMerged, TaskStatusFailed, TaskEventBuildPathMismatch},
	// Agent's pre-PR integration verification failed.
	{TaskStatusInProgress, TaskStatusVerificationFailed, TaskEventVerificationFailed},
	// Operator retry.
	{TaskStatusVerificationFailed, TaskStatusInProgress, TaskEventRetry},
}

// ErrInvalidTransition is returned by ApplyTaskEvent when the current status
// doesn't allow the requested event. The projector treats it as a no-op.
var ErrInvalidTransition = errors.New("invalid task state transition")

// ApplyTaskEvent computes the next status given the current state and an event.
// Returns ErrInvalidTransition if the transition isn't in the table.
//
// Pure function — no I/O, no logging. Callers that need to react to invalid
// transitions check errors.Is(err, ErrInvalidTransition).
func ApplyTaskEvent(current TaskStatus, event TaskEvent) (TaskStatus, error) {
	if current.IsTerminal() {
		return current, ErrInvalidTransition
	}
	for _, t := range allowedTransitions {
		if t.From == current && t.Event == event {
			return t.To, nil
		}
	}
	return current, ErrInvalidTransition
}
