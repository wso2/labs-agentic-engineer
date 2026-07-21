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

import (
	"context"
	"fmt"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// UsageReader serves the spec phase's token-usage rollups (#245): lifetime
// turn spend plus the current drafting cycle — spend since the newest
// published `v*` tag (its creatordate is the cycle boundary; no tags, or no
// ready repo yet, means everything spent so far IS the cycle).
type UsageReader struct {
	turns TurnRepository
	repo  sourcecontrol.RepoRepository
	git   GitGateway
}

// NewUsageReader builds the spec-phase usage reader.
func NewUsageReader(turns TurnRepository, repo sourcecontrol.RepoRepository, git GitGateway) *UsageReader {
	return &UsageReader{turns: turns, repo: repo, git: git}
}

// ProjectTurnUsage returns the project's lifetime spec/design turn usage and
// the current drafting cycle's share.
func (r *UsageReader) ProjectTurnUsage(ctx context.Context, orgID, projectID string) (all, draftCycle contracts.TokenUsage, err error) {
	all, err = r.turns.SumUsage(ctx, orgID, projectID, nil)
	if err != nil {
		return contracts.TokenUsage{}, contracts.TokenUsage{}, err
	}
	since, err := r.lastPublishedAt(ctx, orgID, projectID)
	if err != nil {
		return contracts.TokenUsage{}, contracts.TokenUsage{}, err
	}
	if since == nil {
		return all, all, nil // nothing published yet — the whole spend is the cycle
	}
	draftCycle, err = r.turns.SumUsage(ctx, orgID, projectID, since)
	if err != nil {
		return contracts.TokenUsage{}, contracts.TokenUsage{}, err
	}
	return all, draftCycle, nil
}

// lastPublishedAt resolves the newest `v*` tag's creatordate — the drafting
// cycle boundary. nil when no tag exists or the repo isn't provisioned yet.
// ListTagsLocal suffices: the platform cuts every v* tag through the mirror,
// and this is a best-effort rollup read that must not pay a network fetch.
func (r *UsageReader) lastPublishedAt(ctx context.Context, orgID, projectID string) (*time.Time, error) {
	repoRecord, err := r.repo.GetByOrgAndProjectID(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get repo: %w", err)
	}
	if repoRecord == nil || repoRecord.Status != "ready" {
		return nil, nil
	}
	ref, err := sourcecontrol.ResolveWorkspaceRef(ctx, r.git.Resolver(), orgID, repoRecord)
	if err != nil {
		return nil, err
	}
	tags, err := r.git.Workspace().ListTagsLocal(ctx, ref, "v")
	if err != nil {
		return nil, err
	}
	var newest time.Time
	for _, tag := range tags {
		if tag.CreatedAt.After(newest) {
			newest = tag.CreatedAt
		}
	}
	if newest.IsZero() {
		return nil, nil
	}
	return &newest, nil
}
