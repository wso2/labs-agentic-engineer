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

// build_credentials_service.go — the build's git credential reference.
//
// StageBuildSecret answers which SecretReference a component build's
// checkout step clones the repo with: the org's `github-pat` reference, read
// from its org_secrets row (R7). The build is triggered with
// `repository.secretRef = <that name>`; the upstream `dockerfile-builder`
// ClusterWorkflow synthesises the `<workflowRunName>-git-secret`
// ExternalSecret from it and ESO materialises the Secret the checkout step
// mounts, taking the `password` entry the gitpat write stores beside `token`
// (the checkout defaults the username to `git`).
//
// No credential value passes through aep-api for a build: nothing is minted,
// read or written here, only a reference name is returned. The reference is
// written by the gitpat submit and removed by the gitpat disconnect, so a
// disconnected org has no row and its builds are refused, never dispatched
// with an empty secretRef.
package organization

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// StageResult is returned to the BFF: the SecretReference the build
// WorkflowRun sets as repository.secretRef. Never empty on success.
type StageResult struct {
	SecretRef string `json:"secretRef"`
}

// Errors with stable codes the API layer maps to phase2.md §5.2 status codes:
//
//   - ErrRepoNotInOrg    → 404 (the (ocOrgId, repoSlug) tuple doesn't match
//     an active repo — server-side ownership fence)
//   - ErrOrgDisconnected → 409 (the org has no `github-pat` reference: never
//     connected, or disconnected)
//
// A failed row read falls through as 500-class.
var (
	ErrRepoNotInOrg    = errors.New("stage-build-secret: repo not in org")
	ErrOrgDisconnected = errors.New("stage-build-secret: org disconnected")
)

// BuildCredentialsService resolves the SecretReference a component build
// clones with.
type BuildCredentialsService struct {
	repos sourcecontrol.RepoRepository
	refs  OrgSecretRefReader
}

func NewBuildCredentialsService(repos sourcecontrol.RepoRepository, refs OrgSecretRefReader) *BuildCredentialsService {
	return &BuildCredentialsService{repos: repos, refs: refs}
}

// StageBuildSecret returns the SecretRef the caller passes to the build
// WorkflowRun:
//
//  1. Validate (ocOrgId, repoSlug) maps to an active git_repositories row —
//     server-side ownership fence.
//  2. Read the org's `github-pat` reference name from its org_secrets row.
//     No row → ErrOrgDisconnected.
//
// workflowRunName is retained for log correlation only — the reference is
// per-org, not per-run.
func (s *BuildCredentialsService) StageBuildSecret(
	ctx context.Context, ocOrgID, repoSlug, workflowRunName string,
) (*StageResult, error) {
	if ocOrgID == "" || repoSlug == "" || workflowRunName == "" {
		return nil, fmt.Errorf("stage-build-secret: ocOrgId, repoSlug, workflowRunName are required")
	}

	repo, err := s.repos.GetByOrgAndSlug(ctx, ocOrgID, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("stage-build-secret: lookup repo: %w", err)
	}
	if repo == nil {
		return nil, ErrRepoNotInOrg
	}

	name, ok, err := RecordedOrgSecretRef(ctx, s.refs, ocOrgID, OrgSecretGitHubPAT)
	if err != nil {
		return nil, fmt.Errorf("stage-build-secret: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("%w: no %s reference", ErrOrgDisconnected, OrgSecretGitHubPAT)
	}

	slog.InfoContext(ctx, "stage-build-secret: build references the gitpat",
		"ocOrgId", ocOrgID, "repoSlug", repoSlug,
		"workflowRunName", workflowRunName, "secretRef", name)
	return &StageResult{SecretRef: name}, nil
}
