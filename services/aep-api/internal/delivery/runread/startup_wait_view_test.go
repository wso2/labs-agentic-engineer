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
// the reason, since when, and when the attempt fails (dispatch + grace) — no
// cluster read.
func TestCycleView_StartupWaitFromTheRow(t *testing.T) {
	t.Parallel()
	dispatched := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	since := dispatched.Add(30 * time.Second)
	row := &delivery.RunCycle{
		ID: "c1", Kind: delivery.CycleKindValidation, Attempts: 1,
		DispatchedAt: &dispatched, UpdatedAt: dispatched,
		StartupWaitReason: "Unschedulable", StartupWaitSince: &since,
	}

	got := runread.CycleView(row, gen.RunCycleViewRecordingLive).StartupWait
	want := gen.RunCycleStartupWait{Reason: "Unschedulable", Since: since, FailsAt: dispatched.Add(delivery.CycleStartupGrace)}
	if got == nil || *got != want {
		t.Fatalf("startupWait = %+v, want %+v", got, want)
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
