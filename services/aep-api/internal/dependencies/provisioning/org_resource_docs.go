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
	"fmt"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

const (
	resourceDocsRepoName  = "org-resource-docs"
	resourceDocsProjectID = "_resource-docs"
)

// gitOrgResourceDocs commits resource-docs files into the per-org
// org-resource-docs repo. It never writes org-skills / _skills and never
// calls Workspace.PutReferences.
type gitOrgResourceDocs struct {
	repos sourcecontrol.RepoService
	git   sourcecontrol.GitOpsService
}

// NewGitOrgResourceDocs wires the org-resource-docs store over EnsureBareRepo
// + Workspace.Mutate. Wired only at the composition root.
func NewGitOrgResourceDocs(repos sourcecontrol.RepoService, git sourcecontrol.GitOpsService) OrgResourceDocs {
	return &gitOrgResourceDocs{repos: repos, git: git}
}

func (s *gitOrgResourceDocs) CommitUTF8(ctx context.Context, orgID, logicalName, fileName, content string) (string, error) {
	path := logicalName + "/" + fileName
	repo, err := s.repos.EnsureBareRepo(ctx, orgID, resourceDocsProjectID, resourceDocsRepoName)
	if err != nil {
		return "", fmt.Errorf("ensure org-resource-docs repo: %w", err)
	}
	ref, err := sourcecontrol.ResolveWorkspaceRef(ctx, s.git.Resolver(), orgID, repo)
	if err != nil {
		return "", fmt.Errorf("resolve org-resource-docs workspace: %w", err)
	}
	author, committer := s.git.ResolveSaveIdentities(ref.Cred)
	if _, err := s.git.Workspace().Mutate(ctx, ref, func(tx sourcecontrol.Tx) error {
		tx.Write(path, []byte(content))
		return nil
	}, sourcecontrol.CommitOpts{
		Message:   "docs: add " + path,
		Author:    author,
		Committer: committer,
	}); err != nil {
		return "", fmt.Errorf("commit org-resource-docs %q: %w", path, err)
	}
	return path, nil
}
