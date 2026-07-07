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
	"io"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/credentials"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/models"
)

// The consumer ports the Task surface drives. Each is the narrow slice a service
// actually uses (the house pattern), so the composition root wires concrete
// providers and tests supply fakes.

// IssueClient is the GitHub issue surface: create/list/comment/edit and the
// label stamping the command + projection paths need. gitrepo.IssueService
// satisfies it.
type IssueClient interface {
	CreateIssue(ctx context.Context, orgID, projectID string, req gitrepo.CreateIssueRequest) (*gitrepo.IssueResult, error)
	ListIssues(ctx context.Context, orgID, projectID string, labels []string) ([]gitrepo.IssueInfo, error)
	CommentIssue(ctx context.Context, orgID, projectID string, number int, body string) error
	EditIssueBody(ctx context.Context, orgID, projectID string, number int, body string) error
	EditIssueTitle(ctx context.Context, orgID, projectID string, number int, title string) error
	AddLabels(ctx context.Context, orgID, projectID string, number int, labels []string) error
	RemoveLabel(ctx context.Context, orgID, projectID string, number int, label string) error
}

// RepoResolver looks up the project's git repo row (its RepoURL yields the
// owner/name and repo full name the funnel/dispatcher key on).
type RepoResolver interface {
	GetRepo(ctx context.Context, orgID, projectID string) (*models.GitRepository, error)
}

// VersionReader lists approved (tagged) spec/design versions and reads a bundle
// at a tag — the lineage stamps and the incremental-plan baseline diff (§6).
type VersionReader interface {
	ListRequirementsVersions(ctx context.Context, orgID, projectID string) ([]artifacts.RequirementsVersionInfo, error)
	ListDesignVersions(ctx context.Context, orgID, projectID string) ([]artifacts.DesignVersionInfo, error)
	// LatestDesignTag is the newest design tag name (`v<N>-<M>`) read WITHOUT a
	// network fetch — the best-effort input to the stale-design attention flag.
	// The list read path (ListDesignVersions) still fetches; this one must not,
	// so a task-list page load pays no per-read GitHub round-trip for it.
	LatestDesignTag(ctx context.Context, orgID, projectID string) string
	GetRequirementsAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
	GetDesignAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
}

// GitReader is the workspace-backed git surface the plan turn drives: the
// snapshot refs (Head), the lineage diff (Workspace.Diff between a Task's
// lineage tag and the current tag, §6), and per-op credentials.
// gitrepo.GitOpsService satisfies it.
type GitReader interface {
	Workspace() gitrepo.Workspace
	Resolver() credentials.Resolver
}

// SkillsRepoResolver ensures the org's _skills repo is provisioned (the
// task-planning flow skill is seeded there) and returns its row — the source
// of the plan turn's SkillsRef snapshot. Wired at the composition root from
// the skills feature so task holds no skills edge.
type SkillsRepoResolver func(ctx context.Context, orgID string) (*models.GitRepository, error)

// ExecutionReader is the read side of the executions rows (the platform-owned
// half), consumed org-scoped by the read path to fuse derived status. It is the
// repositories.ExecutionRepository scoped methods — the shared kernel, not the
// execution feature, so the §1 package boundary holds.
type ExecutionReader interface {
	LatestPerKindScoped(ctx context.Context, orgID, repo string, issueNumber int) (map[string]*models.Execution, error)
	LatestPerKindForRepoScoped(ctx context.Context, orgID, repo string) (map[int]map[string]*models.Execution, error)
	ListByIssueScoped(ctx context.Context, orgID, repo string, issueNumber int) ([]models.Execution, error)
}

// AnthropicKeyResolver resolves the org's effective Anthropic key. Empty key +
// nil error means "org has none" → the plan turn raises ErrNoAnthropicKey.
type AnthropicKeyResolver func(ctx context.Context, orgID string) (string, error)

// TurnClient is the agents-service turn client — the plan turn POSTs a
// toolset:"task-plan" turn and streams raw StreamPart frames back for the tap.
type TurnClient interface {
	Turn(ctx context.Context, conversationID, orgID, anthropicKey string, req agentsvc.TurnRequest) (io.ReadCloser, error)
}

// RepoLocator resolves a GitHub "owner/name" to the owning org + project — the
// issues.* webhook delivers a repo full name; the handlers need org+project to
// drive the org-scoped issue ops. Wired from repositories at the composition
// root (the same lookup feature/execution uses).
type RepoLocator interface {
	ByFullName(ctx context.Context, fullName string) (orgID, projectID string, err error)
}
