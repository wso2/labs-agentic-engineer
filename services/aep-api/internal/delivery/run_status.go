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

package delivery

import "fmt"

// How a milestone run is ADDRESSED, and what it answers when asked. It lives at
// the root because starting, signalling and querying a run is a contract
// between the supervisor and everything that reaches it, not a private detail
// of the supervisor.

// The registered workflow types. THREE of them, one per run kind: a version is
// delivered, a defect is worked, and a shipped version is judged. They share one
// task queue, one worker and one Activities struct — what differs is the
// bookends, so the split is three registrations rather than three packages.
const (
	// DevRunWorkflowName delivers a version: fill the milestone, work it to
	// deployed-green, mint the validation task, settle.
	DevRunWorkflowName = "DevRunWorkflow"
	// ValidationRunWorkflowName judges a deployed version against its acceptance
	// criteria. It has no working set and builds nothing.
	ValidationRunWorkflowName = "ValidationRunWorkflow"
	// TaskRunWorkflowName works a defect inside a version somebody already
	// delivered.
	TaskRunWorkflowName = "TaskRunWorkflow"
)

// RunWorkflowName is the registered workflow type a run of this kind executes.
// An unknown kind yields "" — nothing may silently become a dev run.
func RunWorkflowName(kind string) string {
	switch kind {
	case RunKindDev:
		return DevRunWorkflowName
	case RunKindValidation:
		return ValidationRunWorkflowName
	case RunKindTask:
		return TaskRunWorkflowName
	default:
		return ""
	}
}

// QueryRunStatus is the query name a live run answers with RunStatus.
const QueryRunStatus = "run-status"

// MilestoneRunWorkflowID is a run's Temporal workflow id: its KIND, then the
// milestone it works.
//
// The milestone is the key — "work milestone M" — and a milestone sees
// SEQUENTIAL runs of one kind across its life, which is why the id is reused
// after a terminal run rather than made unique per run.
//
// The KIND PREFIX is not cosmetic. Ids are reused under
// WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE, so a single grammar would let three
// executions on one milestone claim the same id in turn: a stale signal aimed at
// a settled dev run would land on the validation run that claimed the id
// afterwards, and the validation run would act on a merge that was never its
// own. The run ROW is the routing table — the event plane resolves a row before
// signalling anything, and the row's kind gives the prefix.
//
// An empty kind is read as dev, which is the migration-safe reading: it is the
// only kind a row could have carried before the column existed (see
// RunKindForOrigin), and it is the kind whose id a pre-split execution already
// answers to.
func MilestoneRunWorkflowID(kind, orgID, projectID string, milestoneNumber int) string {
	if kind == "" {
		kind = RunKindDev
	}
	return fmt.Sprintf("%s-%s-%s-%d", kind, orgID, projectID, milestoneNumber)
}

// RunStatus is a live run's self-report — the loop position no database column
// holds. The run row carries the durable facts (state, reason, budgets,
// verdict); this carries WHERE IN THE LOOP the run is right now, which is
// deliberately not persisted because fix and conflict cycles re-enter earlier
// phases and a stored phase enum would lie mid-loop.
type RunStatus struct {
	RunID           string `json:"runId"`
	MilestoneNumber int    `json:"milestoneNumber"`
	// Kind is what the run does (dev | task | validation) — the thing every
	// branch below was taken on. Origin is where it came from, carried so a live
	// status reads the same as the row.
	Kind   string `json:"kind"`
	Origin string `json:"origin"`

	// State is one of the RunState* values; TerminalReason is a RunReason* and
	// is empty until the run settles into a non-success terminal state.
	State          string `json:"state"`
	TerminalReason string `json:"terminalReason,omitempty"`

	// Phase names what the run is doing inside a cycle (RunPhase*). It is empty
	// while the run is waiting.
	Phase string `json:"phase,omitempty"`

	// WaitingReason names WHY a `waiting` run is waiting (a RunWaitingOn* value),
	// and BlockingDependencies names what it is waiting on. Both empty for the
	// ordinary between-cycles park, which needs no explanation.
	//
	// The deploy gate's park does. It is unbounded, only cancellation exits it,
	// and the thing that ends it is a person typing a credential — so a run that
	// does not say "I am waiting for YOU, about THESE" reads as a hang, and the
	// developer who could clear it in ten seconds never learns that they can.
	WaitingReason        string   `json:"waitingReason,omitempty"`
	BlockingDependencies []string `json:"blockingDependencies,omitempty"`

	// CycleKind / CycleAttempt / CyclePR describe the cycle in flight.
	CycleKind    string `json:"cycleKind,omitempty"`
	CycleAttempt int    `json:"cycleAttempt,omitempty"`
	CyclePR      int    `json:"cyclePr,omitempty"`

	// Budget counters as the workflow itself counts them — the authority the
	// run row's columns mirror.
	CyclesTotal    int `json:"cyclesTotal"`
	CycleCeiling   int `json:"cycleCeiling"`
	FixCycles      int `json:"fixCycles"`
	ConflictCycles int `json:"conflictCycles"`

	// ValidationIssue is the version's validation task, as this run knows it: a
	// DEV run learns it at deployed-green when it files it, and a VALIDATION run
	// when it adopts it. 0 on a task run, and on a project with no acceptance
	// oracle. ValidationVerdict is the property the deployment surface reads, and
	// only a validation run ever sets it — an empty one on a settled dev run means
	// "delivered, not yet judged".
	ValidationIssue   int    `json:"validationIssue,omitempty"`
	ValidationVerdict string `json:"validationVerdict,omitempty"`

	// Version is the last VersionState the run read: every component in the
	// design, with what it should be serving and what it is (ADR-0026).
	//
	// Live status rather than a column, because it is a fact about the WORLD and
	// not about the run — the next read may differ, and the run row must not
	// carry a stale copy of something a reader can ask the cluster for. What
	// outlives the run is the terminal reason `version-incomplete`, plus the
	// settle log naming the components that were not serving.
	Version VersionState `json:"version,omitzero"`
}

// Run phases — where a cycle is. They are a read-model label only: no platform
// decision branches on them, which is what lets the loop re-enter earlier
// phases without inventing a state machine.
const (
	// RunPhasePlanning is the run FILLING its milestone: minting the version's
	// dependency gates, then planning its Tasks into it. It runs once, before the
	// first cycle boundary, and only for a run that owns a version — every other
	// origin adopts a milestone somebody already filled.
	//
	// It is a phase rather than a pre-run step because planning is the longest
	// and most failure-prone thing a version does, and the click had nowhere
	// durable to put it. As the workflow's own first phase it survives a worker
	// restart and retries a blip, where a detached goroutine could do neither.
	RunPhasePlanning = "planning"
	// RunPhaseWaiting is the unbounded wait between cycles, and the state a
	// dispatch-holding gate parks the run in.
	RunPhaseWaiting = "waiting"
	// RunPhaseCoding is dispatched, waiting for the agent's pull request to
	// land.
	RunPhaseCoding = "coding"
	// RunPhaseBuilding is merged, waiting for the fan-out's BUILDS to settle.
	// Builds only — what happens to the images afterwards is the next phase.
	RunPhaseBuilding = "building"
	// RunPhaseDeploying is built, waiting for the components this cycle touched
	// to actually SERVE: the release is cut, the binding is pinned and wired, and
	// the loop is watching each binding's Ready condition.
	//
	// The phase exists because the platform performs the deploy. While
	// OpenChoreo's AutoDeploy promoted releases on its own, a cycle could not
	// honestly claim a deployment — the chain was kicked off from inside the
	// build and reconciled afterwards with no link back to the cycle that caused
	// it, so a green WorkflowRun meant the deployment had been ASKED for. Now the
	// run pins the release itself, which is what makes "deployed" a fact this
	// loop owns and can wait on.
	RunPhaseDeploying = "deploying"
	// RunPhaseValidating is the validation cycle.
	RunPhaseValidating = "validating"
	// RunPhaseSettling is the terminal bookkeeping: the milestone close and the
	// run row's final write.
	RunPhaseSettling = "settling"
)

// RunWaitingOn* values explain a `waiting` run. Only the deploy gate sets one:
// every other park is between cycles and needs no explanation.
const (
	// RunWaitingOnExternalValues is the deploy gate's park (ADR-0023): the run is
	// built and ready to deploy, and every remaining blocker is a value only a
	// human can supply. The one waiting reason the console renders as a call to
	// action rather than as a status.
	RunWaitingOnExternalValues = "external-values"
)
