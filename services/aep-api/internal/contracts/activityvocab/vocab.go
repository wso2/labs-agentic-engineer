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

// Package activityvocab is the activity-feed event vocabulary (issue #239): the
// v1 event-type and actor-kind taxonomy shared by every producer and the store.
// It is a pure domain leaf (contracts/ convention, like taskmeta): the projects
// domain (the feed's owner) and the delivery domain (the devflow producers) both
// import it, never the reverse — the words cross domains, so they live in
// neither.
package activityvocab

// Activity event types — the v1 taxonomy (issue #239). Plain string constants:
// canonical values here, no enum type.
const (
	TypeSpecUpdated   = "spec_updated"
	TypeSpecPublished = "spec_published"
	TypePlanDerived   = "plan_derived"
	TypeTaskStarted   = "task_started"
	TypeTaskDeployed  = "task_deployed"
	TypeTaskFailed    = "task_failed"
	// TypeRunFailed — a version's run settled failed. Carries the tag, the
	// component when the fault named one, and Reason = the failure code (or
	// the terminal reason when no record exists).
	TypeRunFailed = "run_failed"

	ActorUser  = "user"
	ActorAgent = "agent"
)
