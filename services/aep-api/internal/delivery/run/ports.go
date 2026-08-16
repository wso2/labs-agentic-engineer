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
type RunStore interface {
	// TryAdmit inserts the run row unless the spec-run mutex refuses it. Used
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
	// SetState moves the run between waiting and running.
	SetState(ctx context.Context, id, state string) error
	// Settle writes the terminal state and its reason, once.
	Settle(ctx context.Context, id, state, reason string) error
	// BumpBudget increments one counter — bookkeeping for the read model, never
	// the loop's own arithmetic.
	BumpBudget(ctx context.Context, id string, counter delivery.RunBudget) error
	// SetValidationVerdict records the validation cycle's outcome on the run,
	// together with the validation issue that produced it so a settled run stays
	// navigable to its criteria. An issue of 0 leaves the stored one untouched.
	SetValidationVerdict(ctx context.Context, id, verdict string, issue int) error
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
	// row, so a run that validated more than once keeps every attempt's answer
	// rather than only the last. Written after Finish — the verdict comes from the
	// report at the cycle's merge commit, which does not exist until it has one.
	SetValidationVerdict(ctx context.Context, cycleID, verdict string, issue int) error
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

// BuildRunInfo is one OpenChoreo WorkflowRun reduced to the two facts the
// supervisor needs. The OpenChoreo status vocabulary is deliberately mapped in
// the adapter: this package knows "terminal" and "succeeded", nothing about
// condition reasons.
type BuildRunInfo struct {
	Name      string `json:"name"`
	Terminal  bool   `json:"terminal"`
	Succeeded bool   `json:"succeeded"`
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
	// acceptance criteria. Minting here rather than at plan time is what keeps
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
// Idempotent and convergent: the release name is derived from the merge commit,
// so a retried deploy re-pins the same release and changes nothing.
//
// An EMPTY commitSHA converges instead of promoting — it re-asserts the wiring
// of components that are already serving without moving which release is live.
// That is how the stage's last pass supplies the facts that only exist once
// everything is up (a protected API's CORS allowlist is the project's SPA
// origins) without cutting a second release for them.
type Deployer interface {
	Deploy(ctx context.Context, orgID, projectID string, components []string, commitSHA string) ([]delivery.ComponentDeploy, error)
	// PlanDeploymentWaves orders the set: every component in a wave has each of
	// its hard providers in an earlier one. Ordering lives with the deployer
	// because it is read off the same design the writes are composed from.
	PlanDeploymentWaves(ctx context.Context, orgID, projectID string, components []string) ([][]string, error)
}

// Gates authors the version's dependencies and mints its `aep:provision` gate
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

// DeploymentReader reads back what the cluster says about those deployments —
// the readiness poll. Separate from Deployer because they run at different
// cadences: the promote happens once per cycle, the read happens every
// deployPollInterval until the answer settles.
type DeploymentReader interface {
	DeploymentState(ctx context.Context, orgID, projectID string, components []string) ([]delivery.ComponentDeploy, error)
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
	MintDeployFixIssues(ctx context.Context, orgID, projectID string, milestoneNumber int,
		components []string, reasons map[string]string, commitSHA string) ([]int, error)
}
