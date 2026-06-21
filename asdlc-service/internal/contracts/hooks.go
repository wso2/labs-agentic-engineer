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

package contracts

import "context"

// Cross-feature lifecycle hooks (§4 cycle proof, §6.10 wire map). Each hook
// type is OWNED BY NEITHER its emitter nor its provider — it lives here so the
// package that declares it is imported by neither side's concrete package,
// which is what keeps the design↔task and task↔codingagent edges acyclic.
// They are wired (emitter EMITS, provider PROVIDES) at the composition root.

// TaskTransitions is the task projector's status-write surface (sole writer of
// ComponentTask.Status). Provided by task's projector; consumed by codingagent
// (build dispatch + watchers) so they never import the task feature concretely.
type TaskTransitions interface {
	MarkBuilding(ctx context.Context, taskID, sha, runName string) error
	ApplyBuildResult(ctx context.Context, taskID string, event TaskEvent, errMsg string) error
}
