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
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The consumer ports the supervisor's ACTIVITIES drive. Workflow code touches
// none of them — it only calls activities — so every I/O the loop performs is
// named here, in one list, and every one of them is faked by a plain struct in
// the tests.

// RunStore is the run row's write surface plus the one read the start path
// needs. Satisfied by an adapter over delivery.MilestoneRunRepository.
//
// Note what is NOT here: no read of the budget counters. The workflow counts
// its own budgets deterministically and writes them outwards for the read
// model — reading them back would make the loop's control flow depend on a
// database round trip that a replay cannot reproduce.
//
// CancelRequested is the deliberate exception, and it is not the same kind of
// read. A budget is the workflow's OWN arithmetic, so reading it back would let
// a database disagree with a replay. A cancellation is somebody ELSE's fact
// about the run, which is exactly the shape of every other thing the loop polls
// — the milestone's issues, the cycle's merge — and it goes through an activity
// like all of them, so history records the answer and a replay reproduces it.
type RunStore interface {
	// TryAdmit inserts the run row unless the build mutex refuses it. Used
	// only by the adoption/sweep start path, which must admit and supervise
	// together; the plan path admits its own row before planning.
	TryAdmit(ctx context.Context, row *delivery.MilestoneRun) (admitted bool, out *delivery.MilestoneRun, err error)
	// LiveRunForMilestone returns the non-terminal run already working this
	// milestone, or (nil, nil). It is what makes StartRun idempotent under the
	// reconcile sweep, which re-offers the same milestone every pass.
	LiveRunForMilestone(ctx context.Context, orgID, projectID string, milestoneNumber int) (*delivery.MilestoneRun, error)
	// MilestoneSpecTag returns the `v<N>` version the milestone's existing runs
	// belong to, "" when it has none. An incident run is admitted against a
	// milestone somebody else's build already claimed, and it holds no tag of its
	// own — without inheriting one it would surface in the version ledger under
	// the milestone's GitHub name instead of a version.
	MilestoneSpecTag(ctx context.Context, orgID, projectID string, milestoneNumber int) (string, error)
	// ListByMilestone returns the milestone's runs, newest first. It is how a
	// validation run learns how many times this VERSION has already been judged:
	// the allowance is per version and each attempt is its own run, so the count
	// lives in the ledger and nowhere else.
	//
	// It is not a read of the loop's own arithmetic — the thing this port
	// deliberately does not offer. A budget the workflow counts itself must never
	// be read back, because a replay has to reproduce the same decisions without a
	// database; how many runs a milestone has had is somebody ELSE's fact, the
	// same shape as the milestone poll, and it goes through an activity like all
	// of them so history records the answer.
	ListByMilestone(ctx context.Context, orgID, projectID string, milestoneNumber int) ([]delivery.MilestoneRun, error)
	// SetState moves the run between waiting and running.
	SetState(ctx context.Context, id, state string) error
	// SetWaiting is SetState(waiting) plus the reason it is waiting and the
	// dependencies it is waiting ON. `waiting` is unbounded and only cancellation
	// exits it, so a park with no stated reason is indistinguishable from a hang
	// — the reason is what makes the state actionable rather than alarming.
	SetWaiting(ctx context.Context, id, reason string, dependencies []string) error
	// Settle writes the terminal state and its reason, once.
	Settle(ctx context.Context, id, state, reason string) error
	// BumpBudget increments one counter — bookkeeping for the read model, never
	// the loop's own arithmetic.
	BumpBudget(ctx context.Context, id string, counter delivery.RunBudget) error
	// SetValidationVerdict records the validation cycle's outcome on the run,
	// together with the validation issue that produced it so a settled run stays
	// navigable to its criteria. An issue of 0 leaves the stored one untouched.
	SetValidationVerdict(ctx context.Context, id, verdict string, issue int) error
	// CancelRequested reports whether a person has asked this run to stop.
	//
	// The DURABLE half of cancel. The cancel signal is the fast path — it stops
	// the loop at its next safe point rather than at its next poll — and this is
	// the evidence: a signal whose delivery failed, or one that arrived while the
	// workflow was mid-activity and got drained by a wait the loop has since left,
	// is still recoverable from here. Without it a pod the cancel REAPED is
	// indistinguishable from an agent that died on its own, so the loop spends a
	// re-dispatch and opens a fresh cycle over a run the user just stopped.
	//
	// A run that no longer exists answers false: there is nothing left to cancel.
	CancelRequested(ctx context.Context, orgID, runID string) (bool, error)
}

// CycleStore is the cycle-record surface: one row per dispatch. Satisfied by an
// adapter over delivery.RunCycleRepository.
//
// The supervisor writes the dispatch half (append, attempts, Job ref) and reads
// the half the EVENT PLANE writes (branch, pull request, merge SHA) — that read
// is the ground truth behind "did this cycle actually land?", which is why the
// loop never trusts a merge signal's payload on its own.
type CycleStore interface {
	Append(ctx context.Context, cycle *delivery.RunCycle) (cycleID string, err error)
	NoteDispatch(ctx context.Context, cycleID, jobRef string) error
	Finish(ctx context.Context, cycleID, mergeSHA string) error
	// SetValidationVerdict records one validation ATTEMPT's outcome on its own cycle
	// row — the verdict, the issue it was dispatched at, and the DIGEST of the
	// evidence — so a version judged more than once keeps every attempt's answer
	// rather than only the last. Written after Finish: the verdict comes from the
	// report at the cycle's merge commit, which does not exist until it has one.
	//
	// The digest rides this call because the underlying write is fenced write-once
	// on an empty verdict, so nothing recorded afterwards could ever land.
	SetValidationVerdict(ctx context.Context, cycleID, verdict string, issue int, digest string) error
	// LatestValidationDigest returns the newest digest recorded by any of these
	// runs' validation cycles, or "" when none did. This is how one attempt reads
	// what the PREVIOUS attempt concluded — the comparison spans runs, so it
	// cannot live in workflow state.
	LatestValidationDigest(ctx context.Context, orgID string, runIDs []string) (string, error)
	Latest(ctx context.Context, orgID, runID string) (*delivery.RunCycle, error)
}

// MilestoneReader is the milestone's ground truth: the one-call dispatch
// predicate input, and the close that marks a settled version.
// sourcecontrol.IssueService satisfies it.
type MilestoneReader interface {
	// MilestoneIssueCounts is the whole cycle-boundary poll — gates, working set
	// and total in a single GraphQL round trip. Never the REST milestone's
	// open_issues field, which counts pull requests.
	MilestoneIssueCounts(ctx context.Context, orgID, projectID string, number int) (*sourcecontrol.MilestoneIssueCounts, error)
	// CloseMilestone closes a settled version's milestone. DISPLAY ONLY: no
	// platform logic branches on milestone state, and a closed milestone still
	// accepts new issues, which is what lets an incident adopt into an old
	// version with no reopening choreography.
	CloseMilestone(ctx context.Context, orgID, projectID string, number int) error
}

// PRReader reads a merged pull request's changed files — the path-diff input
// behind "which components did this cycle's merge rebuild?".
// sourcecontrol.IssueService satisfies it.
type PRReader interface {
	ListPullRequestFiles(ctx context.Context, orgID, projectID string, number int) ([]string, error)
}

// DesignReader maps each component to its App Path. An empty App Path means the
// component builds from the repo root and therefore matches any change.
type DesignReader interface {
	ComponentPaths(ctx context.Context, orgID, projectID string) (map[string]string, error)
}

// BuildRunInfo is one OpenChoreo WorkflowRun reduced to the facts the
// supervisor needs. The OpenChoreo status vocabulary is deliberately mapped in
// the adapter: this package knows "terminal" and "succeeded", nothing about
// condition reasons.
type BuildRunInfo struct {
	Name      string `json:"name"`
	Terminal  bool   `json:"terminal"`
	Succeeded bool   `json:"succeeded"`
	// CommitSHA is the commit this run built. It is the version state's DESIRED
	// half: the newest succeeded run's commit names the release the component
	// should be serving.
	//
	// A field rather than a substring of Name, which also carries a short
	// commit: the name is composed through k8sname.Bounded, which truncates a
	// long readable head and appends a digest, so parsing it back is sound for
	// some projects and silently wrong for others. Empty for a run triggered
	// without a pinned commit, which cannot be matched to a merge and therefore
	// cannot be promoted.
	CommitSHA string `json:"commitSha,omitempty"`
	// StartedAt is when the cluster admitted the run — what orders two green
	// builds of one component, since the host returns them unordered.
	StartedAt time.Time `json:"startedAt"`
}

// BuildReader reads back a component's WorkflowRuns.
//
// Read-only, deliberately: the supervisor never triggers a build and never
// re-triggers one. The automatic re-trigger budget belongs to the event plane
// and is DERIVED from these very runs, so the supervisor counts the same runs
// to learn the same verdict rather than keeping a second tally that could
// disagree.
type BuildReader interface {
	ListBuildRuns(ctx context.Context, orgID, projectID, component string) ([]BuildRunInfo, error)
}

// ValidationCoordinator is the validation half of the loop, reached as a port
// because `delivery/validation` is a sibling slice.
//
// Both calls are idempotent and both degrade to "skipped" rather than failing:
// a project with no acceptance oracle simply has nothing to validate, and that
// is a legitimate verdict, not an error.
type ValidationCoordinator interface {
	// EnsureValidationIssue mints the run's validation issue into the milestone
	// at deployed-green and returns its number, or 0 when the project has no
	// validation criteria. Minting here rather than at plan time is what keeps
	// an unworkable issue out of the working set for the whole run.
	EnsureValidationIssue(ctx context.Context, orgID, projectID string, milestoneNumber int) (int, error)
	// Verdict reads the validation runner's committed report AT `at` — the
	// validation cycle's own merge commit — and returns one of the
	// delivery.ValidationVerdict* values, plus a DIGEST of the evidence it was
	// derived from.
	//
	// The commit is not optional in spirit: the report lives at one fixed path
	// that every run overwrites, so reading the branch tip returns the newest
	// run's results regardless of which run is asking. A run whose agent shipped
	// no report would inherit its predecessor's and be handed a confidently wrong
	// verdict. An empty `at` still reads the tip, for a caller that has no commit.
	//
	// The digest exists so the loop can tell a repeat attempt that learned something
	// from one that learned nothing. It covers the criteria and their outcomes ONLY,
	// never the raw file — the report embeds the commit it was generated at, so a
	// whole-file hash would differ on every attempt and could never match.
	Verdict(ctx context.Context, orgID, projectID, at string) (verdict, digest string, err error)

	// MintRepairIssues turns a `failed` attempt's report into ordinary work: one
	// issue per failed criterion, filed into the milestone, which the next cycle
	// picks out of the working set like any other. Returns the issue numbers.
	//
	// It reads the report at the same pinned commit the verdict came from. cycleID
	// is the attempt's identity and becomes the issues' dedupe key, so a retry
	// within one attempt files nothing new while the next attempt files fresh work.
	MintRepairIssues(ctx context.Context, orgID, projectID string, milestoneNumber int, at, cycleID string) ([]int, error)

	// CloseValidationIssue closes the version's validation task, leaving a comment
	// that names the verdict (or its absence).
	//
	// The PLATFORM owns this close. The validation pull request references its
	// issue with `Validates #N`, which is deliberately NOT one of GitHub's closing
	// keywords, so merging does not close it and this call is the only thing that
	// does. That single ownership is what lets the run close the task on endings
	// where no verdict was reached at all — an agent that died through its whole
	// re-dispatch budget — which is the loop's only termination guarantee: the
	// sweep starts a validation run because the task is OPEN.
	CloseValidationIssue(ctx context.Context, orgID, projectID string, issue int, verdict string) error
}

// Dispatcher launches one agent run over the milestone. It is the locally
// declared consumer view of the root delivery.MilestoneDispatcher port, which
// the coding agent satisfies — the supervisor names a capability, not a
// package.
type Dispatcher interface {
	Dispatch(ctx context.Context, req delivery.MilestoneDispatch) (jobRef string, err error)
}

// Deployer promotes a cycle's built components into the environment and reports
// what it did per component. projects.DeploymentService satisfies it.
//
// This is the port that took deploy away from OpenChoreo's AutoDeploy. The
// supervisor has to own the verb for one reason: a version is not delivered when
// its builds are green, it is delivered when its components are SERVING, and
// only a caller that performs the promote can know when to start waiting for it.
// While the controller promoted on its own, the loop's only honest option was to
// call a green WorkflowRun the end of the cycle and validate against whatever
// happened to be running.
//
// Idempotent and convergent: the release name is derived from the commit, so a
// retried deploy re-pins the same release and changes nothing.
//
// An EMPTY commitSHA on a target converges instead of promoting — it re-asserts
// the wiring of components that are already serving without moving which release
// is live. That is how the stage's last pass supplies the facts that only exist
// once everything is up (a protected API's CORS allowlist is the project's SPA
// origins) without cutting a second release for them.
type Deployer interface {
	// Deploy promotes each target at ITS OWN commit. Per target rather than per
	// call because a reconcile promotes each component at its own newest green
	// build, which is a different commit for each of them in exactly the case
	// that motivated the model (ADR-0026).
	Deploy(ctx context.Context, orgID, projectID string, targets []delivery.DeployTarget) ([]delivery.ComponentDeploy, error)
	// PlanDeploymentWaves plans one reconcile pass over the version's state:
	// which behind components may be promoted, in which order, which to wait for
	// and which are held on a provider that is not serving.
	//
	// Planning lives with the deployer because it is read off the same design the
	// writes are composed from — and it reads the WHOLE design's edges, which is
	// what makes a provider with no release an unsatisfied edge rather than an
	// assumed-satisfied one.
	PlanDeploymentWaves(ctx context.Context, orgID, projectID string, state delivery.VersionState) (delivery.DeployPlan, error)
}

// Gates authors the version's dependencies and mints its `provision` gate
// issues into the milestone. Satisfied by an app-root adapter over the
// provisioning service — `run` names no sibling, exactly as `build` reaches the
// same capability through its own GateResolver port.
//
// It runs BEFORE Planner, and the order is the contract: an open gate is a
// dispatch hold, so minting the gates first is what makes the dispatch
// predicate honest from the moment the first Task lands.
type Gates interface {
	ProvisionForBuild(ctx context.Context, orgID, projectID, tag string, milestoneNumber int, inputs []delivery.ProvisionInput) error
}

// Planner runs the version's planning turn, minting one prose issue per planned
// Task into the milestone. Satisfied by `*task.PlanService` at the composition
// root; declaring it here rather than importing keeps `task ⊥ run` intact, which
// TestTaskRunSplit enforces as an import ban in both directions.
//
// It BLOCKS for the length of an LLM turn. That is precisely why it belongs in
// the workflow: as an activity it is durable across a worker restart, retried on
// a blip and failed fast on an answer — none of which a detached goroutine had.
type Planner interface {
	PlanIntoMilestone(ctx context.Context, orgID, projectID string, milestoneNumber int) error
}

// DeployGate answers whether the project may deploy at all: every external
// dependency configured, every platform resource provisioned (ADR-0023).
//
// The two blockers come back SEPARATELY because the supervisor treats them as
// different kinds of fact. A platform resource that is still provisioning is the
// platform working, so the stage polls. An unconfigured external dependency is a
// human who has not supplied a credential, so the run parks in `waiting` and
// says whose turn it is. Collapsing them into one boolean would force the loop
// to guess which of those two it is looking at.
//
// Satisfied by the provisioning service's readiness method directly — not
// through its HTTP handler, which is a thin projection of the same call.
type DeployGate interface {
	DeploymentReadiness(ctx context.Context, orgID, projectID, env string) (unconfigured, provisioning []string, err error)
}

// DeploymentReader reads back what the cluster says about those deployments —
// the readiness poll. Separate from Deployer because they run at different
// cadences: the promote happens once per cycle, the read happens every
// deployPollInterval until the answer settles.
type DeploymentReader interface {
	DeploymentState(ctx context.Context, orgID, projectID string, components []string) ([]delivery.ComponentDeploy, error)
}

// WorkHalter marks the working-set issues a FAILED run could not finish, so the
// reconcile sweep does not restart them. Satisfied by the event plane, reached
// through this port for the same reason DeployIssueMinter is: the supervisor
// observes, the plane owns every issue write, its labels and its prose.
//
// It takes the RUN's kind because the working set is per species, and the halt
// must never reach outside the population this run was responsible for — a dev
// run halting a bug a concurrent task run is working would abandon live work.
//
// Returns the issues it marked. The supervisor logs them and nothing else: the
// halt is a fact about the milestone, not about the run row.
type WorkHalter interface {
	HaltUnfinishedWork(ctx context.Context, orgID, projectID string, milestoneNumber int,
		runKind, reason string) (halted []int, err error)
}

// WorkCanceller closes the issues a CANCELLED run had in flight, so the reconcile
// sweep does not restart the very run the user just abandoned. Satisfied by the
// event plane, reached through this port for the same reason WorkHalter is: the
// supervisor observes, the plane owns every issue write, its labels and its prose.
//
// It is the sibling of the halt and takes the same shape — the RUN's kind, because
// what a cancel abandons is per species: a build's cancel abandons the whole
// increment, a bug-fix run's abandons only the defects it was working, and a
// validation run's own consequence (closing the task it adopted) is already
// performed by the workflow that adopted it.
//
// Returns the issues it closed. The supervisor logs them and nothing else: the
// closes are a fact about the milestone, not about the run row.
type WorkCanceller interface {
	CloseCancelledWork(ctx context.Context, orgID, projectID string, milestoneNumber int,
		runKind string) (closed []int, err error)
}

// DeployIssueMinter files the fix work for components whose deployment did not
// come up. Satisfied by the event plane, reached through this port so the
// supervisor still writes no issue of its own.
//
// It is the one recovery issue the event plane cannot mint on its own initiative:
// every other one has a webhook behind it, and a ReleaseBinding that never
// becomes Ready delivers nothing. The supervisor observes it and asks; the plane
// still owns the write, the labels and the dedupe key.
type DeployIssueMinter interface {
	// The commit is per component, riding the target: a reconcile's failures can
	// be at different commits in one pass, and the dedupe key is (component,
	// commit) — so one commit for the whole list would file the next version's
	// failure against the previous version's key, or the reverse.
	MintDeployFixIssues(ctx context.Context, orgID, projectID string, milestoneNumber int,
		failed []delivery.DeployTarget, reasons map[string]string) ([]int, error)
}
