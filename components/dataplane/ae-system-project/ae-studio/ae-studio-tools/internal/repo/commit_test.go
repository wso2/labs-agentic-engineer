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
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// blobAt is origin's blob sha of path at the branch tip.
func blobAt(t *testing.T, fx *Fixture, path string) string {
	t.Helper()
	return fx.Origin.Git(t, "rev-parse", "main:"+path)
}

// changedPaths lists the paths commit changed on origin, sorted.
func changedPaths(t *testing.T, fx *Fixture, commit string) []string {
	t.Helper()
	paths := strings.Fields(fx.Origin.Git(t, "diff-tree", "--no-commit-id", "--name-only", "-r", commit))
	sort.Strings(paths)
	return paths
}

// Every baseSha holds: one commit lands writes (an edit and a create) and a
// delete, authored and committed by the given identity.
func TestCommit_LandsWritesAndDeletesAsOneCommit(t *testing.T) {
	fx := NewFixture(t, map[string]string{"specs/a.md": "a1", "specs/gone.md": "g"})
	ctx := context.Background()
	author := &repo.GitIdentity{Name: "Alice", Email: "alice@example.com"}

	res, conflicts, err := fx.Engine.Commit(ctx, fx.Ref,
		[]repo.CommitWrite{
			{Path: "specs/a.md", Content: []byte("a2"), BaseSHA: blobAt(t, fx, "specs/a.md")},
			{Path: "specs/new.md", Content: []byte("n")},
		},
		[]repo.CommitDelete{{Path: "specs/gone.md", BaseSHA: blobAt(t, fx, "specs/gone.md")}},
		"edit specs", author, nil)
	if err != nil || conflicts != nil {
		t.Fatalf("Commit = %+v, %v, %v", res, conflicts, err)
	}
	if !res.Changed || res.CommitSHA != fx.Origin.HeadSHA(t) {
		t.Fatalf("result = %+v, want a changed commit at origin's tip", res)
	}
	if got, want := changedPaths(t, fx, res.CommitSHA), []string{"specs/a.md", "specs/gone.md", "specs/new.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed %v, want %v", got, want)
	}
	if got := fx.Origin.FileAt(t, "main", "specs/a.md"); got != "a2" {
		t.Fatalf("specs/a.md = %q", got)
	}
	if who := fx.Origin.Git(t, "log", "-1", "--format=%an <%ae>|%cn <%ce>|%s", res.CommitSHA); who != "Alice <alice@example.com>|Alice <alice@example.com>|edit specs" {
		t.Fatalf("commit identity/subject = %q", who)
	}
}

// A nil author commits as the engine's AEP default identity.
func TestCommit_NilAuthorIsTheDefaultIdentity(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	res, _, err := fx.Engine.Commit(context.Background(), fx.Ref,
		[]repo.CommitWrite{{Path: "specs/x.md", Content: []byte("x")}}, nil, "x", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if who := fx.Origin.Git(t, "log", "-1", "--format=%an <%ae>", res.CommitSHA); who != "AEP <noreply@aep.dev>" {
		t.Fatalf("author = %q, want the AEP default", who)
	}
}

// A committer apart from the author is recorded as the commit's committer.
func TestCommit_DistinctCommitter(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	author := &repo.GitIdentity{Name: "Alice", Email: "alice@example.com"}
	committer := &repo.GitIdentity{Name: "AEP Bot", Email: "bot@example.com"}
	res, _, err := fx.Engine.Commit(context.Background(), fx.Ref,
		[]repo.CommitWrite{{Path: "specs/x.md", Content: []byte("x")}}, nil, "x", author, committer)
	if err != nil {
		t.Fatal(err)
	}
	if who := fx.Origin.Git(t, "log", "-1", "--format=%an <%ae>|%cn <%ce>", res.CommitSHA); who != "Alice <alice@example.com>|AEP Bot <bot@example.com>" {
		t.Fatalf("identities = %q", who)
	}
}

// Every failed precondition is reported, and nothing is applied.
func TestCommit_StaleBaseShaIsAConflictAndAppliesNothing(t *testing.T) {
	fx := NewFixture(t, map[string]string{"specs/a.md": "a1", "specs/b.md": "b1"})
	ctx := context.Background()
	tip := fx.Origin.HeadSHA(t)
	curA, curB := blobAt(t, fx, "specs/a.md"), blobAt(t, fx, "specs/b.md")
	const stale = "0123456789012345678901234567890123456789"

	res, conflicts, err := fx.Engine.Commit(ctx, fx.Ref,
		[]repo.CommitWrite{
			{Path: "specs/a.md", Content: []byte("a2"), BaseSHA: stale}, // stale
			{Path: "specs/b.md", Content: []byte("b2")},                 // must not exist, does
			{Path: "specs/c.md", Content: []byte("c"), BaseSHA: stale},  // expected, absent
		},
		[]repo.CommitDelete{{Path: "specs/missing.md"}}, // must exist
		"stale", nil, nil)
	if !errors.Is(err, repo.ErrCommitConflict) {
		t.Fatalf("err = %v, want ErrCommitConflict", err)
	}
	if res != (repo.CommitResult{}) {
		t.Fatalf("result = %+v, want zero on conflict", res)
	}
	want := []repo.Conflict{
		{Path: "specs/a.md", BaseSHA: stale, CurrentSHA: curA},
		{Path: "specs/b.md", BaseSHA: "", CurrentSHA: curB},
		{Path: "specs/c.md", BaseSHA: stale, CurrentSHA: ""},
		{Path: "specs/missing.md", BaseSHA: "", CurrentSHA: ""},
	}
	if !reflect.DeepEqual(conflicts, want) {
		t.Fatalf("conflicts = %+v, want %+v", conflicts, want)
	}
	if got := fx.Origin.HeadSHA(t); got != tip {
		t.Fatalf("origin moved to %s on a conflict", got)
	}
}

// Commit is the engine's commit only: a design.cell write lands alone, with
// no component skeletons scaffolded beside it.
func TestCommit_DoesNotScaffold(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	cell := "component lunch-api service\ncomponent lunch-web web-application\n"
	res, _, err := fx.Engine.Commit(context.Background(), fx.Ref,
		[]repo.CommitWrite{{Path: "specs/design/design.cell", Content: []byte(cell)}}, nil, "cell", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := changedPaths(t, fx, res.CommitSHA); !reflect.DeepEqual(got, []string{"specs/design/design.cell"}) {
		t.Fatalf("changed %v, want the cell only", got)
	}
}

// Content identical to the tip makes no commit.
func TestCommit_IdenticalContentIsNoChange(t *testing.T) {
	fx := NewFixture(t, map[string]string{"specs/a.md": "a1"})
	tip := fx.Origin.HeadSHA(t)
	res, _, err := fx.Engine.Commit(context.Background(), fx.Ref,
		[]repo.CommitWrite{{Path: "specs/a.md", Content: []byte("a1"), BaseSHA: blobAt(t, fx, "specs/a.md")}}, nil, "same", nil, nil)
	if err != nil || res.Changed || res.CommitSHA != tip {
		t.Fatalf("Commit = %+v, %v; want unchanged at %s", res, err, tip)
	}
}

// The preconditions are checked inside the CAS loop: when origin advances
// between our fetch and our push, the retry re-checks against the new tip. A
// race on an unrelated path still commits; a race on the written path is a
// conflict.
func TestCommit_RechecksPreconditionsOnCASRetry(t *testing.T) {
	for _, tc := range []struct {
		name, racedPath string
		wantConflict    bool
	}{
		{"unrelated path", "specs/other.md", false},
		{"written path", "specs/a.md", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := NewFixture(t, map[string]string{"specs/a.md": "a1"})
			ctx := context.Background()
			base := blobAt(t, fx, "specs/a.md")
			mustHead(t, fx, "") // prime the mirror

			raced := false
			repo.SetExecHook(fx.Engine, func(args, _ []string) {
				if !raced && subcommand(args) == "push" {
					raced = true
					fx.Origin.Commit(t, map[string]string{tc.racedPath: "theirs"}, "raced")
				}
			})
			t.Cleanup(func() { repo.SetExecHook(fx.Engine, nil) })

			res, conflicts, err := fx.Engine.Commit(ctx, fx.Ref,
				[]repo.CommitWrite{{Path: "specs/a.md", Content: []byte("mine"), BaseSHA: base}}, nil, "mine", nil, nil)
			if tc.wantConflict {
				if !errors.Is(err, repo.ErrCommitConflict) || len(conflicts) != 1 || conflicts[0].Path != "specs/a.md" {
					t.Fatalf("Commit = %+v, %+v, %v; want one conflict on specs/a.md", res, conflicts, err)
				}
				return
			}
			if err != nil || !res.Changed {
				t.Fatalf("Commit = %+v, %v; want a commit on the raced tip", res, err)
			}
			if got := fx.Origin.FileAt(t, "main", "specs/other.md"); got != "theirs" {
				t.Fatalf("the raced commit was lost: %q", got)
			}
		})
	}
}

// CheckPreconditions: a write's "" baseSha means must not exist; a delete's
// "" means whatever is there, but the path must exist.
func TestCheckPreconditions(t *testing.T) {
	current := map[string]string{"a": "sha-a", "b": "sha-b"}
	for _, tc := range []struct {
		name    string
		writes  []repo.CommitWrite
		deletes []repo.CommitDelete
		want    []repo.Conflict
	}{
		{"all hold", []repo.CommitWrite{{Path: "a", BaseSHA: "sha-a"}, {Path: "new"}}, []repo.CommitDelete{{Path: "b"}}, nil},
		{"stale write", []repo.CommitWrite{{Path: "a", BaseSHA: "old"}}, nil, []repo.Conflict{{Path: "a", BaseSHA: "old", CurrentSHA: "sha-a"}}},
		{"create over existing", []repo.CommitWrite{{Path: "b"}}, nil, []repo.Conflict{{Path: "b", CurrentSHA: "sha-b"}}},
		{"edit of absent", []repo.CommitWrite{{Path: "x", BaseSHA: "sha-x"}}, nil, []repo.Conflict{{Path: "x", BaseSHA: "sha-x"}}},
		{"delete of absent", nil, []repo.CommitDelete{{Path: "x"}}, []repo.Conflict{{Path: "x"}}},
		{"stale delete", nil, []repo.CommitDelete{{Path: "a", BaseSHA: "old"}}, []repo.Conflict{{Path: "a", BaseSHA: "old", CurrentSHA: "sha-a"}}},
		{"delete pinned", nil, []repo.CommitDelete{{Path: "a", BaseSHA: "sha-a"}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := repo.CheckPreconditions(current, tc.writes, tc.deletes); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("CheckPreconditions = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// BlobSHA is git's blob object name.
func TestBlobSHA(t *testing.T) {
	// `git hash-object --stdin </dev/null`: the empty blob.
	if got := repo.BlobSHA(nil); got != "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" {
		t.Fatalf("BlobSHA(empty) = %s", got)
	}
	fx := NewFixture(t, map[string]string{"a.md": "hello\n"})
	if got := repo.BlobSHA([]byte("hello\n")); got != fx.Origin.BlobSHA(t, "a.md") {
		t.Fatalf("BlobSHA = %s, want git's %s", got, fx.Origin.BlobSHA(t, "a.md"))
	}
}
