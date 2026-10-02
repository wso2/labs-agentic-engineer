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

package files_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/projects"
	"github.com/wso2/aep/ae-studio-tools/internal/projects/projectstest"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/repotest"
)

const testOrg = "acme"

type rig struct {
	reader   files.Reader
	origin   *repotest.Origin
	projects *projectstest.Fake
	root     string
}

// newRig serves project "greeter" from an origin seeded with seed. aep-api
// names the repo Acme/Greeter-App; the clone URL is the file:// origin.
func newRig(t *testing.T, seed map[string]string) *rig {
	t.Helper()
	origin := repotest.NewOrigin(t, seed)
	root := filepath.Join(t.TempDir(), "studio-data")
	engine, _, err := repo.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := projectstest.NewFake(map[string]projects.Repository{"greeter": {
		Owner: "Acme", Repo: "Greeter-App", DefaultBranch: repotest.Branch, CloneURL: origin.URL(),
	}})
	return &rig{
		reader:   files.Reader{Engine: engine, Projects: fake, Org: testOrg},
		origin:   origin,
		projects: fake,
		root:     root,
	}
}

var ctx = context.Background()

func TestReader_ListFiltersByPrefix(t *testing.T) {
	r := newRig(t, map[string]string{
		"specs/requirements/prd.md":    "req",
		"specs/design/domain-model.md": "des",
		"README.md":                    "root",
	})
	metas, err := r.reader.List(ctx, "greeter", "specs/design/")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].Path != "specs/design/domain-model.md" || metas[0].SHA == "" || metas[0].Size != 3 {
		t.Fatalf("list = %+v", metas)
	}
	all, err := r.reader.List(ctx, "greeter", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].Path != "README.md" || all[2].Path != "specs/requirements/prd.md" {
		t.Fatalf("list sorted by path = %+v", all)
	}
}

func TestReader_ReadAtGatesPathsAndRefs(t *testing.T) {
	r := newRig(t, map[string]string{
		"specs/requirements/prd.md":    "v1",
		"tests/acceptance/report.json": "{}",
		"src/main.go":                  "package main",
	})
	first := r.origin.Commit(t, map[string]string{})
	r.origin.Commit(t, map[string]string{"specs/requirements/prd.md": "v2"})

	got, err := r.reader.ReadAt(ctx, "greeter", "specs/requirements/prd.md", "")
	if err != nil || got.Content != "v2" || got.Path != "specs/requirements/prd.md" || got.SHA == "" {
		t.Fatalf("read at tip = %+v, %v", got, err)
	}
	pinned, err := r.reader.ReadAt(ctx, "greeter", "specs/requirements/prd.md", first)
	if err != nil || pinned.Content != "v1" {
		t.Fatalf("read at %s = %+v, %v", first, pinned, err)
	}
	if _, err := r.reader.ReadAt(ctx, "greeter", "tests/acceptance/report.json", ""); err != nil {
		t.Fatalf("allow-listed report: %v", err)
	}

	for _, c := range []struct {
		path, ref string
		want      error
	}{
		{"src/main.go", "", files.ErrPathInvalid},
		{"specs/../src/main.go", "", files.ErrPathInvalid},
		{"specs/requirements/prd.md", "main", files.ErrPathInvalid},
		{"specs/requirements/missing.md", "", files.ErrFileNotFound},
		{"specs/requirements", "", files.ErrFileNotFound}, // a directory is not a file
		{"specs/requirements/prd.md", "0123456789abcdef0123456789abcdef01234567", repo.ErrRefNotFound},
	} {
		if _, err := r.reader.ReadAt(ctx, "greeter", c.path, c.ref); !errors.Is(err, c.want) {
			t.Errorf("ReadAt(%q, %q) = %v, want %v", c.path, c.ref, err, c.want)
		}
	}
}

func TestReader_BundleIsOneCommitWithinTheGate(t *testing.T) {
	r := newRig(t, map[string]string{
		"specs/requirements/prd.md":    "req",
		"specs/design/domain-model.md": "des",
		"README.md":                    "readme",
		"src/main.go":                  "package main",
		"svc/workload.yaml":            "kind: Workload",
	})
	b, err := r.reader.Bundle(ctx, "greeter", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range b.Files {
		paths = append(paths, f.Path)
		if f.SHA == "" {
			t.Errorf("%s has no sha", f.Path)
		}
	}
	want := []string{"specs/design/domain-model.md", "specs/requirements/prd.md", "svc/workload.yaml"}
	if len(paths) != len(want) || paths[0] != want[0] || paths[1] != want[1] || paths[2] != want[2] {
		t.Fatalf("bundle paths = %v, want %v", paths, want)
	}

	narrowed, err := r.reader.Bundle(ctx, "greeter", "specs/design/", "")
	if err != nil || len(narrowed.Files) != 1 || narrowed.Files[0].Content != "des" {
		t.Fatalf("narrowed bundle = %+v, %v", narrowed, err)
	}

	before := b.CommitSHA
	r.origin.Commit(t, map[string]string{"specs/design/domain-model.md": "des v2"})
	pinned, err := r.reader.Bundle(ctx, "greeter", "specs/design/", before)
	if err != nil || pinned.CommitSHA != before || pinned.Files[0].Content != "des" {
		t.Fatalf("pinned bundle = %+v, %v", pinned, err)
	}
	tip, err := r.reader.Bundle(ctx, "greeter", "specs/design/", "")
	if err != nil || tip.CommitSHA == before || tip.Files[0].Content != "des v2" {
		t.Fatalf("tip bundle = %+v, %v", tip, err)
	}

	if _, err := r.reader.Bundle(ctx, "greeter", "", "HEAD~1"); !errors.Is(err, files.ErrPathInvalid) {
		t.Errorf("revision expression ref = %v, want ErrPathInvalid", err)
	}
	if _, err := r.reader.Bundle(ctx, "greeter", "", "0123456789abcdef0123456789abcdef01234567"); !errors.Is(err, repo.ErrRefNotFound) {
		t.Errorf("unknown ref = %v, want ErrRefNotFound", err)
	}
}

// The project is resolved through aep-api on every call, and an unknown or
// unresolvable project never reaches git.
func TestReader_ResolvesEveryCallAndFailsClosed(t *testing.T) {
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "req"})

	for i := 0; i < 2; i++ {
		if _, err := r.reader.ReadAt(ctx, "greeter", "specs/requirements/prd.md", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.reader.List(ctx, "greeter", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.reader.Bundle(ctx, "greeter", "", ""); err != nil {
		t.Fatal(err)
	}
	if n := r.projects.CallCount(); n != 4 {
		t.Fatalf("resolver calls = %d, want 4 (one per call)", n)
	}
	// The clone sits under the pod's org and a slug derived from aep-api's
	// owner/repo, never from the clone URL.
	if _, err := os.Stat(filepath.Join(repo.ReposDir(r.root), testOrg, "greeter", "acme-greeter-app", "git")); err != nil {
		t.Fatalf("clone location: %v", err)
	}

	if _, err := r.reader.ReadAt(ctx, "nope", "specs/requirements/prd.md", ""); !errors.Is(err, projects.ErrUnknown) {
		t.Errorf("unknown project = %v, want ErrUnknown", err)
	}
	r.projects.SetErr(projects.ErrUnavailable)
	if _, err := r.reader.List(ctx, "greeter", ""); !errors.Is(err, projects.ErrUnavailable) {
		t.Errorf("aep-api down = %v, want ErrUnavailable", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(repo.ReposDir(r.root), testOrg)); len(entries) != 1 {
		t.Fatalf("a refused project reached git: %v", entries)
	}
}

// A path check runs before the project is resolved: an invalid read costs no
// aep-api call.
func TestReader_InvalidPathCostsNoLookup(t *testing.T) {
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "req"})
	if _, err := r.reader.ReadAt(ctx, "greeter", "src/main.go", ""); !errors.Is(err, files.ErrPathInvalid) {
		t.Fatal(err)
	}
	if n := r.projects.CallCount(); n != 0 {
		t.Fatalf("resolver calls = %d, want 0", n)
	}
}
