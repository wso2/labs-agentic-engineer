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

package task

import (
	"context"

	"github.com/wso2/asdlc/asdlc-service/internal/contracts"
)

// Consumer ports for task's HTTP edge (task_controller). §4 rule 1: the
// consumer declares the narrow interface it calls. codingagent's
// DispatchService / ProgressService structurally satisfy these (their DTOs
// were hoisted to contracts), so the task feature drives coding-agent dispatch
// and progress WITHOUT importing codingagent concretely — which is what keeps
// the task↔codingagent cycle cut (arch_test.TestTaskCodingagentCycleBroken).
// The compile-time `var _` satisfaction assertion lives at the composition
// root, not here, so this package never imports codingagent.

// TaskDispatcher is the coding-agent dispatch surface the task controller
// invokes (dispatch, agent-driven verification failure, operator retry).
type TaskDispatcher interface {
	DispatchTasks(ctx context.Context, orgID, projectID string) ([]contracts.DispatchResult, error)
	MarkVerificationFailed(ctx context.Context, taskID, diagnostic string) error
	RetryTask(ctx context.Context, taskID string) (contracts.DispatchResult, error)
}

// ProgressReader is the /progress/* read surface the task controller invokes.
// Errors surface as contracts.ErrProgressUnavailable when Observer is down.
type ProgressReader interface {
	GetAgentProgress(ctx context.Context, taskID string, sinceMillis int64, limit int) (*contracts.ProgressResponse, error)
	GetBuildProgress(ctx context.Context, taskID string, sinceMillis int64) (*contracts.ProgressResponse, error)
}
