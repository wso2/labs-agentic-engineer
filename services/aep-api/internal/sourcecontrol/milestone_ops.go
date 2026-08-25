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

package sourcecontrol

// issueService's milestone half (interface declared in issue_service.go). One
// spec version == one milestone, so these are the version-ledger reads and
// writes: mint at plan, list to project, count to decide dispatch, close at
// settle.
//
// Every method routes through resolveRepoAndCredential like its issue
// siblings, so the multi-tenant invariant holds at the same single place. The
// host adapter owns milestone idempotency and the case-insensitive title rule
// (see the IssueOps port contract) — nothing is re-implemented here.

import (
	"context"
	"fmt"
	"strings"
)

func (s *issueService) CreateMilestone(ctx context.Context, orgID, projectID string, req CreateMilestoneRequest) (*MilestoneResult, error) {
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		return nil, fmt.Errorf("milestone title is required")
	}
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.github.CreateMilestone(ctx, owner, repoName, cred, req)
}

func (s *issueService) CloseMilestone(ctx context.Context, orgID, projectID string, number int) error {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return s.github.CloseMilestone(ctx, owner, repoName, cred, number)
}

func (s *issueService) ReopenMilestone(ctx context.Context, orgID, projectID string, number int) error {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return s.github.ReopenMilestone(ctx, owner, repoName, cred, number)
}

func (s *issueService) ListMilestones(ctx context.Context, orgID, projectID, state string) ([]Milestone, error) {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.github.ListMilestones(ctx, owner, repoName, cred, state)
}

func (s *issueService) ListMilestoneIssues(ctx context.Context, orgID, projectID string, filter MilestoneIssuesFilter) ([]IssueInfo, error) {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.github.ListMilestoneIssues(ctx, owner, repoName, cred, filter)
}

func (s *issueService) MilestoneIssueCounts(ctx context.Context, orgID, projectID string, number int) (*MilestoneIssueCounts, error) {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.github.MilestoneIssueCounts(ctx, owner, repoName, cred, number)
}

func (s *issueService) ListMilestoneIssueComments(ctx context.Context, orgID, projectID string, number, perIssue int) (map[int][]IssueComment, error) {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.github.ListMilestoneIssueComments(ctx, owner, repoName, cred, number, perIssue)
}
