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

// skills_repository.go — where the org's skills library lives, as AE Studio's
// tools pod resolves it before it writes a turn's Org skills snapshot (07 §6).
// Like a project lookup it is asked on every turn and never cached in the pod.

import (
	"context"
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

var (
	// ErrSkillsRepositoryNotFound means the org has no skills repository row.
	ErrSkillsRepositoryNotFound = errors.New("skills repository not found")
	// ErrSkillsUnavailable means the org's skills library could not be
	// reconciled, so its repository is not usable right now.
	ErrSkillsUnavailable = errors.New("org skills repository unavailable")
)

// SkillsReconciler brings the org's skills library up to date with the
// platform's shipped skills, provisioning the repository on first use
// (spec.SkillService satisfies it).
type SkillsReconciler interface {
	Reconcile(ctx context.Context, orgID string) (int, error)
}

// SkillsRepositories resolves an org to its skills repository.
type SkillsRepositories struct {
	skills SkillsReconciler
	repos  RepoReader
}

// NewSkillsRepositories returns the lookup over skills and repos.
func NewSkillsRepositories(skills SkillsReconciler, repos RepoReader) *SkillsRepositories {
	return &SkillsRepositories{skills: skills, repos: repos}
}

// Lookup reconciles org's skills library, so the platform skills shipped
// after it was provisioned land before a turn snapshots it, then returns the
// org's _skills repository row (the row every turn has read its skills from).
// A failed reconcile is ErrSkillsUnavailable; no row is
// ErrSkillsRepositoryNotFound; a store failure or a stored URL that is not a
// GitHub repo is any other error.
func (s *SkillsRepositories) Lookup(ctx context.Context, org string) (ProjectRepository, error) {
	if _, err := s.skills.Reconcile(ctx, org); err != nil {
		return ProjectRepository{}, fmt.Errorf("%w: reconcile: %w", ErrSkillsUnavailable, err)
	}
	row, err := s.repos.GetRepo(ctx, org, sourcecontrol.SkillsRepoSentinelProjectID)
	if errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		return ProjectRepository{}, ErrSkillsRepositoryNotFound
	}
	if err != nil {
		return ProjectRepository{}, fmt.Errorf("skills repository: %w", err)
	}
	owner, repo, err := sourcecontrol.ParseOwnerRepo(row.RepoURL)
	if err != nil {
		return ProjectRepository{}, fmt.Errorf("skills repository: repo url: %w", err)
	}
	return ProjectRepository{Owner: owner, Repo: repo, DefaultBranch: row.DefaultBranch, CloneURL: row.RepoURL}, nil
}
