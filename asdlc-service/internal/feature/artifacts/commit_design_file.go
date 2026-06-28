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

package artifacts

// A single-file commit primitive for `specs/design/` that lands a change on
// remote main directly — NO version tag. The tagged `SaveDesign` flow always
// mints the next `v<N>-<M>` revision; this path is for internal durability
// touch-ups (e.g. the P3.5 grant cascade persisting
// `exposesAPI.orgPublished:true`) where minting a new design version would be
// wrong. It reuses the same Git Data API primitives + service-identity
// credential as saveDesignViaAPI, but commits exactly one blob and stops short
// of the tag step.
//
// See docs/design/artifact-store-v2.md §8 for the underlying save-flow shape.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// CommitDesignFile writes a single file under `specs/design/` to the working
// tree and commits it directly to remote main via the GitHub Git Data API,
// without creating a version tag. `subPath` is relative to `specs/design/`
// (forward slashes; nested components allowed); `content` is the new file body;
// `message` is the commit message. Returns the new commit SHA, or
// ("", nil) when the file already matches main (no-op, no commit).
//
// Credentials are the org's service-identity (the same
// gitOps.Resolver().Resolve(orgID) + ResolveSaveIdentities path SaveDesign and
// the agent dispatch use) — NO user JWT is required, so this is safe to call
// from the deploy cascade.
func (s *artifactService) CommitDesignFile(ctx context.Context, orgID, projectID, subPath, content, message string) (string, error) {
	repoPath := path.Join(DesignDir, subPath) // forward-slash repo path
	if err := validateRelPath(repoPath); err != nil {
		return "", err
	}

	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return "", err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return "", fmt.Errorf("ensure clone: %w", err)
	}

	owner, repo := models.OwnerRepoFromURL(repoRecord.RepoURL)
	if owner == "" || repo == "" {
		return "", fmt.Errorf("cannot derive owner/repo from RepoURL %q", repoRecord.RepoURL)
	}
	cred, err := s.gitOps.Resolver().Resolve(ctx, repoRecord.OrgID)
	if err != nil {
		return "", fmt.Errorf("resolve credential: %w", err)
	}

	// Commit the single blob over current main under CAS retry. base_tree =
	// current main tree carries every sibling file forward unchanged.
	author, committer := s.gitOps.ResolveSaveIdentities(cred)
	bucketKey := repoRecord.OrgID + ":" + repoRecord.ProjectID

	var newCommitSHA string
	err = retryOnCASConflict(ctx, bucketKey, func() error {
		mainSHA, ferr := s.gitOps.GitHubClient().GetRef(ctx, owner, repo, cred, "heads/"+repoRecord.DefaultBranch)
		if ferr != nil {
			return fmt.Errorf("get ref main: %w", ferr)
		}
		mainCommit, ferr := s.gitOps.GitHubClient().GetCommit(ctx, owner, repo, cred, mainSHA)
		if ferr != nil {
			return fmt.Errorf("get commit %s: %w", mainSHA, ferr)
		}

		blobSHA, berr := s.gitOps.GitHubClient().CreateBlob(ctx, owner, repo, cred, []byte(content))
		if berr != nil {
			return fmt.Errorf("create blob %s: %w", subPath, berr)
		}
		treeSHA, terr := s.gitOps.GitHubClient().CreateTree(ctx, owner, repo, cred, mainCommit.TreeSHA, []gitrepo.TreeEntry{{
			Path: repoPath,
			Mode: "100644",
			Type: "blob",
			SHA:  blobSHA,
		}})
		if terr != nil {
			return fmt.Errorf("create tree: %w", terr)
		}
		// If the new tree equals main's tree the content is unchanged — skip the
		// commit so we never push an empty diff.
		if treeSHA == mainCommit.TreeSHA {
			newCommitSHA = ""
			return nil
		}
		commitSHA, cerr := s.gitOps.GitHubClient().CreateCommit(ctx, owner, repo, cred, gitrepo.CreateCommitRequest{
			Message:   message,
			TreeSHA:   treeSHA,
			Parents:   []string{mainSHA},
			Author:    author,
			Committer: committer,
		})
		if cerr != nil {
			return fmt.Errorf("create commit: %w", cerr)
		}
		if uerr := s.gitOps.GitHubClient().UpdateRef(ctx, owner, repo, cred, "heads/"+repoRecord.DefaultBranch, commitSHA, false); uerr != nil {
			return uerr
		}
		newCommitSHA = commitSHA
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrConflictBudgetExhausted) {
			return "", fmt.Errorf("commit design file: %w", err)
		}
		return "", fmt.Errorf("commit + update ref: %w", err)
	}

	if newCommitSHA == "" {
		return "", nil // unchanged — nothing committed.
	}

	// Best-effort sync the local clone so subsequent reads see the change.
	if perr := s.gitOps.BestEffortPullDefaultBranch(ctx, repoRecord); perr != nil {
		slog.WarnContext(ctx, "best-effort pull after design-file commit failed",
			"project", repoRecord.ProjectID, "error", perr)
	}

	slog.InfoContext(ctx, "design file committed via api (untagged)",
		"project", repoRecord.ProjectID, "path", repoPath, "commit", newCommitSHA)
	return newCommitSHA, nil
}
