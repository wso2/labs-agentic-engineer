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
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// The start clock is written once per attempt: the first write lands, a later
// one keeps it, and neither moves updated_at. A re-dispatch clears it for the
// new attempt.
func TestStartupClock_WrittenOncePerAttemptAndClearedByRedispatch(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	cycles := delivery.NewRunCycleRepository(db, nil)
	ctx := context.Background()

	run := admitRun(t, runs, "orgsc1", "proj", 1, "v1")
	c := appendCycle(t, cycles, run, delivery.CycleKindCoding, "ca-sc1-2610081000")
	row, err := cycles.NoteDispatch(ctx, c.ID, "ca-sc1-2610081000")
	if err != nil || row == nil {
		t.Fatalf("NoteDispatch = (%v, %v)", row, err)
	}
	before, _ := cycles.GetByIDScoped(ctx, "orgsc1", c.ID)

	first := time.Now().UTC().Truncate(time.Microsecond)
	landed, err := cycles.NoteStartupClock(ctx, c.ID, row.Attempts, first)
	if err != nil || !landed {
		t.Fatalf("first NoteStartupClock = (%v, %v), want it to land", landed, err)
	}
	landed, err = cycles.NoteStartupClock(ctx, c.ID, row.Attempts, first.Add(time.Minute))
	if err != nil || landed {
		t.Fatalf("second NoteStartupClock = (%v, %v), want no write", landed, err)
	}
	got, _ := cycles.GetByIDScoped(ctx, "orgsc1", c.ID)
	if got.StartupClockAt == nil || !got.StartupClockAt.Equal(first) {
		t.Fatalf("startup_clock_at = %v, want the first write %v", got.StartupClockAt, first)
	}
	if !got.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("updated_at moved %v → %v", before.UpdatedAt, got.UpdatedAt)
	}

	row, err = cycles.NoteDispatch(ctx, c.ID, "ca-sc1-2610081010")
	if err != nil || row == nil {
		t.Fatalf("re-dispatch = (%v, %v)", row, err)
	}
	if row.StartupClockAt != nil {
		t.Fatalf("a re-dispatch must start the attempt with no clock: %v", row.StartupClockAt)
	}
}

// The cross-replica race: a watcher tick that read attempt N writes its clock
// after another replica re-dispatched the cycle to N+1. The write is fenced on
// the attempt, so it changes nothing; a closed cycle takes no clock either.
func TestStartupClock_StaleAttemptAndClosedCycleWriteNothing(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	cycles := delivery.NewRunCycleRepository(db, nil)
	ctx := context.Background()

	run := admitRun(t, runs, "orgsc2", "proj", 2, "v2")
	c := appendCycle(t, cycles, run, delivery.CycleKindCoding, "ca-sc2-2610081000")
	attemptN, err := cycles.NoteDispatch(ctx, c.ID, "ca-sc2-2610081000")
	if err != nil || attemptN == nil {
		t.Fatalf("NoteDispatch = (%v, %v)", attemptN, err)
	}
	if _, err := cycles.NoteDispatch(ctx, c.ID, "ca-sc2-2610081010"); err != nil {
		t.Fatal(err)
	}

	landed, err := cycles.NoteStartupClock(ctx, c.ID, attemptN.Attempts, time.Now().UTC())
	if err != nil || landed {
		t.Fatalf("stale attempt's NoteStartupClock = (%v, %v), want no write", landed, err)
	}
	got, _ := cycles.GetByIDScoped(ctx, "orgsc2", c.ID)
	if got.StartupClockAt != nil {
		t.Fatalf("a stale attempt wrote the clock: %v", got.StartupClockAt)
	}

	if _, err := cycles.FinishAgentFailed(ctx, c.ID, "startup_failed:not_applied"); err != nil {
		t.Fatal(err)
	}
	landed, err = cycles.NoteStartupClock(ctx, c.ID, got.Attempts, time.Now().UTC())
	if err != nil || landed {
		t.Fatalf("closed cycle's NoteStartupClock = (%v, %v), want no write", landed, err)
	}
}
