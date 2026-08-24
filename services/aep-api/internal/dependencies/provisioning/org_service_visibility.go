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
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// StartOrgServiceVisibility drives the automated cross-project org-service flow
// for one APPROVED-but-unresolved org-service dependency at build time (issue
// #164, Task 4). The build's tag path does not block on an unresolved
// org-service (its hard gate is structural buildability only), so Build proceeds;
// this closes the gap by HOLDING the consumer's coding task until the provider
// publishes org-wide. It:
//
//  1. finds the consumer component that declares this org-service dep,
//  2. records the AccessRequest + mints/reuses the PROVIDER-side org-publish gate
//     issue (shared with the interactive RequestAccess flow via recordAccessRequest),
//  3. mints a CONSUMER-side `provision` gate issue keyed by the dep name — the
//     funnel's provisionByDep[dep] picks it up and holds the consumer's coding
//     task (funnel depsGate gates org-service ONLY when this gate exists), and
//  4. best-effort triggers the provider project's build so it deploys (and, on
//     deploy, GrantByProviderComponent publishes org-wide + resolves this gate).
//
// It is idempotent — re-clicking Build reuses the open provider issue (dedup in
// recordAccessRequest) and does not re-mint the consumer gate (dedup below), and
// the provider-build trigger is itself idempotent (Task 5's adapter treats an
// already-running provider devflow as success).
func (s *Service) StartOrgServiceVisibility(ctx context.Context, orgID, consumerProjectID, dep string) error {
	// (1) the consumer component that declares this org-service dep — the identity
	// the AccessRequest row records on the consumer side.
	consumerComponent, err := s.componentDeclaringOrgService(ctx, orgID, consumerProjectID, dep)
	if err != nil {
		return err
	}
	// (2) resolve the provider + record the access request + mint/reuse the
	// provider org-publish gate issue (the exact machinery RequestAccess uses).
	ar, err := s.recordAccessRequest(ctx, orgID, consumerProjectID, consumerComponent, dep)
	if err != nil {
		return err
	}
	// (3) mint the consumer-side visibility gate (deduped).
	if err := s.ensureConsumerVisibilityGate(ctx, orgID, consumerProjectID, dep); err != nil {
		return err
	}
	// (4) kick the provider's build so it deploys + publishes (best-effort).
	s.triggerProviderBuild(ctx, orgID, ar.ProviderProjectID)
	return nil
}

// componentDeclaringOrgService returns the name of the consumer component that
// declares depName as an org-service dependency. Returns ErrDepNotFound when no
// component declares it (or it exists as another kind).
func (s *Service) componentDeclaringOrgService(ctx context.Context, orgID, projectID, depName string) (string, error) {
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return "", fmt.Errorf("provisioning: read design: %w", err)
	}
	for i := range comps {
		for j := range comps[i].Dependencies {
			d := &comps[i].Dependencies[j]
			if strings.EqualFold(d.Name, depName) && d.Kind == spec.DependencyKindOrgService {
				return comps[i].Name, nil
			}
		}
	}
	return "", dependencies.ErrDepNotFound
}

// ensureConsumerVisibilityGate mints the CONSUMER-side gate issue for an
// org-service dep. Its aep:dep/<slug> label keys it to the dependency, so an
// open gate holds the consumer's next dispatch until the grant cascade closes
// it. Deduped like every other gate: an open gate already keyed to this dep is
// not re-minted, so re-clicking Build is idempotent.
func (s *Service) ensureConsumerVisibilityGate(ctx context.Context, orgID, consumerProjectID, dep string) error {
	existing, err := s.openProvisionDeps(ctx, orgID, consumerProjectID)
	if err != nil {
		return err
	}
	if existing[strings.ToLower(dep)] > 0 {
		return nil // an open visibility/provision gate already holds this dep
	}
	title := fmt.Sprintf("Awaiting org-service `%s`: provider must publish org-wide", dep)
	body := fmt.Sprintf("This project consumes `%s` cross-project; it must be published org-wide before dependent "+
		"components can deploy.\n\n## Awaiting `%s` org-wide visibility\n\nThis component depends on `%s` as a "+
		"cross-project org-service that is not yet published org-wide. The platform has notified the provider project "+
		"and kicked its build. This gate closes automatically once the provider publishes (namespace visibility) — no "+
		"manual action is needed here.", dep, dep, dep)
	req := sourcecontrol.CreateIssueRequest{
		Title:     title,
		Body:      body,
		Labels:    gateLabels(dep),
		DedupeKey: "gate:visibility:" + consumerProjectID + ":" + strings.ToLower(dep),
	}
	if _, err := s.issues.CreateIssue(ctx, orgID, consumerProjectID, req); err != nil {
		return fmt.Errorf("provisioning: create org-service visibility gate: %w", err)
	}
	return nil
}

// triggerProviderBuild kicks the provider project's build (best-effort). A nil
// trigger (unwired) or an error is logged and swallowed — the funnel still holds
// the consumer, and the sweep heals once the provider deploys by any other path.
func (s *Service) triggerProviderBuild(ctx context.Context, orgID, providerProjectID string) {
	if s.providerBuild == nil {
		slog.InfoContext(ctx, "provisioning: provider-build trigger not wired — skipping provider build kick",
			"providerProject", providerProjectID)
		return
	}
	if err := s.providerBuild.TriggerBuild(ctx, orgID, providerProjectID); err != nil {
		slog.WarnContext(ctx, "provisioning: trigger provider build failed (best-effort)",
			"providerProject", providerProjectID, "error", err)
	}
}

// resolveConsumerVisibilityGate completes a provision run on the consumer-side
// org-service visibility gate so it derives StatusDeployed and the consumer's
// held coding task dispatches — the grant-cascade tail (issue #164, Task 4). It
// mirrors SaveValues' tail (admit → StartWithRun → completeProvisionRow, which
// closes the gate). Best-effort throughout: a missing
// gate (an interactive-flow rider) or any step failure is logged and swallowed
// so the deploy cascade never fails over it.
func (s *Service) resolveConsumerVisibilityGate(ctx context.Context, orgID, consumerProjectID, dep string) {
	if consumerProjectID == "" || dep == "" {
		return
	}
	issueNumber, found, err := s.findProvisionIssue(ctx, orgID, consumerProjectID, dep)
	if err != nil {
		slog.WarnContext(ctx, "provisioning: find consumer visibility gate failed", "project", consumerProjectID, "dep", dep, "error", err)
		return
	}
	if !found {
		return // no consumer gate (interactive RequestAccess rider) — nothing to resolve
	}
	repo, rerr := s.repos.RepoFullName(ctx, orgID, consumerProjectID)
	if rerr != nil {
		slog.WarnContext(ctx, "provisioning: resolve consumer repo failed", "project", consumerProjectID, "error", rerr)
		return
	}
	row, admitted, aerr := s.admitProvisionRow(ctx, orgID, consumerProjectID, repo, dep, issueNumber)
	if aerr != nil {
		slog.WarnContext(ctx, "provisioning: admit consumer visibility run failed", "project", consumerProjectID, "dep", dep, "error", aerr)
		return
	}
	if !admitted {
		return // a run is already active/complete for this gate — idempotent
	}
	if _, serr := s.execs.StartWithRun(ctx, row.ID, dep); serr != nil {
		slog.WarnContext(ctx, "provisioning: start consumer visibility run failed", "execution", row.ID, "error", serr)
	}
	s.completeProvisionRow(ctx, orgID, consumerProjectID, dep, issueNumber, row.ID,
		fmt.Sprintf("Org-service `%s` published org-wide by the provider.", dep))
}
