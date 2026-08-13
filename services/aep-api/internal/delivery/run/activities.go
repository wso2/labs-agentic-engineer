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

package run

import (
	"context"
	"errors"
	"log/slog"

	"go.temporal.io/sdk/temporal"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// errNotConfigured is returned by an activity whose port was not wired. It is
// deliberately an ERROR for the stores (a supervisor that cannot record its own
// outcome must not pretend it did) and deliberately NOT one for the optional
// collaborators, which degrade to "nothing to do".
var errNotConfigured = errors.New("run: activity dependency not configured")

// Activities is every I/O the run loop performs. Each one is a thin adapter
// over a port — there is no loop logic here, and no decision: the workflow
// decides, the activity fetches or records.
type Activities struct {
	runs       RunStore
	cycles     CycleStore
	milestones MilestoneReader
	prs        PRReader
	design     DesignReader
	builds     BuildReader
	validation ValidationCoordinator
	dispatcher delivery.MilestoneDispatcher
	apiTraits  APITraitSyncer
}

// Deps carries the activity adapters. runs/cycles/milestones are required; the
// rest degrade (see each activity).
type Deps struct {
	Runs       RunStore
	Cycles     CycleStore
	Milestones MilestoneReader
	PRs        PRReader
	Design     DesignReader
	Builds     BuildReader
	Validation ValidationCoordinator
	Dispatcher delivery.MilestoneDispatcher
	APITraits  APITraitSyncer
}

// NewActivities wires the activity adapters.
func NewActivities(d Deps) *Activities {
	return &Activities{
		runs:       d.Runs,
		cycles:     d.Cycles,
		milestones: d.Milestones,
		prs:        d.PRs,
		design:     d.Design,
		builds:     d.Builds,
		validation: d.Validation,
		dispatcher: d.Dispatcher,
		apiTraits:  d.APITraits,
	}
}

// ---- run row ---------------------------------------------------------------

// SetRunStateInput moves a run between the two non-terminal states.
type SetRunStateInput struct {
	RunID string `json:"runId"`
	State string `json:"state"`
}

// SetRunState mirrors the loop's waiting ⇄ running oscillation onto the run
// row, which is what the console polls. A run that has already settled is a
// no-op in the repository, so a late write cannot resurrect it.
func (a *Activities) SetRunState(ctx context.Context, in SetRunStateInput) error {
	if a.runs == nil {
		return errNotConfigured
	}
	return a.runs.SetState(ctx, in.RunID, in.State)
}

// SettleRunInput ends a run with its terminal state and reason.
type SettleRunInput struct {
	RunID  string `json:"runId"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// SettleRun writes the run's outcome. Guarded in the repository on the run not
// already being terminal, so the first settle wins.
func (a *Activities) SettleRun(ctx context.Context, in SettleRunInput) error {
	if a.runs == nil {
		return errNotConfigured
	}
	return a.runs.Settle(ctx, in.RunID, in.State, in.Reason)
}

// BumpRunBudgetInput names the counter to increment.
type BumpRunBudgetInput struct {
	RunID   string `json:"runId"`
	Counter string `json:"counter"`
}

// BumpRunBudget increments one budget counter on the run row. It is READ-MODEL
// BOOKKEEPING only: the workflow counts its own budgets deterministically, so a
// failed bump costs the console a number, never the loop a decision.
func (a *Activities) BumpRunBudget(ctx context.Context, in BumpRunBudgetInput) error {
	if a.runs == nil {
		return errNotConfigured
	}
	return a.runs.BumpBudget(ctx, in.RunID, delivery.RunBudget(in.Counter))
}

// SetValidationVerdictInput records one validation ATTEMPT's outcome and the issue
// it came from. Issue is 0 when there is no validation issue to name (an incident
// run, or a skip decided before minting). CycleID is empty for a verdict that
// belongs to no cycle — `skipped`, decided before any validation cycle opens.
type SetValidationVerdictInput struct {
	RunID   string `json:"runId"`
	CycleID string `json:"cycleId,omitempty"`
	Verdict string `json:"verdict"`
	Issue   int    `json:"issue,omitempty"`
}

// SetValidationVerdict records the attempt's verdict in the two places that need
// it, in one activity so they cannot drift: the CYCLE row, which keeps this
// attempt's own answer for good, and the RUN row, which carries the latest
// attempt's answer because that is what the deployment surface reads.
//
// The cycle write comes first. If only one of the two lands, the run row lagging
// its cycle ledger is the recoverable direction — the workflow retries and the
// cycle write is write-once, so the retry is a no-op there and completes the run
// write. The reverse would leave a run claiming a verdict no attempt admits to.
func (a *Activities) SetValidationVerdict(ctx context.Context, in SetValidationVerdictInput) error {
	if a.runs == nil {
		return errNotConfigured
	}
	if in.CycleID != "" {
		if a.cycles == nil {
			return errNotConfigured
		}
		if err := a.cycles.SetValidationVerdict(ctx, in.CycleID, in.Verdict, in.Issue); err != nil {
			return err
		}
	}
	return a.runs.SetValidationVerdict(ctx, in.RunID, in.Verdict, in.Issue)
}

// ---- cycle record ----------------------------------------------------------

// AppendCycleInput opens a new cycle record under a run.
type AppendCycleInput struct {
	RunID     string `json:"runId"`
	OrgID     string `json:"orgId"`
	ProjectID string `json:"projectId"`
	Kind      string `json:"kind"`
}

// AppendCycle opens the cycle record for a dispatch and returns its id.
func (a *Activities) AppendCycle(ctx context.Context, in AppendCycleInput) (string, error) {
	if a.cycles == nil {
		return "", errNotConfigured
	}
	return a.cycles.Append(ctx, &delivery.RunCycle{
		RunID:     in.RunID,
		OrgID:     in.OrgID,
		ProjectID: in.ProjectID,
		Kind:      in.Kind,
	})
}

// NoteCycleDispatchInput records one dispatch attempt against a cycle.
type NoteCycleDispatchInput struct {
	CycleID string `json:"cycleId"`
	JobRef  string `json:"jobRef"`
}

// NoteCycleDispatch increments the cycle's attempt count and re-points it at
// the newly launched Job.
func (a *Activities) NoteCycleDispatch(ctx context.Context, in NoteCycleDispatchInput) error {
	if a.cycles == nil {
		return errNotConfigured
	}
	return a.cycles.NoteDispatch(ctx, in.CycleID, in.JobRef)
}

// FinishCycleInput closes a cycle record.
type FinishCycleInput struct {
	CycleID  string `json:"cycleId"`
	MergeSHA string `json:"mergeSha,omitempty"`
}

// FinishCycle closes the cycle. Usually a no-op: the event plane already closed
// it on the merge webhook, and the repository's open-cycle guard makes the
// second close change nothing. The supervisor calls it anyway because a cycle
// that ended WITHOUT a merge — agent death, a conflict, a cancel — has no
// webhook to close it.
func (a *Activities) FinishCycle(ctx context.Context, in FinishCycleInput) error {
	if a.cycles == nil {
		return errNotConfigured
	}
	return a.cycles.Finish(ctx, in.CycleID, in.MergeSHA)
}

// CycleFactsInput asks for the run's current cycle record.
type CycleFactsInput struct {
	OrgID string `json:"orgId"`
	RunID string `json:"runId"`
}

// CycleFacts is what the EVENT PLANE learned about the cycle from webhooks —
// the supervisor's ground truth for "did this cycle land?".
type CycleFacts struct {
	CycleID  string `json:"cycleId"`
	Attempts int    `json:"attempts"`
	Branch   string `json:"branch,omitempty"`
	PRNumber int    `json:"prNumber,omitempty"`
	MergeSHA string `json:"mergeSha,omitempty"`
	Ended    bool   `json:"ended"`
}

// ReadCycleFacts reads the cycle record back.
//
// This is the poll behind "never trust the signal payload alone": a merge
// signal wakes the loop, and THIS is what tells it a merge really happened —
// which is also how a cycle whose merge webhook was lost still finishes, off
// the deadline path.
func (a *Activities) ReadCycleFacts(ctx context.Context, in CycleFactsInput) (CycleFacts, error) {
	if a.cycles == nil {
		return CycleFacts{}, errNotConfigured
	}
	row, err := a.cycles.Latest(ctx, in.OrgID, in.RunID)
	if err != nil || row == nil {
		return CycleFacts{}, err
	}
	return CycleFacts{
		CycleID:  row.ID,
		Attempts: row.Attempts,
		Branch:   row.Branch,
		PRNumber: row.PRNumber,
		MergeSHA: row.MergeSHA,
		Ended:    row.EndedAt != nil,
	}, nil
}

// ---- milestone -------------------------------------------------------------

// MilestoneRef identifies the milestone a poll or a close is about.
type MilestoneRef struct {
	OrgID           string `json:"orgId"`
	ProjectID       string `json:"projectId"`
	MilestoneNumber int    `json:"milestoneNumber"`
}

// PollMilestone is the cycle-boundary read of ground truth — ONE GraphQL round
// trip returning the gate count, the working set and the total.
//
// Every boundary decision is made from this and nothing else: whether to
// dispatch, whether a gate is holding, whether the version is finished, and
// whether the last cycle made progress.
func (a *Activities) PollMilestone(ctx context.Context, in MilestoneRef) (MilestoneSnapshot, error) {
	if a.milestones == nil {
		return MilestoneSnapshot{}, errNotConfigured
	}
	counts, err := a.milestones.MilestoneIssueCounts(ctx, in.OrgID, in.ProjectID, in.MilestoneNumber)
	if err != nil {
		return MilestoneSnapshot{}, sourceControlErr(err)
	}
	if counts == nil {
		return MilestoneSnapshot{}, nil
	}
	return MilestoneSnapshot{
		Work:  counts.OpenNonGateWork(),
		Gates: counts.OpenProvision,
		Total: counts.OpenTotal,
	}, nil
}

// CloseMilestone closes the settled version's milestone. Display only, and
// best-effort by contract: the run's outcome is the run row, so a close that
// fails must not turn a succeeded run into a failed one.
func (a *Activities) CloseMilestone(ctx context.Context, in MilestoneRef) error {
	if a.milestones == nil {
		return nil
	}
	if err := a.milestones.CloseMilestone(ctx, in.OrgID, in.ProjectID, in.MilestoneNumber); err != nil {
		slog.WarnContext(ctx, "run: closing the settled milestone failed — the run still succeeded",
			"project", in.ProjectID, "milestone", in.MilestoneNumber, "error", err)
	}
	return nil
}

// ---- builds ----------------------------------------------------------------

// CycleBuildsInput asks how far a cycle's build fan-out has got.
type CycleBuildsInput struct {
	OrgID     string `json:"orgId"`
	ProjectID string `json:"projectId"`
	PRNumber  int    `json:"prNumber"`
	MergeSHA  string `json:"mergeSha"`
}

// PollCycleBuilds derives the cycle's build state from OpenChoreo.
//
// It recomputes the SAME path diff the event plane fanned out over — from the
// merged pull request's files and the design's App Paths, through the root
// delivery.DiffComponents — because the expected set has to match the triggered
// set exactly or the loop would either hang on a component nobody built or
// settle before one reported.
//
// Nothing here is stored: per-component build state is derived on read, always.
func (a *Activities) PollCycleBuilds(ctx context.Context, in CycleBuildsInput) (CycleBuildState, error) {
	if a.builds == nil || a.prs == nil || a.design == nil || in.MergeSHA == "" || in.PRNumber == 0 {
		// Nothing to observe is genuinely green: a cycle whose merge touched no
		// component (a validation run's tests-and-report pull request) has no
		// build to wait for.
		return CycleBuildState{}, nil
	}
	files, err := a.prs.ListPullRequestFiles(ctx, in.OrgID, in.ProjectID, in.PRNumber)
	if err != nil {
		return CycleBuildState{}, sourceControlErr(err)
	}
	paths, err := a.design.ComponentPaths(ctx, in.OrgID, in.ProjectID)
	if err != nil {
		return CycleBuildState{}, sourceControlErr(err)
	}
	diff := delivery.DiffComponents(files, paths)
	out := CycleBuildState{Expected: len(diff.Components)}
	for _, component := range diff.Components {
		runs, lerr := a.builds.ListBuildRuns(ctx, in.OrgID, in.ProjectID, component)
		if lerr != nil {
			return CycleBuildState{}, lerr
		}
		switch classifyComponentBuild(runs, delivery.BuildRunNamePrefix(in.ProjectID, component, in.MergeSHA)) {
		case buildGreen:
			out.Settled++
		case buildRed:
			out.Settled++
			out.Red = append(out.Red, component)
		case buildPending:
		}
	}
	return out, nil
}

// ---- managed-API traits -----------------------------------------------------

// ProjectRef names the project an activity acts on, for the activities whose
// scope is the whole project rather than one milestone or one cycle.
type ProjectRef struct {
	OrgID     string `json:"orgId"`
	ProjectID string `json:"projectId"`
}

// SyncAPITraits lands the per-environment `api-configuration` trait config on
// every protected component's ReleaseBinding in the project.
//
// Called once per cycle at builds-green, which is the earliest point in a run
// where the write target exists: OpenChoreo creates the ReleaseBinding from the
// workload the build's last step generates, so before green there may be
// nothing to patch. The supervisor observes green on a poll up to
// buildPollInterval after the WorkflowRun actually completed, by which time the
// deploy chain has long since produced the binding.
//
// Degrades to "nothing to do" when unwired, like the other optional
// collaborators: a deployment with no trait emitter has no managed-API policy
// to converge, which is a legitimate configuration rather than a failed run.
func (a *Activities) SyncAPITraits(ctx context.Context, in ProjectRef) error {
	if a.apiTraits == nil {
		return nil
	}
	if err := a.apiTraits.SyncProjectAPITraits(ctx, in.OrgID, in.ProjectID); err != nil {
		// Logged here as well as returned: Temporal retries this activity, and the
		// per-attempt cause is otherwise only visible in workflow history.
		slog.ErrorContext(ctx, "run: managed-API trait sync failed",
			"orgID", in.OrgID, "projectID", in.ProjectID, "error", err)
		return err
	}
	return nil
}

// ---- validation ------------------------------------------------------------

// EnsureValidationIssue mints the run's validation issue into the milestone and
// returns its number, or 0 when there is nothing to validate.
//
// An unwired coordinator returns 0 rather than an error: "this deployment has
// no acceptance oracle" and "this deployment has no validation feature" are the
// same thing from the loop's point of view, and neither is a failed run.
func (a *Activities) EnsureValidationIssue(ctx context.Context, in MilestoneRef) (int, error) {
	if a.validation == nil {
		return 0, nil
	}
	issue, err := a.validation.EnsureValidationIssue(ctx, in.OrgID, in.ProjectID, in.MilestoneNumber)
	return issue, sourceControlErr(err)
}

// ValidationReportRef identifies the report to read: the project, plus the commit
// to read it at. At is the validation cycle's merge SHA, which is what makes the
// report belong to THIS run rather than to whichever run last overwrote the path.
type ValidationReportRef struct {
	OrgID     string `json:"orgId"`
	ProjectID string `json:"projectId"`
	At        string `json:"at,omitempty"`
}

// ValidationOutcome is one attempt's answer: the verdict, and a digest of the
// evidence behind it.
//
// The digest is what lets the loop stop early. Two attempts whose reports agree on
// every criterion and every message learned the same nothing — the repair did not
// change the answer — so spending the rest of the attempt budget could only produce
// the same report a third time. It covers the criteria and their outcomes only, not
// the file, because the report embeds the commit it was generated at.
type ValidationOutcome struct {
	Verdict string `json:"verdict"`
	Digest  string `json:"digest,omitempty"`
}

// ReadValidationVerdict reads the runner's committed report at the cycle's merge
// commit and returns one of the delivery.ValidationVerdict* values.
//
// An unwired coordinator yields "skipped" — there is genuinely no validation in
// that configuration. A report that is absent AT THAT COMMIT is a different
// matter and yields "unreported", which fails the run once its attempts are spent:
// the read is pinned, so an absence is a fact about this run rather than a
// propagation artifact.
func (a *Activities) ReadValidationVerdict(ctx context.Context, in ValidationReportRef) (ValidationOutcome, error) {
	if a.validation == nil {
		return ValidationOutcome{Verdict: delivery.ValidationVerdictSkipped}, nil
	}
	verdict, digest, err := a.validation.Verdict(ctx, in.OrgID, in.ProjectID, in.At)
	if err != nil {
		return ValidationOutcome{}, sourceControlErr(err)
	}
	return ValidationOutcome{Verdict: verdict, Digest: digest}, nil
}

// MintValidationRepairIssuesInput names the attempt whose failures become work.
type MintValidationRepairIssuesInput struct {
	OrgID           string `json:"orgId"`
	ProjectID       string `json:"projectId"`
	MilestoneNumber int    `json:"milestoneNumber"`
	// At is the validation cycle's merge commit — the same pin the verdict was read
	// at, so the failures filed are the ones this attempt actually reported.
	At string `json:"at"`
	// CycleID is the attempt's identity and the issues' dedupe key.
	CycleID string `json:"cycleId"`
}

// MintValidationRepairIssues files one issue per failed criterion into the
// milestone, and returns their numbers.
//
// An unwired coordinator mints nothing, like the other optional collaborators —
// but the count matters to the caller here, because an empty result means the next
// boundary poll will find no new work.
func (a *Activities) MintValidationRepairIssues(ctx context.Context, in MintValidationRepairIssuesInput) ([]int, error) {
	if a.validation == nil {
		return nil, nil
	}
	filed, err := a.validation.MintRepairIssues(ctx, in.OrgID, in.ProjectID, in.MilestoneNumber, in.At, in.CycleID)
	return filed, sourceControlErr(err)
}

// ---- dispatch --------------------------------------------------------------

// DispatchAgent launches the cycle's agent run and returns the Job reference.
//
// Two non-retryable failure classes are stamped here (Temporal must not retry
// either): agent death — a launch that did not happen, answered by the cycle's
// re-dispatch budget — and quota blocked — entitlement refused, not death.
// Letting Temporal retry agent death would spend that budget invisibly; quota
// blocked cannot be cleared by retry.
func (a *Activities) DispatchAgent(ctx context.Context, in delivery.MilestoneDispatch) (string, error) {
	if a.dispatcher == nil {
		return "", errNotConfigured
	}
	jobRef, err := a.dispatcher.Dispatch(ctx, in)
	if errors.Is(err, delivery.ErrAgentQuotaExceeded) {
		// A sentinel does not survive the activity boundary — Temporal
		// round-trips errors as data — so the refusal is re-expressed as a
		// TYPED, non-retryable ApplicationError the workflow can branch on.
		// Non-retryable because no retry can free a billing slot.
		return "", temporal.NewNonRetryableApplicationError(
			err.Error(), delivery.ErrTypeAgentQuotaBlocked, err)
	}
	return jobRef, err
}
