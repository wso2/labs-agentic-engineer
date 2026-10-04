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

package provisioning

import (
	"context"
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

const (
	resourceDocsRepoName  = "org-resource-docs"
	resourceDocsProjectID = "_resource-docs"
)

// gitOrgResourceDocs commits resource-docs files into the per-org
// org-resource-docs repo and reads them back. It never writes org-skills /
// _skills.
type gitOrgResourceDocs struct {
	repos sourcecontrol.RepoService
	// git is the org's AE Studio pod: reads and commits.
	git sourcecontrol.Git
}

// NewGitOrgResourceDocs wires the org-resource-docs store over EnsureBareRepo
// and the pod's Git port. Wired only at the composition root.
func NewGitOrgResourceDocs(repos sourcecontrol.RepoService, git sourcecontrol.Git) OrgResourceDocs {
	return &gitOrgResourceDocs{repos: repos, git: git}
}

func (s *gitOrgResourceDocs) CommitUTF8(ctx context.Context, orgID, logicalName, fileName, content string) (string, error) {
	path := logicalName + "/" + fileName
	repo, err := s.repos.EnsureBareRepo(ctx, orgID, resourceDocsProjectID, resourceDocsRepoName)
	if err != nil {
		return "", fmt.Errorf("ensure org-resource-docs repo: %w", err)
	}
	ref, err := sourcecontrol.RefForRow(orgID, repo)
	if err != nil {
		return "", fmt.Errorf("resolve org-resource-docs repository: %w", err)
	}
	// The write replaces whatever the tip holds at path: each attempt reads
	// the path's blob sha as the commit's baseSha, and a concurrent writer
	// that moved it first is re-read and retried.
	if _, err := sourcecontrol.CommitRetrying(ctx, s.git, ref, func(ctx context.Context) (sourcecontrol.CommitRequest, error) {
		_, base, err := s.git.ReadFile(ctx, ref, "", path)
		if err != nil && !errors.Is(err, sourcecontrol.ErrPathNotFound) {
			return sourcecontrol.CommitRequest{}, err
		}
		return sourcecontrol.CommitRequest{
			Writes:  []sourcecontrol.FileWrite{{Path: path, Content: content, BaseSHA: base}},
			Message: "docs: add " + path,
		}, nil
	}); err != nil {
		return "", fmt.Errorf("commit org-resource-docs %q: %w", path, err)
	}
	return path, nil
}

func (s *gitOrgResourceDocs) ReadUTF8(ctx context.Context, orgID, path string) (string, error) {
	repo, err := s.repos.EnsureBareRepo(ctx, orgID, resourceDocsProjectID, resourceDocsRepoName)
	if err != nil {
		return "", fmt.Errorf("ensure org-resource-docs repo: %w", err)
	}
	ref, err := sourcecontrol.RefForRow(orgID, repo)
	if err != nil {
		return "", fmt.Errorf("resolve org-resource-docs repository: %w", err)
	}
	content, _, err := s.git.ReadFile(ctx, ref, "", path)
	if err != nil {
		return "", fmt.Errorf("read org-resource-docs %q: %w", path, err)
	}
	return string(content), nil
}
