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

// Package types re-exports the workflow-boundary DTOs from the shared
// orchestration contract module as local aliases, so this service's workflow
// and activity code can use short names (types.DevelopmentFlowInput) while the
// single source of truth stays in packages/contracts/orchestration — importable
// by aep-api without touching this internal package. Enum values also come from
// the shared contract module.
package types

import "github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"

type (
	GatePolicy           = orchestration.GatePolicy
	DevelopmentFlowInput = orchestration.DevelopmentFlowInput
	TaskSpec             = orchestration.TaskSpec
	TaskLifecycleInput   = orchestration.TaskLifecycleInput
	GateChecksInput      = orchestration.GateChecksInput
	GateChecksResult     = orchestration.GateChecksResult
	GateStatus           = orchestration.GateStatus
	TaskStateView        = orchestration.TaskStateView
	CycleStateView       = orchestration.CycleStateView
)
