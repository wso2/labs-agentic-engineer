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

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/run"
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

func (a runRuns) BumpBudget(ctx context.Context, id string, counter delivery.RunBudget) error {
	_, err := a.runs.BumpBudget(ctx, id, counter)
	return err
}

func (a runRuns) SetValidationVerdict(ctx context.Context, id, verdict string, issue int) error {
	_, err := a.runs.SetValidationVerdict(ctx, id, verdict, issue)
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

func (a runCycles) NoteDispatch(ctx context.Context, cycleID, jobRef string) error {
	_, err := a.cycles.NoteDispatch(ctx, cycleID, jobRef)
	return err
}

func (a runCycles) Finish(ctx context.Context, cycleID, mergeSHA string) error {
	_, err := a.cycles.Finish(ctx, cycleID, mergeSHA)
	return err
}

func (a runCycles) SetValidationVerdict(ctx context.Context, cycleID, verdict string, issue int, digest string) error {
	_, err := a.cycles.SetValidationVerdict(ctx, cycleID, verdict, issue, digest)
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
// is left is a delegation plus the ref-pinned report read, which is the whole
// reason this type exists at the boundary.
type runValidation struct {
	svc   *validation.Service
	files spec.FilesService
}

func (a runValidation) EnsureValidationIssue(ctx context.Context, orgID, projectID string, milestoneNumber int) (int, error) {
	return a.svc.EnsureValidationIssue(ctx, orgID, projectID, milestoneNumber)
}

func (a runValidation) Verdict(ctx context.Context, orgID, projectID, at string) (string, string, error) {
	raw, err := a.report(ctx, orgID, projectID, at)
	if err != nil {
		return "", "", err
	}
	// Both derived from the same bytes, in one read: the verdict the run stores, and
	// the digest that tells a later attempt whether anything changed.
	return validation.VerdictFromReport(raw), validation.ReportDigest(raw), nil
}

func (a runValidation) CloseValidationIssue(ctx context.Context, orgID, projectID string, issue int, verdict string) error {
	return a.svc.CloseValidationIssue(ctx, orgID, projectID, issue, verdict)
}

func (a runValidation) MintRepairIssues(ctx context.Context, orgID, projectID string, milestoneNumber int, at, cycleID string) ([]int, error) {
	raw, err := a.report(ctx, orgID, projectID, at)
	if err != nil {
		return nil, err
	}
	return a.svc.MintRepairIssues(ctx, orgID, projectID, milestoneNumber, raw, cycleID)
}

// report reads the runner's committed report at a pinned commit. It is the ONE
// reader of that file on the run path — the verdict and the repair issues are
// separate activities, each deriving from ground truth on its own retry, but they
// derive from the same read implementation so they can never disagree about which
// bytes the attempt produced.
//
// An absent file is not an error: the validation cycle merged and committed no
// report AT ITS OWN MERGE COMMIT, which is a fact about this run rather than a
// stale read. Nil bytes are what VerdictFromReport maps to `unreported`.
func (a runValidation) report(ctx context.Context, orgID, projectID, at string) ([]byte, error) {
	fc, err := a.files.ReadAt(ctx, orgID, projectID, validation.ReportFilePath, at)
	if err != nil {
		if errors.Is(err, spec.ErrFileNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return []byte(fc.Content), nil
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

// deployGate projects the provisioning service's readiness read onto the run
// supervisor's DeployGate. It calls the service METHOD, not its HTTP handler:
// the handler is a thin projection of this same call, and routing an in-process
// workflow activity back through the edge would buy nothing but a socket.
type deployGate struct {
	prov *provisioning.Service
}

func (g deployGate) DeploymentReadiness(ctx context.Context, orgID, projectID, env string) ([]string, []string, error) {
	readiness, err := g.prov.DeploymentReadiness(ctx, orgID, projectID, env)
	if err != nil {
		return nil, nil, err
	}
	return readiness.Unconfigured, readiness.Provisioning, nil
}
