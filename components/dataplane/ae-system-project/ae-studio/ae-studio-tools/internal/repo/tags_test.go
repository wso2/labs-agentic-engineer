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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// Ported from services/aep-api/internal/platform/gitfs/tags_test.go (Task
// 4.2a): behaviour 1:1.

// originTags lists the origin's tag names.
func originTags(t *testing.T, fx *Fixture) []string {
	t.Helper()
	return strings.Fields(fx.Origin.Git(t, "tag", "-l"))
}

func TestTagCreatesAnnotatedTagOnOrigin(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	sha1 := fx.Origin.HeadSHA(t)
	fx.Origin.Commit(t, map[string]string{"README.md": "v2\n"}, "second")

	// Tag a pinned (non-tip) commit — approve is tag-only at the pinned sha.
	err := fx.Engine.Tag(ctx, fx.Ref, repo.TagSpec{
		Name: "v1", Target: sha1, Message: "requirements v1",
		Tagger: &repo.GitIdentity{Name: "Alice", Email: "alice@example.com"},
	})
	if err != nil {
		t.Fatalf("Tag: %v", err)
	}
	if tags := originTags(t, fx); !slices.Contains(tags, "v1") {
		t.Fatalf("origin tags = %v, want v1", tags)
	}
	// Annotated (object type "tag"), peels to the pinned commit.
	if typ := gitOut(t, fx.Origin.Dir(), "cat-file", "-t", "refs/tags/v1"); typ != "tag" {
		t.Fatalf("origin v1 object type = %q, want annotated tag", typ)
	}
	if peeled := gitOut(t, fx.Origin.Dir(), "rev-parse", "refs/tags/v1^{commit}"); peeled != sha1 {
		t.Fatalf("origin v1 peels to %s, want %s", peeled, sha1)
	}
	if tagger := gitOut(t, fx.Origin.Dir(), "for-each-ref", "--format=%(taggername) %(taggeremail)", "refs/tags/v1"); tagger != "Alice <alice@example.com>" {
		t.Fatalf("origin v1 tagger = %q, want Alice <alice@example.com>", tagger)
	}

	// Empty target tags the default-branch tip.
	if err := fx.Engine.Tag(ctx, fx.Ref, repo.TagSpec{Name: "v1-1", Message: "design v1-1"}); err != nil {
		t.Fatalf("Tag(tip): %v", err)
	}
	head := fx.Origin.HeadSHA(t)
	if peeled := gitOut(t, fx.Origin.Dir(), "rev-parse", "refs/tags/v1-1^{commit}"); peeled != head {
		t.Fatalf("origin v1-1 peels to %s, want tip %s", peeled, head)
	}
}

func TestTagCollisionOnOrigin(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	fx.Origin.Tag(t, "v1", "existing")

	err := fx.Engine.Tag(context.Background(), fx.Ref, repo.TagSpec{Name: "v1", Message: "mine"})
	if !errors.Is(err, repo.ErrTagAlreadyExists) {
		t.Fatalf("Tag(taken name) err = %v, want ErrTagAlreadyExists", err)
	}
}

func TestTagEmptyNameRefused(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	if err := fx.Engine.Tag(context.Background(), fx.Ref, repo.TagSpec{Message: "nameless"}); err == nil {
		t.Fatal("Tag(empty name) succeeded, want an error")
	}
}

func TestTagPushRaceCollisionRollsBackLocalTag(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	mustHead(t, fx, "") // prime the mirror so the race window is push-side

	// Cross-writer window: the tag lands on origin AFTER our fetch but
	// BEFORE our push — inject it when the engine is about to push.
	injected := false
	repo.SetExecHook(fx.Engine, func(args, env []string) {
		if !injected && subcommand(args) == "push" {
			injected = true
			fx.Origin.Tag(t, "v9", "raced you")
		}
	})
	t.Cleanup(func() { repo.SetExecHook(fx.Engine, nil) })

	err := fx.Engine.Tag(ctx, fx.Ref, repo.TagSpec{Name: "v9", Message: "mine"})
	if !errors.Is(err, repo.ErrTagAlreadyExists) {
		t.Fatalf("Tag(push race) err = %v, want ErrTagAlreadyExists", err)
	}
	repo.SetExecHook(fx.Engine, nil)

	// The loser rolled its local tag back — ListTags shows origin's version.
	tags, err := fx.Engine.ListTags(ctx, fx.Ref, "v9")
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 1 || tags[0].Message != "raced you" {
		t.Fatalf("ListTags(v9) = %+v, want origin's tag only", tags)
	}
}

func TestListTagsPrefixPeelAndMessage(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	sha1 := fx.Origin.HeadSHA(t)
	fx.Origin.Tag(t, "v1", "requirements v1")
	sha2 := fx.Origin.Commit(t, map[string]string{"README.md": "v2\n"}, "second")
	fx.Origin.Tag(t, "v1-1", "design v1-1")
	// A lightweight tag (no tag object) and an out-of-prefix tag.
	gitOut(t, fx.Origin.Dir(), "tag", "lw", sha1)
	gitOut(t, fx.Origin.Dir(), "tag", "-a", "-m", "other", "release", sha2)

	tags, err := fx.Engine.ListTags(ctx, fx.Ref, "v")
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	byName := map[string]repo.TagInfo{}
	for _, tag := range tags {
		byName[tag.Name] = tag
	}
	if len(tags) != 2 {
		t.Fatalf("ListTags(v) = %+v, want v1 + v1-1 only", tags)
	}
	if got := byName["v1"]; got.CommitHash != sha1 || got.Message != "requirements v1" || got.CreatedAt.IsZero() {
		t.Fatalf("v1 = %+v, want peeled %s + message + creation date", got, sha1)
	}
	if got := byName["v1-1"]; got.CommitHash != sha2 || got.Message != "design v1-1" {
		t.Fatalf("v1-1 = %+v, want peeled %s + message", got, sha2)
	}

	// Lightweight tag: CommitHash is the commit itself, message empty.
	lw, err := fx.Engine.ListTags(ctx, fx.Ref, "lw")
	if err != nil || len(lw) != 1 {
		t.Fatalf("ListTags(lw) = (%+v, %v)", lw, err)
	}
	if lw[0].CommitHash != sha1 || lw[0].Message != "" {
		t.Fatalf("lightweight = %+v, want commit %s + empty message", lw[0], sha1)
	}

	// No matches → empty, not an error.
	none, err := fx.Engine.ListTags(ctx, fx.Ref, "zz")
	if err != nil || len(none) != 0 {
		t.Fatalf("ListTags(zz) = (%+v, %v), want empty", none, err)
	}
}

func TestListTagsLocalSkipsFetch(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()

	// Prime the mirror (clone) BEFORE any v* tag exists on origin.
	mustHead(t, fx, "")

	// A tag pushed out-of-band to origin AFTER the mirror was cloned — the
	// mirror has no way to know about it without a fetch.
	fx.Origin.Tag(t, "v1-1", "design v1-1")

	// ListTagsLocal serves the mirror as-is → does NOT see the un-fetched tag.
	local, err := fx.Engine.ListTagsLocal(ctx, fx.Ref, "v")
	if err != nil {
		t.Fatalf("ListTagsLocal: %v", err)
	}
	if len(local) != 0 {
		t.Fatalf("ListTagsLocal = %+v, want empty (no fetch)", local)
	}

	// ListTags fetches first → sees the out-of-band tag (the freshness-critical
	// path is unchanged).
	fetched, err := fx.Engine.ListTags(ctx, fx.Ref, "v")
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(fetched) != 1 || fetched[0].Name != "v1-1" {
		t.Fatalf("ListTags = %+v, want v1-1 after fetch", fetched)
	}

	// A tag created THROUGH the engine updates the mirror, so ListTagsLocal
	// sees it without a fetch — the platform-owned tag path stays correct.
	if err := fx.Engine.Tag(ctx, fx.Ref, repo.TagSpec{Name: "v1-2", Message: "design v1-2"}); err != nil {
		t.Fatalf("Tag: %v", err)
	}
	local2, err := fx.Engine.ListTagsLocal(ctx, fx.Ref, "v")
	if err != nil {
		t.Fatalf("ListTagsLocal(after engine tag): %v", err)
	}
	names := map[string]bool{}
	for _, tg := range local2 {
		names[tg.Name] = true
	}
	if !names["v1-2"] {
		t.Fatalf("ListTagsLocal = %+v, want to include engine-created v1-2", local2)
	}
}

// installENOSPCGit puts a git wrapper first on PATH that fails every
// invocation naming subcommand with git's strerror for a full disk, and runs
// the real git otherwise. The engine's children inherit PATH (baseEnv).
func installENOSPCGit(t *testing.T, subcommand string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do
  if [ "$a" = %q ]; then
    echo "fatal: write error: No space left on device" >&2
    exit 128
  fi
done
exec %q "$@"
`, subcommand, real)
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil { //nolint:gosec // a test git wrapper must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The ported methods answer a full disk like every other engine op: a
// DiskFullError (an ErrDiskFull, the 503 disk_full) after queueing the
// reaper's forced sweep, never a plain git error.
func TestPortedMethodsMapENOSPCToDiskFull(t *testing.T) {
	for _, tc := range []struct {
		name, failing string
		primed        bool // clone the mirror before the disk fills
		call          func(ctx context.Context, fx *Fixture) error
	}{
		{"Tag push", "push", true, func(ctx context.Context, fx *Fixture) error {
			return fx.Engine.Tag(ctx, fx.Ref, repo.TagSpec{Name: "v1", Message: "v1"})
		}},
		{"HeadLocal clone", "clone", false, func(ctx context.Context, fx *Fixture) error {
			_, err := fx.Engine.HeadLocal(ctx, fx.Ref)
			return err
		}},
		{"ListTags fetch", "fetch", true, func(ctx context.Context, fx *Fixture) error {
			_, err := fx.Engine.ListTags(ctx, fx.Ref, "v")
			return err
		}},
		{"ListTagsLocal clone", "clone", false, func(ctx context.Context, fx *Fixture) error {
			_, err := fx.Engine.ListTagsLocal(ctx, fx.Ref, "v")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := NewFixture(t, seedFiles())
			ctx := context.Background()
			if tc.primed {
				mustHead(t, fx, "")
			}
			var sweeps atomic.Int32
			fx.Engine.SetOnENOSPC(func() { sweeps.Add(1) })
			installENOSPCGit(t, tc.failing)

			err := tc.call(ctx, fx)
			var full *repo.DiskFullError
			if !errors.As(err, &full) || !errors.Is(err, repo.ErrDiskFull) {
				t.Fatalf("err = %v, want a DiskFullError", err)
			}
			if sweeps.Load() != 1 {
				t.Fatalf("forced sweeps queued = %d, want 1", sweeps.Load())
			}
		})
	}
}
