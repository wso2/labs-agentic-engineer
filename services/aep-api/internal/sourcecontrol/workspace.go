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

package sourcecontrol

import (
	"context"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// The Workspace / SnapshotProvider domain ports: the local
// git-object surface that replaced the per-object REST walkers. The definitions
// live in internal/platform/gitfs — internal/platform must stay feature-free
// (internal/arch), so the domain re-exports them as aliases. Consumers keep
// importing sourcecontrol.*; `errors.Is` identities are shared with gitfs because
// alias vars ARE the gitfs values.
type (
	// Workspace is the single collapse point for the four read walkers, the
	// three write paths, tagging, and ref comparison. See gitfs.Workspace.
	Workspace = gitfs.Workspace
	// SnapshotProvider materializes the immutable per-SHA snapshot dirs the
	// agents mount reads. See gitfs.SnapshotProvider.
	SnapshotProvider = gitfs.SnapshotProvider
	// WorkspaceRef addresses one repo on the workspace mount.
	WorkspaceRef = gitfs.RepoRef
	// Tx is the staged write overlay handed to a Mutate fn.
	Tx = gitfs.Tx
	// Snapshot is a read-only view of one commit's tree.
	Snapshot = gitfs.Snapshot
	// Entry is one blob of a tree listing.
	Entry = gitfs.Entry
	// CommitOpts parametrizes one Mutate (message, identities, retry policy).
	CommitOpts = gitfs.CommitOpts
	// CommitResult reports what a Mutate did.
	CommitResult = gitfs.CommitResult
	// TagSpec describes one annotated tag.
	TagSpec = gitfs.TagSpec
	// RetryPolicy bounds Mutate's CAS retry loop (the one shared retry
	// policy for all writers).
	RetryPolicy = gitfs.RetryPolicy
)

// Workspace-read sentinels, aliased from gitfs (the raiser). Later phases
// map them to 404.
var (
	// ErrRefNotFound: `at` (branch, tag, or sha) did not resolve to a commit.
	ErrRefNotFound = gitfs.ErrRefNotFound
	// ErrPathNotFound: the path is not a file in the addressed tree.
	ErrPathNotFound = gitfs.ErrPathNotFound
)

// WorkspaceRefFor derives the mount WorkspaceRef from a git_repositories row —
// the path key is a pure function of the DB row, never of client input
// (design D6). orgID is passed explicitly (not read off the row) so callers
// keep addressing exactly the org they authenticated. The slug comes from
// (*GitRepository).WorkspaceSlug — RepoSlug (URL-backfilled), except
// the per-org skills repo whose leaf is pinned to spec.SkillsRepoDirName
// (design §4: repos/<orgId>/_skills/org-skills/; the agents service derives
// the skills snapshot path structurally from that fixed name). The default
// branch falls back to "main".
func WorkspaceRefFor(orgID string, repo *GitRepository, cred secrets.Credential) WorkspaceRef {
	slug := repo.WorkspaceSlug()
	branch := repo.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	return WorkspaceRef{
		OrgID:         orgID,
		ProjectID:     repo.ProjectID,
		RepoSlug:      slug,
		CloneURL:      repo.RepoURL,
		DefaultBranch: branch,
		Cred:          cred,
	}
}

// ResolveWorkspaceRef is WorkspaceRefFor with the credential resolved from
// the org resolver — the one call consumers start every read/write from.
func ResolveWorkspaceRef(ctx context.Context, resolver secrets.Resolver, orgID string, repo *GitRepository) (WorkspaceRef, error) {
	cred, err := resolver.Resolve(ctx, orgID)
	if err != nil {
		return WorkspaceRef{}, fmt.Errorf("resolve credential: %w", err)
	}
	return WorkspaceRefFor(orgID, repo, cred), nil
}
