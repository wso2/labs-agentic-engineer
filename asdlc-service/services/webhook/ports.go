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

package webhook

import (
	"context"

	"github.com/wso2/asdlc/asdlc-service/models"
)

// BuildOps is the auth-retry surface webhook triggers: the build watcher
// re-mints a build token and recreates the WorkflowRun when a build failed on
// an expired/invalid token. (Merge → build dispatch now lives in the task
// feature, off pull_request.closed, so it's no longer part of this port.)
// services.WorkflowRunService satisfies it structurally (superset); wired at
// the composition root so webhook needn't import services.
type BuildOps interface {
	RetryAuthFailedBuild(ctx context.Context, task *models.ComponentTask) (runName string, err error)
}

// OnHoldDispatcher re-dispatches a project's deferred (on_hold) tasks when an
// upstream dependency deploys. Returns the count dispatched (the on_hold
// watcher only logs len(results)). A composition-root closure adapts
// services.DispatchService.DispatchTasks (which returns []DispatchResult).
type OnHoldDispatcher func(ctx context.Context, orgID, projectID string) (dispatched int, err error)
