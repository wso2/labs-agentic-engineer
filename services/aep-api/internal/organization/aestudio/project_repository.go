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

package aestudio

// project_repository.go — where a project's code lives, as AE Studio's tools
// pod resolves it on every request (04 §2). The pod keeps no cache: it asks
// aep-api each time, so a repo rename or a project deleted from the org shows
// up on the next request.

import (
	"context"
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ProjectRepository is a project's GitHub repository.
type ProjectRepository struct {
	Owner         string
	Repo          string
	DefaultBranch string
	CloneURL      string
}

// ErrProjectNotFound means the org has no repository for the project. It is
// the same answer whether the project does not exist or belongs to another
// org, so a caller learns nothing about other orgs.
var ErrProjectNotFound = errors.New("project not found")

// ErrRepositoryURLInvalid means the org's project exists but its stored
// repository URL is not a GitHub repository: a permanent data fault, not a
// missing project.
var ErrRepositoryURLInvalid = errors.New("repository url is not a GitHub repository")

// RepoReader reads a project's repository row, scoped to the org
// (sourcecontrol.RepoService satisfies it).
type RepoReader interface {
	GetRepo(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error)
}

// ProjectRepositories resolves an org's project to its repository.
type ProjectRepositories struct {
	repos RepoReader
}

// NewProjectRepositories returns the lookup over repos.
func NewProjectRepositories(repos RepoReader) *ProjectRepositories {
	return &ProjectRepositories{repos: repos}
}

// Lookup returns org's repository for project, or ErrProjectNotFound when org
// has none. A store failure, or a stored URL that is not a GitHub repo
// (ErrRepositoryURLInvalid), is an error, never ErrProjectNotFound.
func (p *ProjectRepositories) Lookup(ctx context.Context, org, project string) (ProjectRepository, error) {
	row, err := p.repos.GetRepo(ctx, org, project)
	if errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		return ProjectRepository{}, ErrProjectNotFound
	}
	if err != nil {
		return ProjectRepository{}, fmt.Errorf("project %s: %w", project, err)
	}
	owner, repo, err := sourcecontrol.ParseOwnerRepo(row.RepoURL)
	if err != nil {
		return ProjectRepository{}, fmt.Errorf("project %s: %w: %w", project, ErrRepositoryURLInvalid, err)
	}
	return ProjectRepository{Owner: owner, Repo: repo, DefaultBranch: row.DefaultBranch, CloneURL: row.RepoURL}, nil
}
