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
	"slices"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// componentWithResource is a component design.json declaring one platform
// resource.
func componentWithResource(name, resource string) string {
	return `{"name":"` + name + `","type":"service","dependencies":[` +
		`{"kind":"platform-resource","name":"` + resource + `","resourceType":"redis"}]}`
}

// bundleReads answers the read-bundle calls f recorded, in order.
func bundleReads(f *aestudiotest.Fake) []aestudiotest.Call {
	var out []aestudiotest.Call
	for _, c := range f.Calls() {
		if c.Op == aestudiotest.OpReadBundle {
			out = append(out, c)
		}
	}
	return out
}

// TestVersionFacts_OneBundlePerSha pins the design read behind the resource
// rows: ONE component bundle per commit (never a list plus a read per
// design.json), addressed by the commit's sha so the adapter's cache serves
// it, filtered to the components' JSON.
func TestVersionFacts_OneBundlePerSha(t *testing.T) {
	ctx := context.Background()
	f := aestudiotest.New()
	ref := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}
	f.SeedRepo(ref, map[string]string{
		"specs/requirements/prd.md":                    "# prd",
		"specs/design/design.cell":                     "cell",
		"specs/design/components/orders/design.json":   componentWithResource("orders", "orders-cache"),
		"specs/design/components/payments/design.json": componentWithResource("payments", "payments-db"),
		"specs/design/components/web/design.json":      componentWithResource("web", "web-sessions"),
	})
	svc := NewArtifactService(memRepos(t, "default", "p", "https://github.com/acme/greeter"), f)
	head, err := f.Head(ctx, ref, "")
	if err != nil {
		t.Fatal(err)
	}

	facts, err := svc.BuildVersionFacts(ctx, "default", "p")
	if err != nil {
		t.Fatal(err)
	}
	rows := factRows(t, facts)
	for _, name := range []string{"orders-cache", "payments-db", "web-sessions"} {
		if got := rows[name]; got.Kind != VersionChangeKindResource || got.State != VersionChangeNew {
			t.Errorf("row %s = %+v, want a new platform resource", name, got)
		}
	}

	reads := bundleReads(f)
	if len(reads) != 1 {
		t.Fatalf("read-bundle calls = %d, want exactly 1 (one per sha): %+v", len(reads), reads)
	}
	c := reads[0]
	if c.At != head {
		t.Errorf("bundle read at %q, want the head sha %q", c.At, head)
	}
	if c.Filter.Prefix != "specs/design/components/" || !slices.Equal(c.Filter.Exts, []string{".json"}) || len(c.Filter.Paths) != 0 {
		t.Errorf("bundle filter = %+v, want prefix specs/design/components/ and exts [.json]", c.Filter)
	}
	for _, call := range f.Calls() {
		if call.Op == aestudiotest.OpReadFile {
			t.Errorf("a per-file read at %q: the design must come from the bundle", call.At)
		}
	}
}

// TestVersionFacts_OneBundlePerShaAcrossAVersion: with a version cut and the
// design moved on, the two designs compared are one bundle each, at the
// version's commit and at the head.
func TestVersionFacts_OneBundlePerShaAcrossAVersion(t *testing.T) {
	ctx := context.Background()
	f := aestudiotest.New()
	ref := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}
	f.SeedRepo(ref, map[string]string{
		"specs/design/components/orders/design.json": componentWithResource("orders", "orders-cache"),
	})
	if err := f.Tag(ctx, ref, sourcecontrol.TagSpec{Name: "m1", Message: specTagSubject + "m1"}); err != nil {
		t.Fatal(err)
	}
	tagged, _ := f.Head(ctx, ref, "")
	_, blob, err := f.ReadFile(ctx, ref, "", "specs/design/components/orders/design.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.Commit(ctx, ref, sourcecontrol.CommitRequest{Message: "move", Writes: []sourcecontrol.FileWrite{{
		Path: "specs/design/components/orders/design.json", Content: componentWithResource("orders", "orders-queue"), BaseSHA: blob,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewArtifactService(memRepos(t, "default", "p", "https://github.com/acme/greeter"), f)

	facts, err := svc.BuildVersionFacts(ctx, "default", "p")
	if err != nil {
		t.Fatal(err)
	}
	rows := factRows(t, facts)
	if rows["orders-queue"].State != VersionChangeNew || rows["orders-cache"].State != VersionChangeRemoved {
		t.Errorf("resource rows = %+v, want orders-queue new and orders-cache removed", facts.Changes)
	}

	var ats []string
	for _, c := range bundleReads(f) {
		ats = append(ats, c.At)
	}
	slices.Sort(ats)
	want := []string{tagged, res.CommitSHA}
	slices.Sort(want)
	if !slices.Equal(ats, want) {
		t.Fatalf("bundle reads at %v, want one at each of %v", ats, want)
	}
}
