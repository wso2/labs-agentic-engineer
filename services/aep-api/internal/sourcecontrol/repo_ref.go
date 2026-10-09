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
)

// repo_ref.go — the one rule for where a project lives as far as its org's
// AE Studio pod is concerned: a pure function of the git_repositories row,
// never of client input.

// ProjectRepoRows reads a project's git_repositories row inside an org: nil
// when the org has none. RepoRepository satisfies it.
type ProjectRepoRows interface {
	GetByOrgAndProjectID(ctx context.Context, ocOrgID, projectID string) (*GitRepository, error)
}

// defaultBranchFallback is the branch of a row that never recorded one (the
// column's own default).
const defaultBranchFallback = "main"

// RefForRow addresses row's repository in org. org is passed explicitly, not
// read off the row, so callers keep addressing exactly the org they
// authenticated. A nil row is ErrRepoNotFound; a row whose URL names no
// owner/repo is an error.
func RefForRow(org string, row *GitRepository) (RepoRef, error) {
	if row == nil {
		return RepoRef{}, ErrRepoNotFound
	}
	owner, repo, err := ParseOwnerRepo(row.RepoURL)
	if err != nil {
		return RepoRef{}, fmt.Errorf("project %s: repository url: %w", row.ProjectID, err)
	}
	branch := row.DefaultBranch
	if branch == "" {
		branch = defaultBranchFallback
	}
	return RepoRef{Org: org, Owner: owner, Repo: repo, DefaultBranch: branch}, nil
}

// RepoRefFor resolves an org's project to its repository and the row it read.
// No row in the org is ErrRepoNotFound, so another org's project reads
// exactly like a missing one. The row's status is the caller's to judge.
func RepoRefFor(ctx context.Context, repos ProjectRepoRows, org, projectID string) (RepoRef, *GitRepository, error) {
	row, err := repos.GetByOrgAndProjectID(ctx, org, projectID)
	if err != nil {
		return RepoRef{}, nil, fmt.Errorf("read project repository: %w", err)
	}
	if row == nil {
		return RepoRef{}, nil, ErrRepoNotFound
	}
	ref, err := RefForRow(org, row)
	if err != nil {
		return RepoRef{}, nil, err
	}
	return ref, row, nil
}
