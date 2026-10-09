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

package spec_test

// DB tier for the project_conversations store (#430): the current-pointer
// partial unique (ux_project_conversations_current), race-safe lazy create,
// and rotation — against a real migrated Postgres (dbtest; skipped under
// -short).

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/spec"
)

func TestConversationRepo_ResolveCreatesOnceAndSticks(t *testing.T) {
	t.Parallel()
	repo := spec.NewConversationRepository(dbtest.New(t))
	ctx := context.Background()

	first, err := repo.ResolveCurrent(ctx, "o1", "p1", "general", "ada")
	if err != nil {
		t.Fatalf("first ResolveCurrent: %v", err)
	}
	if first.ID == "" || !first.Current {
		t.Fatalf("first resolve = %+v, want a current row with an id", first)
	}
	if first.CreatedBy != "ada" {
		t.Fatalf("CreatedBy = %q, want the first resolver", first.CreatedBy)
	}

	// A later resolve — any caller — returns the SAME thread, and the creator
	// stamp does not move.
	again, err := repo.ResolveCurrent(ctx, "o1", "p1", "general", "grace")
	if err != nil {
		t.Fatalf("second ResolveCurrent: %v", err)
	}
	if again.ID != first.ID {
		t.Fatalf("second resolve minted a new thread: %q != %q", again.ID, first.ID)
	}
	if again.CreatedBy != "ada" {
		t.Fatalf("CreatedBy moved to %q on a later resolve", again.CreatedBy)
	}

	// Scope is (org, project, use_case): a different project gets its own.
	other, err := repo.ResolveCurrent(ctx, "o1", "p2", "general", "ada")
	if err != nil {
		t.Fatalf("other-project ResolveCurrent: %v", err)
	}
	if other.ID == first.ID {
		t.Fatalf("projects share a thread: %q", other.ID)
	}
}

// The reason the partial unique exists: two teammates racing the very first
// resolve must converge on ONE thread — the #420 admission pattern.
func TestConversationRepo_RacingResolversConverge(t *testing.T) {
	t.Parallel()
	repo := spec.NewConversationRepository(dbtest.New(t))
	ctx := context.Background()

	const racers = 8
	ids := make([]string, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			row, err := repo.ResolveCurrent(ctx, "o1", "p1", "general", "user")
			if err != nil {
				t.Errorf("racer %d: %v", n, err)
				return
			}
			ids[n] = row.ID
		}(i)
	}
	wg.Wait()
	for i := 1; i < racers; i++ {
		if ids[i] != ids[0] {
			t.Fatalf("racers diverged: ids[%d]=%q != ids[0]=%q", i, ids[i], ids[0])
		}
	}
}

func TestConversationRepo_RotateMovesCurrent(t *testing.T) {
	t.Parallel()
	repo := spec.NewConversationRepository(dbtest.New(t))
	ctx := context.Background()

	old, err := repo.ResolveCurrent(ctx, "o1", "p1", "general", "ada")
	if err != nil {
		t.Fatalf("ResolveCurrent: %v", err)
	}

	fresh, err := repo.Rotate(ctx, "o1", "p1", "general", "grace")
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if fresh.ID == old.ID {
		t.Fatalf("Rotate returned the old thread")
	}
	if !fresh.Current || fresh.CreatedBy != "grace" {
		t.Fatalf("rotated row = %+v, want current, created by the rotator", fresh)
	}

	// Resolve now answers with the rotated thread; the old row survives
	// non-current (the multi-conversation future's history).
	now, err := repo.ResolveCurrent(ctx, "o1", "p1", "general", "ada")
	if err != nil {
		t.Fatalf("post-rotate ResolveCurrent: %v", err)
	}
	if now.ID != fresh.ID {
		t.Fatalf("resolve after rotate = %q, want %q", now.ID, fresh.ID)
	}

	// IsCurrent is the turn-admission fence (the single-era 409 rule).
	if ok, err := repo.IsCurrent(ctx, "o1", "p1", "general", old.ID); err != nil || ok {
		t.Fatalf("IsCurrent(old) = (%v, %v), want (false, nil)", ok, err)
	}
	if ok, err := repo.IsCurrent(ctx, "o1", "p1", "general", fresh.ID); err != nil || !ok {
		t.Fatalf("IsCurrent(fresh) = (%v, %v), want (true, nil)", ok, err)
	}
}

// Two members clicking New conversation near-simultaneously: under READ
// COMMITTED the loser's demote can miss the winner's phantom row and its
// insert collides with the partial unique — which must retry, never surface
// as an error. End state: every rotation minted a thread, exactly one is
// current.
func TestConversationRepo_RacingRotatesBothSucceed(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewConversationRepository(db)
	ctx := context.Background()

	if _, err := repo.ResolveCurrent(ctx, "o1", "p1", "general", "ada"); err != nil {
		t.Fatalf("seed ResolveCurrent: %v", err)
	}

	const racers = 6
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, errs[n] = repo.Rotate(ctx, "o1", "p1", "general", "user")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: %v", i, err)
		}
	}

	var current, total int64
	if err := db.Model(&spec.ProjectConversation{}).Where("org_id = 'o1' AND current").Count(&current).Error; err != nil {
		t.Fatalf("count current: %v", err)
	}
	if err := db.Model(&spec.ProjectConversation{}).Where("org_id = 'o1'").Count(&total).Error; err != nil {
		t.Fatalf("count total: %v", err)
	}
	if current != 1 {
		t.Fatalf("current rows = %d, want exactly 1", current)
	}
	if total != racers+1 {
		t.Fatalf("total rows = %d, want %d (every rotation minted, every thread survives)", total, racers+1)
	}
}

// The rehydrate read: a demoted thread still Exists; an unknown id does not;
// and a NON-UUID id — validConversationID admits far more than uuid syntax —
// must answer false, never a Postgres cast error surfacing as a 500.
func TestConversationRepo_ExistsAndNonUUIDSafety(t *testing.T) {
	t.Parallel()
	repo := spec.NewConversationRepository(dbtest.New(t))
	ctx := context.Background()

	old, err := repo.ResolveCurrent(ctx, "o1", "p1", "general", "ada")
	if err != nil {
		t.Fatalf("ResolveCurrent: %v", err)
	}
	if _, err := repo.Rotate(ctx, "o1", "p1", "general", "ada"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	if ok, err := repo.Exists(ctx, "o1", "p1", "general", old.ID); err != nil || !ok {
		t.Fatalf("Exists(demoted) = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := repo.Exists(ctx, "o1", "p1", "general", "11111111-1111-4111-8111-111111111111"); err != nil || ok {
		t.Fatalf("Exists(unknown) = (%v, %v), want (false, nil)", ok, err)
	}
	if ok, err := repo.Exists(ctx, "o1", "p1", "general", "abc123"); err != nil || ok {
		t.Fatalf("Exists(non-uuid) = (%v, %v), want (false, nil) — not a cast error", ok, err)
	}
	if ok, err := repo.IsCurrent(ctx, "o1", "p1", "general", "abc123"); err != nil || ok {
		t.Fatalf("IsCurrent(non-uuid) = (%v, %v), want (false, nil) — not a cast error", ok, err)
	}
}

// Rotating a project that never resolved is a create, not an error — the
// console's New-conversation works on a project whose thread was never opened.
func TestConversationRepo_RotateOnVirginProjectCreates(t *testing.T) {
	t.Parallel()
	repo := spec.NewConversationRepository(dbtest.New(t))
	ctx := context.Background()

	fresh, err := repo.Rotate(ctx, "o1", "p1", "general", "ada")
	if err != nil {
		t.Fatalf("Rotate on virgin project: %v", err)
	}
	if fresh.ID == "" || !fresh.Current {
		t.Fatalf("rotated row = %+v, want a current row", fresh)
	}
	now, err := repo.ResolveCurrent(ctx, "o1", "p1", "general", "grace")
	if err != nil || now.ID != fresh.ID {
		t.Fatalf("resolve after virgin rotate = (%+v, %v), want the rotated row", now, err)
	}
}

// The automatic rotation of a full thread: every sender that saw the same
// full thread asks to rotate IT, and exactly one successor is minted — the
// others find it already rotated and mint nothing.
func TestConversationRepo_RotateIfCurrentMintsOneSuccessor(t *testing.T) {
	t.Parallel()
	repo := spec.NewConversationRepository(dbtest.New(t))
	ctx := context.Background()

	full, err := repo.ResolveCurrent(ctx, "o1", "p1", "general", "ada")
	if err != nil {
		t.Fatalf("ResolveCurrent: %v", err)
	}

	const senders = 8
	minted := make([]*spec.ProjectConversation, senders)
	var wg sync.WaitGroup
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			row, err := repo.RotateIfCurrent(ctx, "o1", "p1", "general", full.ID, "user")
			if err != nil {
				t.Errorf("sender %d: %v", n, err)
				return
			}
			minted[n] = row
		}(i)
	}
	wg.Wait()

	var successors []string
	for _, row := range minted {
		if row != nil {
			successors = append(successors, row.ID)
		}
	}
	if len(successors) != 1 {
		t.Fatalf("%d senders minted %d successors (%v), want exactly 1", senders, len(successors), successors)
	}
	now, err := repo.ResolveCurrent(ctx, "o1", "p1", "general", "ada")
	if err != nil {
		t.Fatalf("post-rotate ResolveCurrent: %v", err)
	}
	if now.ID != successors[0] {
		t.Fatalf("current = %q, want the one successor %q", now.ID, successors[0])
	}

	// A thread that is no longer current is never rotated: the current one
	// stays put.
	if row, err := repo.RotateIfCurrent(ctx, "o1", "p1", "general", full.ID, "user"); err != nil || row != nil {
		t.Fatalf("RotateIfCurrent(demoted) = (%v, %v), want (nil, nil)", row, err)
	}
	if row, err := repo.RotateIfCurrent(ctx, "o1", "p1", "general", "abc123", "user"); err != nil || row != nil {
		t.Fatalf("RotateIfCurrent(non-uuid) = (%v, %v), want (nil, nil) — not a cast error", row, err)
	}
	if again, _ := repo.ResolveCurrent(ctx, "o1", "p1", "general", "ada"); again.ID != now.ID {
		t.Fatalf("a stale RotateIfCurrent moved current: %q -> %q", now.ID, again.ID)
	}
}

// CreatedAt reads a thread's creation time — current or demoted — and the
// zero time for an unknown or non-uuid id (never a cast error).
func TestConversationRepo_CreatedAt(t *testing.T) {
	t.Parallel()
	repo := spec.NewConversationRepository(dbtest.New(t))
	ctx := context.Background()

	old, err := repo.ResolveCurrent(ctx, "o1", "p1", "general", "ada")
	if err != nil {
		t.Fatalf("ResolveCurrent: %v", err)
	}
	fresh, err := repo.Rotate(ctx, "o1", "p1", "general", "ada")
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	for _, row := range []*spec.ProjectConversation{old, fresh} {
		got, err := repo.CreatedAt(ctx, "o1", "p1", "general", row.ID)
		// Postgres keeps microseconds; the row in hand carries Go's nanoseconds.
		if err != nil || got.Sub(row.CreatedAt).Abs() >= time.Microsecond {
			t.Fatalf("CreatedAt(%s) = (%v, %v), want %v", row.ID, got, err, row.CreatedAt)
		}
	}
	if got, err := repo.CreatedAt(ctx, "o1", "p1", "issues", fresh.ID); err != nil || !got.IsZero() {
		t.Fatalf("CreatedAt(other use case) = (%v, %v), want zero", got, err)
	}
	if got, err := repo.CreatedAt(ctx, "o1", "p1", "general", "abc123"); err != nil || !got.Equal(time.Time{}) {
		t.Fatalf("CreatedAt(non-uuid) = (%v, %v), want (zero, nil)", got, err)
	}
}

// UseCaseOf names the use case of any thread — current or demoted — so a read
// addressed by thread id alone (rehydrate) finds the namespace its turns were
// stored under; an id the project does not hold names none.
func TestConversationRepo_UseCaseOf(t *testing.T) {
	t.Parallel()
	repo := spec.NewConversationRepository(dbtest.New(t))
	ctx := context.Background()

	issue, err := repo.ResolveCurrent(ctx, "o1", "p1", "issue-7", "ada")
	if err != nil {
		t.Fatalf("ResolveCurrent: %v", err)
	}
	if _, err := repo.Rotate(ctx, "o1", "p1", "issue-7", "ada"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	main, err := repo.ResolveCurrent(ctx, "o1", "p1", "general", "ada")
	if err != nil {
		t.Fatalf("ResolveCurrent general: %v", err)
	}

	cases := []struct {
		project, id, want string
	}{
		{"p1", issue.ID, "issue-7"},
		{"p1", main.ID, "general"},
		{"p2", issue.ID, ""},
		{"p1", "11111111-1111-4111-8111-111111111111", ""},
		{"p1", "abc123", ""},
	}
	for _, tc := range cases {
		got, err := repo.UseCaseOf(ctx, "o1", tc.project, tc.id)
		if err != nil || got != tc.want {
			t.Errorf("UseCaseOf(%s, %s) = (%q, %v), want (%q, nil)", tc.project, tc.id, got, err, tc.want)
		}
	}
}

// DeleteUseCase removes every thread of one use case — current and demoted —
// and names them; every other scope's threads stay.
func TestConversationRepo_DeleteUseCase(t *testing.T) {
	t.Parallel()
	repo := spec.NewConversationRepository(dbtest.New(t))
	ctx := context.Background()

	resolve := func(project, useCase string) string {
		t.Helper()
		row, err := repo.ResolveCurrent(ctx, "o1", project, useCase, "ada")
		if err != nil {
			t.Fatalf("ResolveCurrent %s/%s: %v", project, useCase, err)
		}
		return row.ID
	}
	demoted := resolve("p1", "issue-7")
	current, err := repo.Rotate(ctx, "o1", "p1", "issue-7", "ada")
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	keep := map[string][2]string{
		"main":     {"general", resolve("p1", "general")},
		"issues":   {"issues", resolve("p1", "issues")},
		"issue 8":  {"issue-8", resolve("p1", "issue-8")},
		"issue 70": {"issue-70", resolve("p1", "issue-70")},
	}
	otherProject := resolve("p2", "issue-7")
	// The org fence: another org's project of the same id keeps its thread.
	otherOrgRow, err := repo.ResolveCurrent(ctx, "o2", "p1", "issue-7", "ada")
	if err != nil {
		t.Fatalf("ResolveCurrent o2/p1/issue-7: %v", err)
	}

	got, err := repo.DeleteUseCase(ctx, "o1", "p1", "issue-7", time.Now())
	if err != nil {
		t.Fatalf("DeleteUseCase: %v", err)
	}
	if len(got) != 2 || !containsAll(got, demoted, current.ID) {
		t.Fatalf("deleted = %v, want the demoted %s and current %s", got, demoted, current.ID)
	}
	for _, id := range []string{demoted, current.ID} {
		if ok, err := repo.Exists(ctx, "o1", "p1", "issue-7", id); err != nil || ok {
			t.Errorf("Exists(%s) = (%v, %v) after delete, want (false, nil)", id, ok, err)
		}
	}
	for name, k := range keep {
		if ok, err := repo.IsCurrent(ctx, "o1", "p1", k[0], k[1]); err != nil || !ok {
			t.Errorf("%s thread gone after deleting issue-7: (%v, %v)", name, ok, err)
		}
	}
	if ok, err := repo.IsCurrent(ctx, "o1", "p2", "issue-7", otherProject); err != nil || !ok {
		t.Errorf("another project's issue-7 thread gone: (%v, %v)", ok, err)
	}
	if ok, err := repo.IsCurrent(ctx, "o2", "p1", "issue-7", otherOrgRow.ID); err != nil || !ok {
		t.Errorf("another org's issue-7 thread gone: (%v, %v)", ok, err)
	}

	// Idempotent: nothing left to delete.
	again, err := repo.DeleteUseCase(ctx, "o1", "p1", "issue-7", time.Now())
	if err != nil || len(again) != 0 {
		t.Fatalf("second DeleteUseCase = (%v, %v), want (none, nil)", again, err)
	}
	// A later resolve mints a fresh thread.
	if fresh := resolve("p1", "issue-7"); fresh == demoted || fresh == current.ID {
		t.Fatalf("resolve after delete returned a deleted thread %s", fresh)
	}
}

// DeleteUseCase takes only the threads created before its bound — the event
// that caused the removal; a thread started after it stays, current.
func TestConversationRepo_DeleteUseCaseKeepsLaterThreads(t *testing.T) {
	t.Parallel()
	repo := spec.NewConversationRepository(dbtest.New(t))
	ctx := context.Background()

	earlier, err := repo.ResolveCurrent(ctx, "o1", "p1", "issue-7", "ada")
	if err != nil {
		t.Fatalf("ResolveCurrent: %v", err)
	}
	reopened := time.Now()
	later, err := repo.Rotate(ctx, "o1", "p1", "issue-7", "ada")
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	got, err := repo.DeleteUseCase(ctx, "o1", "p1", "issue-7", reopened)
	if err != nil {
		t.Fatalf("DeleteUseCase: %v", err)
	}
	if len(got) != 1 || got[0] != earlier.ID {
		t.Fatalf("deleted = %v, want only the earlier thread %s", got, earlier.ID)
	}
	if ok, err := repo.IsCurrent(ctx, "o1", "p1", "issue-7", later.ID); err != nil || !ok {
		t.Fatalf("the thread started after the bound: IsCurrent = (%v, %v), want it kept", ok, err)
	}
}

func containsAll(have []string, want ...string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
