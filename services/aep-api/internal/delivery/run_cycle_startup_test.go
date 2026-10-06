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
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

func TestIsStartupFailure(t *testing.T) {
	t.Parallel()
	for reason, want := range map[string]bool{
		"startup_failed:Unschedulable: 0/1 nodes are available": true,
		"startup_failed:no_pod_scheduled":                       true,
		"timed_out":                                             false,
		"agent_failed:OOMKilled":                                false,
		delivery.CycleReasonCancelled:                           false,
		delivery.CycleReasonModelProviderLimit:                  false,
		"":                                                      false,
	} {
		if got := delivery.IsStartupFailure(reason); got != want {
			t.Errorf("IsStartupFailure(%q) = %v, want %v", reason, got, want)
		}
	}
}

// The grace runs from the attempt's dispatch; a row that predates
// dispatched_at falls back to its last write. The deadline the run view shows
// is that start plus the grace the watcher applies.
func TestRunCycle_StartupDeadline(t *testing.T) {
	t.Parallel()
	dispatched := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	updated := dispatched.Add(3 * time.Minute)

	c := delivery.RunCycle{DispatchedAt: &dispatched, UpdatedAt: updated}
	if got := c.StartupGraceStart(); !got.Equal(dispatched) {
		t.Fatalf("grace start = %v, want the dispatch %v", got, dispatched)
	}
	if got, want := c.StartupDeadline(), dispatched.Add(delivery.CycleStartupGrace); !got.Equal(want) {
		t.Fatalf("deadline = %v, want %v", got, want)
	}

	legacy := delivery.RunCycle{UpdatedAt: updated}
	if got := legacy.StartupGraceStart(); !got.Equal(updated) {
		t.Fatalf("legacy grace start = %v, want updated_at %v", got, updated)
	}
}

// The overview's build stage and the build ledger share this rule, so it is
// pinned once here: validation-phase reasons on any kind, and an agent that
// could not start only on a VALIDATION run — on a dev or task run that agent
// is the coding agent, and the build did fail.
func TestEndedInValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind, reason string
		want         bool
	}{
		{delivery.RunKindValidation, delivery.RunReasonValidationFailed, true},
		{delivery.RunKindValidation, delivery.RunReasonValidationUnreported, true},
		{delivery.RunKindValidation, delivery.RunReasonAgentStartFailed, true},
		{delivery.RunKindDev, delivery.RunReasonValidationFailed, true},
		{delivery.RunKindDev, delivery.RunReasonAgentStartFailed, false},
		{delivery.RunKindTask, delivery.RunReasonAgentStartFailed, false},
		{delivery.RunKindValidation, delivery.RunReasonRedispatchBudget, false},
		{delivery.RunKindValidation, "", false},
	}
	for _, c := range cases {
		if got := delivery.EndedInValidation(c.kind, c.reason); got != c.want {
			t.Errorf("EndedInValidation(%q, %q) = %v, want %v", c.kind, c.reason, got, c.want)
		}
	}
	// Every reason IsValidationTerminalReason names is covered on every kind.
	for _, r := range []string{delivery.RunReasonValidationFailed, delivery.RunReasonValidationUnreported} {
		if !delivery.IsValidationTerminalReason(r) || !delivery.EndedInValidation(delivery.RunKindDev, r) {
			t.Errorf("%q: the two rules disagree", r)
		}
	}
}
