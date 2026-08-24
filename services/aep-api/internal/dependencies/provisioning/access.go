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

package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// RequestAccess records a consumer's request to consume a cross-project
// org-service and ensures a provider-side `provision` org-publish gate issue
// exists (deduped: a second consumer of the same provider component rides the
// existing issue). It re-keys upstream's ProviderTaskID (a component_tasks row)
// to the provider org-publish ISSUE (dependency-management §3.2 item 5). The
// functional gate is Phase 5's proceed-gate over the live catalog; this row is
// the tracking chip + the provider-side publish nudge.
func (s *Service) RequestAccess(ctx context.Context, orgID, consumerProjectID, consumerComponent, orgServiceName string) (*dependencies.AccessRequest, error) {
	if s.access == nil || s.providers == nil {
		return nil, ErrOrgServiceNotFound
	}
	// (a) the addressed dependency must be an org-service on the consumer design.
	if _, err := s.findDepInProject(ctx, orgID, consumerProjectID, orgServiceName, spec.DependencyKindOrgService); err != nil {
		return nil, err
	}
	return s.recordAccessRequest(ctx, orgID, consumerProjectID, consumerComponent, orgServiceName)
}

// recordAccessRequest resolves the org-service provider, dedupes against an open
// request for that provider component (a second consumer rides the existing
// provider org-publish issue), creates the provider-side org-publish gate issue
// when none is open, and records the dependencies.AccessRequest row. It is the shared
// core of BOTH the interactive RequestAccess flow (access_huma.go) and the
// automated build-time StartOrgServiceVisibility flow (issue #164, Task 4) — the
// single place the provider is resolved + the org-publish issue is minted, so the
// two entry points never diverge or double-mint. It does NOT validate the
// consumer design (callers do that with the context they have).
func (s *Service) recordAccessRequest(ctx context.Context, orgID, consumerProjectID, consumerComponent, orgServiceName string) (*dependencies.AccessRequest, error) {
	if s.access == nil || s.providers == nil {
		return nil, ErrOrgServiceNotFound
	}
	// resolve the provider (at any visibility — the point is to request a
	// not-yet-published one).
	target, ok, err := s.providers.FindByComponent(ctx, orgID, orgServiceName)
	if err != nil {
		return nil, fmt.Errorf("provisioning: resolve org-service %q: %w", orgServiceName, err)
	}
	if !ok {
		return nil, ErrOrgServiceNotFound
	}
	providerProject := target.Project
	providerComponent := logicalComponent(target.Project, target.Component)

	// idempotency: an open request/issue for this provider component → the new
	// consumer rides the same provider issue.
	open, err := s.access.FindOpenForTarget(ctx, orgID, providerProject, providerComponent)
	if err != nil {
		return nil, fmt.Errorf("provisioning: find open access request: %w", err)
	}

	ar := &dependencies.AccessRequest{
		OrgID:                 orgID,
		ConsumerProjectID:     consumerProjectID,
		ConsumerComponentName: consumerComponent,
		OrgServiceName:        orgServiceName,
		ProviderProjectID:     providerProject,
		ProviderComponentName: providerComponent,
		Status:                dependencies.AccessRequestStatusRequested,
	}
	if open != nil {
		ar.ProviderTaskID = open.ProviderTaskID
		ar.ProviderIssueNumber = open.ProviderIssueNumber
		ar.ProviderIssueURL = open.ProviderIssueURL
	} else {
		// create the provider-side org-publish gate issue.
		issue, cerr := s.createOrgPublishIssue(ctx, orgID, providerProject, providerComponent, orgServiceName)
		if cerr != nil {
			return nil, cerr
		}
		ar.ProviderIssueNumber = issue.Number
		ar.ProviderIssueURL = issue.URL
		ar.ProviderTaskID = providerTaskKey(providerProject, issue.Number)
	}
	if err := s.access.Create(ctx, ar); err != nil {
		return nil, fmt.Errorf("provisioning: create access request: %w", err)
	}
	return ar, nil
}

// ListAccessRequests returns a consumer project's access requests (newest first
// is the repository's concern).
func (s *Service) ListAccessRequests(ctx context.Context, orgID, consumerProjectID string) ([]dependencies.AccessRequest, error) {
	if s.access == nil {
		return nil, nil
	}
	return s.access.ListByConsumerProject(ctx, orgID, consumerProjectID)
}

// OnComponentDeployed is the deploy-observer entry point (codingagent.DeployObserver):
// a component just deployed, so grant any pending cross-project access request
// targeting it. Delegates to GrantByProviderComponent.
func (s *Service) OnComponentDeployed(ctx context.Context, orgID, projectID, component string) error {
	return s.GrantByProviderComponent(ctx, orgID, projectID, component)
}

// GrantByProviderComponent is the deploy-cascade hook: when a provider component
// deploys, any open access request targeting it flips to granted, the
// exposesAPI.orgPublished durability marker is committed on the provider design,
// and its org-publish issue closes. Best-effort; only the gating read returns an
// error. (The functional resume is the provider going namespace-visible, which
// Phase 5's proceed-gate observes over the live catalog; the marker persists the
// publish decision across redeploys.)
func (s *Service) GrantByProviderComponent(ctx context.Context, orgID, providerProjectID, providerComponent string) error {
	if s.access == nil {
		return nil
	}
	open, err := s.access.FindOpenForTarget(ctx, orgID, providerProjectID, providerComponent)
	if err != nil {
		return fmt.Errorf("provisioning: find open access request: %w", err)
	}
	if open == nil {
		return nil // a normal deploy with no pending cross-project access request
	}
	// Persist the provider's publish decision (best-effort — the live-catalog
	// gate already resolves the consumer even if this commit hiccups).
	if s.orgPublish != nil {
		if merr := s.orgPublish.MarkOrgPublished(ctx, orgID, providerProjectID, providerComponent); merr != nil {
			slog.WarnContext(ctx, "provisioning: commit orgPublished marker failed",
				"project", providerProjectID, "component", providerComponent, "error", merr)
		}
	}
	rows, err := s.access.ListByProviderTask(ctx, open.ProviderTaskID)
	if err != nil {
		slog.WarnContext(ctx, "provisioning: list riders for grant failed", "providerTask", open.ProviderTaskID, "error", err)
		rows = []dependencies.AccessRequest{*open}
	}
	for i := range rows {
		if rows[i].Status == dependencies.AccessRequestStatusGranted {
			continue
		}
		if uerr := s.access.UpdateStatus(ctx, rows[i].ID, dependencies.AccessRequestStatusGranted); uerr != nil {
			slog.WarnContext(ctx, "provisioning: grant access request failed", "id", rows[i].ID, "error", uerr)
		}
		// Resolve the CONSUMER-side visibility gate (issue #164, Task 4): the
		// automated build flow minted a `provision` gate in the consumer project
		// keyed by this org-service dep. Complete a provision run on it so it derives
		// deployed and the consumer's held coding task dispatches. A rider from the
		// interactive RequestAccess flow has no consumer gate — that resolves to a
		// silent no-op. Best-effort: never fail the deploy cascade over it.
		s.resolveConsumerVisibilityGate(ctx, rows[i].OrgID, rows[i].ConsumerProjectID, rows[i].OrgServiceName)
	}
	if open.ProviderIssueNumber > 0 {
		comment := fmt.Sprintf("Published %s org-wide (namespace visibility). Closing — consumers will resume automatically.", providerComponent)
		if cerr := s.issues.CloseIssue(ctx, orgID, providerProjectID, open.ProviderIssueNumber, comment); cerr != nil {
			slog.WarnContext(ctx, "provisioning: close org-publish issue failed", "issue", open.ProviderIssueNumber, "error", cerr)
		}
	}
	return nil
}

// OnIssueClosed is the issues/closed webhook handler (registered alongside
// task's noop via the router's handler chain). It is the "declined" signal for
// the reject cascade: an org-publish gate issue that closes while its consumer
// access requests are still ungranted is a decline, so those riders flip to
// rejected. The grant path (GrantByProviderComponent) flips riders to granted
// BEFORE it closes the issue, so a platform-driven close finds them granted and
// this no-ops — only a manual/decline close catches open riders. Best-effort:
// a non-org-publish issue (no riders keyed to it) is a silent no-op, and the
// handler never fails the delivery for a routine close.
func (s *Service) OnIssueClosed(ctx context.Context, _, _ string, payload []byte) error {
	if s.access == nil || s.repos == nil {
		return nil
	}
	var p struct {
		Issue struct {
			Number int `json:"number"`
		} `json:"issue"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil // malformed payload — ack, nothing to do
	}
	if p.Repository.FullName == "" || p.Issue.Number == 0 {
		return nil
	}
	orgID, providerProjectID, err := s.repos.ByFullName(ctx, p.Repository.FullName)
	if err != nil {
		return nil // unknown repo — not one of ours, ack
	}
	taskKey := providerTaskKey(providerProjectID, p.Issue.Number)
	riders, err := s.access.ListByProviderTask(ctx, taskKey)
	if err != nil {
		return fmt.Errorf("provisioning: list riders for reject: %w", err)
	}
	if len(riders) == 0 {
		return nil // not an org-publish gate issue (no access requests key to it)
	}
	rejected := 0
	for i := range riders {
		switch riders[i].Status {
		case dependencies.AccessRequestStatusGranted, dependencies.AccessRequestStatusRejected:
			continue // already resolved — a grant-driven close, not a decline
		}
		if uerr := s.access.UpdateStatus(ctx, riders[i].ID, dependencies.AccessRequestStatusRejected); uerr != nil {
			slog.WarnContext(ctx, "provisioning: reject access request failed", "id", riders[i].ID, "error", uerr)
			continue
		}
		rejected++
	}
	if rejected > 0 {
		slog.InfoContext(ctx, "provisioning: org-publish declined — rejected pending access requests",
			"orgID", orgID, "project", providerProjectID, "issue", p.Issue.Number, "count", rejected)
	}
	return nil
}

// createOrgPublishIssue mints the provider-side org-publish gate issue. It
// carries no secrets — only the component name being published, on its
// aep:dep/<slug> label and in its prose.
func (s *Service) createOrgPublishIssue(ctx context.Context, orgID, providerProjectID, providerComponent, orgServiceName string) (*sourcecontrol.IssueResult, error) {
	title := fmt.Sprintf("Publish %s org-wide: add namespace visibility", providerComponent)
	body := fmt.Sprintf("Another project requested access to `%s`.\n\n## Publish `%s` org-wide\n\nA consumer "+
		"project depends on this component as an org-service. Publish it (set `exposesAPI.orgPublished` + redeploy "+
		"with namespace visibility) so the consumer can resolve it. The platform closes this issue and grants the "+
		"request once the component is published.", orgServiceName, providerComponent)
	issue, err := s.issues.CreateIssue(ctx, orgID, providerProjectID, sourcecontrol.CreateIssueRequest{
		Title:     title,
		Body:      body,
		Labels:    gateLabels(providerComponent),
		DedupeKey: "gate:publish:" + providerProjectID + ":" + strings.ToLower(providerComponent),
	})
	if err != nil {
		return nil, fmt.Errorf("provisioning: create org-publish issue: %w", err)
	}
	return issue, nil
}

// logicalComponent strips the `<project>-` prefix an OC component name carries,
// yielding the design's logical component name.
func logicalComponent(project, ocComponent string) string {
	return strings.TrimPrefix(ocComponent, project+"-")
}

// providerTaskKey is the stable ProviderTaskID re-keyed to the provider issue
// (was a component_tasks row id upstream): "<providerProject>#<issueNumber>".
func providerTaskKey(providerProjectID string, issueNumber int) string {
	return fmt.Sprintf("%s#%d", providerProjectID, issueNumber)
}
