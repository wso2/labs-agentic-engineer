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

// Package services — build credentials.
//
// StageBuildSecret is the BFF entry point for provisioning the git credential
// a component build's checkout step uses to clone the repo. It mints a fresh
// token and lands it on the workflow plane via OpenChoreo's GitSecret API
// (`CreateGitSecret`), which stores the value in OpenBao and creates a
// per-org SecretReference. The build is then triggered with
// `repository.secretRef = <BuildGitSecretName>`; the upstream
// `dockerfile-builder` ClusterWorkflow synthesises the
// `<workflowRunName>-git-secret` ExternalSecret from that SecretReference and
// ESO materialises the Secret the checkout step mounts.
//
// Why OC and not a direct K8s write: the BFF runs on the control plane and the
// build runs on a separate workflow plane (CP/WP split in cloud). A direct
// in-cluster client can't reach the WP. OC owns the cross-plane write, and the
// same call works on local k3d (single cluster) where OpenBao + ESO + the
// Vault-backed `default` ClusterSecretStore are also present — so this is one
// unified path for both environments, no env flag.
//
// The git token is a short-lived GitHub App installation token, so the value
// is refreshed (delete + create — OC has no update) on every build dispatch.
// The SecretReference is per-org (BuildGitSecretName, isolated by OC's org
// namespace) and reused across builds; concurrent builds clobber the value
// benignly because installation tokens are account-scoped and any valid one
// clones any of the org's repos.
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/internal/credentials"
	"github.com/wso2/asdlc/asdlc-service/repositories"
)

// BuildGitSecretName is the OC GitSecret / SecretReference name carrying the
// org's git credential for component builds. Per-org and deterministic: OC
// scopes it by the org namespace, so the same name is safe across orgs, and
// both the refresh (delete+create) and the disconnect cleanup target it by
// name. Passed to the build WorkflowRun as `repository.secretRef`.
const BuildGitSecretName = "app-factory-component-build-git-secret"

// StageResult is returned to the BFF. The token never crosses the boundary;
// only the SecretRef the build WorkflowRun should reference.
type StageResult struct {
	// SecretRef is the OC GitSecret name to set as repository.secretRef on
	// the build. Empty when the git-secret client is unavailable (degraded:
	// the build falls back to an unauthenticated clone).
	SecretRef string `json:"secretRef"`
}

// Errors with stable codes the API layer maps to phase2.md §5.2 status codes:
//
//   - ErrRepoNotInOrg    → 404 (the (ocOrgId, repoSlug) tuple doesn't match
//     an active repo — server-side ownership fence)
//   - ErrOrgDisconnected → 409 (credential row is suspended or disconnected)
//
// Transient OC / credential-store failures fall through as 500-class.
var (
	ErrRepoNotInOrg    = errors.New("stage-build-secret: repo not in org")
	ErrOrgDisconnected = errors.New("stage-build-secret: org disconnected")
)

// BuildCredentialsService provisions the per-org build git secret on the
// workflow plane via OpenChoreo. It reads the org credential from the
// resolver, mints a token, and upserts the OC GitSecret.
//
// gitSecrets may be nil in tests or when the OC client isn't configured — in
// that case provisioning is skipped (with a loud warning) and StageBuildSecret
// returns an empty SecretRef so the build runs unauthenticated and fails at
// clone with a clearer signal than a silent misroute.
type BuildCredentialsService struct {
	repos      repositories.RepoRepository
	resolver   credentials.Resolver
	gitSecrets openchoreo.GitSecretClient
}

func NewBuildCredentialsService(
	repos repositories.RepoRepository,
	resolver credentials.Resolver,
	gitSecrets openchoreo.GitSecretClient,
) *BuildCredentialsService {
	return &BuildCredentialsService{
		repos:      repos,
		resolver:   resolver,
		gitSecrets: gitSecrets,
	}
}

// StageBuildSecret provisions the org's build git secret on the workflow plane
// and returns the SecretRef the caller passes to the build WorkflowRun.
//
// Flow:
//
//  1. Validate (ocOrgId, repoSlug) maps to an active git_repositories row —
//     server-side ownership fence.
//  2. Resolve the org's credential. Refuses if status != active.
//  3. cred.Token(ctx) → fresh token (App: per-installation mint, cached;
//     PAT: postgres read).
//  4. Refresh the per-org OC GitSecret (delete + create) with the fresh token.
//
// workflowRunName is retained for log correlation only — the OC GitSecret is
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

	cred, err := s.resolver.Resolve(ctx, ocOrgID)
	if err != nil {
		var notActive *credentials.OrgNotActiveError
		var notFound *credentials.OrgNotFoundError
		if errors.As(err, &notActive) || errors.As(err, &notFound) {
			return nil, fmt.Errorf("%w: %v", ErrOrgDisconnected, err)
		}
		return nil, fmt.Errorf("stage-build-secret: resolve credential: %w", err)
	}

	token, _, err := cred.Token(ctx)
	if err != nil {
		return nil, classifyMintErr(err)
	}

	if s.gitSecrets == nil {
		slog.WarnContext(ctx, "stage-build-secret: git-secret client not configured — provisioning skipped (build will fail at clone)",
			"ocOrgId", ocOrgID, "workflowRunName", workflowRunName)
		return &StageResult{SecretRef: ""}, nil
	}

	if err := s.provisionGitSecret(ctx, ocOrgID, usernameForCredential(cred), token); err != nil {
		// Degrade gracefully instead of blocking the build. On dev cloud the
		// OpenChoreo GitSecret API is unreachable — the platform-api does not
		// route /api/v1alpha1/gitsecrets, so CreateGitSecret 404s (cross-plane
		// private-repo secret delivery is tracked by wso2-enterprise/wso2cloud#319).
		// Returning an empty SecretRef lets the build dispatch and clone
		// unauthenticated, which is correct for the public repos app-factory
		// creates by default; a private repo would fail later at checkout with a
		// clear git error rather than a stuck task. Where provisioning DOES work
		// (local k3d, single cluster), this branch is not taken and the real
		// SecretRef is returned below.
		slog.WarnContext(ctx, "stage-build-secret: git secret provisioning failed — dispatching build without a git secret (clones public repos only; see wso2cloud#319)",
			"ocOrgId", ocOrgID, "repoSlug", repoSlug,
			"workflowRunName", workflowRunName, "error", err)
		return &StageResult{SecretRef: ""}, nil
	}

	slog.InfoContext(ctx, "stage-build-secret: git secret provisioned",
		"ocOrgId", ocOrgID, "repoSlug", repoSlug,
		"workflowRunName", workflowRunName, "secretRef", BuildGitSecretName)

	return &StageResult{SecretRef: BuildGitSecretName}, nil
}

// provisionGitSecret refreshes the per-org build GitSecret with the current
// token. OC has no update verb and Create 409s on a duplicate name, so we
// delete (tolerating not-found) then create. Both legs tolerate the benign
// concurrent state: a not-found delete and a conflicting create both mean
// another same-org build raced us. The delete window and a conflicting create
// are safe for in-flight builds — each build has its own ExternalSecret/target
// Secret, ESO defaults to deletionPolicy=Retain, and installation tokens are
// account-scoped/interchangeable (the racing build's token clones our repo too).
func (s *BuildCredentialsService) provisionGitSecret(ctx context.Context, ocOrgID, username, token string) error {
	if err := s.gitSecrets.DeleteGitSecret(ctx, ocOrgID, BuildGitSecretName); err != nil && !errors.Is(err, openchoreo.ErrNotFound) {
		return fmt.Errorf("refresh (delete) git secret: %w", err)
	}
	// Tolerate ErrConflict (409): a concurrent build re-created the secret
	// between our delete and create. The secret exists with a valid token, so
	// the build can proceed — don't fail it on the race.
	if _, err := s.gitSecrets.CreateGitSecret(ctx, ocOrgID, openchoreo.CreateGitSecretRequest{
		Name:       BuildGitSecretName,
		SecretType: openchoreo.GitSecretBasicAuth,
		Username:   username,
		Token:      token,
		// WorkflowPlaneKind/Name left zero — the client defaults them to
		// ClusterWorkflowPlane/default.
	}); err != nil && !errors.Is(err, openchoreo.ErrConflict) {
		return fmt.Errorf("create git secret: %w", err)
	}
	return nil
}

// DeleteBuildSecretsForOrg removes the org's build GitSecret. Called from the
// org.disconnected cascade so a staged token doesn't linger after the
// credential row is wiped. Idempotent — a not-found delete is a no-op.
func (s *BuildCredentialsService) DeleteBuildSecretsForOrg(ctx context.Context, ocOrgID string) error {
	if s.gitSecrets == nil {
		return nil
	}
	if err := s.gitSecrets.DeleteGitSecret(ctx, ocOrgID, BuildGitSecretName); err != nil && !errors.Is(err, openchoreo.ErrNotFound) {
		return fmt.Errorf("delete build git secret for org %s: %w", ocOrgID, err)
	}
	slog.InfoContext(ctx, "stage-build-secret: deleted org build git secret on disconnect",
		"ocOrgId", ocOrgID, "secretRef", BuildGitSecretName)
	return nil
}

// usernameForCredential derives the HTTPS basic-auth username for git push/pull.
//
//   - App-installation: "x-access-token" is GitHub's documented username for
//     installation-access-token HTTPS auth.
//   - User-PAT: any non-empty string works; we use the PAT owner's login for
//     audit clarity, falling back to "git" if the identity is missing.
//
// Distinguishes the two without type-switching on Credential (forbidden by
// the package contract — see pkg/credentials/credential.go §3 rules) by
// reading the credential's WebhookStrategy: App mode is WebhookPlatform,
// PAT mode is WebhookPerRepo.
func usernameForCredential(cred credentials.Credential) string {
	if cred.WebhookStrategy() == credentials.WebhookPlatform {
		return "x-access-token"
	}
	if login := cred.Identity().Login; login != "" {
		return login
	}
	return "git"
}

// classifyMintErr maps credential-package errors onto the
// BuildCredentialsService stable error set. ErrSecretNotFound (credential
// missing from store) is treated as ErrOrgDisconnected so the BFF can mark
// the task abandoned.
func classifyMintErr(err error) error {
	if errors.Is(err, credentials.ErrSecretNotFound) {
		return fmt.Errorf("%w: %v", ErrOrgDisconnected, err)
	}
	return fmt.Errorf("stage-build-secret: token: %w", err)
}
