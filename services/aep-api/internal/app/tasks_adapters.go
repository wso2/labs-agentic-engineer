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

package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/eventcore"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/delivery/codingagent"
	"github.com/wso2/aep/aep-api/internal/delivery/execution"
	"github.com/wso2/aep/aep-api/internal/delivery/task"
	"github.com/wso2/aep/aep-api/internal/dependencies/provisioning"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The composition-root adapters that satisfy the tasks/execution/codingagent
// consumer ports whose method shapes do not match a service verbatim. Each is a
// thin projection — the features stay free of the concrete services these wrap.

// repoLocator resolves a GitHub "owner/name" to the owning org + project.
// Satisfies eventcore.RepoLookup and provisioning.RepoLocator.
type repoLocator struct{ db *gorm.DB }

func (r repoLocator) ByFullName(_ context.Context, fullName string) (string, string, error) {
	return sourcecontrol.LookupOrgProjectByRepoURL(r.db, fullName)
}

// taskSnapshotAdapter reads a Task's current snapshot for the task-log stream's
// `task` frame: the full TaskDetail JSON (forwarded verbatim — the stream never
// unmarshals it) plus the derived status the stream uses to detect settle. The
// projection lives here at the composition root because execution never imports
// the task read path. Satisfies execution.TaskSnapshotReader.
type taskSnapshotAdapter struct{ reads *task.Reads }

func (a taskSnapshotAdapter) TaskSnapshot(ctx context.Context, orgID, projectID string, issueNumber int) (*execution.TaskSnapshot, error) {
	detail, err := a.reads.Get(ctx, orgID, projectID, issueNumber)
	if err != nil {
		if errors.Is(err, task.ErrTaskNotFound) {
			return nil, nil // not a Task → the stream answers 404 before opening
		}
		return nil, err
	}
	// The `task` frame carries the TaskView (header/status/lineage) only — the
	// stream's `execution` frames own the live execution list, so the redundant
	// executionHistory stays off the wire.
	raw, err := json.Marshal(detail.TaskView)
	if err != nil {
		return nil, err
	}
	return &execution.TaskSnapshot{JSON: raw, DerivedStatus: detail.DerivedStatus}, nil
}

// executionsByIssueAdapter lists a Task's execution rows (oldest first) for the
// task-log stream to walk into one chronological timeline — repo full name via
// the repo row, then the org-fenced by-issue history. Satisfies
// execution.ExecutionHistory.
type executionsByIssueAdapter struct {
	repos sourcecontrol.RepoRepository
	execs delivery.ExecutionRepository
}

func (a executionsByIssueAdapter) ByIssue(ctx context.Context, orgID, projectID string, issueNumber int) ([]delivery.Execution, error) {
	full, err := repoFullNameLookup{repos: a.repos}.RepoFullName(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return a.execs.ListByIssueScoped(ctx, orgID, full, issueNumber)
}

// designComponents exposes the design's component names at HEAD for the funnel's
// dispatch-time re-verification. Satisfies eventcore.DesignReader.
type designComponents struct{ store *spec.ArtifactStore }

func (d designComponents) ComponentNames(ctx context.Context, orgID, projectID string) (map[string]bool, error) {
	design, err := d.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if design == nil {
		return nil, nil
	}
	names := make(map[string]bool, len(design.Components))
	for _, c := range design.Components {
		names[strings.ToLower(c.Name)] = true
	}
	return names, nil
}

// ComponentPaths maps each design component's name (verbatim, as authored in
// the design) to its source directory (appPath) for the path-based build
// trigger. Callers match against these keys case-insensitively. Satisfies
// eventcore.DesignReader.
func (d designComponents) ComponentPaths(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	design, err := d.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if design == nil {
		return nil, nil
	}
	paths := make(map[string]string, len(design.Components))
	for _, c := range design.Components {
		// Real (design) name — the build path uses it verbatim to trigger the
		// build; matching against facts.Component is done case-insensitively.
		paths[c.Name] = strings.Trim(c.AppPath, "/")
	}
	return paths, nil
}

// DeclaredResources maps each design component to its App Path plus the wiring
// its design says it consumes — resource refs and sibling endpoint targets, both
// read off each dependency's platform-stamped `wiring` (spec/derive_wiring.go). A
// dependency with no wiring is skipped: it is not derivable yet, so no agent could
// have wired it. Satisfies eventcore.DesignReader's conformance half.
func (d designComponents) DeclaredResources(ctx context.Context, orgID, projectID string) (map[string]eventcore.ComponentResources, error) {
	design, err := d.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if design == nil {
		return nil, nil
	}
	out := make(map[string]eventcore.ComponentResources, len(design.Components))
	for _, c := range design.Components {
		entry := eventcore.ComponentResources{AppPath: strings.Trim(c.AppPath, "/")}
		for _, dep := range c.Dependencies {
			switch {
			case dep.Wiring == nil:
			case dep.Wiring.Endpoint != nil && dep.Wiring.Endpoint.Component != "":
				entry.EndpointTargets = append(entry.EndpointTargets, dep.Wiring.Endpoint.Component)
			case dep.Wiring.Ref != "":
				entry.Refs = append(entry.Refs, dep.Wiring.Ref)
			}
		}
		out[c.Name] = entry
	}
	return out, nil
}

// ReadDesignComponents exposes the project's authored design components at HEAD.
// Satisfies provisioning.DesignReader (and dependencies/resources.DesignReader).
func (d designComponents) ReadDesignComponents(ctx context.Context, orgID, projectID string) ([]spec.DesignComponent, error) {
	design, err := d.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if design == nil {
		return nil, nil
	}
	return design.Components, nil
}

// ProvisionDepNames exposes each component's provisioning dependencies (external
// + platform-resource) for the funnel's dependency-kind-aware gate. Satisfies
// eventcore.DesignReader.
func (d designComponents) ProvisionDepNames(ctx context.Context, orgID, projectID string) (map[string][]string, error) {
	design, err := d.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if design == nil {
		return nil, nil
	}
	out := make(map[string][]string, len(design.Components))
	for _, c := range design.Components {
		if deps := c.ProvisionDependsOn(); len(deps) > 0 {
			out[strings.ToLower(c.Name)] = deps
		}
	}
	return out, nil
}

// OrgServiceDepNames exposes each component's cross-project org-service
// dependencies for the funnel's conditional org-service gate (issue #164, Task 4).
// Satisfies eventcore.DesignReader.
func (d designComponents) OrgServiceDepNames(ctx context.Context, orgID, projectID string) (map[string][]string, error) {
	design, err := d.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if design == nil {
		return nil, nil
	}
	out := make(map[string][]string, len(design.Components))
	for _, c := range design.Components {
		if deps := c.OrgServiceDependsOn(); len(deps) > 0 {
			out[strings.ToLower(c.Name)] = deps
		}
	}
	return out, nil
}

// runnerSecretResolver adapts provisioning.Service.ResolveComponentRunnerSecrets
// onto the codingagent.RunnerSecretResolver port, mapping the resources DTO to
// the codingagent input type (so codingagent holds no provisioning/resources
// import). Satisfies codingagent.RunnerSecretResolver.
type runnerSecretResolver struct{ svc *provisioning.Service }

func (r runnerSecretResolver) ResolveRunnerSecrets(ctx context.Context, orgID, projectID, component, env string) ([]codingagent.ExternalResourceSecretInputs, error) {
	srs, err := r.svc.ResolveComponentRunnerSecrets(ctx, orgID, projectID, component, env)
	if err != nil {
		return nil, err
	}
	out := make([]codingagent.ExternalResourceSecretInputs, 0, len(srs))
	for _, s := range srs {
		out = append(out, codingagent.ExternalResourceSecretInputs{KVPath: s.KVPath, Keys: s.Keys})
	}
	return out, nil
}

// repoNamer resolves an org+project to its GitHub repo full name ("owner/name")
// — the provision Execution row's Repo must equal the gate issue's repo full
// name so the funnel gate resolves the run. Satisfies provisioning.RepoLocator.
type repoNamer struct {
	repos sourcecontrol.RepoRepository
	db    *gorm.DB
}

// RepoFullName delegates to the shared repoFullNameLookup implementation (they
// resolve identically); repoNamer only adds the reverse ByFullName lookup below.
func (r repoNamer) RepoFullName(ctx context.Context, orgID, projectID string) (string, error) {
	return repoFullNameLookup{repos: r.repos}.RepoFullName(ctx, orgID, projectID)
}

// ByFullName is the reverse lookup (`<owner>/<repo>` → org/project) the
// issues/closed webhook uses to find the provider project of a declined
// org-publish gate issue. Satisfies provisioning.RepoLocator.
func (r repoNamer) ByFullName(_ context.Context, fullName string) (string, string, error) {
	return sourcecontrol.LookupOrgProjectByRepoURL(r.db, fullName)
}

// provisionProjects enumerates an org's ready projects for the provisioning
// feature's cross-project design scan (external-resource consumers, teardown).
// Satisfies provisioning.ProjectLister.
type provisionProjects struct{ repos sourcecontrol.RepoRepository }

func (p provisionProjects) ListProjects(ctx context.Context, orgID string) ([]provisioning.ProjectRef, error) {
	rows, err := p.repos.ListAllReady(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]provisioning.ProjectRef, 0, len(rows))
	for i := range rows {
		if rows[i].OrgID != orgID {
			continue
		}
		out = append(out, provisioning.ProjectRef{OrgID: rows[i].OrgID, ProjectID: rows[i].ProjectID})
	}
	return out, nil
}

// identities projects organization.CredentialService.IdentityFor onto the
// codingagent.Identities port.
type identities struct {
	cred *organization.CredentialService
}

func (a identities) IdentityFor(ctx context.Context, ocOrgID string) (name, email, login string, err error) {
	id, err := a.cred.IdentityFor(ctx, ocOrgID)
	if err != nil {
		return "", "", "", err
	}
	login = id.Login
	if login == "" {
		login = id.GitHubLogin
	}
	return id.Name, id.Email, login, nil
}

// githubBotLogin returns the platform's GitHub App bot login used for webhook
// echo suppression (§9.2). App-authored actions arrive with sender.login =
// "<slug>[bot]". Empty slug (dev/PAT mode) disables suppression — idempotency
// (TryAdmit, convergent repair) keeps that path safe.
func githubBotLogin(appSlug string) string {
	if appSlug == "" {
		return ""
	}
	return appSlug + "[bot]"
}

// cycleOrgLookup resolves a run-cycle id to its owning org handle — the
// RunnerAuthorizer's publisher-cc branch.
//
// It reads run_cycles because that is what a runner callback names: every agent
// pod is launched by the milestone supervisor, which carries the cycle id to the
// pod as AEP_TASK_ID. It deliberately does NOT fall back to the executions table.
// The only rows left there are dependency-provisioning gates, which run no agent
// and are not a callback identity, so an execution id named in a path resolves to
// nothing and the request fails closed.
func cycleOrgLookup(db *gorm.DB) func(ctx context.Context, cycleID string) (string, error) {
	return func(ctx context.Context, cycleID string) (string, error) {
		var row delivery.RunCycle
		if err := db.WithContext(ctx).Select("org_id").First(&row, "id = ?", cycleID).Error; err != nil {
			return "", err
		}
		return row.OrgID, nil
	}
}
