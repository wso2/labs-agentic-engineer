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

package build

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// The version ledger's whole vocabulary, pinned. A version's story is its run's
// story, and this is the one place the translation happens — so a run state
// added without a home here would silently read as `in_progress` forever.
func TestStatusFromRunState(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  string
		why   string
	}{
		{delivery.RunStateSucceeded, statusCompleted, "the increment was delivered"},
		{delivery.RunStateFailed, statusFailed, "the platform could not deliver it"},
		{delivery.RunStateCancelled, statusCancelled,
			"A PERSON STOPPED IT. Not a failure: folding the two told whoever pressed " +
				"the button that the platform had broken, and left the version header " +
				"contradicting the run row two lines below it."},
		{delivery.RunStateBlocked, statusFailed,
			"blocked is 'did not finish, and here is what a human must supply' — nobody chose it, " +
				"which is the whole difference from a cancel"},
		{delivery.RunStateWaiting, statusInProgress, "parked is still unfinished"},
		{delivery.RunStateRunning, statusInProgress, ""},
		{delivery.RunStatePlanning, statusInProgress, "the platform is filling the milestone"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			if got := statusFromRunState(tc.state); got != tc.want {
				t.Errorf("statusFromRunState(%q) = %q, want %q — %s", tc.state, got, tc.want, tc.why)
			}
		})
	}
}

// Every value this maps onto must be one the CONTRACT declares, or the ledger
// serves a status no generated client can represent — which is precisely how a
// cancelled version came to render as "Unknown" in the console before the enum
// gained its own value.
func TestStatusFromRunState_OnlyEmitsContractValues(t *testing.T) {
	for _, state := range []string{
		delivery.RunStateSucceeded, delivery.RunStateFailed, delivery.RunStateCancelled,
		delivery.RunStateBlocked, delivery.RunStateWaiting, delivery.RunStateRunning,
		delivery.RunStatePlanning, "a-state-nobody-has-invented-yet",
	} {
		got := gen.BuildSummaryStatus(statusFromRunState(state))
		if !got.Valid() {
			t.Errorf("run state %q maps to %q, which the contract's BuildSummary.status enum does not declare",
				state, got)
		}
	}
}
