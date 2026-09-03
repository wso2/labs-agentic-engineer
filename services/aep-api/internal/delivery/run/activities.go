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
	"sort"

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
	deployer   Deployer
	deployRead DeploymentReader
	deployMint DeployIssueMinter
	halter     WorkHalter
	canceller  WorkCanceller
	gates      Gates
	planner    Planner
	deployGate DeployGate
}

// Deps carries the activity adapters. runs/cycles/milestones are required; the
// rest degrade (see each activity).
type Deps struct {
	Runs         RunStore
	Cycles       CycleStore
	Milestones   MilestoneReader
	PRs          PRReader
	Design       DesignReader
	Builds       BuildReader
	Validation   ValidationCoordinator
	Dispatcher   delivery.MilestoneDispatcher
	Deploy       Deployer
	Deployments  DeploymentReader
	DeployIssues DeployIssueMinter
	Halter       WorkHalter
	Canceller    WorkCanceller
	Gates        Gates
	Planner      Planner
	DeployGate   DeployGate
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
		deployer:   d.Deploy,
		deployRead: d.Deployments,
		deployMint: d.DeployIssues,
		halter:     d.Halter,
		canceller:  d.Canceller,
		gates:      d.Gates,
		planner:    d.Planner,
		deployGate: d.DeployGate,
	}
}

// ---- run row ---------------------------------------------------------------

// SetRunStateInput moves a run between the two non-terminal states.
type SetRunStateInput struct {
	RunID string `json:"runId"`
	State string `json:"state"`
	// WaitingReason / BlockingDependencies are carried only into `waiting`, and
	// only by the deploy gate — every other park is self-evident from the phase.
	WaitingReason        string   `json:"waitingReason,omitempty"`
	BlockingDependencies []string `json:"blockingDependencies,omitempty"`
}

// SetRunState mirrors the loop's waiting ⇄ running oscillation onto the run
// row, which is what the console polls. A run that has already settled is a
// no-op in the repository, so a late write cannot resurrect it.
func (a *Activities) SetRunState(ctx context.Context, in SetRunStateInput) error {
	if a.runs == nil {
		return errNotConfigured
	}
	if in.State == delivery.RunStateWaiting && in.WaitingReason != "" {
		return a.runs.SetWaiting(ctx, in.RunID, in.WaitingReason, in.BlockingDependencies)
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

// SetValidationVerdictInput records one validation ATTEMPT's outcome, the issue
// it came from, and the digest of the evidence behind it. Issue is 0 when there is
// no validation issue to name (a skip decided before minting). CycleID is empty
// for a verdict that belongs to no cycle — `skipped`, decided before any
// validation cycle opens.
type SetValidationVerdictInput struct {
	RunID   string `json:"runId"`
	CycleID string `json:"cycleId,omitempty"`
	Verdict string `json:"verdict"`
	Issue   int    `json:"issue,omitempty"`
	// Digest fingerprints what the attempt CONCLUDED, and is what the NEXT
	// attempt compares against to tell a repair that moved something from one
	// that did not. It rides this input rather than a write of its own because
	// the cycle write is fenced write-once on an empty verdict: a digest written
	// afterwards could never land on the cycle it belongs to.
	Digest string `json:"digest,omitempty"`
}

// SetValidationVerdict records the attempt's verdict in the two places that need
// it, in one activity so they cannot drift: the CYCLE row, which keeps this
// attempt's own answer and its digest for good, and the RUN row, which carries
// the latest attempt's answer because that is what the deployment surface reads.
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
		if err := a.cycles.SetValidationVerdict(ctx, in.CycleID, in.Verdict, in.Issue, in.Digest); err != nil {
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

// CycleFacts is the loop's GROUND TRUTH about a wait it is currently in: what
// the event plane learned about the cycle from webhooks, plus whether a person
// has asked the run to stop.
//
// The two travel together because they answer the same question at the same
// moment — "should this wait carry on?" — and because reading them in one
// activity is what lets cancel be honoured with no round trip the loop was not
// already making.
type CycleFacts struct {
	CycleID  string `json:"cycleId"`
	Attempts int    `json:"attempts"`
	Branch   string `json:"branch,omitempty"`
	PRNumber int    `json:"prNumber,omitempty"`
	MergeSHA string `json:"mergeSha,omitempty"`
	Ended    bool   `json:"ended"`
	// CancelRequested is the run row's cancellation stamp, not the signal. The
	// signal is a wake-up; this is the evidence — which is what stops a reaped
	// agent pod from reading as agent death and buying a re-dispatch.
	CancelRequested bool `json:"cancelRequested,omitempty"`
}

// ReadCycleFacts reads the cycle record and the run's cancel stamp back.
//
// This is the poll behind "never trust the signal payload alone": a merge
// signal wakes the loop, and THIS is what tells it a merge really happened —
// which is also how a cycle whose merge webhook was lost still finishes, off
// the deadline path. Cancel rides it for the same reason and gains the same
// property: a cancel whose signal never arrived costs latency, not correctness.
//
// The cancel read comes FIRST and is independent of the cycle row, because the
// boundary consults this before a run has dispatched anything — a milestone run
// with no cycle yet must still be able to notice it was cancelled.
func (a *Activities) ReadCycleFacts(ctx context.Context, in CycleFactsInput) (CycleFacts, error) {
	if a.cycles == nil || a.runs == nil {
		return CycleFacts{}, errNotConfigured
	}
	cancelled, err := a.runs.CancelRequested(ctx, in.OrgID, in.RunID)
	if err != nil {
		return CycleFacts{}, err
	}
	facts := CycleFacts{CancelRequested: cancelled}
	row, err := a.cycles.Latest(ctx, in.OrgID, in.RunID)
	if err != nil || row == nil {
		return facts, err
	}
	facts.CycleID = row.ID
	facts.Attempts = row.Attempts
	facts.Branch = row.Branch
	facts.PRNumber = row.PRNumber
	facts.MergeSHA = row.MergeSHA
	facts.Ended = row.EndedAt != nil
	return facts, nil
}

// ---- milestone -------------------------------------------------------------

// MilestoneRef identifies the milestone a poll or a close is about.
type MilestoneRef struct {
	OrgID           string `json:"orgId"`
	ProjectID       string `json:"projectId"`
	MilestoneNumber int    `json:"milestoneNumber"`
}

// PollMilestone is the cycle-boundary read of ground truth — ONE GraphQL round
// trip returning the gate count, BOTH working sets, the total, and how much
// verdict-sourced repair work is open.
//
// Every boundary decision is made from this and nothing else: whether to
// dispatch, whether a gate is holding, whether the version is finished, whether
// the last cycle made progress, and — for a task run — whether draining the
// milestone should reopen the version's validation task.
//
// Both working sets ride one answer rather than the caller's own. The counts all
// arrive in the same GraphQL response, so computing only one of them would save
// nothing and would put the choice of population in the activity, which is the
// workflow's to make (see bookends).
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
		DevWork:           counts.OpenDevWork(),
		TaskWork:          counts.OpenTaskWork(),
		Gates:             counts.OpenProvision,
		Total:             counts.OpenTotal,
		ValidationRepairs: counts.OpenValidationRepairs,
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
	out := CycleBuildState{Expected: len(diff.Components), Components: diff.Components}
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

// ---- planning ---------------------------------------------------------------

// PlanMilestoneInput fills a version's milestone: mint its dependency gates,
// then plan its Tasks into it.
type PlanMilestoneInput struct {
	OrgID           string                    `json:"orgId"`
	ProjectID       string                    `json:"projectId"`
	MilestoneNumber int                       `json:"milestoneNumber"`
	Tag             string                    `json:"tag"`
	ProvisionInputs []delivery.ProvisionInput `json:"provisionInputs,omitempty"`
}

// ProvisionGates authors the version's dependencies and mints its gate issues.
//
// Unwired degrades to "nothing to do", like the other optional collaborators: a
// project with no dependency drawer has no gates to mint, which is an ordinary
// configuration rather than a failed version.
func (a *Activities) ProvisionGates(ctx context.Context, in PlanMilestoneInput) error {
	if a.gates == nil {
		return nil
	}
	// Heartbeat: this activity WAITS — for OpenChoreo to cut a release per platform
	// resource — and a cancel pressed during that wait has to reach the authoring,
	// not just free the workflow. See heartbeating.
	//
	// provisionErr on the inside is the sharper of the two guards this call has.
	// It fails a NAMED permanent fault (a missing ClusterResourceType) on the
	// first attempt with the provisioner's own message; the bounded retry policy
	// in gateActivityCtx is the backstop for the modes nobody has named yet.
	return heartbeating(ctx, func(ctx context.Context) error {
		return provisionErr(a.gates.ProvisionForBuild(ctx, in.OrgID, in.ProjectID, in.Tag, in.MilestoneNumber, in.ProvisionInputs))
	})
}

// PlanMilestone runs the version's planning turn.
//
// Idempotent by construction, which is what makes it safe under Temporal's
// retries AND under a fresh execution started to recover a run: minting dedupes
// on the title slug against the milestone's own issues, so a re-plan is additive
// only. A retry after a partially-minted plan completes it rather than doubling
// it.
func (a *Activities) PlanMilestone(ctx context.Context, in PlanMilestoneInput) error {
	if a.planner == nil {
		return nil
	}
	// Heartbeat, for the same reason as ProvisionGates: an agent turn is minutes
	// long, and a cancel pressed mid-turn should end the turn rather than let it
	// run on to mint a plan for a version nobody is building.
	return heartbeating(ctx, func(ctx context.Context) error {
		return planErr(a.planner.PlanIntoMilestone(ctx, in.OrgID, in.ProjectID, in.MilestoneNumber))
	})
}

// ---- deploy -----------------------------------------------------------------

// ProjectRef names the project an activity acts on, for the activities whose
// scope is the whole project rather than one milestone or one cycle.
type ProjectRef struct {
	OrgID     string `json:"orgId"`
	ProjectID string `json:"projectId"`
}

// DeployGateVerdict is what the gate saw, with the two blockers kept apart so
// the workflow can tell "the platform is still working" from "a human has not
// acted yet". Both empty means deploy.
type DeployGateVerdict struct {
	Unconfigured []string `json:"unconfigured,omitempty"`
	Provisioning []string `json:"provisioning,omitempty"`
}

// CheckDeployReadiness reads the project's deploy gate (ADR-0023): every
// external dependency configured, every platform resource provisioned.
//
// Unwired FAILS CLOSED, and is the one collaborator here that does — every
// other activity degrades to "nothing to do" because the worst case is work not
// happening. The worst case here is the opposite: a deploy that publishes an
// application with empty credentials, which is the exact outcome the gate
// exists to prevent. Non-retryable, because no amount of waiting wires a port.
//
// The env is left empty so the readiness service applies its own default rather
// than the run package pinning an environment name it does not own.
func (a *Activities) CheckDeployReadiness(ctx context.Context, in ProjectRef) (DeployGateVerdict, error) {
	if a.deployGate == nil {
		return DeployGateVerdict{}, temporal.NewNonRetryableApplicationError(
			"run: deploy gate not configured", "deploy-gate-not-configured", errNotConfigured)
	}
	unconfigured, provisioning, err := a.deployGate.DeploymentReadiness(ctx, in.OrgID, in.ProjectID, "")
	if err != nil {
		return DeployGateVerdict{}, err
	}
	return DeployGateVerdict{Unconfigured: unconfigured, Provisioning: provisioning}, nil
}

// ReadVersionState classifies EVERY component in the design against what has
// been built and what is deployed — the read behind both the reconcile and the
// delivery gate (ADR-0026).
//
// One activity rather than three, so the classification lands in workflow
// history the way the wave plan does: which components were behind, which were
// held and what each was waiting on is then readable off the run rather than
// inferred from what the stage went on to write.
//
// Unwired degrades to an EMPTY state, which reads as serving — the same answer
// DeployCycle's missing deployer gives, and for the same reason: a plane with no
// OpenChoreo behind it must not have its runs fail on a gate that has nothing to
// gate.
func (a *Activities) ReadVersionState(ctx context.Context, in ProjectRef) (delivery.VersionState, error) {
	if a.design == nil || a.builds == nil || a.deployRead == nil {
		return delivery.VersionState{}, nil
	}
	paths, err := a.design.ComponentPaths(ctx, in.OrgID, in.ProjectID)
	if err != nil {
		return delivery.VersionState{}, sourceControlErr(err)
	}
	// Sorted, because the map's iteration order is random and this list decides
	// the order of every promote, log line and issue that follows. The waves
	// re-order it by the design's edges; within a wave it is this order.
	components := make([]string, 0, len(paths))
	for name := range paths {
		components = append(components, name)
	}
	sort.Strings(components)

	builds := make(map[string][]BuildRunInfo, len(components))
	for _, name := range components {
		runs, lerr := a.builds.ListBuildRuns(ctx, in.OrgID, in.ProjectID, name)
		if lerr != nil {
			return delivery.VersionState{}, lerr
		}
		builds[name] = runs
	}
	states, err := a.deployRead.DeploymentState(ctx, in.OrgID, in.ProjectID, components)
	if err != nil {
		return delivery.VersionState{}, deployErr(err)
	}
	deploys := make(map[string]delivery.ComponentDeploy, len(states))
	for _, st := range states {
		deploys[st.Component] = st
	}

	out := versionState(in.ProjectID, components, builds, deploys)
	// Logged like the wave plan, and for the same reason: what is serving is the
	// premise every later decision rests on, and the symptom of getting it wrong
	// appears in a browser rather than anywhere near this code.
	serving, total := out.ServingCount()
	slog.InfoContext(ctx, "run: version state read",
		"orgID", in.OrgID, "projectID", in.ProjectID,
		"serving", serving, "components", total, "notServing", out.Describe())
	return out, nil
}

// PromoteInput promotes each target at its own commit — one wave of a plan, or
// the stage's closing converge.
type PromoteInput struct {
	OrgID     string                  `json:"orgId"`
	ProjectID string                  `json:"projectId"`
	Targets   []delivery.DeployTarget `json:"targets"`
}

// PromoteWave promotes one wave: cut each component's release from the Workload
// its build posted, then write the binding that pins it — wiring and pin in one
// object write.
//
// Each target carries its OWN commit, which is what makes this a reconcile
// rather than a cycle deploy: a pass promotes every behind component at the
// commit its newest green build was cut from, and those differ. A target with
// an empty commit CONVERGES — no release is re-cut and no component's live
// release moves.
//
// Degrades to "nothing to do" when unwired, like the other optional
// collaborators. That is a real configuration (a plane with no OpenChoreo
// behind it), not a failed run — but note what it means for the loop: with no
// deployer nothing ever becomes Ready and the stage reports everything
// deployed, which is the same answer the loop gave before it had a deploy stage
// at all.
func (a *Activities) PromoteWave(ctx context.Context, in PromoteInput) ([]delivery.ComponentDeploy, error) {
	if a.deployer == nil || len(in.Targets) == 0 {
		return nil, nil
	}
	out, err := a.deployer.Deploy(ctx, in.OrgID, in.ProjectID, in.Targets)
	if err != nil {
		// Logged as well as returned: Temporal retries this activity, and the
		// per-attempt cause is otherwise only visible in workflow history.
		slog.ErrorContext(ctx, "run: deploy failed",
			"orgID", in.OrgID, "projectID", in.ProjectID,
			"targets", delivery.TargetNames(in.Targets), "error", err)
		return nil, deployErr(err)
	}
	return out, nil
}

// PlanDeployInput asks what one reconcile pass should do, given what the
// version is serving.
type PlanDeployInput struct {
	OrgID     string                `json:"orgId"`
	ProjectID string                `json:"projectId"`
	Version   delivery.VersionState `json:"version"`
}

// PlanDeployWaves plans the pass: the behind components that may be promoted,
// levelled so every provider precedes the consumers whose start-up config
// carries its address, plus what to wait for and what is held.
//
// Unwired degrades to ONE wave of everything behind, matching PromoteWave's own
// no-deployer behaviour: a plane without OpenChoreo still walks the stage, it
// just has nothing to write.
func (a *Activities) PlanDeployWaves(ctx context.Context, in PlanDeployInput) (delivery.DeployPlan, error) {
	if a.deployer == nil {
		return unorderedPlan(in.Version), nil
	}
	plan, err := a.deployer.PlanDeploymentWaves(ctx, in.OrgID, in.ProjectID, in.Version)
	if err != nil {
		slog.ErrorContext(ctx, "run: deploy pass could not be planned",
			"orgID", in.OrgID, "projectID", in.ProjectID, "error", err)
		return delivery.DeployPlan{}, deployErr(err)
	}
	// Logged, not merely returned. The order is the stage's whole premise — a
	// consumer promoted before its provider is a blank page, and the symptom
	// appears in a browser rather than anywhere near this code. So is the HELD
	// set, which is the one thing a reader cannot infer from what was written.
	slog.InfoContext(ctx, "run: deploy pass planned",
		"orgID", in.OrgID, "projectID", in.ProjectID,
		"waves", planWaveNames(plan), "waiting", plan.Waited, "held", heldNames(plan))
	return plan, nil
}

// WaitSetInput is the readiness poll's population: everything a pass promoted,
// plus what was already converging.
type WaitSetInput struct {
	OrgID      string   `json:"orgId"`
	ProjectID  string   `json:"projectId"`
	Components []string `json:"components"`
}

// PollDeployments reads back each waited-on component's binding.
//
// With no reader wired every component reads as Ready, which keeps a plane
// without OpenChoreo from parking its runs in the deploy stage forever.
func (a *Activities) PollDeployments(ctx context.Context, in WaitSetInput) (CycleDeployState, error) {
	if a.deployRead == nil || len(in.Components) == 0 {
		return CycleDeployState{Expected: len(in.Components), Ready: len(in.Components)}, nil
	}
	states, err := a.deployRead.DeploymentState(ctx, in.OrgID, in.ProjectID, in.Components)
	if err != nil {
		return CycleDeployState{}, deployErr(err)
	}
	return classifyCycleDeploys(len(in.Components), states), nil
}

// MintDeployFixIssuesInput names the components a pass could not get running,
// each at the commit whose release it was promoting.
type MintDeployFixIssuesInput struct {
	OrgID           string                  `json:"orgId"`
	ProjectID       string                  `json:"projectId"`
	MilestoneNumber int                     `json:"milestoneNumber"`
	Failed          []delivery.DeployTarget `json:"failed"`
	Reasons         map[string]string       `json:"reasons,omitempty"`
}

// MintDeployFixIssues files one issue per component that did not come up.
//
// Unwired is a NO-OP rather than an error, matching the other optional
// collaborators — but it is the one whose absence changes the loop's outcome:
// with nothing minted the next boundary poll finds an empty working set and
// settles the run, so a plane without an issue minter reports a version
// delivered that never started. Wiring it is not optional in any real
// deployment.
func (a *Activities) MintDeployFixIssues(ctx context.Context, in MintDeployFixIssuesInput) ([]int, error) {
	if a.deployMint == nil || len(in.Failed) == 0 {
		return nil, nil
	}
	filed, err := a.deployMint.MintDeployFixIssues(ctx, in.OrgID, in.ProjectID, in.MilestoneNumber,
		in.Failed, in.Reasons)
	return filed, sourceControlErr(err)
}

// HaltWorkInput names the milestone whose unfinished work a failed run is giving
// up on, and why.
type HaltWorkInput struct {
	OrgID           string `json:"orgId"`
	ProjectID       string `json:"projectId"`
	MilestoneNumber int    `json:"milestoneNumber"`
	// Kind is the RUN's kind, which selects the working set — the population this
	// run was responsible for and no other. A dev run's halt must not reach a bug
	// a concurrent task run is working, and a task run's must not reach the
	// planned work it was never allowed to touch.
	Kind string `json:"kind"`
	// Reason is the run's terminal reason, quoted verbatim into each comment so a
	// human reading the issue learns which budget ran out without opening the run.
	Reason string `json:"reason"`
}

// HaltUnfinishedWork stamps `aep:halted` and a comment on every working-set issue
// a FAILED run could not finish, and returns their numbers.
//
// It is what keeps a budget meaning something. A run that exhausts one settles
// `failed` with its issues still OPEN, and the reconcile sweep's rule is "open
// work of this kind on a milestone with no live run starts a workflow" — so
// without this the sweep restarts, within a tick, exactly the work the run just
// gave up on, with fresh budgets, forever. The symptom is a cloud bill rather
// than a failing test, which is why the halt is not left to a later pass.
//
// The write goes through the event plane like MintDeployFixIssues, for the same
// reason: the supervisor still writes no issue of its own, and the plane owns the
// label vocabulary and the prose.
//
// Unwired is a NO-OP rather than an error — the same posture as every optional
// collaborator — and the cost is the same restart loop described above, so it is
// not optional in a real deployment.
func (a *Activities) HaltUnfinishedWork(ctx context.Context, in HaltWorkInput) ([]int, error) {
	if a.halter == nil {
		return nil, nil
	}
	halted, err := a.halter.HaltUnfinishedWork(ctx, in.OrgID, in.ProjectID, in.MilestoneNumber, in.Kind, in.Reason)
	return halted, sourceControlErr(err)
}

// CloseCancelledWorkInput names the milestone whose in-flight work a cancelled
// run is closing.
//
// No reason field, unlike HaltWorkInput: a cancel has exactly one cause — a
// person asked for it — so there is no failure class to quote onto the issues.
type CloseCancelledWorkInput struct {
	OrgID           string `json:"orgId"`
	ProjectID       string `json:"projectId"`
	MilestoneNumber int    `json:"milestoneNumber"`
	// Kind is the RUN's kind, which selects what the cancel abandons: a dev run's
	// whole milestone, a task run's bugs and conflicts, and nothing at all for a
	// validation run (whose own task close is the workflow's).
	Kind string `json:"kind"`
}

// CloseCancelledWork comments on, stamps `aep:cancelled` on, and closes every open
// issue a cancel abandons, and returns their numbers.
//
// It is what makes a cancel STICK. The reconcile sweep's trigger is "open work of
// this kind on a milestone with no live run starts a workflow", so leaving the
// issues open would have the sweep restart, within a tick, exactly the run the
// person just stopped — the same mechanism the halt exists to defeat, reached from
// the other ending.
//
// The write goes through the event plane like HaltUnfinishedWork and
// MintDeployFixIssues, for the same reason: the supervisor writes no issue of its
// own, and the plane owns the label vocabulary and the prose.
//
// Unwired is a NO-OP rather than an error — the same posture as every optional
// collaborator — and the cost is the restart loop above, so it is not optional in
// a real deployment.
func (a *Activities) CloseCancelledWork(ctx context.Context, in CloseCancelledWorkInput) ([]int, error) {
	if a.canceller == nil {
		return nil, nil
	}
	closed, err := a.canceller.CloseCancelledWork(ctx, in.OrgID, in.ProjectID, in.MilestoneNumber, in.Kind)
	return closed, sourceControlErr(err)
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

// ValidationHistoryInput asks what this milestone's earlier validation runs
// concluded. RunID is the run ASKING, excluded from the digest read so an attempt
// never compares itself against itself.
type ValidationHistoryInput struct {
	OrgID           string `json:"orgId"`
	ProjectID       string `json:"projectId"`
	MilestoneNumber int    `json:"milestoneNumber"`
	RunID           string `json:"runId"`
}

// ValidationHistory is how many times this version has been judged and what the
// previous judgement concluded — the two facts that SPAN validation runs.
type ValidationHistory struct {
	// Attempts counts the milestone's `validation` runs INCLUDING the one asking,
	// which is why the asking run compares `>=` against its allowance: it is the
	// attempt being spent.
	Attempts int `json:"attempts"`
	// Digest is the newest digest recorded by an EARLIER attempt, or "" for a
	// first one. Two consecutive identical digests prove the repair moved nothing.
	Digest string `json:"digest,omitempty"`
}

// ReadValidationHistory derives the version's attempt count and its previous
// report digest from the ledger.
//
// Derived rather than carried, because there is nothing to carry them IN: each
// attempt is its own run and its own workflow execution, so the previous
// attempt's state is gone by the time this one starts, and a value threaded
// through the run row would have to be written by a run that is ending and read
// by one that does not exist yet. The rows already say both things.
//
// An unwired store answers a first attempt with no history — the same reading a
// genuinely first attempt gets, which is the safe direction: it spends an attempt
// and files repair work, where a wrongly-large count would refuse to repair
// anything.
func (a *Activities) ReadValidationHistory(ctx context.Context, in ValidationHistoryInput) (ValidationHistory, error) {
	if a.runs == nil || a.cycles == nil {
		return ValidationHistory{Attempts: 1}, nil
	}
	rows, err := a.runs.ListByMilestone(ctx, in.OrgID, in.ProjectID, in.MilestoneNumber)
	if err != nil {
		return ValidationHistory{}, err
	}
	out := ValidationHistory{}
	priors := make([]string, 0, len(rows))
	for i := range rows {
		if rows[i].Kind != delivery.RunKindValidation {
			continue
		}
		out.Attempts++
		if rows[i].ID != in.RunID {
			priors = append(priors, rows[i].ID)
		}
	}
	if out.Attempts == 0 {
		// The asking run's own row was not in the list — it can only have been
		// deleted underneath a live workflow. Count this attempt anyway: the
		// alternative is an allowance that never depletes.
		out.Attempts = 1
	}
	digest, err := a.cycles.LatestValidationDigest(ctx, in.OrgID, priors)
	if err != nil {
		return ValidationHistory{}, err
	}
	out.Digest = digest
	return out, nil
}

// CloseValidationIssueInput closes the version's validation task. Verdict is
// carried only for the comment the close leaves behind.
type CloseValidationIssueInput struct {
	OrgID     string `json:"orgId"`
	ProjectID string `json:"projectId"`
	Issue     int    `json:"issue"`
	Verdict   string `json:"verdict,omitempty"`
}

// CloseValidationIssue closes the validation task the run adopted.
//
// The PLATFORM owns this close, which is why it is an activity at all: the
// validation pull request references its issue with `Validates #N` — not a GitHub
// closing keyword — so merging leaves the issue open and this is the only thing
// that shuts it. Two owners would race on every attempt, and the loser's reopen
// would be indistinguishable from a human's.
//
// An unwired coordinator is a no-op, like the other optional collaborators. Note
// what that costs: with nothing closing the task, the reconcile sweep sees an
// open `validation` issue and starts another validation run every pass. Wiring it
// is not optional in any real deployment.
func (a *Activities) CloseValidationIssue(ctx context.Context, in CloseValidationIssueInput) error {
	if a.validation == nil || in.Issue == 0 {
		return nil
	}
	return sourceControlErr(a.validation.CloseValidationIssue(ctx, in.OrgID, in.ProjectID, in.Issue, in.Verdict))
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
// Three non-retryable failure classes are stamped here (Temporal must not
// retry any): agent death — a launch that did not happen, answered by the
// cycle's re-dispatch budget; quota blocked — entitlement refused, not death;
// publisher credentials missing — Job create cannot stamp the SecretReference.
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
	if errors.Is(err, delivery.ErrPublisherCredentialsMissing) {
		return "", temporal.NewNonRetryableApplicationError(
			delivery.PublisherCredentialsMissingMessage, delivery.ErrTypePublisherCredentialsMissing, err)
	}
	return jobRef, err
}
