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

package app

import (
	"context"
	"errors"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// git_reads.go — the composition root's reads of a project's repository
// through its org's AE Studio pod (sourcecontrol.Git), for the adapters that
// hand single files and directories to features holding no git port.

// projectFiles reads an org's project repository: the row names the
// repository (never client input), the pod serves its content.
type projectFiles struct {
	git   sourcecontrol.Git
	repos sourcecontrol.ProjectRepoRows
}

// readFile reads path at `at` ("" = the default-branch tip, fetched first; a
// 40-hex sha = that commit). A path the tree does not hold is found=false
// with no error.
func (p projectFiles) readFile(ctx context.Context, orgID, projectID, at, path string) (content, blobSHA string, found bool, err error) {
	ref, _, err := sourcecontrol.RepoRefFor(ctx, p.repos, orgID, projectID)
	if err != nil {
		return "", "", false, err
	}
	raw, sha, err := p.git.ReadFile(ctx, ref, at, path)
	if errors.Is(err, sourcecontrol.ErrPathNotFound) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return string(raw), sha, true, nil
}

// readBundle reads the files f selects at `at`, all from ONE commit, keyed by
// full path. A commit the repository does not have (an empty repository's
// tip, a pinned sha it lacks) is found=false with no error.
func (p projectFiles) readBundle(ctx context.Context, orgID, projectID, at string, f sourcecontrol.BundleFilter) (files map[string]string, found bool, err error) {
	ref, _, err := sourcecontrol.RepoRefFor(ctx, p.repos, orgID, projectID)
	if err != nil {
		return nil, false, err
	}
	files, _, err = p.git.ReadBundle(ctx, ref, at, f)
	if errors.Is(err, sourcecontrol.ErrRefNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return files, true, nil
}
