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

package gittest

// Smoke tests proving the harness end to end: clone/seed/tag over the file://
// remote. (The gitfs Workspace engine is exercised by internal/platform/gitfs's
// own suite.)

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// --- Remote: clone / seed / tag ---------------------------------------------

func TestRemote_CloneReflectsSeed(t *testing.T) {
	t.Parallel()
	r := NewRemote(t, WithSeed(map[string]string{
		"specs/requirements/main.md": "# hello",
	}, "seed"))

	clone := r.Clone(t)
	got, err := os.ReadFile(filepath.Join(clone, "specs", "requirements", "main.md"))
	if err != nil {
		t.Fatalf("read cloned file: %v", err)
	}
	if string(got) != "# hello" {
		t.Fatalf("cloned content = %q, want %q", got, "# hello")
	}
}

func TestRemote_SeedAndTagVisibleAfterFetch(t *testing.T) {
	t.Parallel()
	r := NewRemote(t, WithSeed(map[string]string{"README.md": "root"}, "seed"))

	newSHA := r.Seed(t, map[string]string{"specs/requirements/main.md": "reqs"}, "add reqs")
	r.Tag(t, "v1", "Requirements v1")

	if head := r.HeadSHA(t); head != newSHA {
		t.Fatalf("HeadSHA = %s, want seeded %s", head, newSHA)
	}
	if tags := r.Tags(t); len(tags) != 1 || tags[0] != "v1" {
		t.Fatalf("Tags = %v, want [v1]", tags)
	}
	if content := r.FileAt(t, "main", "specs/requirements/main.md"); content != "reqs" {
		t.Fatalf("FileAt(main) = %q, want %q", content, "reqs")
	}

	// A fresh clone sees the seeded commit and tag — i.e. they really landed in
	// the bare repo and propagate over the file:// protocol (as `git fetch`
	// would).
	clone := r.Clone(t)
	if _, err := os.Stat(filepath.Join(clone, "specs", "requirements", "main.md")); err != nil {
		t.Fatalf("seeded file missing in clone: %v", err)
	}
	tagsOut, err := runGit(clone, nil, nil, "tag", "-l")
	if err != nil {
		t.Fatalf("git tag -l in clone: %v", err)
	}
	if got := bytes.TrimSpace([]byte(tagsOut)); string(got) != "v1" {
		t.Fatalf("clone tags = %q, want v1", got)
	}
}

func TestRemote_RemoveDeletesPaths(t *testing.T) {
	t.Parallel()
	r := NewRemote(t, WithSeed(map[string]string{
		"specs/requirements/main.md": "keep",
		"specs/requirements/gone.md": "delete me",
	}, "seed"))

	r.Remove(t, "rm gone.md", "specs/requirements/gone.md")

	if _, err := r.exec(nil, nil, "cat-file", "blob", "main:specs/requirements/gone.md"); err == nil {
		t.Fatal("gone.md still present after Remove")
	}
	if got := r.FileAt(t, "main", "specs/requirements/main.md"); got != "keep" {
		t.Fatalf("survivor main.md = %q, want %q", got, "keep")
	}
}
