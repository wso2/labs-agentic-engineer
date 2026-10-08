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
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

type adoptionIssues struct {
	*fakeIssues
	issue sourcecontrol.IssueInfo
	// missing answers the host's (nil, nil) for an issue it does not have.
	missing bool
}

func (f *adoptionIssues) GetIssue(context.Context, string, string, int) (*sourcecontrol.IssueInfo, error) {
	if f.missing {
		return nil, nil
	}
	return &f.issue, nil
}

func TestSREAdoptionArmsCodeIssueInOrdinaryMilestoneQueue(t *testing.T) {
	h := newHarness(t, aRun("deployed", 5, delivery.RunStateSucceeded))
	h.events.p.Issues = &adoptionIssues{fakeIssues: h.issues, issue: sourcecontrol.IssueInfo{
		Number: 31, State: "open", Labels: []string{"bug", "incident", "dedupe:sre-code-123"},
	}}
	require.NoError(t, h.events.AdoptIssue(context.Background(), testOrg, testProject, AdoptTarget{Number: 31}))
	require.Equal(t, []string{"31+aep"}, h.issues.labelled)
	require.Equal(t, []string{"31->5"}, h.issues.assigned)
	require.Len(t, h.sup.started, 1)
	require.Equal(t, delivery.RunKindTask, h.sup.started[0].Kind)
	require.Equal(t, 5, h.sup.started[0].MilestoneNumber)
}

// The refusals are the caller's to say: a hand-off from the console or the
// issue agent reads them as a 409 or a tool error, so none is a silent no-op.
func TestAdoptionRefusesWhatItWillNotAdopt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		state  string
		reason string
		labels []string
		want   error
	}{
		{"config", "open", "", []string{"bug", "incident", "dedupe:sre-config-123"}, delivery.ErrNotCodingWork},
		{"provision", "open", "", []string{"provision", "incident", "dedupe:sre-code-123"}, delivery.ErrNotCodingWork},
		{"validation", "open", "", []string{"aep", "validation"}, delivery.ErrNotCodingWork},
		{"planned work", "open", "", []string{"aep", "development"}, delivery.ErrNotCodingWork},
		{"not_planned incident", "closed", "not_planned", []string{"bug", "incident", "dedupe:sre-code-123"}, delivery.ErrIssueClosed},
		{"closed bug", "closed", "completed", []string{"bug"}, delivery.ErrIssueClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, aRun("deployed", 5, delivery.RunStateSucceeded))
			h.events.p.Issues = &adoptionIssues{fakeIssues: h.issues, issue: sourcecontrol.IssueInfo{
				Number: 31, State: tc.state, StateReason: tc.reason, Labels: tc.labels,
			}}
			for range 2 {
				err := h.events.AdoptIssue(context.Background(), testOrg, testProject, AdoptTarget{Number: 31})
				require.ErrorIs(t, err, tc.want)
			}
			require.Empty(t, h.sup.started)
			require.Empty(t, h.issues.assigned)
			require.Empty(t, h.issues.labelled)
			require.Empty(t, h.issues.reopened)
		})
	}
}

// A bare issue handed over from the console or by its agent is ARMED by the
// hand-off: the task run's working set reads only armed issues, so an unarmed
// one would join the milestone and never be worked.
func TestAdoptionArmsABareIssueHandedOver(t *testing.T) {
	h := newHarness(t, aRun("deployed", 5, delivery.RunStateSucceeded))
	h.events.p.Issues = &adoptionIssues{fakeIssues: h.issues, issue: sourcecontrol.IssueInfo{
		Number: 12, State: "open", Labels: []string{},
	}}
	require.NoError(t, h.events.AdoptIssue(context.Background(), testOrg, testProject, AdoptTarget{Number: 12}))
	require.Equal(t, []string{"12+aep"}, h.issues.labelled)
	require.Equal(t, []string{"12->5"}, h.issues.assigned)
	require.Len(t, h.sup.started, 1)
	require.Equal(t, delivery.RunKindTask, h.sup.started[0].Kind)
}

// A halted issue handed over again is a person deciding the work is worth
// another attempt: the halt is cleared, or the reconcile sweep would keep
// skipping the issue the hand-off just put in front of a run.
func TestAdoptionClearsAHalt(t *testing.T) {
	h := newHarness(t, aRun("deployed", 5, delivery.RunStateSucceeded))
	h.events.p.Issues = &adoptionIssues{fakeIssues: h.issues, issue: sourcecontrol.IssueInfo{
		Number: 12, State: "open", Labels: []string{"bug", delivery.LabelHalted},
	}}
	require.NoError(t, h.events.AdoptIssue(context.Background(), testOrg, testProject, AdoptTarget{Number: 12}))
	require.Equal(t, []string{"12+aep", "12-aep:halted"}, h.issues.labelled)
	require.Len(t, h.sup.started, 1)
}

// An issue the host does not have is said as such, so the route can answer 404.
func TestAdoptionOfAMissingIssueIsNotFound(t *testing.T) {
	h := newHarness(t, aRun("deployed", 5, delivery.RunStateSucceeded))
	h.events.p.Issues = &adoptionIssues{fakeIssues: h.issues, missing: true}
	err := h.events.AdoptIssue(context.Background(), testOrg, testProject, AdoptTarget{Number: 12})
	require.ErrorIs(t, err, sourcecontrol.ErrIssueNotFound)
	require.Empty(t, h.issues.labelled)
}
