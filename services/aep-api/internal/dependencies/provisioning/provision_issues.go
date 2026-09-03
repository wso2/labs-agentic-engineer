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

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// provisionDep is one distinct provisioning dependency discovered in a design.
type provisionDep struct {
	name         string
	resourceType string // platform-resource only
}

// EnsureProvisionIssues mints one `provision` gate issue per distinct external
// / platform-resource dependency in the project's approved design, deduped per
// project (an open gate issue for a dependency name is never re-created). It is
// idempotent and safe to call after every plan (dependency-management §3.6 step
// 4: "Planning mints coding issues AND provisioning issues"). The gate issues
// hold their consumer coding tasks until each derives deployed. Best-effort per
// issue: a single create failure is logged and does not abort the rest.
//
// milestoneNumber joins each minted gate to the version's milestone AT CREATION
// (one call, no follow-up PATCH) so the run's dispatch predicate — "no open
// `provision` issue in this milestone" — can see it. Zero leaves gates
// unassigned.
//
// A gate is PROSE plus two labels (gate_labels.go): the `provision` kind
// and aep:dep/<slug>. designTag no longer appears anywhere on the issue — the
// milestone IS the version.
func (s *Service) EnsureProvisionIssues(ctx context.Context, orgID, projectID, designTag string, milestoneNumber int) (map[string]int, error) {
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("provisioning: read design: %w", err)
	}
	distinct := distinctProvisionDeps(comps)
	if len(distinct) == 0 {
		return nil, nil
	}

	existing, filed, err := s.provisionGatesForVersion(ctx, orgID, projectID, designTag)
	if err != nil {
		return nil, err
	}

	// gateByDep maps a lowercased dep name → its OPEN `provision` gate issue
	// number, for both pre-existing gates and the ones minted below. The build path
	// threads these numbers directly into provisioning (read-your-write from the
	// CreateIssue result) instead of re-looking them up via GitHub's
	// eventually-consistent label-filtered list, which often lags a just-created
	// gate (issue #164).
	gateByDep := make(map[string]int, len(distinct))
	for key, num := range existing {
		gateByDep[key] = num
	}

	var created int
	for key, dep := range distinct {
		// TWO reasons to skip, and the second is the one that stops a retried
		// activity filling the milestone with duplicates. `existing` is an OPEN
		// gate — a live hold, whatever version filed it. `filed` is a gate for
		// THIS version in any state, including one this same activity minted and
		// then closed on an earlier attempt (settleReadyGates closes the gate of
		// every dependency that is already Ready). Only the pair makes the mint
		// idempotent, which is what its callers have always assumed it was.
		if existing[key] > 0 || filed[key] {
			continue
		}
		title := provisionIssueTitle(dep)
		req := sourcecontrol.CreateIssueRequest{
			Title:  title,
			Body:   provisionIssueRationale(dep) + "\n\n" + provisionIssueScope(dep),
			Labels: gateLabels(dep.name),
			// Idempotent across a crashed re-run: the same dependency in the same
			// version mints the same key, and the host dedupes on it.
			DedupeKey: "gate:" + projectID + ":" + designTag + ":" + strings.ToLower(dep.name),
		}
		// The version rides a LABEL as well as the dedupe key, because the key is
		// resolved host-side against OPEN issues only and the label is what lets
		// the lookup above see a gate this version already closed.
		req.Labels = withGateVersion(req.Labels, designTag)
		if milestoneNumber > 0 {
			n := milestoneNumber
			req.Milestone = &n
		}
		res, cerr := s.issues.CreateIssue(ctx, orgID, projectID, req)
		if cerr != nil {
			slog.WarnContext(ctx, "provisioning: create gate issue failed", "dep", dep.name, "error", cerr)
			continue
		}
		// Capture the minted number from the CREATE result — read-your-write, no list.
		if res != nil {
			gateByDep[key] = res.Number
		}
		created++
	}
	if created > 0 {
		slog.InfoContext(ctx, "provisioning: minted gate issues", "project", projectID, "count", created)
	}
	return gateByDep, nil
}

// distinctProvisionDeps collects the project's distinct platform-resource
// dependencies. External values are no longer a build-time collection gate.
func distinctProvisionDeps(comps []spec.DesignComponent) map[string]provisionDep {
	out := map[string]provisionDep{}
	for i := range comps {
		for j := range comps[i].Dependencies {
			d := comps[i].Dependencies[j]
			key := strings.ToLower(d.Name)
			if key == "" {
				continue
			}
			if _, seen := out[key]; seen {
				continue
			}
			switch d.Kind {
			case spec.DependencyKindPlatformResource:
				out[key] = provisionDep{name: d.Name, resourceType: d.ResourceType}
			}
		}
	}
	return out
}

// openProvisionDeps returns dependency slugs that already have an open gate
// issue, mapped to that gate's issue number. The query is the LABEL — one
// server-side filtered list, then the aep:dep/<slug> label read back off each
// result. Only pre-existing (listable) gates are returned: a JUST-created gate
// races GitHub's eventually-consistent list, so the build path captures those
// numbers from the CreateIssue result instead (issue #164).
func (s *Service) openProvisionDeps(ctx context.Context, orgID, projectID string) (map[string]int, error) {
	open, _, err := s.provisionGatesForVersion(ctx, orgID, projectID, "")
	return open, err
}

// provisionGatesForVersion reads the project's dependency gates once and answers
// the mint's two different questions about them.
//
//	open   dep slug → the number of an OPEN gate for it, ANY version. A live
//	       hold, and the number the build path threads into provisioning so a
//	       provision run is admitted against the gate it will close.
//	filed  dep slugs that already have a gate for THIS version, in any state.
//	       Suppresses the mint and nothing else — a closed gate is not a hold, so
//	       it must never be threaded anywhere as one.
//
// The two are separate because a closed gate has to suppress a re-mint while
// remaining invisible as a hold, and collapsing them would either resurrect
// duplicate gates or admit a provision run against an issue nothing derives
// from. An empty tag asks only the first question, which is what
// openProvisionDeps wants.
//
// State is read off the issue rather than filtered server-side: this is one
// label-filtered list and both answers come out of the same pass.
func (s *Service) provisionGatesForVersion(ctx context.Context, orgID, projectID, tag string) (
	open map[string]int, filed map[string]bool, err error) {
	issues, err := s.issues.ListIssues(ctx, orgID, projectID, []string{delivery.KindProvision})
	if err != nil {
		return nil, nil, fmt.Errorf("provisioning: list issues: %w", err)
	}
	open, filed = map[string]int{}, map[string]bool{}
	for _, issue := range issues {
		dep := gateDepFromLabels(issue.Labels)
		if dep == "" {
			continue
		}
		if gateIsForVersion(issue.Labels, tag) {
			filed[dep] = true
		}
		if strings.EqualFold(issue.State, "open") {
			open[dep] = issue.Number
		}
	}
	return open, filed, nil
}

func provisionIssueTitle(dep provisionDep) string {
	if dep.resourceType != "" {
		return fmt.Sprintf("Provision resource: %s (%s)", dep.name, dep.resourceType)
	}
	return "Provision resource: " + dep.name
}

func provisionIssueRationale(provisionDep) string {
	return "A platform resource this project depends on must be provisioned before dependent components can deploy."
}

func provisionIssueScope(dep provisionDep) string {
	return fmt.Sprintf("## Provision `%s`\n\nConfirm the provisioning parameters for this platform resource in the "+
		"architecture drawer. The platform provisions it and closes this issue once the resource is ready — "+
		"no manual action on this issue is needed.", dep.name)
}
