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

import "context"

// MilestoneDispatch is one cycle's agent launch: work the open issues in a
// milestone. It is the WHOLE instruction — there is no task list, no ordering
// and no branch name in it, because the runner derives all three itself (it
// re-lists the milestone before picking each issue, so an issue adopted
// mid-cycle joins the same cycle, and it owns its own `aep/m<n>-c<k>` branch
// identity so a crash can resume one).
//
// It lives at the domain ROOT because the two ends may not name each other:
// the supervisor in `delivery/run` asks for it, and the coding agent in
// `delivery/codingagent` performs it, and they are peer sub-packages.
type MilestoneDispatch struct {
	OrgID     string `json:"orgId"`
	ProjectID string `json:"projectId"`

	// MilestoneNumber is the platform key of the milestone to work;
	// MilestoneTitle is what the runner's `gh issue list --milestone "<title>"`
	// discovery call needs (there is no `gh milestone` command group, and the
	// number is not accepted there).
	MilestoneNumber int    `json:"milestoneNumber"`
	MilestoneTitle  string `json:"milestoneTitle"`

	// Kind is the RunCycle kind this dispatch serves (CycleKind*). It selects
	// the runner's skill and prompt shape: every kind but validation is the
	// ordinary milestone loop, and validation swaps in the `aep-validation`
	// skill anchored to IssueNumber.
	Kind string `json:"kind"`

	// IssueNumber anchors a dispatch to ONE issue instead of the milestone's
	// working set. It is set for the validation cycle and zero otherwise —
	// recovery cycles (fix, conflict) are deliberately not anchored, because a
	// fix or conflict issue is ordinary work that joins the working set like
	// any other.
	IssueNumber int `json:"issueNumber,omitempty"`

	// RunID and CycleID are the platform's own correlation keys, carried so the
	// launched Job can be tied back to the cycle record that dispatched it.
	RunID   string `json:"runId"`
	CycleID string `json:"cycleId"`
}

// MilestoneDispatcher launches ONE agent run over a milestone and returns a
// reference to the launched Job (the cycle record's JobRef).
//
// It is the root port that keeps the supervisor and the coding agent peer
// sub-packages, exactly as BuildTerminalObserver does for the watcher: the
// implementation lives with the executor that owns image selection, secret
// staging and the dispatch shape, and the supervisor names only this.
//
// Returning promptly is part of the contract: the call launches work, it does
// not wait for it. Everything after the launch reaches the supervisor as a
// webhook-derived signal.
type MilestoneDispatcher interface {
	Dispatch(ctx context.Context, req MilestoneDispatch) (jobRef string, err error)
}
