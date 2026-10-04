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

package aestudiotools

// skills.go — mirror-skills: the pod copies the org's skills library into a
// project's .claude/skills in one commit.

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// MirrorSkills mirrors the enabled coding skills of the skills library (plus
// pinned) into project. Both repositories are in project's org. A mirror that
// lost a race on the project's branch (the pod's 409 conflict or
// not_fast_forward) is ErrCommitConflict: the caller is best-effort and
// mirrors again on the next save.
//
//deadcode:keep wired in Task 4.16 (SyncProjectSkills calls the pod's mirror)
func (a *Adapter) MirrorSkills(ctx context.Context, project, skills RepoRef, pinned []string) (sourcecontrol.CommitResult, error) {
	if err := validRef(project); err != nil {
		return sourcecontrol.CommitResult{}, err
	}
	if err := validRef(skills); err != nil {
		return sourcecontrol.CommitResult{}, err
	}
	if skills.Org != project.Org {
		return sourcecontrol.CommitResult{}, fmt.Errorf("ae studio: the skills repository is in org %q, the project in %q", skills.Org, project.Org)
	}
	body := gen.SkillsMirrorRequest{
		SkillsRepo: gen.SkillsRepo{Owner: skills.Owner, Repo: skills.Repo, DefaultBranch: skills.DefaultBranch},
		Pinned:     nonNil(pinned),
	}
	var reply gen.SkillsMirrorResult
	err := a.do(ctx, project.Org, "mirror-skills", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.MirrorSkills(ctx, project.Owner, project.Repo, &gen.MirrorSkillsParams{DefaultBranch: optional(project.DefaultBranch), XImpersonateOrg: org}, body, auth)
	})
	if errors.Is(err, sourcecontrol.ErrRefNotFastForward) {
		return sourcecontrol.CommitResult{}, fmt.Errorf("%w: %w", &sourcecontrol.CommitConflictError{}, err)
	}
	if err != nil {
		return sourcecontrol.CommitResult{}, err
	}
	return sourcecontrol.CommitResult{CommitSHA: reply.CommitSha, Changed: reply.Changed}, nil
}
