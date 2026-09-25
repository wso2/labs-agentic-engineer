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

package sourcecontrol

import "testing"

func TestNoChangeUnverifiedAttention(t *testing.T) {
	thirdAttempt := "Original evidence\n\n## Recurrence 1\nFirst evidence\n\n## Recurrence 2\nSecond evidence"
	fourthAttempt := thirdAttempt + "\n\n## Recurrence 3\nThird evidence"
	fifthAttempt := fourthAttempt + "\n\n## Recurrence 4\nFourth evidence"
	for _, tc := range []struct {
		name, state, reason, body, attention string
		labels                               []string
		noChange, unverified                 bool
	}{
		{name: "terminal SRE verdict", state: "closed", reason: "not_planned", labels: []string{"incident"}, noChange: true, attention: "no_change_verdict"},
		{name: "ordinary rejection", state: "closed", reason: "not_planned", labels: []string{"bug"}},
		{name: "completed SRE", state: "closed", reason: "completed", labels: []string{"incident"}},
		{name: "reopened disarmed", state: "open", reason: "reopened", labels: []string{"incident"}, unverified: true, attention: "unverified_fix"},
		{name: "reopened armed", state: "open", reason: "reopened", labels: []string{"incident", "aep"}},
		{name: "new ledger", state: "open", labels: []string{"incident"}},
		{name: "ordinary reopened", state: "open", reason: "reopened", labels: []string{"bug"}},
		{name: "third attempt", state: "open", reason: "reopened", body: thirdAttempt, labels: []string{"incident", "aep"}},
		{name: "fourth attempt", state: "open", reason: "reopened", body: fourthAttempt, labels: []string{"incident", "aep"}, attention: "escalated"},
		{name: "fourth recurrence armed", state: "open", reason: "reopened", body: fifthAttempt, labels: []string{"incident", "aep"}, attention: "escalated"},
		{name: "fourth attempt disarmed", state: "open", reason: "reopened", body: fourthAttempt, labels: []string{"incident"}, unverified: true, attention: "escalated"},
		{name: "fifth attempt", state: "open", reason: "reopened", body: fifthAttempt, labels: []string{"incident"}, unverified: true, attention: "escalated"},
		{name: "completed fourth", state: "closed", reason: "completed", body: fourthAttempt, labels: []string{"incident"}},
		{name: "terminal overrides escalation", state: "closed", reason: "not_planned", body: fourthAttempt, labels: []string{"incident"}, noChange: true, attention: "no_change_verdict"},
		{name: "ordinary fourth", state: "open", reason: "reopened", body: fourthAttempt, labels: []string{"bug"}},
		{name: "heading mentioned in prose", state: "open", body: "Discuss ## Recurrence 4", labels: []string{"incident"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issue := IssueInfo{State: tc.state, StateReason: tc.reason, Body: tc.body, Labels: tc.labels}
			if got := IsNoChangeVerdict(issue); got != tc.noChange {
				t.Errorf("no change = %v, want %v", got, tc.noChange)
			}
			if got := IsUnverifiedFix(issue); got != tc.unverified {
				t.Errorf("unverified = %v, want %v", got, tc.unverified)
			}
			if got := AttentionReasonFor(issue); got != tc.attention {
				t.Errorf("attention = %q, want %q", got, tc.attention)
			}
		})
	}
}
