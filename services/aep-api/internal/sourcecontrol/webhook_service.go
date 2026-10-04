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

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// WebhookService manages the per-repo webhook that delivers a project's
// GitHub events to its org's AE Studio pod. The pod owns the delivery URL and
// the signing secret; this service only asks for the hook and remembers its
// ID on the repo row.
type WebhookService interface {
	// Register installs the webhook on the project's repo and persists its
	// hook ID. Idempotent: the pod answers an existing hook to its URL.
	Register(ctx context.Context, orgID, projectID string) (hookID *int64, err error)

	// Unregister removes the hook Register installed, addressed by the hook ID
	// persisted on the repo row. It is the project teardown's counterpart to
	// Register and is total: a project with no repo row, no stored hook, or a
	// hook GitHub no longer has all resolve to "nothing to remove" and return
	// nil. Only a live failure to reach the pod or GitHub is an error, and even
	// that is best-effort at the call site — the delete is never blocked by
	// webhook cleanup.
	Unregister(ctx context.Context, orgID, projectID string) error
}

// subscribedEvents are the events every project hook carries. Repo-level
// webhooks only — App-installation events like installation_repositories are
// rejected by GitHub on repo webhooks (422). "issues" joins the set for the
// tasks-github-native model (§9.2): task birth, command labels, block
// validation/repair, close/reopen. The pod refuses any other event.
var subscribedEvents = []string{"pull_request", "push", "issue_comment", "issues"}

type webhookService struct {
	repo    RepoRepository
	github  WebhookOps
	repoSvc RepoService
}

func NewWebhookService(repo RepoRepository, github WebhookOps, repoSvc RepoService) WebhookService {
	return &webhookService{repo: repo, github: github, repoSvc: repoSvc}
}

func (s *webhookService) Register(ctx context.Context, orgID, projectID string) (*int64, error) {
	ref, _, err := RepoRefFor(ctx, s.repo, orgID, projectID)
	if err != nil {
		return nil, err
	}

	hookID, err := s.github.RegisterWebhook(ctx, ref, subscribedEvents)
	if err != nil {
		return nil, fmt.Errorf("register webhook: %w", err)
	}

	// Reconcile the event list on the hook. RegisterWebhook's already-exists
	// path returns a pre-existing hook WITHOUT updating its events, so a hook
	// created before "issues" joined the subscription would never receive
	// issue deliveries. PATCHing the events every register makes cutover
	// idempotent (§9.2). Best-effort: a reconcile failure must not block a
	// successful registration.
	if patchErr := s.github.UpdateWebhookEvents(ctx, ref, hookID, subscribedEvents); patchErr != nil {
		slog.WarnContext(ctx, "reconcile webhook events failed", "project", projectID, "hookId", hookID, "error", patchErr)
	}

	if err := s.repoSvc.SetWebhookID(ctx, orgID, projectID, hookID); err != nil {
		return nil, fmt.Errorf("persist webhook id: %w", err)
	}
	return &hookID, nil
}

// Unregister removes the hook Register installed. See the interface for the
// contract; the shape below is Register's, run backwards.
//
// It must be called BEFORE the repo row is deleted: the row carries both the
// hook ID and the repo identity, and once it is gone the platform has no way
// left to name the hook it created.
func (s *webhookService) Unregister(ctx context.Context, orgID, projectID string) error {
	// The hook ID is the whole point: it is what makes this removal precise. A
	// project that never registered one — a failed registration, a repo
	// provisioned before hooks existed — has nothing of ours on the repo to
	// remove.
	repo, err := s.repoSvc.GetRepo(ctx, orgID, projectID)
	if err != nil {
		if errors.Is(err, ErrRepoNotFound) {
			return nil
		}
		return fmt.Errorf("get repo: %w", err)
	}
	if repo == nil || repo.WebhookID == nil {
		return nil
	}
	ref, err := RefForRow(orgID, repo)
	if err != nil {
		return err
	}

	if err := s.github.DeleteWebhook(ctx, ref, *repo.WebhookID); err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	slog.InfoContext(ctx, "webhook unregistered from repo",
		"org", orgID, "project", projectID, "hookId", *repo.WebhookID)
	return nil
}
