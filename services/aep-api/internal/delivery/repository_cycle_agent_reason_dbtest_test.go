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
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

func TestFinishAgentFailed_ClosesTheCycleWithItsReason(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	cycles := delivery.NewRunCycleRepository(db, nil)
	ctx := context.Background()

	run := admitRun(t, runs, "orgar", "proj", 7, "v7")
	cycle := appendCycle(t, cycles, run, delivery.CycleKindCoding, "ca-dead-2608061000")

	got, err := cycles.FinishAgentFailed(ctx, cycle.ID, "timed_out")
	if err != nil {
		t.Fatalf("FinishAgentFailed: %v", err)
	}
	if got == nil || got.EndedAt == nil || got.AgentReason != "timed_out" {
		t.Fatalf("unexpected cycle: %+v", got)
	}
}

// A cycle that already produced a pull request has landed side effects. The pod
// exiting afterwards says nothing new, and closing the cycle here would swallow
// the pull_request webhook that is the run's real completion.
func TestFinishAgentFailed_LeavesACycleThatOpenedAPullRequest(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	cycles := delivery.NewRunCycleRepository(db, nil)
	ctx := context.Background()

	run := admitRun(t, runs, "orgar2", "proj", 8, "v8")
	cycle := appendCycle(t, cycles, run, delivery.CycleKindCoding, "ca-pr-2608061000")
	if _, err := cycles.NotePullRequest(ctx, cycle.ID, delivery.CyclePullRequest{Number: 12, Branch: "aep/m8-x"}); err != nil {
		t.Fatalf("NotePullRequest: %v", err)
	}

	got, err := cycles.FinishAgentFailed(ctx, cycle.ID, "agent_failed")
	if err != nil {
		t.Fatalf("FinishAgentFailed: %v", err)
	}
	if got != nil {
		t.Fatalf("a cycle with a pull request must not be closed by the watcher, got %+v", got)
	}
}

func TestFinishAgentFailed_IsIdempotentOnAClosedCycle(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	cycles := delivery.NewRunCycleRepository(db, nil)
	ctx := context.Background()

	run := admitRun(t, runs, "orgar3", "proj", 9, "v9")
	cycle := appendCycle(t, cycles, run, delivery.CycleKindCoding, "ca-twice-2608061000")
	if _, err := cycles.FinishAgentFailed(ctx, cycle.ID, "timed_out"); err != nil {
		t.Fatalf("first FinishAgentFailed: %v", err)
	}

	got, err := cycles.FinishAgentFailed(ctx, cycle.ID, "agent_failed")
	if err != nil {
		t.Fatalf("second FinishAgentFailed: %v", err)
	}
	if got != nil {
		t.Fatalf("a closed cycle must not be re-closed, got %+v", got)
	}
}
