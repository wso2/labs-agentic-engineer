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
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

func TestHeadAtBranchTagAndRawSha(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()

	sha1 := fx.Origin.HeadSHA(t)
	if got := mustHead(t, fx, ""); got != sha1 {
		t.Fatalf("Head(\"\") = %s, want origin tip %s", got, sha1)
	}

	// Branch addressing stays fresh: advance origin behind the mirror's back.
	sha2 := fx.Origin.Commit(t, map[string]string{"README.md": "hello v2\n"}, "second")
	if got := mustHead(t, fx, ""); got != sha2 {
		t.Fatalf("Head(\"\") after origin advance = %s, want %s", got, sha2)
	}

	// Annotated tag peels to the commit — both bare and "tags/" forms.
	fx.Origin.Tag(t, "v1", "release v1")
	for _, at := range []string{"v1", "tags/v1"} {
		if got := mustHead(t, fx, at); got != sha2 {
			t.Fatalf("Head(%q) = %s, want peeled commit %s", at, got, sha2)
		}
	}

	// Raw sha accepted verbatim.
	if got := mustHead(t, fx, sha1); got != sha1 {
		t.Fatalf("Head(sha1) = %s, want %s", got, sha1)
	}

	// Missing ref and missing object → ErrRefNotFound.
	if _, err := fx.Engine.Head(ctx, fx.Ref, "no-such-ref"); !errors.Is(err, repo.ErrRefNotFound) {
		t.Fatalf("Head(missing ref) err = %v, want ErrRefNotFound", err)
	}
	missing := strings.Repeat("deadbeef", 5)
	if _, err := fx.Engine.Head(ctx, fx.Ref, missing); !errors.Is(err, repo.ErrRefNotFound) {
		t.Fatalf("Head(missing sha) err = %v, want ErrRefNotFound", err)
	}
}

func TestListReadFileReadBundle(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	head := fx.Origin.HeadSHA(t)

	entries, gotHead, err := fx.Engine.List(ctx, fx.Ref, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if gotHead != head {
		t.Fatalf("List head = %s, want %s", gotHead, head)
	}
	byPath := map[string]repo.Entry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	req, ok := byPath["specs/requirements/prd.md"]
	if !ok || len(entries) != 2 {
		t.Fatalf("List entries = %+v, want README.md + specs file", entries)
	}
	if req.Size != int64(len("req v1\n")) || len(req.SHA) != 40 {
		t.Fatalf("List entry = %+v, want size %d and a 40-hex blob sha", req, len("req v1\n"))
	}

	content, blobSHA, err := fx.Engine.ReadFile(ctx, fx.Ref, "", "specs/requirements/prd.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "req v1\n" || blobSHA != req.SHA {
		t.Fatalf("ReadFile = (%q, %s), want (%q, %s)", content, blobSHA, "req v1\n", req.SHA)
	}

	if _, _, err := fx.Engine.ReadFile(ctx, fx.Ref, "", "specs/nope.md"); !errors.Is(err, repo.ErrPathNotFound) {
		t.Fatalf("ReadFile(missing) err = %v, want ErrPathNotFound", err)
	}
	// A directory path is not a file.
	if _, _, err := fx.Engine.ReadFile(ctx, fx.Ref, "", "specs"); !errors.Is(err, repo.ErrPathNotFound) {
		t.Fatalf("ReadFile(dir) err = %v, want ErrPathNotFound", err)
	}

	files, bundleHead, err := fx.Engine.ReadBundle(ctx, fx.Ref, "", func(rel string) bool {
		return strings.HasPrefix(rel, "specs/")
	})
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}
	if bundleHead != head || len(files) != 1 || files["specs/requirements/prd.md"] != "req v1\n" {
		t.Fatalf("ReadBundle = (%v, %s), want the one specs file at %s", files, bundleHead, head)
	}
	// nil keep = keep everything.
	all, _, err := fx.Engine.ReadBundle(ctx, fx.Ref, "", nil)
	if err != nil || len(all) != 2 {
		t.Fatalf("ReadBundle(nil keep) = (%v, %v), want both files", all, err)
	}
}

// Tree listings must emit pathnames VERBATIM. The default `ls-tree -l -z`
// long format is the only one that does: a custom `--format=%(path)` C-quotes
// non-ASCII paths even under -z, and core.quotepath=off still quotes `"` and
// `\` — this pins the raw-byte contract for every hostile shape at once.
func TestListAndReadBundle_HostilePathsVerbatim(t *testing.T) {
	seed := map[string]string{
		"specs/仕様-résumé ノート.md": "unicode content\n",
		`specs/we"ird\path.md`:   "quoted content\n",
	}
	fx := NewFixture(t, seed)
	ctx := context.Background()

	entries, _, err := fx.Engine.List(ctx, fx.Ref, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byPath := map[string]repo.Entry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	for path, content := range seed {
		e, ok := byPath[path]
		if !ok {
			t.Fatalf("List missing verbatim path %q; got %+v", path, entries)
		}
		if e.Size != int64(len(content)) {
			t.Errorf("List %q size = %d, want %d", path, e.Size, len(content))
		}
		got, blobSHA, rerr := fx.Engine.ReadFile(ctx, fx.Ref, "", path)
		if rerr != nil {
			t.Fatalf("ReadFile(%q): %v", path, rerr)
		}
		if string(got) != content || blobSHA != e.SHA {
			t.Errorf("ReadFile(%q) = (%q, %s), want (%q, %s)", path, got, blobSHA, content, e.SHA)
		}
	}

	files, _, err := fx.Engine.ReadBundle(ctx, fx.Ref, "", nil)
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}
	for path, content := range seed {
		if files[path] != content {
			t.Errorf("ReadBundle[%q] = %q, want %q (keys must be verbatim)", path, files[path], content)
		}
	}
}

func TestReadsAtTagAndShaSeeOldTree(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	sha1 := fx.Origin.HeadSHA(t)
	fx.Origin.Tag(t, "v1", "release v1")
	fx.Origin.Commit(t, map[string]string{"specs/requirements/prd.md": "req v2\n"}, "update")

	for _, at := range []string{"v1", sha1} {
		content, _, err := fx.Engine.ReadFile(ctx, fx.Ref, at, "specs/requirements/prd.md")
		if err != nil {
			t.Fatalf("ReadFile at %q: %v", at, err)
		}
		if string(content) != "req v1\n" {
			t.Fatalf("ReadFile at %q = %q, want old content", at, content)
		}
	}
	content, _, err := fx.Engine.ReadFile(ctx, fx.Ref, "", "specs/requirements/prd.md")
	if err != nil || string(content) != "req v2\n" {
		t.Fatalf("ReadFile at branch = (%q, %v), want new content", content, err)
	}
}

func TestRawShaReadsUseLocalObjects(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	sha1 := mustHead(t, fx, "") // primes the mirror

	rec := recordCommands(t, fx.Engine)
	if _, _, err := fx.Engine.ReadFile(ctx, fx.Ref, sha1, "README.md"); err != nil {
		t.Fatalf("ReadFile at raw sha: %v", err)
	}
	if n := rec.countSubcommand("fetch"); n != 0 {
		t.Fatalf("raw-sha read fetched %d times, want 0 (local objects)", n)
	}

	// A sha the mirror has never seen → exactly one fetch, then it resolves.
	sha2 := fx.Origin.Commit(t, map[string]string{"README.md": "v2\n"}, "second")
	rec.reset()
	if _, _, err := fx.Engine.ReadFile(ctx, fx.Ref, sha2, "README.md"); err != nil {
		t.Fatalf("ReadFile at unseen sha: %v", err)
	}
	if n := rec.countSubcommand("fetch"); n != 1 {
		t.Fatalf("unseen-sha read fetched %d times, want exactly 1", n)
	}
}

func TestInvalidPathSegmentsRejected(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	bad := fx.Ref
	bad.Owner = "../escape"
	if _, err := fx.Engine.Head(context.Background(), bad, ""); err == nil {
		t.Fatal("Head with traversal owner segment succeeded, want validation error")
	}
	bad = fx.Ref
	bad.Repo = "a/b"
	if _, err := fx.Engine.Head(context.Background(), bad, ""); err == nil {
		t.Fatal("Head with separator repo succeeded, want validation error")
	}
}

// Ported from gitfs reads_test.go (Task 4.2a): HeadLocal serves the mirror's
// default-branch tip without a fetch.
func TestHeadLocalServesMirrorWithoutFetch(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()

	// First-ever access: ensureMirror clones (the one-time network cost), then
	// the tip resolves locally. The pod's clone carries its own refspec fetch
	// into staging (cloneMirror); HeadLocal adds none.
	rec := recordCommands(t, fx.Engine)
	sha1, err := fx.Engine.HeadLocal(ctx, fx.Ref)
	if err != nil {
		t.Fatalf("HeadLocal: %v", err)
	}
	if want := fx.Origin.HeadSHA(t); sha1 != want {
		t.Fatalf("HeadLocal = %s, want origin head %s", sha1, want)
	}
	if n := rec.countSubcommand("clone"); n != 1 {
		t.Fatalf("first access cloned %d times, want 1", n)
	}
	if n := rec.countSubcommand("fetch"); n != 1 {
		t.Fatalf("first access fetched %d times, want only the clone's own fetch", n)
	}

	// Origin moves ahead out-of-band — HeadLocal serves the mirror as-is
	// (stale-but-local), still without a fetch.
	sha2 := fx.Origin.Commit(t, map[string]string{"README.md": "v2\n"}, "second")
	rec.reset()
	got, err := fx.Engine.HeadLocal(ctx, fx.Ref)
	if err != nil {
		t.Fatalf("HeadLocal after origin move: %v", err)
	}
	if got != sha1 || got == sha2 {
		t.Fatalf("HeadLocal = %s, want stale mirror head %s (never un-fetched %s)", got, sha1, sha2)
	}
	if n := rec.countSubcommand("fetch"); n != 0 {
		t.Fatalf("HeadLocal fetched %d times, want 0", n)
	}

	// A write THROUGH the engine updates the mirror before returning, so
	// HeadLocal sees it without a fetch — the committed-truth write path keeps
	// local reads current for platform-written content.
	res, err := fx.Engine.Mutate(ctx, fx.Ref, func(tx repo.Tx) error {
		tx.Write("specs/design/design.md", []byte("design\n"))
		return nil
	}, repo.CommitOpts{Message: "add design"})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	rec.reset()
	got2, err := fx.Engine.HeadLocal(ctx, fx.Ref)
	if err != nil {
		t.Fatalf("HeadLocal after engine write: %v", err)
	}
	if got2 != res.CommitSHA {
		t.Fatalf("HeadLocal = %s, want engine commit %s", got2, res.CommitSHA)
	}
	if n := rec.countSubcommand("fetch"); n != 0 {
		t.Fatalf("HeadLocal fetched %d times, want 0", n)
	}
}

func TestHeadLocalMissingRepo(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	bad := fx.Ref
	bad.Repo = "ghost"
	bad.CloneURL = "file:///nonexistent/ghost.git"
	if _, err := fx.Engine.HeadLocal(context.Background(), bad); err == nil {
		t.Fatal("HeadLocal on missing repo succeeded, want clone error")
	}
}
