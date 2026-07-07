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

package workflows_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/worker"

	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/workflows"
)

// TestReplay_TaskLifecycleHappyPath replays a committed event history (captured
// from a real run) against the current workflow code. A non-deterministic edit
// to TaskLifecycleWorkflow — reordering steps, changing the activity/signal
// sequence — fails replay here, before it could break in-flight executions in
// production (ADR-0004). Regenerate the fixture only on an intentional,
// versioned change (see the capture tool referenced in the design docs).
func TestReplay_TaskLifecycleHappyPath(t *testing.T) {
	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(workflows.TaskLifecycleWorkflow)

	err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, "testdata/task_lifecycle_happy.json")
	require.NoError(t, err, "TaskLifecycleWorkflow no longer replays its recorded history — "+
		"a non-deterministic change? guard with workflow.GetVersion or regenerate the fixture")
}
