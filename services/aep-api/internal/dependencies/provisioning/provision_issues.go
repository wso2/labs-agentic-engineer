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

// provisionGate is the version-scoped result of reconciling one dependency's
// gate. completed means the same milestone already has a closed gate, so a
// retried activity must not mint another issue or author the resource again.
type provisionGate struct {
	number    int
	completed bool
}

// EnsureProvisionIssues mints one aep:provision gate issue per distinct
// platform-resource dependency in the project's approved design, deduped per
// version. An open gate is reused; a closed gate is returned as completed so a
// retried activity cannot recreate or re-author it. It is idempotent and safe
// to call after every plan (dependency-management §3.6 step
// 4: "Planning mints coding issues AND provisioning issues"). The gate issues
// hold their consumer coding tasks until each derives deployed. Best-effort per
// issue: a single create failure is logged and does not abort the rest.
//
// milestoneNumber joins each minted gate to the version's milestone AT CREATION
// (one call, no follow-up PATCH) so the run's dispatch predicate — "no open
// aep:provision issue in this milestone" — can see it. Zero leaves gates
// unassigned.
//
// A gate is PROSE plus two labels (gate_labels.go): the aep:provision marker
// and aep:dep/<slug>. designTag no longer appears anywhere on the issue — the
// milestone IS the version.
func (s *Service) EnsureProvisionIssues(ctx context.Context, orgID, projectID, designTag string, milestoneNumber int) (map[string]provisionGate, error) {
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("provisioning: read design: %w", err)
	}
	distinct := distinctProvisionDeps(comps)
	if len(distinct) == 0 {
		return nil, nil
	}

	existing, err := s.versionProvisionDeps(ctx, orgID, projectID, milestoneNumber)
	if err != nil {
		return nil, err
	}

	// gateByDep maps a lowercased dep name to its version-scoped gate, for both
	// pre-existing gates and the ones minted below. The build path
	// threads these numbers directly into provisioning (read-your-write from the
	// CreateIssue result) instead of re-looking them up via GitHub's
	// eventually-consistent label-filtered list, which often lags a just-created
	// gate (issue #164).
	gateByDep := make(map[string]provisionGate, len(distinct))
	for key, gate := range existing {
		gateByDep[key] = gate
	}

	var created int
	for key, dep := range distinct {
		if existing[key].number > 0 {
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
			gateByDep[key] = provisionGate{number: res.Number}
		}
		created++
	}
	if created > 0 {
		slog.InfoContext(ctx, "provisioning: minted gate issues", "project", projectID, "count", created)
	}
	return gateByDep, nil
}

// versionProvisionDeps returns gates already belonging to this version. A
// closed gate is a terminal success for the activity operation and must remain
// visible to retries. Milestone zero is retained for legacy callers/tests and
// can only provide the historical project-wide open-gate behavior.
func (s *Service) versionProvisionDeps(ctx context.Context, orgID, projectID string, milestoneNumber int) (map[string]provisionGate, error) {
	if milestoneNumber == 0 {
		open, err := s.openProvisionDeps(ctx, orgID, projectID)
		if err != nil {
			return nil, err
		}
		out := make(map[string]provisionGate, len(open))
		for dep, number := range open {
			out[dep] = provisionGate{number: number}
		}
		return out, nil
	}
	issues, err := s.issues.ListMilestoneIssues(ctx, orgID, projectID, sourcecontrol.MilestoneIssuesFilter{
		Number: milestoneNumber,
		State:  "all",
		Labels: []string{delivery.LabelProvisionGate},
	})
	if err != nil {
		return nil, fmt.Errorf("provisioning: list milestone issues: %w", err)
	}
	out := map[string]provisionGate{}
	for _, issue := range issues {
		dep := gateDepFromLabels(issue.Labels)
		if dep == "" {
			continue
		}
		gate := provisionGate{number: issue.Number, completed: strings.EqualFold(issue.State, "closed")}
		// Prefer an open gate if historical duplicates exist: it is the live hold
		// this attempt must reconcile. Otherwise retain the closed terminal gate.
		if current, ok := out[dep]; !ok || current.completed {
			out[dep] = gate
		}
	}
	return out, nil
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
	issues, err := s.issues.ListIssues(ctx, orgID, projectID, []string{delivery.LabelProvisionGate})
	if err != nil {
		return nil, fmt.Errorf("provisioning: list issues: %w", err)
	}
	out := map[string]int{}
	for _, issue := range issues {
		if !strings.EqualFold(issue.State, "open") {
			continue
		}
		if dep := gateDepFromLabels(issue.Labels); dep != "" {
			out[dep] = issue.Number
		}
	}
	return out, nil
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
