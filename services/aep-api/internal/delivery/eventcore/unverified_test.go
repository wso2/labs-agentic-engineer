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

package eventcore

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

type unverifiedIssues struct {
	*adoptionIssues
	writes    []string
	removeErr error
}

func (f *unverifiedIssues) RemoveLabel(_ context.Context, _, _ string, _ int, label string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.issue.Labels = slices.DeleteFunc(f.issue.Labels, func(l string) bool { return l == label })
	f.writes = append(f.writes, "disarm")
	return nil
}

func (f *unverifiedIssues) ReopenIssue(context.Context, string, string, int) error {
	f.issue.State, f.issue.StateReason = "open", "reopened"
	f.writes = append(f.writes, "reopen")
	return nil
}

func TestUnverifiedSREFixStaysOpenAndDisarmedAfterMerge(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	issues := &unverifiedIssues{adoptionIssues: &adoptionIssues{fakeIssues: h.issues, issue: sourcecontrol.IssueInfo{
		Number: 12, State: "closed", StateReason: "completed", Labels: []string{"bug", "incident", "aep"},
	}}}
	h.events.p.Issues, h.events.p.Writer = issues, delivery.NewIssueWriter(issues)
	require.NoError(t, h.deliver(t, "pull_request", prBody("closed", "aep/m7-c1", "Resolves #12\nConfidence: low", 42, false, true, "merged")))
	require.Equal(t, []string{"disarm", "reopen"}, issues.writes)
	require.Equal(t, "unverified_fix", sourcecontrol.AttentionReasonFor(issues.issue))
	require.Equal(t, []int{12}, h.issues.commented)
}

func TestUnverifiedCleanupFailureDoesNotBlockMergeOutcome(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.prs.files = []string{"services/order/main.go"}
	issues := &unverifiedIssues{removeErr: errors.New("github unavailable"), adoptionIssues: &adoptionIssues{fakeIssues: h.issues, issue: sourcecontrol.IssueInfo{
		Number: 12, State: "closed", StateReason: "completed", Labels: []string{"bug", "incident", "aep"},
	}}}
	h.events.p.Issues, h.events.p.Writer = issues, delivery.NewIssueWriter(issues)

	err := h.deliver(t, "pull_request", prBody("closed", "aep/m7-c1", "Resolves #12\nConfidence: low", 42, false, true, "abc123def456789"))

	// The cleanup failure is reported so the delivery is retried, but the
	// merge's own outcome is not held hostage to it.
	require.ErrorContains(t, err, "github unavailable")
	require.Equal(t, []string{"cycle-1:abc123def456789"}, h.cycles.closed)
	require.Len(t, h.sup.named(delivery.SigRunPRMerged), 1)
	require.Len(t, h.builds.triggeredFor("order-service"), 1)
}

func TestHighConfidenceSREFixClosesNormally(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	issues := &unverifiedIssues{adoptionIssues: &adoptionIssues{fakeIssues: h.issues, issue: sourcecontrol.IssueInfo{
		Number: 12, State: "closed", StateReason: "completed", Labels: []string{"bug", "incident", "aep"},
	}}}
	h.events.p.Issues, h.events.p.Writer = issues, delivery.NewIssueWriter(issues)

	require.NoError(t, h.deliver(t, "pull_request", prBody("closed", "aep/m7-c1", "Resolves #12\nConfidence: high", 42, false, true, "merged")))

	require.Empty(t, issues.writes)
	require.Equal(t, "", sourcecontrol.AttentionReasonFor(issues.issue))
	require.Empty(t, h.issues.commented)
}

func TestSRENoWorkEventWakesCodingAttemptWithoutPullRequest(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.cycles.latest = aCycle("cycle-1", "run-1")
	h.issues.withCounts(7, 0, 0, 1)
	require.NoError(t, h.deliver(t, "issues", issueBodyWithLabels("unlabeled", 12, 7, "aep", []string{"bug", "incident"}, "coding-agent")))
	require.Len(t, h.sup.named(delivery.SigRunNoWork), 1)
}
