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
	"errors"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// resolveProjectRepo resolves the project's git repo row plus its owner/name,
// mapping a missing or URL-less repo to ErrProjectRepoNotFound. Shared by the
// read path, the command surface, and the plan assembler — the one place the
// project→repo resolution rule lives.
func resolveProjectRepo(ctx context.Context, repos RepoResolver, orgID, projectID string) (repo *sourcecontrol.GitRepository, owner, name string, err error) {
	repo, err = repos.GetRepo(ctx, orgID, projectID)
	if err != nil {
		if errors.Is(err, sourcecontrol.ErrRepoNotFound) {
			return nil, "", "", ErrProjectRepoNotFound
		}
		return nil, "", "", err
	}
	if repo == nil {
		return nil, "", "", ErrProjectRepoNotFound
	}
	owner, name, err = sourcecontrol.ParseOwnerRepo(repo.RepoURL)
	if err != nil {
		return nil, "", "", ErrProjectRepoNotFound
	}
	return repo, owner, name, nil
}
