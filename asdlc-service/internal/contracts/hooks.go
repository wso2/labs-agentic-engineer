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
//
// NOTE: these interfaces are declared now (the `state-machine` phase) so the
// later phases that wire them — projector/webhook, task, codingagent — have a
// shared home to reference. Method signatures use only primitives + contracts
// types (never models structs) to keep contracts a pure leaf; they are
// PROVISIONAL and may be refined when their first real consumer/provider lands.
// Today these still flow through concrete setters (component's
// *TraitSyncService + SetTraitSync; org_disconnect's applyDisconnect func);
// the migration onto these interfaces happens in the owning phases.

// TaskTransitions is the task projector's status-write surface (the sole writer
// of ComponentTask.Status). Consumed by codingagent + webhook so they never
// import the task feature concretely; provided by task's projector
// (§4.9 status write-back).
type TaskTransitions interface {
	MarkBuilding(ctx context.Context, taskID string) error
	MarkBuilt(ctx context.Context, taskID string) error
	MarkFailed(ctx context.Context, taskID, cause string) error
}

// BuildDispatcher (a.k.a. OnTaskMerged) is the merge-time build trigger: task's
// merged-PR handler EMITS it, codingagent's WorkflowRunService PROVIDES it.
// A NEW hook — distinct from the post-deploy OnTaskDeployed cascade (§4 cycle
// proof, §6.10).
type BuildDispatcher interface {
	DispatchTaskBuild(ctx context.Context, taskID string) error
}

// DesignChanged is emitted by the design feature after a design edit and
// consumed by task to reconcile pending work — replacing today's direct
// design→task call so design never imports task (§4).
type DesignChanged interface {
	OnDesignChanged(ctx context.Context, org, projectID string) error
}
