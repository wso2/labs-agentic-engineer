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

// DispatchResult represents the outcome of dispatching a single task.
// BranchName / PullRequestURL are populated later by the
// pull_request.opened webhook handler when the agent opens its PR — they are
// not known at dispatch time.
//
// Hosted on the leaf so codingagent can RETURN it and task's HTTP edge can
// CONSUME it through a task-local TaskDispatcher port — without task importing
// the codingagent feature concretely (§4 / arch_test cycle invariant).
type DispatchResult struct {
	TaskID        string `json:"taskId"`
	ComponentName string `json:"componentName"`
	RunName       string `json:"runName,omitempty"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
}
