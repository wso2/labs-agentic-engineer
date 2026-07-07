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

import "github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/types"

// componentsToTasks maps design components to the implement DAG. Today it is one
// task per component (TaskID = component name) preserving the dependency edges;
// it is the single, pure, unit-tested place to evolve task derivation (e.g.
// multiple tasks per component) without touching workflow code.
func componentsToTasks(components []ComponentSpec) []types.TaskSpec {
	tasks := make([]types.TaskSpec, 0, len(components))
	for _, c := range components {
		deps := make([]string, len(c.DependsOn))
		copy(deps, c.DependsOn)
		tasks = append(tasks, types.TaskSpec{
			TaskID:        c.Name,
			ComponentName: c.Name,
			DependsOn:     deps,
		})
	}
	return tasks
}
