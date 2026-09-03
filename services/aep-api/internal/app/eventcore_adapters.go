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
	"errors"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/eventcore"
	"github.com/wso2/aep/aep-api/internal/delivery/runread"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/naming"
	"github.com/wso2/aep/aep-api/internal/projects"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The composition-root adapters behind the event plane's consumer ports. Each
// is a thin projection of a repository or client the package must not name.

// eventcoreRuns projects the milestone-run repository onto the event plane's
// RunStore: it turns the repository's plain reads into the "is there a live
// run here?" questions every handler gates on.
//
// The non-terminal filtering is done here rather than as another repository
// query because "live" is the event plane's word for it, and the repository
// already exposes the rows it means.
type eventcoreRuns struct {
	runs delivery.MilestoneRunRepository
}

func (a eventcoreRuns) LiveRunForMilestone(ctx context.Context, orgID, projectID string, milestoneNumber int) (*delivery.MilestoneRun, error) {
	rows, err := a.runs.ListByMilestone(ctx, orgID, projectID, milestoneNumber)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if !delivery.IsTerminalRunState(rows[i].State) {
			return &rows[i], nil
		}
	}
	return nil, nil
}

func (a eventcoreRuns) LiveRunsForProject(ctx context.Context, orgID, projectID string) ([]delivery.MilestoneRun, error) {
	rows, err := a.runs.ListByProject(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	live := make([]delivery.MilestoneRun, 0, len(rows))
	for i := range rows {
		if !delivery.IsTerminalRunState(rows[i].State) {
			live = append(live, rows[i])
		}
	}
	return live, nil
}

// DeployedMilestoneRun is the project's most recent SUCCEEDED DEV run — the
// version that is live, and therefore the milestone an incident belongs to.
// Only a dev run delivers a version; ListByProject is newest-first, so the first
// match is the answer.
func (a eventcoreRuns) DeployedMilestoneRun(ctx context.Context, orgID, projectID string) (*delivery.MilestoneRun, error) {
	rows, err := a.runs.ListByProject(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].Kind == delivery.RunKindDev && rows[i].State == delivery.RunStateSucceeded {
			return &rows[i], nil
		}
	}
	return nil, nil
}

// NewestRunForMilestone is the milestone's most recent row of any kind.
// ListByMilestone is newest-first, so the first row is the answer.
func (a eventcoreRuns) NewestRunForMilestone(ctx context.Context, orgID, projectID string, milestoneNumber int) (*delivery.MilestoneRun, error) {
	rows, err := a.runs.ListByMilestone(ctx, orgID, projectID, milestoneNumber)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// KnownMilestones is the distinct set of milestones this project has ever run,
// newest first — the reconcile sweep's walk.
func (a eventcoreRuns) KnownMilestones(ctx context.Context, orgID, projectID string) ([]eventcore.MilestoneRef, error) {
	rows, err := a.runs.ListByProject(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	seen := make(map[int]bool, len(rows))
	out := make([]eventcore.MilestoneRef, 0, len(rows))
	for i := range rows {
		if seen[rows[i].MilestoneNumber] {
			continue
		}
		seen[rows[i].MilestoneNumber] = true
		out = append(out, eventcore.MilestoneRef{Number: rows[i].MilestoneNumber, Title: rows[i].MilestoneTitle})
	}
	return out, nil
}

func (a eventcoreRuns) BumpBudget(ctx context.Context, runID string, counter delivery.RunBudget) error {
	_, err := a.runs.BumpBudget(ctx, runID, counter)
	return err
}

// eventcoreCycles projects the cycle repository onto the event plane's
// CycleStore. The repository's guarded mutators return the row they changed
// (or nil when a closed cycle made them a no-op); the event plane only needs
// to know the write happened, so the row is dropped here.
type eventcoreCycles struct{ cycles delivery.RunCycleRepository }

func (a eventcoreCycles) Latest(ctx context.Context, orgID, runID string) (*delivery.RunCycle, error) {
	return a.cycles.Latest(ctx, orgID, runID)
}

func (a eventcoreCycles) NotePullRequest(ctx context.Context, cycleID string, pr delivery.CyclePullRequest) error {
	_, err := a.cycles.NotePullRequest(ctx, cycleID, pr)
	return err
}

func (a eventcoreCycles) NoteMergeDecision(ctx context.Context, cycleID string, resolves []int, verdict, reason string) error {
	_, err := a.cycles.NoteMergeDecision(ctx, cycleID, resolves, verdict, reason)
	return err
}

func (a eventcoreCycles) FinishCycle(ctx context.Context, cycleID, mergeSHA string) error {
	_, err := a.cycles.Finish(ctx, cycleID, mergeSHA)
	return err
}

// eventcoreBuilds is the OpenChoreo half: stage the org's clone credential,
// trigger a build pinned to a commit under a caller-chosen name, and read a
// component's runs back.
//
// The name matters — it carries the (component, commit, attempt) triple the
// re-trigger budget is counted on. Staging is deliberately NOT keyed to that
// name: the credential is one per-org object, so the name it is staged "under"
// is a log correlation string and nothing more.
type eventcoreBuilds struct {
	oc     openchoreo.ComponentClient
	repos  sourcecontrol.RepoRepository
	stager codingagentBuildStager
}

// codingagentBuildStager is the per-org build git-credential stager (the same
// one the manual build path and the coding executor use). Restated here so
// this file names a capability rather than a service.
type codingagentBuildStager interface {
	StageBuildSecret(ctx context.Context, orgID, repoSlug, runName string) (string, error)
}

func (a eventcoreBuilds) TriggerBuildAtCommit(ctx context.Context, orgID, projectID, component, commitSHA, runName, secretRef string) error {
	_, err := a.oc.TriggerBuildAtCommit(ctx, orgID, projectID, component, commitSHA, secretRef, runName)
	return err
}

// StageBuildCredential mirrors the coding executor's staging contract: no stager
// or no repo slug means clone unauthenticated (correct for a public repo), while
// a staging refusal blocks the build rather than producing a build that cannot
// clone.
//
// The event plane calls this ONCE per fan-out rather than once per component —
// the credential is per-org, so per-component staging was N concurrent writes to
// one object (see the BuildTrigger port contract).
func (a eventcoreBuilds) StageBuildCredential(ctx context.Context, orgID, projectID, correlation string) (string, error) {
	if a.stager == nil || a.repos == nil {
		return "", nil
	}
	repo, err := a.repos.GetByOrgAndProjectID(ctx, orgID, projectID)
	if err != nil || repo == nil || repo.RepoSlug == "" {
		slog.WarnContext(ctx, "eventcore build: no repo slug — cloning unauthenticated",
			"org", orgID, "project", projectID, "error", err)
		return "", nil
	}
	return a.stager.StageBuildSecret(ctx, orgID, repo.RepoSlug, correlation)
}

// ListBuildRuns reads the component's WorkflowRuns — the fact the re-trigger
// budget is derived from. It takes the host's default page rather than a
// cursor walk: attempts at the commit just merged are the newest runs a
// component has, so a first page always contains them, and paging a
// long-lived component's whole history on every build terminal would cost far
// more than the two rows the count needs.
func (a eventcoreBuilds) ListBuildRuns(ctx context.Context, orgID, projectID, component string) ([]eventcore.BuildRun, error) {
	runs, err := a.oc.ListBuildRuns(ctx, orgID, projectID, component)
	if err != nil {
		return nil, err
	}
	out := make([]eventcore.BuildRun, 0, len(runs))
	for _, r := range runs {
		out = append(out, eventcore.BuildRun{
			Name:      r.Name,
			Status:    r.Status,
			Completed: r.Completed,
			Succeeded: r.Succeeded,
		})
	}
	return out, nil
}

// eventcoreRepoLister projects the repo repository onto the sweep's lister.
type eventcoreRepoLister struct{ repos sourcecontrol.RepoRepository }

func (l eventcoreRepoLister) ListAll(ctx context.Context) ([]eventcore.RepoRef, error) {
	rows, err := l.repos.ListAllReady(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]eventcore.RepoRef, 0, len(rows))
	for i := range rows {
		owner, name := naming.OwnerRepoFromURL(rows[i].RepoURL)
		if owner == "" || name == "" {
			continue
		}
		out = append(out, eventcore.RepoRef{OrgID: rows[i].OrgID, ProjectID: rows[i].ProjectID, FullName: owner + "/" + name})
	}
	return out, nil
}

// projectLister projects the same repository onto the deployment converge
// sweep's lister.
//
// The git-repository index rather than the executions table, deliberately: the
// sweep it replaced enumerated projects with dispatched components, and the run
// loop mints no execution rows, so on that rail it saw nothing at all. Every
// project the platform tracks has a repo row.
type projectLister struct{ repos sourcecontrol.RepoRepository }

func (l projectLister) ListAll(ctx context.Context) ([]projects.ProjectRef, error) {
	rows, err := l.repos.ListAllReady(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]projects.ProjectRef, 0, len(rows))
	for i := range rows {
		out = append(out, projects.ProjectRef{OrgID: rows[i].OrgID, ProjectID: rows[i].ProjectID})
	}
	return out, nil
}

// eventcoreComponents is the pre-build component provisioning the merged-PR
// fan-out runs: create or update the component's OpenChoreo Component CR from
// the design facts, then emit any runtime config that rides on it.
//
// It is a COMPOSITE because the two used to run together as the coding
// dispatch's per-component pre-flight, and the fan-out is where that pair now
// belongs — a cycle is scoped to a milestone and may touch several components,
// so no dispatch knows which component is about to be built.
//
// The CR is required (its absence fails the build); the runtime-config emit is
// best-effort and self-no-ops for anything but a web app, exactly as before.
type eventcoreComponents struct {
	comp    componentEnsurer
	runtime componentRuntimeConfigEmitter
}

// componentEnsurer / componentRuntimeConfigEmitter are the two narrow verbs the
// composite needs. projects.ComponentService and
// *runtimeconfig.RuntimeConfigService satisfy them structurally.
type componentEnsurer interface {
	EnsureComponent(ctx context.Context, orgName, projectName, componentName string) error
}

type componentRuntimeConfigEmitter interface {
	EmitForComponent(ctx context.Context, orgID, projectID, componentName string) error
}

func (a eventcoreComponents) EnsureComponent(ctx context.Context, orgID, projectID, component string) error {
	if a.comp == nil {
		return nil
	}
	if err := a.comp.EnsureComponent(ctx, orgID, projectID, component); err != nil {
		return err
	}
	if a.runtime != nil {
		if err := a.runtime.EmitForComponent(ctx, orgID, projectID, component); err != nil {
			slog.WarnContext(ctx, "eventcore: env-config.js emit failed (best-effort)",
				"component", component, "error", err)
		}
	}
	return nil
}

// eventcoreAdopter adapts the event plane onto the task feature's Adopter port:
// the SRE/RCA handoff's promote-from-issue leg hands a freshly filed issue to
// the coding agent. It passes a BARE target — the caller just created the
// issue, so it belongs to no milestone yet and adoption files it under the
// deployed version's.
type eventcoreAdopter struct{ events *eventcore.Events }

func (a eventcoreAdopter) AdoptIssue(ctx context.Context, orgID, projectID string, issueNumber int) error {
	return a.events.AdoptIssue(ctx, orgID, projectID, eventcore.AdoptTarget{Number: issueNumber})
}

// eventcoreRevalidator adapts the event plane onto the run read surface's
// Revalidator port. It is only a type bridge: the read surface resolved the tag
// to a milestone through its own rows, and every guard the revalidation needs
// lives on the other side of this call, where GitHub and the project repo are
// reachable.
type eventcoreRevalidator struct{ events *eventcore.Events }

func (a eventcoreRevalidator) Revalidate(ctx context.Context, orgID, projectID string, target runread.RevalidateTarget) (string, error) {
	return a.events.Revalidate(ctx, orgID, projectID,
		eventcore.MilestoneRef{Number: target.MilestoneNumber, Title: target.MilestoneTitle},
		target.Attempts, target.Ceiling)
}

// projectRunRows adapts the milestone-run + cycle repositories onto the
// projects domain's status/purge port. The read half is the run index the
// overview's build + deploy stages render from; the purge half deletes the
// CYCLES before their runs, so a recreated same-named project cannot inherit
// orphaned cycle records.
type projectRunRows struct {
	runs   delivery.MilestoneRunRepository
	cycles delivery.RunCycleRepository
	// ledger is what the purge does NOT delete. It is retired instead — see
	// DeleteByProject.
	ledger delivery.AgentUsageLedgerRepository
}

func (a projectRunRows) ListByProject(ctx context.Context, orgID, projectID string) ([]delivery.MilestoneRun, error) {
	return a.runs.ListByProject(ctx, orgID, projectID)
}

// LatestCycle lets the validation chip say "validating" only when a validation
// CYCLE is in flight. Read from the cycle records rather than the run row because
// loop position is never stored: a fix or conflict cycle re-enters an earlier
// phase, so only the latest cycle knows where the loop actually is.
func (a projectRunRows) LatestCycle(ctx context.Context, orgID, runID string) (*delivery.RunCycle, error) {
	return a.cycles.Latest(ctx, orgID, runID)
}

// DeleteByProject is delivery's whole answer to a project delete, and it is two
// different verbs on purpose.
//
// The cycles and runs are WORKING STATE and go: a recreated same-named project
// must not inherit a predecessor's timeline, or the status poll would chase
// versions the fresh repo never had. What that work COST is not working state,
// and does not go — the Settings → Usage page is built to keep reporting a
// deleted project's spend, and purging the rows it was stamped on is what
// silently emptied it.
//
// So the ledger is RETIRED first, before the rows it mirrors are removed. The
// order is load-bearing in one direction only — retirement reads nothing from
// the cycles — but doing it first means a failure leaves the spend attributed to
// a live project rather than the rows deleted with their lifetime still open,
// which is the difference between a retry that fixes it and one that cannot.
func (a projectRunRows) DeleteByProject(ctx context.Context, orgID, projectID string) error {
	if err := a.ledger.RetireByProject(ctx, orgID, projectID); err != nil {
		return err
	}
	if err := a.cycles.DeleteByProject(ctx, orgID, projectID); err != nil {
		return err
	}
	return a.runs.DeleteByProject(ctx, orgID, projectID)
}

// projectRunSupervision adapts the milestone-run index + the run supervisor onto
// the projects domain's run-teardown port: on a project delete, end the workflows
// supervising its live runs.
//
// It is keyed by MILESTONE rather than by run, because the workflow is: a run's
// identity is its milestone (delivery.MilestoneRunWorkflowID), so several rows of
// the same milestone name one supervisor and abandoning it once is enough.
type projectRunSupervision struct {
	runs       delivery.MilestoneRunRepository
	supervisor runAbandonment
}

// runAbandonment is the single supervisor verb the teardown needs. It is an
// interface rather than the concrete *run.Supervisor only so the milestone
// SELECTION below can be proven without a Temporal client — the selection is the
// part with a decision in it.
type runAbandonment interface {
	AbandonRun(ctx context.Context, orgID, projectID string, milestoneNumber int) error
}

// AbandonProjectRuns terminates the supervisor of every milestone the project
// still has a LIVE run on. Settled runs are skipped: their workflows have already
// ended, and a run that settled is not one anything is still polling for.
//
// One milestone's failure does not stop the others — this runs inside a delete
// that has already been committed upstream, so the most complete teardown
// possible is worth more than a clean early return.
func (a projectRunSupervision) AbandonProjectRuns(ctx context.Context, orgID, projectID string) error {
	rows, err := a.runs.ListByProject(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	seen := make(map[int]bool, len(rows))
	var errs []error
	for i := range rows {
		if delivery.IsTerminalRunState(rows[i].State) || seen[rows[i].MilestoneNumber] {
			continue
		}
		seen[rows[i].MilestoneNumber] = true
		if aerr := a.supervisor.AbandonRun(ctx, orgID, projectID, rows[i].MilestoneNumber); aerr != nil {
			errs = append(errs, aerr)
		}
	}
	return errors.Join(errs...)
}
