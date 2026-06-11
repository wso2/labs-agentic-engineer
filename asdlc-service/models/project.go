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

package models

type Project struct {
	UID                string `json:"uid,omitempty"`
	Name               string `json:"name"`
	NamespaceName      string `json:"namespaceName,omitempty"`
	DisplayName        string `json:"displayName,omitempty"`
	Description        string `json:"description,omitempty"`
	DeploymentPipeline string `json:"deploymentPipeline,omitempty"`
	CreatedAt          string `json:"createdAt,omitempty"`
	Status             string `json:"status,omitempty"`
}

type ProjectList struct {
	Items []Project `json:"items"`
}

type CreateProjectRequest struct {
	Name               string `json:"name"`
	DisplayName        string `json:"displayName,omitempty"`
	Description        string `json:"description,omitempty"`
	DeploymentPipeline string `json:"deploymentPipeline,omitempty"`
}

// ProjectStatus represents the computed SDLC phase and artifact states.
type ProjectStatus struct {
	Phase        string `json:"phase"`        // "no-repo", "repo-cloning", "prompt", "spec", "architecture", "tasks", "components"
	RepoStatus   string `json:"repoStatus"`   // "", "pending", "cloning", "ready", "error"
	RepoURL      string `json:"repoUrl"`
	HasSpec      bool   `json:"hasSpec"`
	HasDesign    bool   `json:"hasDesign"`
	HasTasks     bool   `json:"hasTasks"`
	SpecStatus   string `json:"specStatus"`   // "", "draft", "approved"
	DesignStatus string `json:"designStatus"` // "", "draft", "approved"
}
