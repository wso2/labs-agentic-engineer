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

package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// RegisterInstallationHandlers wires the handlers for App-mode lifecycle
// events. The receiver pipeline (routing → HMAC → dispatch) runs in front
// of these.
//
// Handlers registered:
//
//   - installation.created      → no-op ack (the connect callback wins
//     the race; webhook recovery is "click
//     Connect again")
//   - installation.deleted      → trigger the disconnect cascade
//   - installation.suspend      → flip status='suspended' on the row
//   - installation.unsuspend    → flip status='active' on the row
//   - installation_repositories.added   → JSON-merge selected_repos
//   - installation_repositories.removed → JSON-merge selected_repos + cascade
//
// workspaceTrash is the disconnect cascade's Phase-F disk hook (trash the
// org's workspace subtree, design §14/D12) — this package builds its own
// OrgDisconnectService, so the hook must be threaded through here too.
// nil-safe (nil = no disk hook).
func RegisterInstallationHandlers(
	router *Router,
	credSvc *organization.CredentialService,
	issueSvc sourcecontrol.IssueService,
	workspaceTrash func(ctx context.Context, ocOrgID string),
) {
	h := &installationHandler{
		credSvc:    credSvc,
		issueSvc:   issueSvc,
		disconnect: organization.NewOrgDisconnectService(credSvc, issueSvc).WithWorkspaceTrash(workspaceTrash),
	}
	router.Register("installation", "created", EventHandlerFunc(h.handleCreated))
	router.Register("installation", "deleted", EventHandlerFunc(h.handleDeleted))
	router.Register("installation", "suspend", EventHandlerFunc(h.handleSuspend))
	router.Register("installation", "unsuspend", EventHandlerFunc(h.handleUnsuspend))
	router.Register("installation_repositories", "added", EventHandlerFunc(h.handleReposAdded))
	router.Register("installation_repositories", "removed", EventHandlerFunc(h.handleReposRemoved))
}

type installationHandler struct {
	credSvc    *organization.CredentialService
	issueSvc   sourcecontrol.IssueService
	disconnect *organization.OrgDisconnectService
}

// installationPayload covers the parts of the installation /
// installation_repositories payloads we care about.
type installationPayload struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	RepositoriesAdded []struct {
		FullName string `json:"full_name"`
	} `json:"repositories_added"`
	RepositoriesRemoved []struct {
		FullName string `json:"full_name"`
	} `json:"repositories_removed"`
}

func (h *installationHandler) parse(payload []byte) (*installationPayload, error) {
	var p installationPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	if p.Installation.ID == 0 {
		return nil, errors.New("missing installation.id")
	}
	return &p, nil
}

// handleCreated is informational only. Bindings are created exclusively
// by the connect callback flow (HandleConnectCallback), which proves
// user-OAuth admin access before writing the platform row. The webhook
// confirms an install happened on the GitHub side but does not auto-bind
// — auto-binding here would re-introduce the cross-tenant binding race
// the binding-centric refactor was designed to eliminate.
func (h *installationHandler) handleCreated(ctx context.Context, _ string, _ string, payload []byte) error {
	p, err := h.parse(payload)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "webhook: installation.created (informational; bindings come from the connect flow)", "installationId", p.Installation.ID)
	return nil
}

// handleDeleted runs the disconnect cascade for the org bound to this
// installation. Same path as DELETE /api/v1/orgs/{ocOrgId}/github.
func (h *installationHandler) handleDeleted(ctx context.Context, _ string, _ string, payload []byte) error {
	p, err := h.parse(payload)
	if err != nil {
		return err
	}
	ocOrgID, err := h.credSvc.OrgIDByInstallationID(ctx, p.Installation.ID)
	if err != nil {
		var nfe *organization.NotFoundError
		if errors.As(err, &nfe) {
			// Install never connected on our side — ack noop.
			slog.InfoContext(ctx, "webhook: installation.deleted: no matching org (ack noop)", "installationId", p.Installation.ID)
			return nil
		}
		return err
	}
	// The install is already gone on GitHub (that's why we got this webhook);
	// the disconnect cascade just needs the platform-side row torn down.
	if err := h.disconnect.Disconnect(ctx, ocOrgID, "installation.deleted"); err != nil {
		if errors.Is(err, organization.ErrOrgNotFound) {
			return nil
		}
		return err
	}
	slog.InfoContext(ctx, "webhook: installation.deleted → cascade complete", "ocOrgId", ocOrgID, "installationId", p.Installation.ID)
	return nil
}

// handleSuspend flips the row to status='suspended'. New dispatches refuse
// (the resolver returns OrgNotActiveError); in-flight tasks remain in
// their current status.
func (h *installationHandler) handleSuspend(ctx context.Context, _ string, _ string, payload []byte) error {
	p, err := h.parse(payload)
	if err != nil {
		return err
	}
	if err := h.credSvc.SuspendInstallation(ctx, p.Installation.ID); err != nil {
		return err
	}
	slog.InfoContext(ctx, "webhook: installation.suspend", "installationId", p.Installation.ID)
	return nil
}

// handleUnsuspend flips the row back to status='active'.
func (h *installationHandler) handleUnsuspend(ctx context.Context, _ string, _ string, payload []byte) error {
	p, err := h.parse(payload)
	if err != nil {
		return err
	}
	if err := h.credSvc.UnsuspendInstallation(ctx, p.Installation.ID); err != nil {
		return err
	}
	slog.InfoContext(ctx, "webhook: installation.unsuspend", "installationId", p.Installation.ID)
	return nil
}

// handleReposAdded merges new selected_repos. The cascade for "added" is
// a no-op (over-permissive reach is a soft failure — the App's actual
// install state is what GitHub enforces at API call time).
func (h *installationHandler) handleReposAdded(ctx context.Context, _ string, _ string, payload []byte) error {
	p, err := h.parse(payload)
	if err != nil {
		return err
	}
	added := make([]string, 0, len(p.RepositoriesAdded))
	for _, r := range p.RepositoriesAdded {
		if r.FullName != "" {
			added = append(added, r.FullName)
		}
	}
	if len(added) == 0 {
		return nil
	}
	if err := h.credSvc.MergeSelectedRepos(ctx, p.Installation.ID, added, nil); err != nil {
		return err
	}
	slog.InfoContext(ctx, "webhook: installation_repositories.added", "installationId", p.Installation.ID, "added", added)
	return nil
}

// handleReposRemoved runs the §6.8 two-phase cascade for
// installation_repositories.removed.
//
// Phase A (org-scoped lock): merge the removed repos into the org's
// `selected_repos` JSON via git-service. Sub-second; releases the lock
// before Phase B starts.
//
// Phase B (no org lock; per-task transactions): confirm via GitHub
// that the install no longer reaches each removed repo (mitigates a
// forged-webhook abandonment), look up tasks targeting the confirmed
// repos, and apply TaskEventRepoUnselected on each — moving them to
// `abandoned` with cause `repo.unselected`. Best-effort posts a comment
// on the task's GitHub issue.
func (h *installationHandler) handleReposRemoved(ctx context.Context, _ string, _ string, payload []byte) error {
	p, err := h.parse(payload)
	if err != nil {
		return err
	}
	removed := make([]string, 0, len(p.RepositoriesRemoved))
	for _, r := range p.RepositoriesRemoved {
		if r.FullName != "" {
			removed = append(removed, r.FullName)
		}
	}
	if len(removed) == 0 {
		return nil
	}

	// --- Phase A — JSON merge under git-service's org lock. ---
	if err := h.credSvc.MergeSelectedRepos(ctx, p.Installation.ID, nil, removed); err != nil {
		return err
	}
	slog.InfoContext(ctx, "webhook: installation_repositories.removed merged",
		"installationId", p.Installation.ID, "removed", removed)

	// Tasks are GitHub issues now (the Task/Execution split): there are no task
	// rows to abandon on repo-unselect. Unselecting a repo severs the install's
	// reach, so its issues become inert to the router (no valid delivery) — the
	// same effect the old per-task cascade produced, now for free.
	return nil
}
