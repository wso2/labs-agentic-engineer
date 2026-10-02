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

package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// fastRetry keeps CAS tests quick.
var fastRetry = repo.RetryPolicy{Attempts: 4, Backoff: []time.Duration{time.Millisecond, 2 * time.Millisecond}}

func TestMutateWriteAndDeleteLandOnOrigin(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()

	res, err := fx.Engine.Mutate(ctx, fx.Ref, func(tx repo.Tx) error {
		tx.Write("specs/design/design.md", []byte("design v1\n"))
		tx.Delete("README.md")
		return nil
	}, repo.CommitOpts{Message: "edit(specs): add design, drop readme", Retry: fastRetry})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if !res.Changed || res.CommitSHA == "" {
		t.Fatalf("Mutate result = %+v, want Changed with a sha", res)
	}
	// Origin is the arbiter: its tip must be exactly the returned commit.
	if head := fx.Origin.HeadSHA(t); head != res.CommitSHA {
		t.Fatalf("origin tip = %s, want pushed commit %s", head, res.CommitSHA)
	}
	if got := fx.Origin.FileAt(t, "main", "specs/design/design.md"); got != "design v1\n" {
		t.Fatalf("origin design.md = %q", got)
	}
	if _, _, err := fx.Engine.ReadFile(ctx, fx.Ref, res.CommitSHA, "README.md"); !errors.Is(err, repo.ErrPathNotFound) {
		t.Fatalf("deleted README.md still readable (err=%v)", err)
	}
	// The untouched seeded file survives the overlay.
	if got := fx.Origin.FileAt(t, "main", "specs/requirements/prd.md"); got != "req v1\n" {
		t.Fatalf("untouched file corrupted: %q", got)
	}
}

func TestMutateNoChange(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	base := fx.Origin.HeadSHA(t)

	// Nothing staged.
	res, err := fx.Engine.Mutate(ctx, fx.Ref, func(repo.Tx) error { return nil },
		repo.CommitOpts{Message: "noop", Retry: fastRetry})
	if err != nil || res.Changed || res.CommitSHA != base {
		t.Fatalf("no-op Mutate = (%+v, %v), want Changed=false at %s", res, err, base)
	}
	// Staged content identical to base → same tree → no commit.
	res, err = fx.Engine.Mutate(ctx, fx.Ref, func(tx repo.Tx) error {
		tx.Write("README.md", []byte("hello\n"))
		return nil
	}, repo.CommitOpts{Message: "identical", Retry: fastRetry})
	if err != nil || res.Changed || res.CommitSHA != base {
		t.Fatalf("identical Mutate = (%+v, %v), want Changed=false at %s", res, err, base)
	}
	if head := fx.Origin.HeadSHA(t); head != base {
		t.Fatalf("origin advanced to %s on a no-change mutate", head)
	}
}

func TestMutateFnErrorAbortsWithoutRetry(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	base := fx.Origin.HeadSHA(t)
	sentinel := errors.New("domain conflict: stale baseSha")
	calls := 0

	_, err := fx.Engine.Mutate(context.Background(), fx.Ref, func(tx repo.Tx) error {
		calls++
		tx.Write("specs/x.md", []byte("x"))
		return sentinel
	}, repo.CommitOpts{Message: "conflict", Retry: fastRetry})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Mutate err = %v, want the fn's own error", err)
	}
	if calls != 1 {
		t.Fatalf("fn ran %d times, want exactly 1 (no retry on fn error)", calls)
	}
	if head := fx.Origin.HeadSHA(t); head != base {
		t.Fatalf("origin advanced to %s despite fn abort", head)
	}
}

func TestMutateBaseSnapshotFeedsPreconditions(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	base := fx.Origin.HeadSHA(t)

	_, err := fx.Engine.Mutate(context.Background(), fx.Ref, func(tx repo.Tx) error {
		if got := tx.Base().CommitSHA(); got != base {
			t.Errorf("Base().CommitSHA() = %s, want %s", got, base)
		}
		content, blobSHA, err := tx.Base().Read("README.md")
		if err != nil || string(content) != "hello\n" || len(blobSHA) != 40 {
			t.Errorf("Base().Read = (%q, %s, %v)", content, blobSHA, err)
		}
		if _, _, err := tx.Base().Read("absent.md"); !errors.Is(err, repo.ErrPathNotFound) {
			t.Errorf("Base().Read(absent) err = %v, want ErrPathNotFound", err)
		}
		var walked []string
		if err := tx.Base().Walk("specs/", func(rel, blobSHA string) error {
			walked = append(walked, rel)
			return nil
		}); err != nil {
			t.Errorf("Walk: %v", err)
		}
		if len(walked) != 1 || walked[0] != "specs/requirements/prd.md" {
			t.Errorf("Walk(specs/) = %v", walked)
		}
		return nil
	}, repo.CommitOpts{Message: "inspect", Retry: fastRetry})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
}

func TestMutateCASRetryLandsAfterOriginAdvance(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	calls := 0

	res, err := fx.Engine.Mutate(ctx, fx.Ref, func(tx repo.Tx) error {
		calls++
		if calls == 1 {
			// Advance origin behind the engine's back, between its fetch
			// and its push — the push lease goes stale.
			fx.Origin.Seed(t, map[string]string{"concurrent.md": "someone else\n"}, "concurrent write")
		}
		tx.Write("specs/mine.md", []byte("mine\n"))
		return nil
	}, repo.CommitOpts{Message: "cas", Retry: fastRetry})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if calls != 2 {
		t.Fatalf("fn ran %d times, want 2 (one rejected push, one retry)", calls)
	}
	if head := fx.Origin.HeadSHA(t); head != res.CommitSHA {
		t.Fatalf("origin tip = %s, want %s", head, res.CommitSHA)
	}
	// Both the concurrent write and ours are present — the overlay replayed
	// on the new base.
	if got := fx.Origin.FileAt(t, "main", "concurrent.md"); got != "someone else\n" {
		t.Fatalf("concurrent write lost: %q", got)
	}
	if got := fx.Origin.FileAt(t, "main", "specs/mine.md"); got != "mine\n" {
		t.Fatalf("our write lost: %q", got)
	}
}

func TestMutateRetryExhaustionSurfacesNonFastForward(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	calls := 0

	_, err := fx.Engine.Mutate(context.Background(), fx.Ref, func(tx repo.Tx) error {
		calls++
		// Every attempt loses the race.
		fx.Origin.Seed(t, map[string]string{"racer.md": time.Now().String()}, "racer")
		tx.Write("specs/mine.md", []byte("mine\n"))
		return nil
	}, repo.CommitOpts{Message: "exhaust", Retry: repo.RetryPolicy{Attempts: 2, Backoff: []time.Duration{time.Millisecond}}})
	if !errors.Is(err, repo.ErrRefNotFastForward) {
		t.Fatalf("Mutate err = %v, want ErrRefNotFastForward", err)
	}
	if calls != 2 {
		t.Fatalf("fn ran %d times, want the bounded 2 attempts", calls)
	}
}

func TestMutateCommitIdentity(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	res, err := fx.Engine.Mutate(context.Background(), fx.Ref, func(tx repo.Tx) error {
		tx.Write("a.md", []byte("a\n"))
		return nil
	}, repo.CommitOpts{
		Message: "identity",
		Author:  &repo.GitIdentity{Name: "Alice", Email: "alice@example.com"},
		Retry:   fastRetry,
	})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	got := gitOut(t, fx.Origin.Dir(), "log", "-1", "--format=%an|%ae|%cn|%ce|%s", res.CommitSHA)
	want := "Alice|alice@example.com|AEP|noreply@aep.dev|identity"
	if got != want {
		t.Fatalf("commit identity = %q, want %q (author=user, committer=AEP bot)", got, want)
	}
}

func TestSleepJitteredZeroBaseNoPanic(t *testing.T) {
	// A 0 base delay must not panic (rand.N(0) would) and must return promptly
	// rather than block — the guard treats <=0 as "no wait".
	done := make(chan error, 1)
	go func() { done <- repo.SleepJittered(context.Background(), []time.Duration{0}, 0) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SleepJittered(0) = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SleepJittered(0) blocked, want a prompt return")
	}
	// The no-wait path still surfaces a canceled ctx.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := repo.SleepJittered(ctx, []time.Duration{0}, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("SleepJittered(0, canceled) = %v, want context.Canceled", err)
	}
}

func TestMutateRejectsHostilePaths(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	for _, path := range []string{"", "/abs.md", "../escape.md", "a/../../b.md", ".git/hooks/x"} {
		_, err := fx.Engine.Mutate(context.Background(), fx.Ref, func(tx repo.Tx) error {
			tx.Write(path, []byte("x"))
			return nil
		}, repo.CommitOpts{Message: "hostile", Retry: fastRetry})
		if err == nil {
			t.Fatalf("Mutate accepted hostile path %q", path)
		}
	}
}
