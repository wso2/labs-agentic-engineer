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
)

// freshen forces one fetch-bearing read so the engine mirror catches up with
// the origin — the tests' stand-in for "the platform wrote this through the
// engine" (real platform writes update the mirror synchronously).
func (r *rig) freshen() {
	r.t.Helper()
	if _, err := r.svc.ListSpecVersionTags(context.Background(), r.org, r.proj); err != nil {
		r.t.Fatalf("freshen read: %v", err)
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
	if snap.HasSpec || snap.HasDesign || snap.SpecVersion != "" || snap.SpecDirty || snap.HasDesignTag {
		t.Fatalf("fresh snapshot not zero: %+v", snap)
	}

	// Requirements land.
	r.seed(map[string]string{"specs/requirements/prd.md": "# req\n"}, "spec")
	r.freshen()
	snap = r.snapshot()
	if !snap.HasSpec || snap.HasDesign || snap.SpecVersion != "" {
		t.Fatalf("after spec: %+v, want HasSpec only", snap)
	}

	// A blank design.cell is NOT a design (the ReadDesign gate): presence alone
	// must not flip the flag.
	r.seed(map[string]string{"specs/design/design.cell": "  \n\n"}, "blank design")
	r.freshen()
	if snap = r.snapshot(); snap.HasDesign {
		t.Fatal("blank design.cell flagged HasDesign")
	}

	// Design + first version tag.
	r.seed(map[string]string{
		"specs/design/design.cell":                "# design\n",
		"specs/design/components/api/design.json": `{"type":"service"}`,
	}, "design")
	r.tag("v1", "spec v1")
	r.freshen()
	snap = r.snapshot()
	if !snap.HasSpec || !snap.HasDesign {
		t.Fatalf("after design: %+v, want HasSpec+HasDesign", snap)
	}
	if snap.SpecVersion != "v1" || snap.SpecDirty {
		t.Fatalf("after tag: version=%q dirty=%v, want v1 clean", snap.SpecVersion, snap.SpecDirty)
	}
	if snap.HasDesignTag {
		t.Fatal("HasDesignTag true without a v<N>-<M> tag")
	}

	// specs/ moves past the tag → dirty; a non-spec change must NOT dirty.
	r.seed(map[string]string{"README.md": "# hi again\n"}, "readme only")
	r.freshen()
	if snap = r.snapshot(); snap.SpecDirty {
		t.Fatal("non-spec commit flagged dirty")
	}
	r.seed(map[string]string{"specs/requirements/prd.md": "# req v2\n"}, "spec edit")
	r.freshen()
	snap = r.snapshot()
	if !snap.SpecDirty || snap.SpecVersion != "v1" {
		t.Fatalf("after spec edit: version=%q dirty=%v, want v1 dirty", snap.SpecVersion, snap.SpecDirty)
	}

	// Legacy v<N>-<M> design tag flips the flat designStatus flag.
	r.tag("v1-1", "design rev")
	r.freshen()
	if snap = r.snapshot(); !snap.HasDesignTag {
		t.Fatal("HasDesignTag false though v1-1 exists")
	}
}

// TestStatusSnapshot_ServesMirrorWithoutFetch pins the poll-path budget: the
// snapshot never fetches, so out-of-band origin movement (commit + tag) stays
// invisible until some fetch-bearing operation runs.
func TestStatusSnapshot_ServesMirrorWithoutFetch(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "# req\n"})
	r.freshen() // prime the mirror

	before := r.snapshot()

	// Origin moves out-of-band: new spec content and a version tag.
	r.seed(map[string]string{"specs/requirements/prd.md": "# req v2\n"}, "oob edit")
	r.tag("v1", "oob tag")

	after := r.snapshot()
	if after.HeadSHA != before.HeadSHA {
		t.Fatalf("snapshot head moved %s → %s without a fetch-bearing op", before.HeadSHA, after.HeadSHA)
	}
	if after.SpecVersion != "" {
		t.Fatalf("snapshot saw un-fetched tag %q", after.SpecVersion)
	}

	// A fetch-bearing read catches the mirror up; the snapshot then sees both.
	r.freshen()
	synced := r.snapshot()
	if synced.SpecVersion != "v1" || synced.HeadSHA == before.HeadSHA {
		t.Fatalf("post-freshen snapshot = %+v, want v1 at the new head", synced)
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
	r.tag("v1", "spec v1")
	r.freshen()

	if n, err := r.svc.ComponentCountAtTag(ctx, r.org, r.proj, "v1"); err != nil || n != 2 {
		t.Fatalf("count at v1 = (%d, %v), want 2", n, err)
	}

	r.seed(map[string]string{"specs/design/components/db/design.json": `{"type":"database"}`}, "add db")
	r.tag("v2", "spec v2")
	r.freshen()

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
