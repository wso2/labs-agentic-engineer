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
	"fmt"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/run"
	"github.com/wso2/aep/aep-api/internal/delivery/runread"
	"github.com/wso2/aep/aep-api/internal/delivery/validation"
	"github.com/wso2/aep/aep-api/internal/dependencies/provisioning"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The composition-root adapters behind the run supervisor's consumer ports.
// Three of its eight ports are satisfied by the issue service and the design
// reader with no adapter at all; these are the four that need one.

// runRuns projects the milestone-run repository onto the supervisor's RunStore.
// The repository's guarded mutators return the row they changed (or nil when a
// terminal run made them a no-op); the supervisor only needs to know the write
// was attempted, so the row is dropped here.
type runRuns struct {
	runs delivery.MilestoneRunRepository
}

func (a runRuns) TryAdmit(ctx context.Context, row *delivery.MilestoneRun) (bool, *delivery.MilestoneRun, error) {
	return a.runs.TryAdmit(ctx, row)
}

// LiveRunForMilestone is the same "is anybody on this milestone?" read the
// event plane makes, restated here because the supervisor's start path must
// answer it without importing a peer.
func (a runRuns) LiveRunForMilestone(ctx context.Context, orgID, projectID string, milestoneNumber int) (*delivery.MilestoneRun, error) {
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

// MilestoneSpecTag reads the version off the milestone's newest run that has
// one. Rows arrive newest-first, so a milestone whose dev run was followed
// by tagless incident runs still answers with the version it was built for.
func (a runRuns) MilestoneSpecTag(ctx context.Context, orgID, projectID string, milestoneNumber int) (string, error) {
	rows, err := a.runs.ListByMilestone(ctx, orgID, projectID, milestoneNumber)
	if err != nil {
		return "", err
	}
	for i := range rows {
		if tag := rows[i].SpecTag(); tag != "" {
			return tag, nil
		}
	}
	return "", nil
}

// ListByMilestone hands the milestone's runs straight through, newest-first. The
// supervisor's own use is narrow — a validation run counting how many times this
// version has been judged — but the filtering belongs on that side: which kinds
// count is a loop decision, and this adapter holds none.
func (a runRuns) ListByMilestone(ctx context.Context, orgID, projectID string, milestoneNumber int) ([]delivery.MilestoneRun, error) {
	return a.runs.ListByMilestone(ctx, orgID, projectID, milestoneNumber)
}

func (a runRuns) SetState(ctx context.Context, id, state string) error {
	_, err := a.runs.SetState(ctx, id, state)
	return err
}

func (a runRuns) SetWaiting(ctx context.Context, id, reason string, dependencies []string) error {
	_, err := a.runs.SetWaiting(ctx, id, reason, dependencies)
	return err
}

func (a runRuns) Settle(ctx context.Context, id, state, reason string) error {
	_, err := a.runs.Settle(ctx, id, state, reason)
	return err
}

func (a runRuns) RecordFailure(ctx context.Context, id string, failure delivery.RunFailure) error {
	_, err := a.runs.RecordFailure(ctx, id, failure)
	return err
}

func (a runRuns) ClearFailure(ctx context.Context, id string) error {
	_, err := a.runs.ClearFailure(ctx, id)
	return err
}

func (a runRuns) BumpBudget(ctx context.Context, id string, counter delivery.RunBudget) error {
	_, err := a.runs.BumpBudget(ctx, id, counter)
	return err
}

func (a runRuns) SetValidationVerdict(ctx context.Context, id, verdict string, issue, regressions int) error {
	_, err := a.runs.SetValidationVerdict(ctx, id, verdict, issue, regressions)
	return err
}

// CancelRequested reads the cancel stamp back. Org-scoped like every other read
// this surface makes, and a row that is gone answers false — a run whose project
// was deleted has nothing left to cancel.
func (a runRuns) CancelRequested(ctx context.Context, orgID, runID string) (bool, error) {
	row, err := a.runs.GetByIDScoped(ctx, orgID, runID)
	if err != nil || row == nil {
		return false, err
	}
	return row.CancelRequestedAt != nil, nil
}

// runCycles projects the cycle repository onto the supervisor's CycleStore.
type runCycles struct{ cycles delivery.RunCycleRepository }

func (a runCycles) Append(ctx context.Context, cycle *delivery.RunCycle) (string, error) {
	if err := a.cycles.Append(ctx, cycle); err != nil {
		return "", err
	}
	return cycle.ID, nil
}

func (a runCycles) NoteDispatch(ctx context.Context, cycleID, jobRef string) (*delivery.RunCycle, error) {
	return a.cycles.NoteDispatch(ctx, cycleID, jobRef)
}

// runJobBindings projects the OpenChoreo component client onto the
// supervisor's JobBindings. A legacy release renders no suspend, so there is
// nothing to write there: that answer is nil, the port's "nothing to do". So is
// a suspend of a binding that is gone.
type runJobBindings struct{ oc openchoreo.ComponentClient }

func (a runJobBindings) ResumeJobBinding(ctx context.Context, orgID, projectID, component, environment string) error {
	err := a.oc.ResumeJobBinding(ctx, orgID, projectID, component, environment)
	if errors.Is(err, openchoreo.ErrSuspendUnsupported) {
		return nil
	}
	return err
}

func (a runJobBindings) SuspendJobBinding(ctx context.Context, orgID, projectID, component, environment string) error {
	err := a.oc.SuspendJobBinding(ctx, orgID, projectID, component, environment)
	if errors.Is(err, openchoreo.ErrSuspendUnsupported) || errors.Is(err, openchoreo.ErrNotFound) {
		return nil
	}
	return err
}

func (a runCycles) NoteLaunch(ctx context.Context, cycleID, host, environment, componentUID string) error {
	_, err := a.cycles.NoteLaunch(ctx, cycleID, host, environment, componentUID)
	return err
}

func (a runCycles) Finish(ctx context.Context, cycleID, mergeSHA string) error {
	_, err := a.cycles.Finish(ctx, cycleID, mergeSHA)
	return err
}

func (a runCycles) SetValidationVerdict(ctx context.Context, cycleID, verdict string, issue int, digest string, regressions int) error {
	_, err := a.cycles.SetValidationVerdict(ctx, cycleID, verdict, issue, digest, regressions)
	return err
}

func (a runCycles) LatestValidationDigest(ctx context.Context, orgID string, runIDs []string) (string, error) {
	return a.cycles.LatestValidationDigest(ctx, orgID, runIDs)
}

func (a runCycles) Latest(ctx context.Context, orgID, runID string) (*delivery.RunCycle, error) {
	return a.cycles.Latest(ctx, orgID, runID)
}

// runBuilds reads a component's OpenChoreo WorkflowRuns back for the
// supervisor, mapping OpenChoreo's condition vocabulary onto the two facts the
// loop reasons about. The supervisor never triggers a build — the event plane
// owns that, and its automatic re-trigger budget is derived from these same
// runs, so both halves count one source.
type runBuilds struct{ oc openchoreo.ComponentClient }

func (a runBuilds) ListBuildRuns(ctx context.Context, orgID, projectID, component string) ([]run.BuildRunInfo, error) {
	runs, err := a.oc.ListBuildRuns(ctx, orgID, projectID, component)
	if err != nil {
		return nil, err
	}
	out := make([]run.BuildRunInfo, 0, len(runs))
	for _, r := range runs {
		out = append(out, run.BuildRunInfo{
			Name:      r.Name,
			Terminal:  r.Completed,
			Succeeded: r.Succeeded,
			CommitSHA: r.CommitSHA,
			StartedAt: r.StartedAt,
		})
	}
	return out, nil
}

// runValidation adapts the validation feature onto the supervisor's
// ValidationCoordinator port: mint the version's validation issue at
// deployed-green, and read the runner's verdict back afterwards.
//
// The milestone is the minter's own concern — it rides the create, so there is
// no assignment step here and no window in which the issue has no version. What
// is left is the ref-pinned report read, and reading it as its version (B4):
// within what the version built, and against the previous validated version —
// which is the whole reason this type exists at the boundary.
type runValidation struct {
	projectFiles
	svc      *validation.Service
	versions interface {
		ValidationScope(ctx context.Context, orgID, projectID, version string) (spec.ValidationScope, bool, error)
	}
	runs   delivery.MilestoneRunRepository
	cycles delivery.RunCycleRepository
}

func (a runValidation) EnsureValidationIssue(ctx context.Context, orgID, projectID string, milestoneNumber int, version string) (int, error) {
	vs, ok, err := a.scopeOf(ctx, orgID, projectID, version)
	if err != nil {
		return 0, err
	}
	var scope *validation.Scope
	if ok {
		scope = validation.NewScope(version, vs.Features, vs.HeldBack, nil)
	}
	return a.svc.EnsureValidationIssue(ctx, orgID, projectID, milestoneNumber, scope)
}

func (a runValidation) Verdict(ctx context.Context, orgID, projectID, version, at string) (string, string, int, error) {
	j, err := a.judge(ctx, orgID, projectID, version, at)
	if err != nil {
		return "", "", 0, err
	}
	// All derived from the same read: the verdict the run stores, the digest that
	// tells a later attempt whether anything changed, and how many failures broke
	// what the previous version had working.
	return j.Report.Verdict(), j.Report.Digest(), j.Regressions(), nil
}

func (a runValidation) CloseValidationIssue(ctx context.Context, orgID, projectID string, issue int, verdict string, repairs []int) error {
	return a.svc.CloseValidationIssue(ctx, orgID, projectID, issue, verdict, repairs)
}

// FailuresOf satisfies build's Repairer: how many scenarios the version's
// final validation attempt failed, within its scope.
func (a runValidation) FailuresOf(ctx context.Context, orgID, projectID, version string) (int, error) {
	j, ok, err := a.finalJudgement(ctx, orgID, projectID, version)
	if err != nil || !ok {
		return 0, err
	}
	return len(j.Failures()), nil
}

// FileRepairs satisfies build's Repairer: the version's final failures become
// the repair version's work, filed as the run would have filed them.
func (a runValidation) FileRepairs(ctx context.Context, orgID, projectID string, milestoneNumber int, version string) error {
	j, ok, err := a.finalJudgement(ctx, orgID, projectID, version)
	if err != nil || !ok {
		return err
	}
	_, err = a.svc.MintRepairIssues(ctx, orgID, projectID, milestoneNumber, j)
	return err
}

// finalJudgement is the version's final validation attempt, read as its
// version; ok is false when it was never judged.
func (a runValidation) finalJudgement(ctx context.Context, orgID, projectID, version string) (validation.Judgement, bool, error) {
	if a.runs == nil || a.cycles == nil {
		return validation.Judgement{}, false, nil
	}
	at, err := a.finalAttempt(ctx, orgID, projectID, version)
	if err != nil || at == "" {
		return validation.Judgement{}, false, err
	}
	j, err := a.judge(ctx, orgID, projectID, version, at)
	return j, err == nil, err
}

// Standing satisfies runread's ValidationJudge: the same reading the run
// made, for the console's per-feature view.
func (a runValidation) Standing(ctx context.Context, orgID, projectID, version, at string) (runread.ValidationStanding, error) {
	j, err := a.judge(ctx, orgID, projectID, version, at)
	if err != nil {
		return runread.ValidationStanding{}, err
	}
	var st runread.ValidationStanding
	if j.Scope != nil {
		st.Scoped, st.Features, st.HeldBack = true, j.Scope.Features, j.Scope.HeldBack
	}
	if j.Baseline != nil {
		st.BaselineVersion, st.BaselineCommit = j.Baseline.Version, j.Baseline.Commit
	}
	st.Regressions, st.StillFailing = j.Standing()
	return st, nil
}

func (a runValidation) MintRepairIssues(ctx context.Context, orgID, projectID string, milestoneNumber int, version, at string) ([]int, error) {
	j, err := a.judge(ctx, orgID, projectID, version, at)
	if err != nil {
		return nil, err
	}
	return a.svc.MintRepairIssues(ctx, orgID, projectID, milestoneNumber, j)
}

// scopeOf is what the version validates. A version with no name (a run that
// predates tags) validates the whole oracle, as one cut before selections does.
func (a runValidation) scopeOf(ctx context.Context, orgID, projectID, version string) (spec.ValidationScope, bool, error) {
	if version == "" || a.versions == nil {
		return spec.ValidationScope{}, false, nil
	}
	return a.versions.ValidationScope(ctx, orgID, projectID, version)
}

// judge reads one attempt's report as its version: within the version's
// built scope, and against the previous validated version's final report.
//
// It is the ONE reader on the run path — the verdict and the repair issues are
// separate activities, each deriving from ground truth on its own retry, but
// they derive from the same read so they can never disagree about which
// scenarios the attempt failed or how each stood before.
func (a runValidation) judge(ctx context.Context, orgID, projectID, version, at string) (validation.Judgement, error) {
	j := validation.Judgement{Version: version}
	vs, ok, err := a.scopeOf(ctx, orgID, projectID, version)
	if err != nil {
		return j, err
	}
	report, err := a.readAs(ctx, orgID, projectID, version, at, vs, ok)
	if err != nil {
		return j, err
	}
	j.Report, j.Built = report, vs.Built
	if ok {
		j.Scope = validation.NewScope(version, vs.Features, vs.HeldBack, nil)
	}
	j.Baseline, err = a.baseline(ctx, orgID, projectID, vs.Earlier)
	return j, err
}

// readAs reads a report at a commit, narrowed to the version's scope with the
// story tags of the oracle at that same commit.
func (a runValidation) readAs(ctx context.Context, orgID, projectID, version, at string, vs spec.ValidationScope, scoped bool) (validation.Report, error) {
	raw, err := a.report(ctx, orgID, projectID, at)
	if err != nil {
		return validation.Report{}, err
	}
	report := validation.ParseReport(raw)
	if !scoped {
		return report, nil
	}
	criteria, err := (acceptanceCriteria{a.projectFiles}).criteriaAt(ctx, orgID, projectID, at)
	if err != nil {
		return validation.Report{}, err
	}
	return report.Within(validation.NewScope(version, vs.Features, vs.HeldBack, criteria)), nil
}

// baseline is the previous validated version's final report: the newest
// earlier version whose validation reached a verdict, read at the merge commit
// of its last judged attempt and within that version's own scope. Versions
// never validated are skipped; nil when none was.
func (a runValidation) baseline(ctx context.Context, orgID, projectID string, earlier []string) (*validation.Baseline, error) {
	if a.runs == nil || a.cycles == nil {
		return nil, nil
	}
	for _, version := range earlier {
		at, err := a.finalAttempt(ctx, orgID, projectID, version)
		if err != nil {
			return nil, err
		}
		if at == "" {
			continue
		}
		vs, ok, err := a.scopeOf(ctx, orgID, projectID, version)
		if err != nil {
			return nil, err
		}
		report, err := a.readAs(ctx, orgID, projectID, version, at, vs, ok)
		if err != nil {
			return nil, err
		}
		return &validation.Baseline{Version: version, Commit: at, Report: report}, nil
	}
	return nil, nil
}

// finalAttempt is the merge commit of a version's last judged validation
// attempt, or "" when it was never judged. Judged means a verdict read from a
// report: skipped and unreported attempts judged nothing.
func (a runValidation) finalAttempt(ctx context.Context, orgID, projectID, version string) (string, error) {
	milestone, found, err := a.runs.MilestoneNumberForTag(ctx, orgID, projectID, version)
	if err != nil || !found {
		return "", err
	}
	rows, err := a.runs.ListByMilestone(ctx, orgID, projectID, milestone) // newest first
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if r.Kind != delivery.RunKindValidation || !delivery.IsTerminalRunState(r.State) {
			continue
		}
		cycles, err := a.cycles.ListByRun(ctx, orgID, r.ID) // oldest first
		if err != nil {
			return "", err
		}
		for i := len(cycles) - 1; i >= 0; i-- {
			c := cycles[i]
			if c.Kind == delivery.CycleKindValidation && c.MergeSHA != "" && judged(c.ValidationVerdict) {
				return c.MergeSHA, nil
			}
		}
	}
	return "", nil
}

func judged(verdict string) bool {
	return verdict != "" && verdict != delivery.ValidationVerdictSkipped && verdict != delivery.ValidationVerdictUnreported
}

// report reads the runner's committed report at a pinned commit.
//
// An absent file is not an error: the validation cycle merged and committed no
// report AT ITS OWN MERGE COMMIT, which is a fact about this run rather than a
// stale read. Nil bytes are a report with nothing in it, whose verdict is
// `unreported`.
func (a runValidation) report(ctx context.Context, orgID, projectID, at string) ([]byte, error) {
	content, _, found, err := a.readFile(ctx, orgID, projectID, at, validation.ReportFilePath)
	if err != nil || !found {
		return nil, err
	}
	return []byte(content), nil
}

// runreadProjectBuilds reads every build WorkflowRun in a project so the run
// read can derive one cycle's builds from its merge SHA. One call rather than
// one per component: the read side does not know which components a merge
// touched, and it does not need to — the run names carry the (component,
// commit, attempt) triple, so delivery.BuildsAtMerge recovers the fan-out by
// filtering. Nothing is stored; this is the same cluster-is-the-truth rule the
// re-trigger budget follows.
type runreadProjectBuilds struct{ oc openchoreo.ComponentClient }

func (a runreadProjectBuilds) ListProjectBuildRuns(ctx context.Context, orgID, projectID string) ([]delivery.MergeBuild, error) {
	list, err := a.oc.ListProjectWorkflowRuns(ctx, orgID, projectID, 0, "")
	if err != nil {
		return nil, err
	}
	if list == nil {
		return nil, nil
	}
	out := make([]delivery.MergeBuild, 0, len(list.Items))
	for _, item := range list.Items {
		out = append(out, delivery.MergeBuild{
			Component: item.ComponentName,
			RunName:   item.Name,
			Status:    item.Status,
			Completed: item.Completed,
			StartedAt: item.StartedAt,
		})
	}
	return out, nil
}

// valuesSavedNotifier bridges the provisioning feature's ValuesSavedNotifier
// onto the run supervisor: external values landed, so a run parked on the deploy
// gate should re-derive readiness now rather than at its next poll.
//
// It lives at the composition root for the reason the port exists at all —
// provisioning must not import delivery/run. The signal carries no payload
// because it carries no instruction: the supervisor re-reads readiness itself,
// so a save that leaves another dependency unset parks the run straight back.
type valuesSavedNotifier struct {
	runs       delivery.MilestoneRunRepository
	supervisor *run.Supervisor
}

func (n valuesSavedNotifier) ValuesSaved(ctx context.Context, orgID, projectID string) error {
	// EVERY parked run, any kind — not just the newest. A task or validation run
	// parks on this gate exactly like a dev run, and they can be live at the same
	// time on their own milestones. One saved value can unblock several at once.
	rows, err := n.runs.RunsWaitingOnValues(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	// Values saved with no parked run is the ordinary case — a developer
	// configuring ahead of the next build. Nothing to wake.
	var errs []error
	for i := range rows {
		// Keep going after a failure: one run whose workflow has already gone
		// (settled between the read and the signal) must not strand its siblings.
		// The supervisor's own not-found handling decides what is benign; anything
		// it still reports is joined and returned.
		if err := n.supervisor.SignalRun(ctx, &rows[i],
			delivery.SigRunValuesSaved, delivery.RunSignal{Signal: delivery.SigRunValuesSaved}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// agentDeathNotifier bridges the coding agent's pod-truth watcher onto the run
// supervisor: a dispatched cycle's agent ended without a pull request, so the
// run waiting on it should stop waiting NOW rather than at its 2h landing
// deadline.
//
// It lives at the composition root for the reason the port exists at all —
// codingagent must not import delivery/run — and it does the run-row read here
// rather than in the watcher because the row is the ROUTING TABLE: the workflow
// id is built from the RUN's kind (dev/task/validation), and a cycle's kind
// (coding/fix/conflict/validation) is a different vocabulary that merely
// overlaps on one word. Guessing one from the other would deliver a fix cycle's
// death to whichever run holds that milestone's dev id.
type agentDeathNotifier struct {
	runs       delivery.MilestoneRunRepository
	supervisor *run.Supervisor
}

func (n agentDeathNotifier) AgentDied(ctx context.Context, orgID, runID, reason string) error {
	row, err := n.runs.GetByIDScoped(ctx, orgID, runID)
	if err != nil {
		return err
	}
	// The run settled between the watcher's write and this read, or its rows are
	// gone. Nothing to wake, and not a failure: the cycle's terminal reason is
	// already durable either way.
	if row == nil {
		return nil
	}
	// The reason rides in Message, which the loop never parses: it re-reads the
	// cycle record for the fact itself.
	return n.supervisor.SignalRun(ctx, row, delivery.SigRunAgentDied, delivery.RunSignal{
		Signal:          delivery.SigRunAgentDied,
		MilestoneNumber: row.MilestoneNumber,
		Message:         reason,
	})
}

// deployGate projects the provisioning service's readiness read onto the run
// supervisor's DeployGate. It calls the service METHOD, not its HTTP handler:
// the handler is a thin projection of this same call, and routing an in-process
// workflow activity back through the edge would buy nothing but a socket.
type deployGate struct {
	prov *provisioning.Service
}

// DeploymentReadiness marks a project with no write target
// delivery.ErrDeployPermanent and delivery.ErrNoWriteTarget: it is a
// configuration fact, so the run settles failed naming the cause instead of
// retrying. Any other read failure is returned as is and retried.
func (g deployGate) DeploymentReadiness(ctx context.Context, orgID, projectID, env string) ([]string, []string, error) {
	readiness, err := g.prov.DeploymentReadiness(ctx, orgID, projectID, env)
	var nwt *openchoreo.ErrNoWriteTarget
	if errors.As(err, &nwt) {
		return nil, nil, fmt.Errorf("%w: %w: %w", delivery.ErrDeployPermanent, delivery.ErrNoWriteTarget, err)
	}
	if err != nil {
		return nil, nil, err
	}
	return readiness.Unconfigured, readiness.Provisioning, nil
}
