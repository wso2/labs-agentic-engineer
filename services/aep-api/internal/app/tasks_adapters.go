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
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/codingagent"
	"github.com/wso2/aep/aep-api/internal/feature/execution"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/internal/feature/orchestration"
	"github.com/wso2/aep/aep-api/internal/feature/orgcreds"
	"github.com/wso2/aep/aep-api/internal/feature/provisioning"
	"github.com/wso2/aep/aep-api/internal/feature/task"
	"github.com/wso2/aep/aep-api/models"
	"github.com/wso2/aep/aep-api/repositories"
	contract "github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
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

// designComponents exposes the design's component names at HEAD for
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

// Components projects the approved design into the orchestrator's DAG shape.
// Satisfies orchestration.DesignReader.
func (d designComponents) Components(ctx context.Context, orgID, projectID string) ([]orchestration.ComponentSpec, error) {
	components, err := d.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]orchestration.ComponentSpec, 0, len(components))
	for _, c := range components {
		out = append(out, orchestration.ComponentSpec{Name: c.Name, DependsOn: c.ComponentDependsOn()})
	}
	return out, nil
}

// ProvisionDepNames exposes each component's provisioning dependencies
// (external + platform-resource). Satisfies execution.DesignReader.
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

// repoNamer resolves an org+project to its GitHub repo full name ("owner/name")
// — the provision Execution row's Repo must equal the gate issue's repo full
// name so provisioning and execution rows resolve to the same Task issue.
// Satisfies provisioning.RepoLocator.
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

// repoLister enumerates ready project repos. Satisfies execution.RepoLister.
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

// prMerger is the minimal PR-merge port orchestrationTaskDriver needs for auto
// code-review mode (§R3.5) — satisfied by gitrepo.IssueService without pulling
// its full surface into this adapter.
type prMerger interface {
	MergePullRequest(ctx context.Context, orgID, projectID string, number int) error
}

// reasonAutoMergedPrefix is stamped on the read-model row after AutoMerge
// merges a task's PR — mirrors execution's reasonPROpenPrefix convention.
const reasonAutoMergedPrefix = "auto-merged pr#"

// orchestrationTaskDriver adapts the Temporal task activities to the preserved
// aep-api task side effects. The workflow is now responsible for dependency
// ordering; this adapter only resolves GitHub task facts and launches the
// preserved executor side effect.
type orchestrationTaskDriver struct {
	reads    *task.Reads
	repos    repositories.RepoRepository
	tasks    execution.TaskFactResolver
	execs    repositories.ExecutionRepository
	executor execution.Executor
	merger   prMerger // nil disables AutoMerge (falls back to a no-op)
}

func (d orchestrationTaskDriver) DispatchTask(ctx context.Context, in contract.TaskLifecycleInput) error {
	repoFullName, issueNumber, err := d.resolveTask(ctx, in)
	if err != nil {
		return err
	}
	facts, ok, err := d.tasks.TaskFactsFor(ctx, repoFullName, issueNumber)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("task %s/%s/%s no longer exists", in.Org, in.Project, in.TaskID)
	}
	row := &models.Execution{
		OrgID:       facts.OrgID,
		ProjectID:   facts.ProjectID,
		Repo:        facts.Repo,
		IssueNumber: facts.IssueNumber,
		Kind:        string(taskmeta.KindCoding),
		Status:      string(taskmeta.ExecQueued),
		DesignTag:   facts.DesignTag,
		Component:   facts.Component,
	}
	admitted, admittedRow, err := d.execs.TryAdmit(ctx, row)
	if err != nil {
		return err
	}
	if !admitted {
		return nil
	}
	if err := d.executor.Run(ctx, execution.DispatchRequest{Execution: admittedRow, Task: facts}); err != nil {
		_, _ = d.execs.Finish(ctx, admittedRow.ID, string(taskmeta.ExecFailed), err.Error())
		d.upsertReadModel(ctx, in, facts, taskmeta.KindCoding, taskmeta.ExecFailed, err.Error())
		return err
	}
	d.upsertReadModel(ctx, in, facts, taskmeta.KindCoding, taskmeta.ExecRunning, "")
	return nil
}

func (d orchestrationTaskDriver) DeployTask(ctx context.Context, in contract.TaskLifecycleInput) error {
	// Components are created with AutoDeploy=true; OpenChoreo drives deployment
	// after the build succeeds. The deploy heartbeating activity observes the
	// ReleaseBinding's Ready condition and signals the workflow (§R3.2/§R4.1);
	// this call only checkpoints the read-model with "deploy requested".
	d.upsertReadModel(ctx, in, execution.TaskFacts{}, taskmeta.KindBuild, taskmeta.ExecRunning, "deploy requested")
	return nil
}

// upsertReadModel is a best-effort checkpoint of the task workflow's position
// into the executions read-model (§R3.3) — it never fails the caller (dispatch/
// deploy/merge already succeeded or is independently retried by Temporal; a
// read-model write hiccup should not roll that back or block a retry).
func (d orchestrationTaskDriver) upsertReadModel(ctx context.Context, in contract.TaskLifecycleInput, facts execution.TaskFacts, kind taskmeta.ExecutionKind, status taskmeta.ExecutionStatus, reason string) {
	row := &models.Execution{
		WorkflowID: contract.TaskWorkflowID(in.Org, in.Project, in.TaskID),
		OrgID:      firstNonEmpty(facts.OrgID, in.Org),
		ProjectID:  firstNonEmpty(facts.ProjectID, in.Project),
		Repo:       facts.Repo,
		Kind:       string(kind),
		Status:     string(status),
		Component:  firstNonEmpty(facts.Component, in.ComponentName, in.TaskID),
		DesignTag:  facts.DesignTag,
		Reason:     reason,
	}
	if _, err := d.execs.UpsertReadModel(ctx, row); err != nil {
		slog.WarnContext(ctx, "orchestration read-model upsert failed",
			"workflowId", row.WorkflowID, "kind", kind, "status", status, "error", err)
	}
}

// AutoMerge merges the task's open PR in auto code-review mode (§R3.5). The PR
// number is recovered from the task's latest coding Execution (the reason
// stamp PullRequestOpened leaves, execution.OpenPRNumber) rather than carried
// on the activity input, since TaskLifecycleInput has no PR-number field.
// Idempotent: MergePullRequest treats an already-merged PR as success, and a
// task with no open PR on record errors (nothing to merge yet) rather than
// silently no-op-ing, so a misordered/duplicate activity call surfaces instead
// of masking a real problem.
func (d orchestrationTaskDriver) AutoMerge(ctx context.Context, in contract.TaskLifecycleInput) error {
	if d.merger == nil {
		return nil
	}
	repoFullName, issueNumber, err := d.resolveTask(ctx, in)
	if err != nil {
		return err
	}
	execs, err := d.execs.LatestPerKind(ctx, repoFullName, issueNumber)
	if err != nil {
		return err
	}
	prNumber := execution.OpenPRNumber(execs[string(taskmeta.KindCoding)])
	if prNumber == 0 {
		return fmt.Errorf("no open PR recorded for task %s/%s/%s", in.Org, in.Project, in.TaskID)
	}
	if err := d.merger.MergePullRequest(ctx, in.Org, in.Project, prNumber); err != nil {
		return err
	}
	d.upsertReadModel(ctx, in, execution.TaskFacts{Repo: repoFullName}, taskmeta.KindCoding, taskmeta.ExecSucceeded,
		reasonAutoMergedPrefix+strconv.Itoa(prNumber))
	return nil
}

func (d orchestrationTaskDriver) resolveTask(ctx context.Context, in contract.TaskLifecycleInput) (string, int, error) {
	repo, err := d.repos.GetByOrgAndProjectID(ctx, in.Org, in.Project)
	if err != nil {
		return "", 0, err
	}
	if repo == nil {
		return "", 0, fmt.Errorf("repo not found for %s/%s", in.Org, in.Project)
	}
	owner, name := models.OwnerRepoFromURL(repo.RepoURL)
	if owner == "" || name == "" {
		return "", 0, fmt.Errorf("repo %q has no owner/name", repo.RepoURL)
	}
	repoFullName := owner + "/" + name

	views, err := d.reads.List(ctx, in.Org, in.Project, "open")
	if err != nil {
		return "", 0, err
	}
	component := strings.ToLower(firstNonEmpty(in.ComponentName, in.TaskID))
	for i := range views {
		if strings.ToLower(views[i].Component) == component && views[i].ExecutorClass == string(taskmeta.ClassCoding) {
			return repoFullName, views[i].IssueNumber, nil
		}
	}
	return "", 0, fmt.Errorf("coding task for component %q not found", component)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
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
