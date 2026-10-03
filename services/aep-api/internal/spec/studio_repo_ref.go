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

package spec

// studio_repo_ref.go — where a project lives as far as its org's AE Studio
// pod is concerned: the org and the project's GitHub owner/repo.

import (
	"context"
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ErrProjectRepoNotFound: the org has no repository row for the project
// (another org's project reads the same).
var ErrProjectRepoNotFound = errors.New("project repository not found")

// ProjectRepos reads a project's repository row inside an org.
// sourcecontrol.RepoService satisfies it (ErrRepoNotFound when absent).
type ProjectRepos interface {
	GetRepo(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error)
}

// RepoRefFor resolves an org's project to the repository the pod serves it
// under, with the row it read. No row in the org is ErrProjectRepoNotFound,
// so another org's project reads exactly like a missing one.
func RepoRefFor(ctx context.Context, repos ProjectRepos, orgID, projectID string) (aestudiotools.RepoRef, *sourcecontrol.GitRepository, error) {
	row, err := repos.GetRepo(ctx, orgID, projectID)
	if errors.Is(err, sourcecontrol.ErrRepoNotFound) || (err == nil && row == nil) {
		return aestudiotools.RepoRef{}, nil, ErrProjectRepoNotFound
	}
	if err != nil {
		return aestudiotools.RepoRef{}, nil, fmt.Errorf("read project repository: %w", err)
	}
	owner, repo, err := sourcecontrol.ParseOwnerRepo(row.RepoURL)
	if err != nil {
		return aestudiotools.RepoRef{}, nil, fmt.Errorf("project %s: repository url: %w", projectID, err)
	}
	return aestudiotools.RepoRef{Org: orgID, Owner: owner, Repo: repo}, row, nil
}
