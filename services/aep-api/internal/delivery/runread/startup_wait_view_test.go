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

package runread_test

import (
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/runread"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// An open cycle whose pod is stuck carries startupWait straight from the row:
// the reason, since when, and when the attempt fails (the start clock plus the
// grace) — no cluster read.
func TestCycleView_StartupWaitFromTheRow(t *testing.T) {
	t.Parallel()
	dispatched := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	since := dispatched.Add(30 * time.Second)
	row := &delivery.RunCycle{
		ID: "c1", Kind: delivery.CycleKindValidation, Attempts: 1,
		DispatchedAt: &dispatched, UpdatedAt: dispatched, StartupClockAt: &dispatched,
		StartupWaitReason: "Unschedulable", StartupWaitSince: &since,
	}

	got := runread.CycleView(row, gen.RunCycleViewRecordingLive).StartupWait
	want := gen.RunCycleStartupWait{Reason: "Unschedulable", Since: since, FailsAt: dispatched.Add(delivery.CycleStartupGrace)}
	if got == nil || *got != want {
		t.Fatalf("startupWait = %+v, want %+v", got, want)
	}
}

// Before OpenChoreo has applied the Job the wait is NotYetApplied and the
// attempt fails at the apply cap from its dispatch; once the watcher has seen
// the Job (startup_clock_at), failsAt moves to the grace from that clock.
func TestCycleView_FailsAtMovesFromTheApplyCapToTheGrace(t *testing.T) {
	t.Parallel()
	dispatched := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	since := dispatched.Add(30 * time.Second)
	row := &delivery.RunCycle{
		ID: "c1", Kind: delivery.CycleKindCoding, Attempts: 1,
		DispatchedAt: &dispatched, UpdatedAt: dispatched,
		StartupWaitReason: "NotYetApplied", StartupWaitSince: &since,
	}
	got := runread.CycleView(row, gen.RunCycleViewRecordingLive).StartupWait
	if got == nil || got.Reason != "NotYetApplied" || !got.FailsAt.Equal(dispatched.Add(30*time.Minute)) {
		t.Fatalf("not applied: startupWait = %+v, want NotYetApplied failing at dispatch + 30m", got)
	}

	clock := dispatched.Add(12 * time.Minute)
	stuck := clock.Add(time.Minute)
	row.StartupClockAt = &clock
	row.StartupWaitReason, row.StartupWaitSince = "Unschedulable", &stuck
	got = runread.CycleView(row, gen.RunCycleViewRecordingLive).StartupWait
	if got == nil || !got.FailsAt.Equal(clock.Add(delivery.CycleStartupGrace)) {
		t.Fatalf("applied: startupWait = %+v, want failsAt = clock + grace", got)
	}
}

// Nothing to show: no wait recorded, or the cycle has ended (an agent that
// never started is told by agentReason then, not by a wait).
func TestCycleView_NoStartupWaitWhenNoneOrEnded(t *testing.T) {
	t.Parallel()
	dispatched := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	ended := dispatched.Add(10 * time.Minute)

	none := &delivery.RunCycle{ID: "c1", Kind: delivery.CycleKindCoding, DispatchedAt: &dispatched}
	if got := runread.CycleView(none, gen.RunCycleViewRecordingLive).StartupWait; got != nil {
		t.Errorf("no wait recorded: startupWait = %+v", got)
	}
	closed := &delivery.RunCycle{
		ID: "c2", Kind: delivery.CycleKindCoding, DispatchedAt: &dispatched, EndedAt: &ended,
		StartupWaitReason: "Unschedulable", StartupWaitSince: &dispatched,
		AgentReason: "startup_failed:Unschedulable",
	}
	if got := runread.CycleView(closed, gen.RunCycleViewRecordingKept).StartupWait; got != nil {
		t.Errorf("ended cycle: startupWait = %+v", got)
	}
}
