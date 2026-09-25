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
}

func (f *adoptionIssues) GetIssue(context.Context, string, string, int) (*sourcecontrol.IssueInfo, error) {
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

func TestSREAdoptionGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		state  string
		reason string
		labels []string
	}{
		{"config", "open", "", []string{"bug", "incident", "dedupe:sre-config-123"}},
		{"provision", "open", "", []string{"provision", "incident", "dedupe:sre-code-123"}},
		{"not_planned", "closed", "not_planned", []string{"bug", "incident", "dedupe:sre-code-123"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, aRun("deployed", 5, delivery.RunStateSucceeded))
			h.events.p.Issues = &adoptionIssues{fakeIssues: h.issues, issue: sourcecontrol.IssueInfo{
				Number: 31, State: tc.state, StateReason: tc.reason, Labels: tc.labels,
			}}
			for range 2 {
				require.NoError(t, h.events.AdoptIssue(context.Background(), testOrg, testProject, AdoptTarget{Number: 31}))
			}
			require.Empty(t, h.sup.started)
			require.Empty(t, h.issues.assigned)
			require.Empty(t, h.issues.labelled)
			require.Empty(t, h.issues.reopened)
		})
	}
}
