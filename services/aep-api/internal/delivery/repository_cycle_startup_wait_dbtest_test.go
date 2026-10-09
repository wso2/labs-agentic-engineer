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

// The startup wait round-trips on an open cycle: the reason follows the
// latest note, the since stamp stays the attempt's first, and neither write
// moves updated_at (the grace of a row without dispatched_at runs from it).
func TestStartupWait_NoteKeepsTheFirstSinceAndLeavesUpdatedAt(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	cycles := delivery.NewRunCycleRepository(db, nil)
	ctx := context.Background()

	run := admitRun(t, runs, "orgsw1", "proj", 1, "v1")
	c := appendCycle(t, cycles, run, delivery.CycleKindValidation, "ca-sw1-2610061000")
	before, _ := cycles.GetByIDScoped(ctx, "orgsw1", c.ID)

	first := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	if err := cycles.NoteStartupWait(ctx, c.ID, "Unschedulable", first); err != nil {
		t.Fatalf("NoteStartupWait: %v", err)
	}
	if err := cycles.NoteStartupWait(ctx, c.ID, "ImagePullBackOff", first.Add(time.Minute)); err != nil {
		t.Fatalf("NoteStartupWait: %v", err)
	}
	got, _ := cycles.GetByIDScoped(ctx, "orgsw1", c.ID)
	if got.StartupWaitReason != "ImagePullBackOff" || got.StartupWaitSince == nil || !got.StartupWaitSince.Equal(first) {
		t.Fatalf("wait = %q since %v, want ImagePullBackOff since %v", got.StartupWaitReason, got.StartupWaitSince, first)
	}
	if !got.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("updated_at moved %v → %v", before.UpdatedAt, got.UpdatedAt)
	}

	if err := cycles.ClearStartupWait(ctx, c.ID); err != nil {
		t.Fatalf("ClearStartupWait: %v", err)
	}
	got, _ = cycles.GetByIDScoped(ctx, "orgsw1", c.ID)
	if got.StartupWaitReason != "" || got.StartupWaitSince != nil || !got.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("after clear: %q since %v, updated_at %v (was %v)", got.StartupWaitReason, got.StartupWaitSince, got.UpdatedAt, before.UpdatedAt)
	}
}

// A closed cycle takes no note (its wait is over), and a re-dispatch starts
// the new attempt with none.
func TestStartupWait_ClosedCycleTakesNoNoteAndRedispatchClears(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	cycles := delivery.NewRunCycleRepository(db, nil)
	ctx := context.Background()

	run := admitRun(t, runs, "orgsw2", "proj", 2, "v2")
	open := appendCycle(t, cycles, run, delivery.CycleKindCoding, "ca-sw2-2610061000")
	if err := cycles.NoteStartupWait(ctx, open.ID, "Unschedulable", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	row, err := cycles.NoteDispatch(ctx, open.ID, "ca-sw2-2610061010")
	if err != nil || row == nil {
		t.Fatalf("NoteDispatch = (%v, %v)", row, err)
	}
	if row.StartupWaitReason != "" || row.StartupWaitSince != nil {
		t.Fatalf("a re-dispatch must start with no wait: %q since %v", row.StartupWaitReason, row.StartupWaitSince)
	}

	if _, err := cycles.FinishAgentFailed(ctx, open.ID, "startup_failed:Unschedulable"); err != nil {
		t.Fatal(err)
	}
	if err := cycles.NoteStartupWait(ctx, open.ID, "Unschedulable", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	got, _ := cycles.GetByIDScoped(ctx, "orgsw2", open.ID)
	if got.StartupWaitReason != "" || got.StartupWaitSince != nil {
		t.Fatalf("a closed cycle took a note: %q since %v", got.StartupWaitReason, got.StartupWaitSince)
	}
}
