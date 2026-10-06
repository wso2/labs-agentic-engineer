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

package organization

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ErrOrgNotFound surfaces from OrgDisconnectService when no credential
// row matches the requested ocOrgId.
var ErrOrgNotFound = errors.New("org credentials: not found")

// OrgDisconnectService runs the BFF-side disconnect cascade defined in
// phase2.md §6.7.
//
// Phase A (confirm — sub-second; runs synchronously on the request):
//   - Calls git-service's internal projection to confirm the row exists.
//     There is no intermediate 'disconnecting' status: the credential row's
//     status CHECK constraint only permits active/suspended/disconnected, and
//     because Phases A–D run synchronously on this path the finalize goes
//     straight to 'disconnected' in Phase D (phase2.md §6.7's staged
//     intermediate state was never wired).
//
// Before Phase D, the 06 §9 gitpat disconnect, in order:
//  1. the repo hooks are unregistered through the pod while it still holds
//     the gitpat (WithRepoHooks). Best effort: a failure is logged and the
//     cascade goes on, since nothing can reach GitHub as the org after the
//     next steps;
//  2. the org's AE Studio Resource is deleted (WithStudioRemover), its
//     clones and reference documents going with the pod. The studio holds
//     the org's converges from here until the cascade ends, so no status
//     read brings the pod back in between;
//  3. the github-pat and github-webhook-secret rows and their references
//     are removed (WithGitHubSecretsRemover), which closes the converge
//     gate for good;
//  4. the org's hook ids are forgotten, so a reconnect's hook repair
//     installs a hook for every project.
//
// Steps 2-4 and Phase D stop the cascade on failure: the credential stays
// active and a retry repeats the cascade, every step of which is idempotent
// (a Resource, row, reference or id already gone is done). A credential
// that is not active also closes the converge gate and the hook repair, so
// a half-run cascade never brings the pod back once Phase D ran.
//
// Phase D (org-scoped finalize):
//   - CredentialService.Disconnect marks the credential 'disconnected'. It
//     deletes no secret; step 3 already removed the vault references.
//
// Under the tasks-github-native model Tasks are GitHub issues (no
// component_tasks rows to abandon): the old Phase B/C task cascade is gone.
// Severing the credential makes the org's issues inert to the webhook router
// (no valid delivery), which is the disconnect effect.
type OrgDisconnectService struct {
	credSvc       *CredentialService
	issueSvc      sourcecontrol.IssueService
	hooks         OrgRepoHooks
	studio        StudioRemover
	removeSecrets func(ctx context.Context, org string) error
}

// OrgRepoHooks is the org-wide hook teardown of a disconnect;
// sourcecontrol.WebhookService satisfies it.
type OrgRepoHooks interface {
	UnregisterOrg(ctx context.Context, org string) error
	ForgetOrg(ctx context.Context, org string) error
}

// StudioRemover deletes an org's AE Studio and holds its converges from
// Remove until Release; aestudio.Service satisfies it.
type StudioRemover interface {
	Remove(ctx context.Context, org string) error
	Release(org string)
}

// NewOrgDisconnectService constructs the cascade orchestrator.
func NewOrgDisconnectService(
	credSvc *CredentialService,
	issueSvc sourcecontrol.IssueService,
) *OrgDisconnectService {
	return &OrgDisconnectService{
		credSvc:  credSvc,
		issueSvc: issueSvc,
	}
}

// WithRepoHooks wires steps 1 and 4: the org's repo hooks unregistered,
// then their ids forgotten. Nil skips both.
func (s *OrgDisconnectService) WithRepoHooks(h OrgRepoHooks) *OrgDisconnectService {
	s.hooks = h
	return s
}

// WithStudioRemover wires step 2, the org's AE Studio Resource delete. Nil
// skips it.
func (s *OrgDisconnectService) WithStudioRemover(r StudioRemover) *OrgDisconnectService {
	s.studio = r
	return s
}

// WithGitHubSecretsRemover wires step 3, the github-pat and
// github-webhook-secret rows and references removed. Nil skips it.
func (s *OrgDisconnectService) WithGitHubSecretsRemover(fn func(ctx context.Context, org string) error) *OrgDisconnectService {
	s.removeSecrets = fn
	return s
}

// Disconnect runs the cascade synchronously. `cause` is recorded on each
// cascaded task's Cause column so audit can distinguish manual disconnect
// from validator/webhook-driven cascades. Empty cause defaults to
// "org.disconnected".
func (s *OrgDisconnectService) Disconnect(ctx context.Context, ocOrgID, cause string) error {
	if cause == "" {
		cause = "org.disconnected"
	}
	// Phase A — confirm the row exists. If not, return ErrOrgNotFound so
	// the controller can return 200 idempotent.
	proj, err := s.credSvc.Status(ctx, ocOrgID)
	if err != nil {
		var nfe *NotFoundError
		if errors.As(err, &nfe) {
			return ErrOrgNotFound
		}
		return fmt.Errorf("disconnect Phase A: status: %w", err)
	}
	slog.InfoContext(ctx, "disconnect: starting cascade", "ocOrgId", ocOrgID, "kind", proj.Kind, "status", proj.Status)
	if proj.Status == "disconnected" {
		// Already finalized — nothing to do.
		slog.InfoContext(ctx, "disconnect: already disconnected", "ocOrgId", ocOrgID)
		return nil
	}

	if err := s.gitpatDisconnect(ctx, ocOrgID); err != nil {
		return err
	}

	// Phase D — finalize on git-service: status flip + OpenBao GC.
	if err := s.credSvc.Disconnect(ctx, ocOrgID); err != nil {
		var nfe *NotFoundError
		if errors.As(err, &nfe) {
			slog.InfoContext(ctx, "disconnect: already finalized during cascade", "ocOrgId", ocOrgID)
			return nil
		}
		return fmt.Errorf("disconnect Phase D: %w", err)
	}

	slog.InfoContext(ctx, "disconnect: cascade complete", "ocOrgId", ocOrgID)
	return nil
}

// gitpatDisconnect runs steps 1-4 (see OrgDisconnectService).
func (s *OrgDisconnectService) gitpatDisconnect(ctx context.Context, ocOrgID string) error {
	if s.hooks != nil {
		if err := s.hooks.UnregisterOrg(ctx, ocOrgID); err != nil {
			slog.WarnContext(ctx, "disconnect: repo hooks not all unregistered — they stay on GitHub and fail to deliver",
				"ocOrgId", ocOrgID, "error", err)
		}
	}
	if s.studio != nil {
		defer s.studio.Release(ocOrgID)
		if err := s.studio.Remove(ctx, ocOrgID); err != nil {
			return fmt.Errorf("disconnect: delete the AE Studio Resource: %w", err)
		}
	}
	if s.removeSecrets != nil {
		if err := s.removeSecrets(ctx, ocOrgID); err != nil {
			return fmt.Errorf("disconnect: remove the GitHub secrets: %w", err)
		}
	}
	if s.hooks != nil {
		if err := s.hooks.ForgetOrg(ctx, ocOrgID); err != nil {
			return fmt.Errorf("disconnect: forget the repo hook ids: %w", err)
		}
	}
	return nil
}
