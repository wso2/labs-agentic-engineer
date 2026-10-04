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

package spec

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// TestStatusSnapshot_TwoHopsThenCache pins the poll's budget against the pod:
// exactly two reads that can move (the mirror's head and its tags, both
// local), and every other read addressed by a sha, which the adapter serves
// from its cache.
func TestStatusSnapshot_TwoHopsThenCache(t *testing.T) {
	ctx := context.Background()
	f := aestudiotest.New()
	ref := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}
	f.SeedRepo(ref, map[string]string{"specs/requirements/prd.md": "r", "specs/design/design.cell": "c"})
	// A version tag (its subject carries specTagSubject), then a later edit:
	// the poll must read the tag's tree at the tag's own sha.
	if err := f.Tag(ctx, ref, sourcecontrol.TagSpec{Name: "v1", Message: specTagSubject + "v1"}); err != nil {
		t.Fatal(err)
	}
	tagSHA, err := f.Head(ctx, ref, "tags/v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Commit(ctx, ref, sourcecontrol.CommitRequest{Message: "edit", Writes: []sourcecontrol.FileWrite{{Path: "specs/requirements/prd.md", Content: "r2", BaseSHA: blobSHA([]byte("r"))}}}); err != nil {
		t.Fatal(err)
	}
	seeded := len(f.Calls())
	svc := NewArtifactService(memRepos(t, "default", "p", "https://github.com/acme/greeter"), f)

	snap, err := svc.StatusSnapshot(ctx, "default", "p")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.HasSpec || !snap.HasDesign || snap.SpecVersion != "v1" || !snap.SpecDirty {
		t.Fatalf("snapshot = %+v, want spec, design, version v1 and dirty", snap)
	}
	mutable, atTag := 0, 0
	for _, c := range f.Calls()[seeded:] {
		if c.Op == aestudiotest.OpHead || c.Op == aestudiotest.OpListTags {
			mutable++
			if !c.Local {
				t.Errorf("%s must be local", c.Op)
			}
		}
		if c.At != "" && len(c.At) != 40 {
			t.Errorf("%s read at %q, want a sha", c.Op, c.At)
		}
		if c.At == tagSHA {
			atTag++
		}
	}
	if mutable != 2 {
		t.Fatalf("mutable hops = %d, want 2", mutable)
	}
	if atTag == 0 {
		t.Fatal("no read at the version tag's sha")
	}
}

func (r *rig) snapshot() *StatusSnapshot {
	r.t.Helper()
	snap, err := r.svc.StatusSnapshot(context.Background(), r.org, r.proj)
	if err != nil {
		r.t.Fatalf("StatusSnapshot: %v", err)
	}
	return snap
}

// TestStatusSnapshot_LadderAndDirty walks the derivation table's git column:
// presence predicates, version tag, dirtiness, and the legacy design-tag flag
// all from one snapshot.
func TestStatusSnapshot_LadderAndDirty(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"README.md": "# hi\n"})

	// Fresh project: nothing yet.
	snap := r.snapshot()
	if snap.HeadSHA == "" {
		t.Fatal("HeadSHA empty on a seeded repo")
	}
	if snap.HasSpec || snap.HasDesign || snap.SpecVersion != "" || snap.SpecDirty {
		t.Fatalf("fresh snapshot not zero: %+v", snap)
	}

	// Requirements land.
	r.seed(map[string]string{"specs/requirements/prd.md": "# req\n"}, "spec")
	snap = r.snapshot()
	if !snap.HasSpec || snap.HasDesign || snap.SpecVersion != "" {
		t.Fatalf("after spec: %+v, want HasSpec only", snap)
	}

	// A blank design.cell is NOT a design (the ReadDesign gate): presence alone
	// must not flip the flag.
	r.seed(map[string]string{"specs/design/design.cell": "  \n\n"}, "blank design")
	if snap = r.snapshot(); snap.HasDesign {
		t.Fatal("blank design.cell flagged HasDesign")
	}

	// Design + first version tag.
	r.seed(map[string]string{
		"specs/design/design.cell":                "# design\n",
		"specs/design/components/api/design.json": `{"type":"service"}`,
	}, "design")
	r.tag("v1", specTagSubject+"v1")
	snap = r.snapshot()
	if !snap.HasSpec || !snap.HasDesign {
		t.Fatalf("after design: %+v, want HasSpec+HasDesign", snap)
	}
	if snap.SpecVersion != "v1" || snap.SpecDirty {
		t.Fatalf("after tag: version=%q dirty=%v, want v1 clean", snap.SpecVersion, snap.SpecDirty)
	}
	// specs/ moves past the tag → dirty; a non-spec change must NOT dirty.
	r.seed(map[string]string{"README.md": "# hi again\n"}, "readme only")
	if snap = r.snapshot(); snap.SpecDirty {
		t.Fatal("non-spec commit flagged dirty")
	}
	r.seed(map[string]string{"specs/requirements/prd.md": "# req v2\n"}, "spec edit")
	snap = r.snapshot()
	if !snap.SpecDirty || snap.SpecVersion != "v1" {
		t.Fatalf("after spec edit: version=%q dirty=%v, want v1 dirty", snap.SpecVersion, snap.SpecDirty)
	}
}

// TestComponentCountAtTag pins the deploy denominator: components are counted
// at the addressed tag's tree (per-tag history preserved), and an unknown tag
// is an error — never a silent zero.
func TestComponentCountAtTag(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{
		"specs/requirements/prd.md":               "# req\n",
		"specs/design/design.cell":                "# design\n",
		"specs/design/components/api/design.json": `{"type":"service"}`,
		"specs/design/components/web/design.json": `{"type":"webapp"}`,
	})
	ctx := context.Background()
	r.tag("v1", specTagSubject+"v1")

	if n, err := r.svc.ComponentCountAtTag(ctx, r.org, r.proj, "v1"); err != nil || n != 2 {
		t.Fatalf("count at v1 = (%d, %v), want 2", n, err)
	}

	r.seed(map[string]string{"specs/design/components/db/design.json": `{"type":"database"}`}, "add db")
	r.tag("v2", specTagSubject+"v2")

	if n, err := r.svc.ComponentCountAtTag(ctx, r.org, r.proj, "v2"); err != nil || n != 3 {
		t.Fatalf("count at v2 = (%d, %v), want 3", n, err)
	}
	if n, err := r.svc.ComponentCountAtTag(ctx, r.org, r.proj, "v1"); err != nil || n != 2 {
		t.Fatalf("count at v1 after v2 = (%d, %v), want 2 (tag-addressed history)", n, err)
	}
	if _, err := r.svc.ComponentCountAtTag(ctx, r.org, r.proj, "v9"); err == nil {
		t.Fatal("unknown tag returned a count, want error (strict deploy stage)")
	}
}
