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

// The milestone-run signal vocabulary: what the EVENT PLANE tells the RUN
// SUPERVISOR. It is deliberately a separate, small set from the task-keyed
// signals above (signals.go), which belong to the tag-keyed workflows.
//
// The direction is one-way and total: the event plane detects, mints and
// signals; the supervisor decides. Every name below is therefore a statement
// of FACT about the world ("the PR merged", "this component's build is red"),
// never an instruction ("re-dispatch"). A supervisor that misses one recovers
// from ground truth — the reconcile sweep and the cycle-boundary poll exist
// precisely so a lost signal costs latency, not correctness.
const (
	// SigRunWorkable — the dispatch predicate turned true for a waiting run
	// (its last gate closed, or an issue joined the milestone).
	SigRunWorkable = "run-workable"
	// SigRunPRMerged — the cycle's pull request squash-merged. Carries the
	// merge SHA the build fan-out was pinned to.
	SigRunPRMerged = "run-pr-merged"
	// SigRunBuildTerminal — one component's build reached a terminal state at a
	// merge SHA. Emitted once per component, after the automatic re-trigger
	// budget for that (component, SHA) is spent.
	SigRunBuildTerminal = "run-build-terminal"
	// SigRunConflict — the cycle's pull request could not be merged and a
	// conflict issue naming it was minted into the milestone.
	SigRunConflict = "run-conflict"
	// SigRunCancel — a human abandoned the increment. It is the ONLY expiry the
	// unbounded wait state has, and the only signal in this set that is an
	// instruction rather than a fact: everything else the supervisor re-derives
	// from ground truth, but "a person changed their mind" has no ground truth
	// to poll.
	//
	// Cancel is delivered as a SIGNAL rather than a Temporal workflow
	// cancellation so the run settles its own row and closes its own cycle on
	// the ordinary code path, with a live context — a cancelled Temporal context
	// cannot run the activities that record the outcome. Stopping the agent pod
	// is the HTTP cancel surface's job (runread.CycleReaper → DeleteComponent),
	// best-effort and immediate — not a Temporal activity.
	SigRunCancel = "run-cancel"
)

// RunSignal is the payload of every milestone-run signal. One struct rather
// than four keeps the supervisor's signal handling uniform; fields are
// optional per signal and MUST only ever be added (a live workflow decodes
// payloads written by an older binary).
type RunSignal struct {
	// Signal is the name this payload was sent under, so a supervisor that
	// multiplexes one channel can branch without a parallel key.
	Signal string `json:"signal"`
	// MilestoneNumber identifies the run's milestone — the platform key.
	MilestoneNumber int `json:"milestoneNumber"`
	// PRNumber / Branch / MergeSHA describe the cycle's pull request, learned
	// from webhooks (the platform never dictates branch identity).
	PRNumber int    `json:"prNumber,omitempty"`
	Branch   string `json:"branch,omitempty"`
	MergeSHA string `json:"mergeSha,omitempty"`
	// Component and Succeeded describe a build terminal.
	Component string `json:"component,omitempty"`
	Succeeded bool   `json:"succeeded,omitempty"`
	// IssueNumber is the issue the event minted or turned on (fix issue,
	// conflict issue).
	IssueNumber int `json:"issueNumber,omitempty"`
	// Message is human-facing detail (a workflow failure reason, a merge
	// refusal). Never parsed.
	Message string `json:"message,omitempty"`
}
