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
	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The consumer ports the Task surface drives. Each is the narrow slice a service
// actually uses (the house pattern), so the composition root wires concrete
// providers and tests supply fakes.

// IssueClient is the GitHub issue surface: create/list/comment/edit and the
// label stamping the command + projection paths need. sourcecontrol.IssueService
// satisfies it.
type IssueClient interface {
	CreateIssue(ctx context.Context, orgID, projectID string, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error)
	ListIssues(ctx context.Context, orgID, projectID string, labels []string) ([]sourcecontrol.IssueInfo, error)
	// GetIssue fetches one issue by number (O(1)); returns sourcecontrol.ErrIssueNotFound
	// when it doesn't exist. Preferred over ListIssues when the number is known.
	GetIssue(ctx context.Context, orgID, projectID string, number int) (*sourcecontrol.IssueInfo, error)
	CommentIssue(ctx context.Context, orgID, projectID string, number int, body string) error
	EditIssueBody(ctx context.Context, orgID, projectID string, number int, body string) error
	EditIssueTitle(ctx context.Context, orgID, projectID string, number int, title string) error
	AddLabels(ctx context.Context, orgID, projectID string, number int, labels []string) error
	RemoveLabel(ctx context.Context, orgID, projectID string, number int, label string) error
}

// RepoResolver looks up the project's git repo row (its RepoURL yields the
// owner/name and repo full name the funnel/dispatcher key on).
type RepoResolver interface {
	GetRepo(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error)
}

// ComponentEnsurer idempotently provisions the OpenChoreo Component CR for a
// design component, erroring if no design component exists by that name.
// component.ComponentService satisfies it. PromoteAndExecute calls it
// synchronously before promoting an ad-hoc issue into a Task, so an unknown
// componentName (e.g. a caller's prefix-stripping bug) fails the dispatch
// call itself instead of only surfacing inside the funnel's async goroutine.
type ComponentEnsurer interface {
	EnsureComponent(ctx context.Context, orgID, projectID, componentName string) error
}

// VersionReader lists approved (tagged) spec/design versions and reads a bundle
// at a tag — the lineage stamps and the incremental-plan baseline diff (§6).
type VersionReader interface {
	ListRequirementsVersions(ctx context.Context, orgID, projectID string) ([]spec.RequirementsVersionInfo, error)
	// LatestSpecTag is the newest spec tag name (`v<N>`) read WITHOUT a
	// network fetch — the best-effort input to the stale-spec attention flag.
	// The list read path (ListRequirementsVersions) still fetches; this one
	// must not, so a task-list page load pays no per-read GitHub round-trip.
	LatestSpecTag(ctx context.Context, orgID, projectID string) string
	GetRequirementsAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
}

// GitReader is the workspace-backed git surface the plan turn drives: the
// snapshot refs (Head), the lineage diff (Workspace.Diff between a Task's
// lineage tag and the current tag, §6), and per-op secrets.
// sourcecontrol.GitOpsService satisfies it.
type GitReader interface {
	Workspace() sourcecontrol.Workspace
	Resolver() secrets.Resolver
}

// SkillsRepoResolver ensures the org's _skills repo is provisioned (the
// task-planning flow skill is seeded there) and returns its row — the source
// of the plan turn's SkillsRef snapshot. Wired at the composition root from
// the skills feature so task holds no skills edge.
type SkillsRepoResolver func(ctx context.Context, orgID string) (*sourcecontrol.GitRepository, error)

// ExecutionReader is the read side of the executions rows (the platform-owned
// half), consumed org-scoped by the read path to fuse derived status. It is the
// delivery.ExecutionRepository scoped methods — the shared kernel, not the
// execution feature, so the §1 package boundary holds.
type ExecutionReader interface {
	LatestPerKindScoped(ctx context.Context, orgID, repo string, issueNumber int) (map[string]*delivery.Execution, error)
	LatestPerKindForRepoScoped(ctx context.Context, orgID, repo string) (map[int]map[string]*delivery.Execution, error)
	ListByIssueScoped(ctx context.Context, orgID, repo string, issueNumber int) ([]delivery.Execution, error)
	// SumUsageByIssue is the batch per-task usage rollup (#245) behind the
	// task list's cost captions — one repo-wide grouped query, like
	// LatestPerKindForRepoScoped.
	SumUsageByIssue(ctx context.Context, orgID, repo string) (map[int]contracts.TokenUsage, error)
}

// DesignReader exposes each component's declared dependencies from the design at
// HEAD, so the read path can compute WHICH provisioning / org-service deps block
// a coding Task (issue #164 follow-up: the board's "On hold — Waiting for X").
// It mirrors the slice execution.DesignReader exposes for the funnel's gate; the
// same app-root designComponents adapter satisfies both. A nil DesignReader
// degrades the read path to component-dep gating only (provision/org-service dep
// resolution is skipped).
type DesignReader interface {
	// ProvisionDepNames returns, per component (lowercased name), the names of
	// that component's provisioning dependencies (external + platform-resource).
	// Returns nil when the project has no design yet.
	ProvisionDepNames(ctx context.Context, orgID, projectID string) (map[string][]string, error)
	// OrgServiceDepNames returns, per component (lowercased name), the names of
	// that component's cross-project org-service dependencies. Returns nil when
	// the project has no design yet.
	OrgServiceDepNames(ctx context.Context, orgID, projectID string) (map[string][]string, error)
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

// Dispatcher is the funnel's entry surface (the platform-owned half). The
// execute endpoint and the hold-release path call INTO it — the design's "no
// sibling entry points" rule (§5, §10.1). This is a consumer-side port so the
// Task package imports NOTHING from feature/execution; the funnel satisfies it
// structurally and is wired at the composition root (task↛execution arch-lock).
type Dispatcher interface {
	OnExecuteIntent(ctx context.Context, repoFullName string, issueNumber int) error
	Reevaluate(ctx context.Context) error
}
