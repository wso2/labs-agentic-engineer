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
// Before Phase D (06 §9 gitpat disconnect), while the org's AE Studio pod
// still holds the gitpat:
//   - the repo hooks are unregistered through the pod (WithHookUnregistrar).
//     Best effort: a failure is logged and the cascade goes on, since nothing
//     can reach GitHub as the org after the next steps;
//   - the org's AE Studio Resource is deleted (WithStudioRemover), and its
//     clones and reference documents go with the pod. A failure stops the
//     cascade before Phase D, so the credential stays and a retry repeats it
//     rather than leaving a pod running with the gitpat of an org that reads
//     as disconnected.
//
// Phase D (org-scoped finalize — git-service GC):
//   - DELETE /internal/credentials/orgs/{ocOrgId} on git-service. Git-service
//     marks status='disconnected' and best-effort GCs OpenBao keys.
//
// Under the tasks-github-native model Tasks are GitHub issues (no
// component_tasks rows to abandon): the old Phase B/C task cascade is gone.
// Severing the credential makes the org's issues inert to the webhook router
// (no valid delivery), which is the disconnect effect.
type OrgDisconnectService struct {
	credSvc         *CredentialService
	issueSvc        sourcecontrol.IssueService
	unregisterHooks func(ctx context.Context, org string) error
	removeStudio    func(ctx context.Context, org string) error
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

// WithHookUnregistrar wires the step that removes the org's repo hooks
// before Phase D. Nil skips it.
func (s *OrgDisconnectService) WithHookUnregistrar(fn func(ctx context.Context, org string) error) *OrgDisconnectService {
	s.unregisterHooks = fn
	return s
}

// WithStudioRemover wires the step that deletes the org's AE Studio
// Resource before Phase D. Nil skips it.
func (s *OrgDisconnectService) WithStudioRemover(fn func(ctx context.Context, org string) error) *OrgDisconnectService {
	s.removeStudio = fn
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

	// 06 §9, while the pod still holds the gitpat: the hooks first (best
	// effort), then the pod itself.
	if s.unregisterHooks != nil {
		if err := s.unregisterHooks(ctx, ocOrgID); err != nil {
			slog.WarnContext(ctx, "disconnect: repo hooks not all unregistered — they stay on GitHub and fail to deliver",
				"ocOrgId", ocOrgID, "error", err)
		}
	}
	if s.removeStudio != nil {
		if err := s.removeStudio(ctx, ocOrgID); err != nil {
			return fmt.Errorf("disconnect: delete the AE Studio Resource: %w", err)
		}
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
