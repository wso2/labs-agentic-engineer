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

package services

// task_state.go is now a thin re-export shim. The canonical task state machine
// (TaskStatus, TaskEvent, ApplyTaskEvent, EventCause, the transition table)
// moved to internal/contracts — the dependency-free algebra layer (§6.9). These
// re-exports keep services.* references compiling for the not-yet-extracted
// consumers (the webhook projector + watchers + dispatch). The shim is deleted
// at the `task` phase, when the projector moves and references contracts directly.

import "github.com/wso2/asdlc/asdlc-service/internal/contracts"

// TaskEvent re-exports contracts.TaskEvent.
type TaskEvent = contracts.TaskEvent

const (
	TaskEventDispatchSuccess         = contracts.TaskEventDispatchSuccess
	TaskEventPRReady                 = contracts.TaskEventPRReady
	TaskEventPRMerged                = contracts.TaskEventPRMerged
	TaskEventPRRejected              = contracts.TaskEventPRRejected
	TaskEventPushMatched             = contracts.TaskEventPushMatched
	TaskEventBuildSucceeded          = contracts.TaskEventBuildSucceeded
	TaskEventBuildFailed             = contracts.TaskEventBuildFailed
	TaskEventOrgDisconnected         = contracts.TaskEventOrgDisconnected
	TaskEventRepoUnselected          = contracts.TaskEventRepoUnselected
	TaskEventBuildAuthRetryExhausted = contracts.TaskEventBuildAuthRetryExhausted
	TaskEventCodingAgentFailed       = contracts.TaskEventCodingAgentFailed
	TaskEventBuildPathMismatch       = contracts.TaskEventBuildPathMismatch
	TaskEventVerificationFailed      = contracts.TaskEventVerificationFailed
	TaskEventRetry                   = contracts.TaskEventRetry
)

// ErrInvalidTransition re-exports contracts.ErrInvalidTransition (errors.Is-able
// across the boundary — same sentinel value).
var ErrInvalidTransition = contracts.ErrInvalidTransition

// ApplyTaskEvent re-exports the pure transition function.
var ApplyTaskEvent = contracts.ApplyTaskEvent

// EventCause re-exports the terminal-cause lookup.
var EventCause = contracts.EventCause
