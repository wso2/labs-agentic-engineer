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

package delivery_test

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// TestIsTerminalRunState pins the settled/unsettled split every guarded run
// transition and the spec-run mutex are written against. The two must agree: a
// state the helper calls terminal is a state the mutex index must NOT count and
// no mutator may write through.
func TestIsTerminalRunState(t *testing.T) {
	t.Parallel()

	terminal := []string{
		delivery.RunStateSucceeded,
		delivery.RunStateFailed,
		delivery.RunStateCancelled,
		delivery.RunStateBlocked,
	}
	for _, s := range terminal {
		if !delivery.IsTerminalRunState(s) {
			t.Errorf("IsTerminalRunState(%q) = false, want true", s)
		}
	}

	nonTerminal := []string{
		delivery.RunStateWaiting,
		delivery.RunStateRunning,
		"", // an unset state is not settled
		"bogus",
	}
	for _, s := range nonTerminal {
		if delivery.IsTerminalRunState(s) {
			t.Errorf("IsTerminalRunState(%q) = true, want false", s)
		}
	}
}

// TestRunBudgetLimits pins the §7 budget numbers as constants so a change to
// any of them is a deliberate, reviewed edit — each one names exactly one
// failure class, which is what keeps the terminal reasons honest.
func TestRunBudgetLimits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  int
		want int
	}{
		{"re-dispatch per cycle", delivery.RunMaxRedispatchPerCycle, 2},
		{"build re-trigger per component per SHA", delivery.RunMaxBuildRetriggersPerComponentSHA, 1},
		{"fix cycles per run", delivery.RunMaxFixCycles, 2},
		{"conflict cycles per run", delivery.RunMaxConflictCycles, 2},
		{"default total-cycle ceiling", delivery.RunDefaultCycleCeiling, 8},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s budget = %d, want %d", c.name, c.got, c.want)
		}
	}
}
