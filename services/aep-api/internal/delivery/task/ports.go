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
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The consumer ports the Task surface drives. Each is the narrow slice a service
// actually uses (the house pattern), so the composition root wires concrete
// providers and tests supply fakes.

// IssueClient is the GitHub issue surface the read path and the plan tap use:
// list, fetch, create and edit. sourcecontrol.IssueService satisfies it.
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
	// ListMilestoneIssues reads one milestone's issues (pull requests excluded).
	// The plan turn reads the milestone it is planning INTO so a re-plan and a
	// crash re-run dedupe against what is already there — the milestone, not a
	// label query, is the version's membership.
	ListMilestoneIssues(ctx context.Context, orgID, projectID string, filter sourcecontrol.MilestoneIssuesFilter) ([]sourcecontrol.IssueInfo, error)
}

// ComponentPathReader maps a design component to its source directory (appPath)
// relative to the repo root — the "App Path" line a planned Task's prose body
// carries so the agent knows where to work. The same app-root designComponents
// adapter satisfies the identical port in the event plane. Optional: an unwired
// reader simply omits the line.
type ComponentPathReader interface {
	ComponentPaths(ctx context.Context, orgID, projectID string) (map[string]string, error)
}

// RepoResolver looks up the project's git repo row (its RepoURL yields the
// owner/name and repo full name the funnel/dispatcher key on).
type RepoResolver interface {
	GetRepo(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error)
}

// ComponentEnsurer idempotently provisions the OpenChoreo Component CR for a
// design component, erroring if no design component exists by that name.
// projects.ComponentService satisfies it. PromoteAndExecute calls it
// synchronously, so an unknown componentName (e.g. a caller's prefix-stripping
// bug) fails that call rather than surfacing later inside a cycle.
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
	// BuildScopeAtTag reads a tag's story scope (#369) — the plan turn's
	// story set and per-component claims.
	BuildScopeAtTag(ctx context.Context, orgID, projectID, tag string) (spec.BuildScope, error)
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
}

// AnthropicKeyResolver resolves the org's effective Anthropic key. Empty key +
// nil error means "org has none" → the plan turn raises ErrNoAnthropicKey.
type AnthropicKeyResolver func(ctx context.Context, orgID string) (string, error)

// TurnClient is the agents-service turn client — the plan turn POSTs a
// toolset:"task-plan" turn and streams raw StreamPart frames back for the tap.
type TurnClient interface {
	Turn(ctx context.Context, conversationID, orgID, anthropicKey string, req agentsvc.TurnRequest) (io.ReadCloser, error)
}

// Adopter hands an issue to the coding agent: file it under the deployed
// version's milestone and start an incident run over that milestone unless one
// is already live. *eventcore.Events satisfies it, wired at the composition
// root — this package names no sibling slice (the task ⊥ run arch lock).
//
// The issue is assumed to be BARE (no milestone): the one caller is the SRE/RCA
// handoff, which files the issue and immediately promotes it.
type Adopter interface {
	AdoptIssue(ctx context.Context, orgID, projectID string, issueNumber int) error
}

// MilestoneResolver resolves a `?tag=v<N>` query to the milestone NUMBER the
// version's Tasks live in, THROUGH THE PLATFORM'S RUN ROWS — never by matching
// titles against GitHub. delivery.MilestoneRunRepository satisfies it.
type MilestoneResolver interface {
	MilestoneNumberForTag(ctx context.Context, orgID, projectID, tag string) (number int, found bool, err error)
}
