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

package files

import (
	"context"
	"fmt"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// The snapshots a turn reads (ticket 04 §6, 07 §6): the agent's project
// lookup on the MCP socket resolves the project, writes the repo snapshot and
// the Org skills snapshot, and learns the stored reference documents. The
// check and the snapshot are one call, so no snapshot exists for a project
// that failed the check.

// ProjectSnapshot is a known project's snapshot: the commit it is of, the
// Org skills commit, and the stored reference documents (sorted names),
// overlaid into the snapshot at repo.ReferenceOverlayDir.
type ProjectSnapshot struct {
	HeadSHA, SkillsSHA string
	References         []string
}

// Snapshot resolves project, writes its snapshot at at (empty: the
// default-branch tip, fetched) with the stored references overlaid, and
// writes the Org skills snapshot at the skills repository's tip. An unknown
// project is projects.ErrUnknown before anything touches the disk; at must
// be a hex object name (ErrPathInvalid). A refused admission is
// repo.ErrDiskFull.
func (r Reader) Snapshot(ctx context.Context, project, at string) (*ProjectSnapshot, error) {
	if err := validateCommit(at); err != nil {
		return nil, err
	}
	rep, ref, err := r.resolve(ctx, project)
	if err != nil {
		return nil, err
	}
	head, err := r.Engine.Head(ctx, ref, at)
	if err != nil {
		return nil, repoError(ref, fmt.Errorf("resolve %s: %w", commitLabel(at), err))
	}
	if _, err := r.Engine.EnsureSnapshot(ctx, ref, head); err != nil {
		return nil, repoError(ref, fmt.Errorf("snapshot %s: %w", head, err))
	}
	store := repo.OwnerRepo{Owner: rep.Owner, Repo: rep.Repo}
	r.Engine.OverlayReferences(ctx, ref, store, head)
	skills, err := r.SkillsSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	refs, err := r.Engine.ListReferences(ctx, store)
	if err != nil {
		return nil, repoError(ref, fmt.Errorf("list references: %w", err))
	}
	if refs == nil {
		refs = []string{}
	}
	return &ProjectSnapshot{HeadSHA: head, SkillsSHA: skills, References: refs}, nil
}

// SkillsSnapshot resolves the org's skills repository through aep-api (every
// call, never cached), writes its snapshot at the tip and answers the tip's
// sha.
func (r Reader) SkillsSnapshot(ctx context.Context) (string, error) {
	rep, err := r.Projects.ResolveSkills(ctx)
	if err != nil {
		return "", err
	}
	ref, err := r.cloneRef(rep, repo.SkillsProject)
	if err != nil {
		return "", err
	}
	head, err := r.Engine.Head(ctx, ref, "")
	if err != nil {
		return "", repoError(ref, fmt.Errorf("resolve skills head: %w", err))
	}
	if _, err := r.Engine.EnsureSkillsSnapshot(ctx, ref, head); err != nil {
		return "", repoError(ref, fmt.Errorf("skills snapshot %s: %w", head, err))
	}
	return head, nil
}
