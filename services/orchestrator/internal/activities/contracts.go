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

package activities

import (
	"context"

	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/types"
)

// The interfaces below are the activity layer's dependencies — the ports the
// real adapters implement. Adapters land when their backing services/clients are
// ported (database client, packages/clients for k8s/OpenChoreo, a GitHub client);
// until then the composition root wires no-ops and tests wire in-memory fakes.
// All implementations must be idempotent (Temporal retries activities, ADR-0004).

// ComponentSpec is a component from the approved design, with its dependency edges.
type ComponentSpec struct {
	Name      string
	DependsOn []string
}

// DesignReader reads the approved design for a project (database/git-backed).
type DesignReader interface {
	Components(ctx context.Context, org, project string) ([]ComponentSpec, error)
}

// GateChecker runs the automated gate checks for a stage in `auto` mode
// (tests / lint / agent self-review).
type GateChecker interface {
	RunChecks(ctx context.Context, in types.GateChecksInput) (types.GateChecksResult, error)
}

// TaskDispatcher provisions the per-org workspace and dispatches a task's
// coding-agent run as a k8s Job (via packages/clients in O4-real).
type TaskDispatcher interface {
	// EnsureOrgWorkspace ensures the per-org namespace (wc-<org>-remote-worker)
	// and its ResourceQuota/LimitRange exist. Idempotent.
	EnsureOrgWorkspace(ctx context.Context, org string) error
	// DispatchTask creates the coding-agent Job for the task. Idempotent: a Job
	// for the same task+commit must not be created twice.
	DispatchTask(ctx context.Context, in types.TaskLifecycleInput) error
	// DeployTask issues the deploy command for the task's built artifact (e.g.
	// an Argo deploy WorkflowRun / OpenChoreo release). Idempotent.
	DeployTask(ctx context.Context, in types.TaskLifecycleInput) error
}

// PRMerger merges a task's PR in `auto` code-review mode (GitHub-backed).
type PRMerger interface {
	// MergePR merges the task's PR. Idempotent: a no-op if already merged.
	MergePR(ctx context.Context, in types.TaskLifecycleInput) error
}
