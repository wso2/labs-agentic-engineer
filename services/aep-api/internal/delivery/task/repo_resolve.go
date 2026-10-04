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

// resolveProjectRepo addresses the project's repository by the one
// row→repo rule (sourcecontrol.RefForRow), mapping a missing or URL-less repo
// to ErrProjectRepoNotFound. Used by the task reads (ListByTag, Get).
func resolveProjectRepo(ctx context.Context, repos RepoResolver, orgID, projectID string) (sourcecontrol.RepoRef, error) {
	row, err := repos.GetRepo(ctx, orgID, projectID)
	if err != nil {
		if errors.Is(err, sourcecontrol.ErrRepoNotFound) {
			return sourcecontrol.RepoRef{}, ErrProjectRepoNotFound
		}
		return sourcecontrol.RepoRef{}, err
	}
	ref, err := sourcecontrol.RefForRow(orgID, row)
	if err != nil {
		return sourcecontrol.RepoRef{}, ErrProjectRepoNotFound
	}
	return ref, nil
}
