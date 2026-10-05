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

package migrate_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

func settleCycle(t *testing.T, cycles delivery.RunCycleRepository, runs delivery.MilestoneRunRepository, n int, jobRef string) *delivery.RunCycle {
	t.Helper()
	ctx := context.Background()
	ok, run, err := runs.TryAdmit(ctx, &delivery.MilestoneRun{
		OrgID: "acme", ProjectID: "shop", MilestoneNumber: n, MilestoneTitle: "v",
		Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild,
	})
	if err != nil || !ok {
		t.Fatalf("TryAdmit: %v %v", ok, err)
	}
	c := &delivery.RunCycle{OrgID: run.OrgID, ProjectID: run.ProjectID, RunID: run.ID, Kind: delivery.CycleKindCoding}
	if err := cycles.Append(ctx, c); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := cycles.NoteDispatch(ctx, c.ID, jobRef); err != nil {
		t.Fatalf("NoteDispatch: %v", err)
	}
	return c
}

func getCycle(t *testing.T, cycles delivery.RunCycleRepository, id string) *delivery.RunCycle {
	t.Helper()
	row, err := cycles.GetByIDScoped(context.Background(), "acme", id)
	if err != nil || row == nil {
		t.Fatalf("Get(%s) = (%v, %v)", id, row, err)
	}
	return row
}

func TestRunCycle_SettleColumnsRoundTrip(t *testing.T) {
	db := dbtest.New(t)
	bootMigrate(t, db)
	ctx := context.Background()
	cycles := delivery.NewRunCycleRepository(db, nil)
	runs := delivery.NewMilestoneRunRepository(db)
	c := settleCycle(t, cycles, runs, 1, "ca-c1")

	if _, err := cycles.NoteLaunch(ctx, c.ID, "api.anthropic.com", "development", "uid-1"); err != nil {
		t.Fatal(err)
	}
	if err := cycles.MarkJobSuspended(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	first := getCycle(t, cycles, c.ID).JobSuspendedAt
	if first == nil {
		t.Fatal("job_suspended_at not set")
	}
	time.Sleep(10 * time.Millisecond)
	if err := cycles.MarkJobSuspended(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if got := getCycle(t, cycles, c.ID); got.ComponentUID != "uid-1" || !got.JobSuspendedAt.Equal(*first) {
		t.Fatalf("uid %q, suspended %v → %v (must be write-once)", got.ComponentUID, first, got.JobSuspendedAt)
	}

	// pod_gone_at: write-once, clearable, then settable again.
	t1 := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	if err := cycles.NotePodGone(ctx, c.ID, t1); err != nil {
		t.Fatal(err)
	}
	if err := cycles.NotePodGone(ctx, c.ID, t1.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := getCycle(t, cycles, c.ID).PodGoneAt; got == nil || !got.Equal(t1) {
		t.Fatalf("pod_gone_at = %v, want %v (write-once)", got, t1)
	}
	if err := cycles.ClearPodGone(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if got := getCycle(t, cycles, c.ID).PodGoneAt; got != nil {
		t.Fatalf("pod_gone_at = %v after clear", got)
	}

	if rows, _ := cycles.ListSettling(ctx, 10); len(rows) != 0 {
		t.Fatal("an open cycle is not settling")
	}
	fin, err := cycles.FinishCancelled(ctx, c.ID)
	if err != nil || fin == nil {
		t.Fatalf("FinishCancelled = (%v, %v)", fin, err)
	}
	if again, err := cycles.FinishCancelled(ctx, c.ID); err != nil || again != nil {
		t.Fatalf("second FinishCancelled = (%v, %v), want (nil, nil)", again, err)
	}
	rows, err := cycles.ListSettling(ctx, 10)
	if err != nil || len(rows) != 1 || rows[0].AgentReason != delivery.CycleReasonCancelled {
		t.Fatalf("settling = %+v, %v", rows, err)
	}
	if err := cycles.MarkComponentDeleted(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	firstDel := getCycle(t, cycles, c.ID).ComponentDeletedAt
	time.Sleep(10 * time.Millisecond)
	_ = cycles.MarkComponentDeleted(ctx, c.ID)
	if got := getCycle(t, cycles, c.ID).ComponentDeletedAt; firstDel == nil || !got.Equal(*firstDel) {
		t.Fatalf("component_deleted_at %v → %v (must be write-once)", firstDel, got)
	}
	if rows, _ := cycles.ListSettling(ctx, 10); len(rows) != 0 {
		t.Fatal("a deleted component is no longer settling")
	}
}

// FinishCancelled has no pr_number fence: a cycle that already opened a pull
// request is still cancellable.
func TestRunCycle_FinishCancelledIgnoresPullRequest(t *testing.T) {
	db := dbtest.New(t)
	bootMigrate(t, db)
	ctx := context.Background()
	cycles := delivery.NewRunCycleRepository(db, nil)
	runs := delivery.NewMilestoneRunRepository(db)
	c := settleCycle(t, cycles, runs, 2, "ca-c2")
	if _, err := cycles.NotePullRequest(ctx, c.ID, delivery.CyclePullRequest{Branch: "b", Number: 7, URL: "u"}); err != nil {
		t.Fatal(err)
	}
	if fin, err := cycles.FinishCancelled(ctx, c.ID); err != nil || fin == nil {
		t.Fatalf("FinishCancelled = (%v, %v)", fin, err)
	}
}

// ListSettling pages fairly: a row the settler has checked goes to the back,
// so a fixed set of rows that never settle (legacy Components with a pod, a
// binding naming a missing release) cannot starve a cycle that closes later.
// A never-checked row comes first, then the least recently checked.
func TestRunCycle_ListSettlingPagesFairly(t *testing.T) {
	db := dbtest.New(t)
	bootMigrate(t, db)
	ctx := context.Background()
	cycles := delivery.NewRunCycleRepository(db, nil)
	runs := delivery.NewMilestoneRunRepository(db)

	var ids []string
	for i := 1; i <= 3; i++ {
		c := settleCycle(t, cycles, runs, 10+i, "ca-f"+string(rune('0'+i)))
		if _, err := cycles.FinishCancelled(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
		// The project's build mutex admits one live run: settle it.
		if _, err := runs.Settle(ctx, c.RunID, delivery.RunStateFailed, delivery.RunReasonPlanFailed); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
		time.Sleep(5 * time.Millisecond) // distinct ended_at, oldest first
	}
	page := func(limit int) []string {
		t.Helper()
		rows, err := cycles.ListSettling(ctx, limit)
		if err != nil {
			t.Fatalf("ListSettling: %v", err)
		}
		var got []string
		for _, r := range rows {
			got = append(got, r.ID)
		}
		return got
	}
	if got := page(2); len(got) != 2 || got[0] != ids[0] || got[1] != ids[1] {
		t.Fatalf("first page = %v, want the two oldest %v", got, ids[:2])
	}
	now := time.Now().UTC()
	for _, id := range ids[:2] {
		if err := cycles.NoteSettleChecked(ctx, id, now); err != nil {
			t.Fatalf("NoteSettleChecked: %v", err)
		}
	}
	if got := page(2); len(got) != 2 || got[0] != ids[2] {
		t.Fatalf("second page = %v, want the unchecked %s first", got, ids[2])
	}
	if err := cycles.NoteSettleChecked(ctx, ids[2], now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := cycles.NoteSettleChecked(ctx, ids[0], now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := page(3); len(got) != 3 || got[0] != ids[1] || got[1] != ids[2] || got[2] != ids[0] {
		t.Fatalf("page = %v, want least recently checked first", got)
	}
	if got := getCycle(t, cycles, ids[0]).SettleCheckedAt; got == nil {
		t.Fatal("settle_checked_at not stored")
	}
}

// The sweep's ordering has a partial index over exactly its predicate.
func TestRunCycle_SettlingIndexExists(t *testing.T) {
	db := dbtest.New(t)
	bootMigrate(t, db)
	var def string
	if err := db.Raw(`SELECT indexdef FROM pg_indexes WHERE tablename = 'run_cycles' AND indexname = 'ix_run_cycles_settling'`).
		Scan(&def).Error; err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"settle_checked_at NULLS FIRST", "ended_at", "ended_at IS NOT NULL", "component_deleted_at IS NULL", "job_ref ~~ 'ca-%'"} {
		if !strings.Contains(def, want) {
			t.Fatalf("index %q lacks %q", def, want)
		}
	}
}
