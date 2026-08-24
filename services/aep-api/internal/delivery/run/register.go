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

package run

import (
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// Register puts the three run workflows on a Temporal worker.
//
// THREE RegisterWorkflow calls and exactly ONE RegisterActivity, and the
// asymmetry is forced. Temporal registers an activity by its reflected METHOD
// NAME, so two activity structs sharing any method name — and three structs
// carved out of one loop would share a great many — panic the worker at Start,
// which is a boot-time crash with a stack trace that names neither workflow. One
// Activities struct with three workflows taking method expressions off it is the
// only shape that cannot break that way, whatever gets added later.
//
// It is a function rather than a worker of its own because a task queue must be
// served by ONE worker that knows every workflow on it: a second worker polling
// the same queue with a disjoint registration would fail whichever tasks it
// picked up by accident. The three workflows therefore share one queue, and
// WorkerWatcher owns it.
func Register(wk worker.Worker, acts *Activities) {
	wk.RegisterWorkflowWithOptions(DevRunWorkflow,
		workflow.RegisterOptions{Name: delivery.DevRunWorkflowName})
	wk.RegisterWorkflowWithOptions(ValidationRunWorkflow,
		workflow.RegisterOptions{Name: delivery.ValidationRunWorkflowName})
	wk.RegisterWorkflowWithOptions(TaskRunWorkflow,
		workflow.RegisterOptions{Name: delivery.TaskRunWorkflowName})
	wk.RegisterActivity(acts)
}
