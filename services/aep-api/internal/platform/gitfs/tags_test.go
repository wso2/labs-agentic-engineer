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

package gitfs_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
)

func TestTagCreatesAnnotatedTagOnOrigin(t *testing.T) {
	fx := workspacetest.New(t, seedFiles())
	ctx := context.Background()
	sha1 := fx.Origin.HeadSHA(t)
	fx.Origin.Seed(t, map[string]string{"README.md": "v2\n"}, "second")

	// Tag a pinned (non-tip) commit — approve is tag-only at the pinned sha.
	err := fx.Engine.Tag(ctx, fx.Ref, gitfs.TagSpec{
		Name: "v1", Target: sha1, Message: "requirements v1",
		Tagger: &gitfs.GitIdentity{Name: "Alice", Email: "alice@example.com"},
	})
	if err != nil {
		t.Fatalf("Tag: %v", err)
	}
	if tags := fx.Origin.Tags(t); !slices.Contains(tags, "v1") {
		t.Fatalf("origin tags = %v, want v1", tags)
	}
	// Annotated (object type "tag"), peels to the pinned commit.
	if typ := gitOut(t, fx.Origin.Dir(), "cat-file", "-t", "refs/tags/v1"); typ != "tag" {
		t.Fatalf("origin v1 object type = %q, want annotated tag", typ)
	}
	if peeled := gitOut(t, fx.Origin.Dir(), "rev-parse", "refs/tags/v1^{commit}"); peeled != sha1 {
		t.Fatalf("origin v1 peels to %s, want %s", peeled, sha1)
	}

	// Empty target tags the default-branch tip.
	if err := fx.Engine.Tag(ctx, fx.Ref, gitfs.TagSpec{Name: "v1-1", Message: "design v1-1"}); err != nil {
		t.Fatalf("Tag(tip): %v", err)
	}
	head := fx.Origin.HeadSHA(t)
	if peeled := gitOut(t, fx.Origin.Dir(), "rev-parse", "refs/tags/v1-1^{commit}"); peeled != head {
		t.Fatalf("origin v1-1 peels to %s, want tip %s", peeled, head)
	}
}

func TestTagCollisionOnOrigin(t *testing.T) {
	fx := workspacetest.New(t, seedFiles())
	fx.Origin.Tag(t, "v1", "existing")

	err := fx.Engine.Tag(context.Background(), fx.Ref, gitfs.TagSpec{Name: "v1", Message: "mine"})
	if !errors.Is(err, gitfs.ErrTagAlreadyExists) {
		t.Fatalf("Tag(taken name) err = %v, want ErrTagAlreadyExists", err)
	}
}

func TestTagPushRaceCollisionRollsBackLocalTag(t *testing.T) {
	fx := workspacetest.New(t, seedFiles())
	ctx := context.Background()
	mustHead(t, fx, "") // prime the mirror so the race window is push-side

	// Cross-replica window: the tag lands on origin AFTER our fetch but
	// BEFORE our push — inject it when the engine is about to push.
	injected := false
	gitfs.SetExecHook(fx.Engine, func(args, env []string) {
		if !injected && subcommand(args) == "push" {
			injected = true
			fx.Origin.Tag(t, "v9", "raced you")
		}
	})
	t.Cleanup(func() { gitfs.SetExecHook(fx.Engine, nil) })

	err := fx.Engine.Tag(ctx, fx.Ref, gitfs.TagSpec{Name: "v9", Message: "mine"})
	if !errors.Is(err, gitfs.ErrTagAlreadyExists) {
		t.Fatalf("Tag(push race) err = %v, want ErrTagAlreadyExists", err)
	}
	gitfs.SetExecHook(fx.Engine, nil)

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
	fx := workspacetest.New(t, seedFiles())
	ctx := context.Background()
	sha1 := fx.Origin.HeadSHA(t)
	fx.Origin.Tag(t, "v1", "requirements v1")
	sha2 := fx.Origin.Seed(t, map[string]string{"README.md": "v2\n"}, "second")
	fx.Origin.Tag(t, "v1-1", "design v1-1")
	// A lightweight tag (no tag object) and an out-of-prefix tag.
	gitOut(t, fx.Origin.Dir(), "tag", "lw", sha1)
	gitOut(t, fx.Origin.Dir(), "tag", "-a", "-m", "other", "release", sha2)

	tags, err := fx.Engine.ListTags(ctx, fx.Ref, "v")
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	byName := map[string]gitfs.TagInfo{}
	for _, tag := range tags {
		byName[tag.Name] = tag
	}
	if len(tags) != 2 {
		t.Fatalf("ListTags(v) = %+v, want v1 + v1-1 only", tags)
	}
	if got := byName["v1"]; got.CommitHash != sha1 || got.Message != "requirements v1" {
		t.Fatalf("v1 = %+v, want peeled %s + message", got, sha1)
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
	fx := workspacetest.New(t, seedFiles())
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
	if err := fx.Engine.Tag(ctx, fx.Ref, gitfs.TagSpec{Name: "v1-2", Message: "design v1-2"}); err != nil {
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

func TestDiffThreeDot(t *testing.T) {
	fx := workspacetest.New(t, seedFiles())
	ctx := context.Background()
	base := fx.Origin.HeadSHA(t)

	// One commit past base: modify README, add design.md, delete requirements.
	res, err := fx.Engine.Mutate(ctx, fx.Ref, func(tx gitfs.Tx) error {
		tx.Write("README.md", []byte("hello\nchanged\n"))
		tx.Write("specs/design/design.md", []byte("design\n"))
		tx.Delete("specs/requirements/prd.md")
		return nil
	}, gitfs.CommitOpts{Message: "reshape", Retry: fastRetry})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}

	cmp, err := fx.Engine.Diff(ctx, fx.Ref, base, res.CommitSHA)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if cmp.Status != "ahead" || cmp.AheadBy != 1 || cmp.BehindBy != 0 || cmp.TotalCommits != 1 || cmp.Truncated {
		t.Fatalf("Diff summary = %+v, want ahead by 1", cmp)
	}
	statuses := map[string]string{}
	adds := map[string]int{}
	for _, f := range cmp.Files {
		statuses[f.Filename] = f.Status
		adds[f.Filename] = f.Additions
	}
	want := map[string]string{
		"README.md":                 "modified",
		"specs/design/design.md":    "added",
		"specs/requirements/prd.md": "removed",
	}
	for path, status := range want {
		if statuses[path] != status {
			t.Fatalf("Diff statuses = %v, want %v", statuses, want)
		}
	}
	if adds["specs/design/design.md"] != 1 || adds["README.md"] != 1 {
		t.Fatalf("Diff additions = %v, want numstat populated", adds)
	}
	// Patch carries the unified hunks (header stripped — GitHub compare shape).
	patches := map[string]string{}
	for _, f := range cmp.Files {
		patches[f.Filename] = f.Patch
	}
	if p := patches["README.md"]; !strings.HasPrefix(p, "@@") || !strings.Contains(p, "+changed") {
		t.Fatalf("README.md patch = %q, want hunks with +changed", p)
	}
	if p := patches["specs/design/design.md"]; !strings.Contains(p, "+design") {
		t.Fatalf("added-file patch = %q, want +design", p)
	}
	if p := patches["specs/requirements/prd.md"]; !strings.Contains(p, "-") || strings.Contains(p, "diff --git") {
		t.Fatalf("removed-file patch = %q, want hunks without the diff header", p)
	}

	// Reverse direction reads as behind; self-diff is identical.
	rev, err := fx.Engine.Diff(ctx, fx.Ref, res.CommitSHA, base)
	if err != nil || rev.Status != "behind" || rev.BehindBy != 1 {
		t.Fatalf("reverse Diff = (%+v, %v), want behind by 1", rev, err)
	}
	same, err := fx.Engine.Diff(ctx, fx.Ref, res.CommitSHA, res.CommitSHA)
	if err != nil || same.Status != "identical" || len(same.Files) != 0 {
		t.Fatalf("self Diff = (%+v, %v), want identical", same, err)
	}

	// Unknown ref → ErrRefNotFound.
	if _, err := fx.Engine.Diff(ctx, fx.Ref, "nope", res.CommitSHA); !errors.Is(err, gitfs.ErrRefNotFound) {
		t.Fatalf("Diff(missing base) err = %v, want ErrRefNotFound", err)
	}
}

func TestDiffBetweenTags(t *testing.T) {
	fx := workspacetest.New(t, seedFiles())
	fx.Origin.Tag(t, "v1", "req v1")
	fx.Origin.Seed(t, map[string]string{"specs/requirements/prd.md": "req v2\n"}, "update")
	fx.Origin.Tag(t, "v2", "req v2")

	cmp, err := fx.Engine.Diff(context.Background(), fx.Ref, "v1", "v2")
	if err != nil {
		t.Fatalf("Diff(tags): %v", err)
	}
	if len(cmp.Files) != 1 || cmp.Files[0].Filename != "specs/requirements/prd.md" ||
		cmp.Files[0].Status != "modified" {
		t.Fatalf("Diff(v1...v2) files = %+v", cmp.Files)
	}
}
