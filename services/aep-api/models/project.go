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
	// RepoURL is the clone URL of the project's git repo, joined from the
	// BFF's own git_repositories rows on list reads (#108); absent when no
	// repo is provisioned.
	RepoURL string `json:"repoUrl,omitempty" doc:"Clone URL of the project's Git repository; absent when no repo is provisioned."`
}

type ProjectList struct {
	Items []Project `json:"items"`
	// NextCursor is the OpenChoreo continuation token for the next page,
	// surfaced verbatim; empty/absent on the last page. The console pages on it.
	NextCursor string `json:"nextCursor,omitempty" doc:"Cursor for the next page; absent on the last page."`
}

type CreateProjectRequest struct {
	Name               string `json:"name"`
	DisplayName        string `json:"displayName,omitempty"`
	Description        string `json:"description,omitempty"`
	DeploymentPipeline string `json:"deploymentPipeline,omitempty"`
	// Prompt is the requirement text the console's create flow captures; it
	// is accepted per the contract but not consumed until spec derivation
	// lands (issue #72).
	Prompt string `json:"prompt,omitempty" doc:"Optional requirement prompt that kicks off spec derivation."`
	// RepoName overrides the provisioned Git repository's name; defaults to
	// the project name. DNS-label slug (validated in the handler).
	RepoName string `json:"repoName,omitempty" doc:"Git repository name override; defaults to the project name."`
}

// ProjectStatus represents the computed SDLC phase and artifact states.
type ProjectStatus struct {
	Phase            string `json:"phase"`      // "no-repo", "repo-cloning", "repo-error", "prompt", "spec", "architecture", "tasks", "components"
	RepoStatus       string `json:"repoStatus"` // "", "pending", "cloning", "ready", "error"
	RepoURL          string `json:"repoUrl"`
	RepoErrorMessage string `json:"repoErrorMessage,omitempty"` // set when phase is "repo-error"
	HasSpec          bool   `json:"hasSpec"`
	HasDesign        bool   `json:"hasDesign"`
	HasTasks         bool   `json:"hasTasks"`
	SpecStatus       string `json:"specStatus"`   // "", "draft", "approved"
	DesignStatus     string `json:"designStatus"` // "", "draft", "approved"

	// CyclePhase is the project's active Temporal development-cycle position
	// (§R2.2 — split responsibility): "requirements" | "design" | "implement" |
	// "merge" | "complete" (packages/contracts/orchestration.Phase, carried as a
	// plain string so this package need not import that module). Nil when
	// orchestration is disabled or the project has no active cycle yet — Phase
	// above keeps reporting artifact facts (repo/spec/design existence)
	// unchanged either way; CyclePhase is the workflow's authoritative position
	// once a cycle exists, not a replacement for Phase.
	CyclePhase *string `json:"cyclePhase,omitempty"`
}
