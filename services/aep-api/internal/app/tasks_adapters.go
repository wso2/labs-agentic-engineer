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
	"strings"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/codingagent"
	"github.com/wso2/aep/aep-api/internal/feature/execution"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/internal/feature/orgcreds"
	"github.com/wso2/aep/aep-api/internal/feature/provisioning"
	"github.com/wso2/aep/aep-api/internal/feature/runtimeconfig"
	"github.com/wso2/aep/aep-api/models"
	"github.com/wso2/aep/aep-api/repositories"
)

// The composition-root adapters that satisfy the tasks/execution/codingagent
// consumer ports whose method shapes do not match a service verbatim. Each is a
// thin projection — the features stay free of the concrete services these wrap.

// repoLocator resolves a GitHub "owner/name" to the owning org + project.
// Satisfies execution.RepoLookup and task.RepoLocator.
type repoLocator struct{ db *gorm.DB }

func (r repoLocator) ByFullName(_ context.Context, fullName string) (string, string, error) {
	return repositories.LookupOrgProjectByRepoURL(r.db, fullName)
}

// designComponents exposes the design's component names at HEAD for the funnel's
// dispatch-time re-verification. Satisfies execution.DesignReader.
type designComponents struct{ store *artifacts.ArtifactStore }

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

// ReadDesignComponents exposes the project's authored design components at HEAD.
// Satisfies provisioning.DesignReader (and dependencies/resources.DesignReader).
func (d designComponents) ReadDesignComponents(ctx context.Context, orgID, projectID string) ([]models.DesignComponent, error) {
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
// execution.DesignReader.
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

// spaDeployObserver adapts the runtime-config emitter onto the codingagent
// DeployObserver port: when any component deploys, re-emit env-config.js across
// every SPA in the project (a sibling backend's deploy can resolve a SPA's dep
// URL — project-wide, mirroring the retired dispatch cascade). The deployed
// component name is unused; emission fans over the project's web-apps. Satisfies
// codingagent.DeployObserver.
type spaDeployObserver struct{ svc *runtimeconfig.RuntimeConfigService }

func (o spaDeployObserver) OnComponentDeployed(ctx context.Context, orgID, projectID, _ string) error {
	return o.svc.EmitForProjectSPAs(ctx, orgID, projectID)
}

// projectTraitSyncer is the narrow project-wide re-emit surface the trait
// deploy observer consumes. Declared consumer-side so this adapter names only
// the one method it needs; *component.TraitSyncService satisfies it
// structurally.
type projectTraitSyncer interface {
	SyncProjectAPITraits(ctx context.Context, orgID, projectID string) error
}

// traitDeployObserver adapts the api-configuration trait emitter onto the
// codingagent DeployObserver port: when any component deploys, re-emit the
// jwtAuth/CORS traitEnvironmentConfigs across every protected API in the
// project. The deployed component name is unused — emission fans over the
// project's protected services, mirroring spaDeployObserver.
//
// Deploy-time is the seam because EnsureComponent sets only the Component CR's
// trait *shape* at create (so OC renders the RestApi); the per-env
// traitEnvironmentConfigs PATCH — which carries the jwtAuth policy the gateway
// enforces — needs the ReleaseBinding, and OC's autoDeploy has created that by
// the time a build succeeds and this observer fires. A sibling SPA's deploy
// also routes here so a newly-deployed SPA's origin lands in each protected
// API's CORS allowlist. Satisfies codingagent.DeployObserver.
type traitDeployObserver struct{ svc projectTraitSyncer }

func (o traitDeployObserver) OnComponentDeployed(ctx context.Context, orgID, projectID, _ string) error {
	return o.svc.SyncProjectAPITraits(ctx, orgID, projectID)
}

// repoNamer resolves an org+project to its GitHub repo full name ("owner/name")
// — the provision Execution row's Repo must equal the gate issue's repo full
// name so the funnel gate resolves the run. Satisfies provisioning.RepoLocator.
type repoNamer struct {
	repos repositories.RepoRepository
	db    *gorm.DB
}

func (r repoNamer) RepoFullName(ctx context.Context, orgID, projectID string) (string, error) {
	gr, err := r.repos.GetByOrgAndProjectID(ctx, orgID, projectID)
	if err != nil {
		return "", err
	}
	if gr == nil {
		return "", gitrepo.ErrRepoNotFound
	}
	owner, name, err := gitrepo.ParseOwnerRepo(gr.RepoURL)
	if err != nil {
		return "", err
	}
	return owner + "/" + name, nil
}

// ByFullName is the reverse lookup (`<owner>/<repo>` → org/project) the
// issues/closed webhook uses to find the provider project of a declined
// org-publish gate issue. Satisfies provisioning.RepoLocator.
func (r repoNamer) ByFullName(_ context.Context, fullName string) (string, string, error) {
	return repositories.LookupOrgProjectByRepoURL(r.db, fullName)
}

// provisionProjects enumerates an org's ready projects for the provisioning
// feature's cross-project design scan (external-resource consumers, teardown).
// Satisfies provisioning.ProjectLister.
type provisionProjects struct{ repos repositories.RepoRepository }

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

// repoLister enumerates ready project repos for the reconciliation sweep.
// Satisfies execution.RepoLister.
type repoLister struct{ repos repositories.RepoRepository }

func (l repoLister) ListAll(ctx context.Context) ([]execution.RepoRef, error) {
	rows, err := l.repos.ListAllReady(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]execution.RepoRef, 0, len(rows))
	for i := range rows {
		owner, name := models.OwnerRepoFromURL(rows[i].RepoURL)
		if owner == "" || name == "" {
			continue
		}
		out = append(out, execution.RepoRef{OrgID: rows[i].OrgID, ProjectID: rows[i].ProjectID, FullName: owner + "/" + name})
	}
	return out, nil
}

// identities projects orgcreds.CredentialService.IdentityFor onto the
// codingagent.Identities port.
type identities struct{ cred *orgcreds.CredentialService }

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

// anthropicProvisioner projects orgcreds.AnthropicCredentialService.ApplyWPSecret
// onto the codingagent.AnthropicProvisioner port.
type anthropicProvisioner struct {
	svc *orgcreds.AnthropicCredentialService
}

func (a anthropicProvisioner) ApplyWPSecret(ctx context.Context, ocOrgID string) (string, error) {
	res, err := a.svc.ApplyWPSecret(ctx, ocOrgID)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	return res.SecretRefName, nil
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

// executionOrgLookup resolves an execution id to its owning org handle — the
// RunnerAuthorizer's publisher-cc branch (re-keyed from task to execution).
func executionOrgLookup(db *gorm.DB) func(ctx context.Context, executionID string) (string, error) {
	return func(ctx context.Context, executionID string) (string, error) {
		var row models.Execution
		if err := db.WithContext(ctx).Select("org_id").First(&row, "id = ?", executionID).Error; err != nil {
			return "", err
		}
		return row.OrgID, nil
	}
}
