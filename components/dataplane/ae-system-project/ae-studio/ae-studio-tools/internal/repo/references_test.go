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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

var greeterRefs = repo.OwnerRepo{Owner: "acme", Repo: "greeter"}

func TestReferenceStoreDir(t *testing.T) {
	got, err := repo.ReferenceStoreDir("/data", repo.OwnerRepo{Owner: "Acme", Repo: "Greeter"})
	if err != nil || got != "/data/references/acme/greeter" {
		t.Fatalf("ReferenceStoreDir = %q, %v (owner/repo are case-insensitive on GitHub)", got, err)
	}
	for _, bad := range []repo.OwnerRepo{{"", "r"}, {"o", ""}, {"..", "r"}, {"o", "a/b"}} {
		if _, err := repo.ReferenceStoreDir("/data", bad); err == nil {
			t.Errorf("ReferenceStoreDir accepted %+v", bad)
		}
	}
}

func TestPutAndListReferences(t *testing.T) {
	e := NewEngine(t, nil)
	ctx := context.Background()

	if names, err := e.ListReferences(ctx, greeterRefs); err != nil || names != nil {
		t.Fatalf("no store: ListReferences = %v, %v; want nil, nil", names, err)
	}
	if err := e.PutReferences(ctx, greeterRefs, []repo.ReferenceDoc{
		{Name: "z.md", Content: []byte("z")},
		{Name: "a.pdf", Content: []byte("%PDF")},
	}); err != nil {
		t.Fatalf("PutReferences: %v", err)
	}
	names, err := e.ListReferences(ctx, greeterRefs)
	if err != nil || !reflect.DeepEqual(names, []string{"a.pdf", "z.md"}) {
		t.Fatalf("ListReferences = %v, %v; want sorted [a.pdf z.md]", names, err)
	}

	// Replace, not merge.
	if err := e.PutReferences(ctx, greeterRefs, []repo.ReferenceDoc{{Name: "b.txt", Content: []byte("b")}}); err != nil {
		t.Fatalf("PutReferences(replace): %v", err)
	}
	if names, _ := e.ListReferences(ctx, greeterRefs); !reflect.DeepEqual(names, []string{"b.txt"}) {
		t.Fatalf("after replace = %v, want [b.txt]", names)
	}

	// Links and names PutReferences could not have written are never listed.
	dir, _ := repo.ReferenceStoreDir(e.Root(), greeterRefs)
	if err := os.Symlink("/etc/hosts", filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if names, _ := e.ListReferences(ctx, greeterRefs); !reflect.DeepEqual(names, []string{"b.txt"}) {
		t.Fatalf("with planted entries = %v, want [b.txt]", names)
	}
}

func TestPutReferencesRejects(t *testing.T) {
	e := NewEngine(t, nil)
	ctx := context.Background()
	eleven := make([]repo.ReferenceDoc, repo.MaxReferenceCount+1)
	for i := range eleven {
		eleven[i] = repo.ReferenceDoc{Name: "d" + string(rune('a'+i)) + ".md"}
	}
	for name, docs := range map[string][]repo.ReferenceDoc{
		"traversal":    {{Name: "../x.md"}},
		"hidden":       {{Name: ".x.md"}},
		"office":       {{Name: "brief.docx"}},
		"no extension": {{Name: "README"}},
		"too large":    {{Name: "big.md", Content: bytes.Repeat([]byte("x"), repo.MaxReferenceBytes+1)}},
		"duplicate":    {{Name: "a.md"}, {Name: "a.md"}},
		"too many":     eleven,
	} {
		if err := e.PutReferences(ctx, greeterRefs, docs); !errors.Is(err, repo.ErrReferenceRejected) {
			t.Errorf("%s: err = %v, want ErrReferenceRejected", name, err)
		}
	}
	e.SetDiskUsagePct(90)
	if err := e.PutReferences(ctx, greeterRefs, []repo.ReferenceDoc{{Name: "a.md"}}); !errors.Is(err, repo.ErrDiskAdmission) {
		t.Fatalf("at 90%%: err = %v, want ErrDiskAdmission", err)
	}
	if names, _ := e.ListReferences(ctx, greeterRefs); names != nil {
		t.Fatalf("a rejected upload stored %v", names)
	}
}

func readManifest(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".aep-references.json"))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	var m struct {
		Written []string `json:"written"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return m.Written
}

// The overlay copies the stored documents into the snapshot's
// specs/requirements/references/ with a manifest of what it wrote; a later
// overlay retires a dropped name, and restores a committed file it had masked.
func TestOverlayReferences(t *testing.T) {
	fx := NewFixture(t, map[string]string{
		"specs/requirements/prd.md":            "req\n",
		"specs/requirements/references/old.md": "committed\n",
	})
	ctx := context.Background()
	sha := mustHead(t, fx, "")
	dir, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha)
	if err != nil {
		t.Fatal(err)
	}
	refsDir := filepath.Join(dir, repo.ReferenceOverlayDir)

	// Nothing stored: the snapshot stays exactly as git produced it.
	fx.Engine.OverlayReferences(ctx, fx.Ref, defaultProject, sha)
	if _, err := os.Stat(filepath.Join(refsDir, ".aep-references.json")); !os.IsNotExist(err) {
		t.Fatalf("manifest written with nothing stored (err=%v)", err)
	}

	if err := fx.Engine.PutReferences(ctx, fx.Ref.OwnerRepo(), []repo.ReferenceDoc{
		{Name: "brief.pdf", Content: []byte("%PDF-1")},
		{Name: "old.md", Content: []byte("uploaded\n")},
	}); err != nil {
		t.Fatal(err)
	}
	fx.Engine.OverlayReferences(ctx, fx.Ref, defaultProject, sha)
	for name, want := range map[string]string{"brief.pdf": "%PDF-1", "old.md": "uploaded\n"} {
		b, err := os.ReadFile(filepath.Join(refsDir, name))
		if err != nil || string(b) != want {
			t.Fatalf("%s = (%q, %v), want %q", name, b, err, want)
		}
	}
	if got := readManifest(t, refsDir); !reflect.DeepEqual(got, []string{"brief.pdf", "old.md"}) {
		t.Fatalf("manifest = %v", got)
	}

	// A replacement that drops both: brief.pdf is removed, old.md (committed
	// at this sha) is restored to its git blob.
	if err := fx.Engine.PutReferences(ctx, fx.Ref.OwnerRepo(), []repo.ReferenceDoc{{Name: "new.txt", Content: []byte("n")}}); err != nil {
		t.Fatal(err)
	}
	fx.Engine.OverlayReferences(ctx, fx.Ref, defaultProject, sha)
	if _, err := os.Stat(filepath.Join(refsDir, "brief.pdf")); !os.IsNotExist(err) {
		t.Fatalf("dropped brief.pdf still overlaid (err=%v)", err)
	}
	if b, _ := os.ReadFile(filepath.Join(refsDir, "old.md")); string(b) != "committed\n" {
		t.Fatalf("old.md = %q, want the committed blob restored", b)
	}
	if b, _ := os.ReadFile(filepath.Join(refsDir, "new.txt")); string(b) != "n" {
		t.Fatalf("new.txt = %q", b)
	}
	if got := readManifest(t, refsDir); !reflect.DeepEqual(got, []string{"new.txt"}) {
		t.Fatalf("manifest = %v, want [new.txt]", got)
	}
	// No staging debris beside the overlaid files.
	entries, _ := os.ReadDir(refsDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".reference-") || strings.HasPrefix(e.Name(), ".restore-") || strings.HasPrefix(e.Name(), ".manifest-") {
			t.Fatalf("staging debris %s", e.Name())
		}
	}
}

// An overlay into a snapshot that does not exist is a no-op: it never
// creates one.
func TestOverlayReferencesNeedsSnapshot(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	sha := mustHead(t, fx, "")
	if err := fx.Engine.PutReferences(ctx, fx.Ref.OwnerRepo(), []repo.ReferenceDoc{{Name: "a.md", Content: []byte("a")}}); err != nil {
		t.Fatal(err)
	}
	fx.Engine.OverlayReferences(ctx, fx.Ref, defaultProject, sha)
	if _, err := os.Stat(projectSnapshotDir(t, fx, sha)); !os.IsNotExist(err) {
		t.Fatalf("overlay created a snapshot (err=%v)", err)
	}
}
