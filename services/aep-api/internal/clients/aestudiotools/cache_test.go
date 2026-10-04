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

package aestudiotools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// bundleServer answers every read-bundle with the commit sha and one file,
// counting the requests in hits.
func bundleServer(t *testing.T, sha string, hits *int) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		*hits++
		mu.Unlock()
		writeJSON(w, 200, `{"commitSha":"`+sha+`","files":{"specs/a.md":"# A"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestReadCache_OnlyImmutable(t *testing.T) {
	sha := strings.Repeat("b", 40)
	var hits int
	srv := bundleServer(t, sha, &hits) // replies {"commitSha": sha, "files": {...}}
	a := New(Config{Endpoints: fixedTarget(srv.URL, "ou"), Tokens: &countingTokens{}, HTTP: srv.Client(), CacheBytes: 1 << 20})
	ref := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "g"}
	_, _, _ = a.ReadBundle(context.Background(), ref, "", sourcecontrol.BundleFilter{Prefix: "specs/"})                         // head: hop, stored under sha
	_, _, _ = a.ReadBundle(context.Background(), ref, sha, sourcecontrol.BundleFilter{Prefix: "specs/"})                        // hit
	_, _, _ = a.ReadBundle(context.Background(), ref, sha, sourcecontrol.BundleFilter{Prefix: "specs/", Exts: []string{".md"}}) // other args: miss
	_, _, _ = a.ReadBundle(context.Background(), ref, "tags/v1", sourcecontrol.BundleFilter{Prefix: "specs/"})                  // tag: hop
	if hits != 3 {
		t.Fatalf("server hits = %d, want 3", hits)
	}
}

// The key is the canonical filter: exts and paths in any order hit, a
// different path set, repo or org misses.
func TestReadCache_KeyIsCanonical(t *testing.T) {
	sha := strings.Repeat("c", 40)
	var hits int
	srv := bundleServer(t, sha, &hits)
	a := New(Config{Endpoints: fixedTarget(srv.URL, "ou"), Tokens: &countingTokens{}, CacheBytes: 1 << 20})
	ctx := context.Background()
	ref := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "g", DefaultBranch: "main"}
	read := func(ref sourcecontrol.RepoRef, f sourcecontrol.BundleFilter) {
		t.Helper()
		if _, _, err := a.ReadBundle(ctx, ref, sha, f); err != nil {
			t.Fatal(err)
		}
	}
	read(ref, sourcecontrol.BundleFilter{Exts: []string{".md", ".yaml"}, Paths: []string{"b", "a"}})
	read(ref, sourcecontrol.BundleFilter{Exts: []string{".yaml", ".md"}, Paths: []string{"a", "b"}}) // same set: hit
	if hits != 1 {
		t.Fatalf("hits = %d, want 1 (exts/paths order is not part of the key)", hits)
	}
	read(ref, sourcecontrol.BundleFilter{Exts: []string{".md", ".yaml"}, Paths: []string{"a"}})
	other := ref
	other.Repo = "h"
	read(other, sourcecontrol.BundleFilter{Exts: []string{".md", ".yaml"}, Paths: []string{"a", "b"}})
	otherOrg := ref
	otherOrg.Org = "other"
	read(otherOrg, sourcecontrol.BundleFilter{Exts: []string{".md", ".yaml"}, Paths: []string{"a", "b"}})
	read(ref, sourcecontrol.BundleFilter{Prefix: "a", Exts: []string{"b"}})
	read(ref, sourcecontrol.BundleFilter{Prefix: "ab"}) // no delimiter collision with the previous key
	if hits != 6 {
		t.Fatalf("hits = %d, want 6 (paths, repo, org and filter each miss)", hits)
	}
}

// A non-sha answer is never stored, a cached answer is a copy, and the
// other cached ops (list-tree, read-file) key on their own op and args.
func TestReadCache_StoresOnlyUnderAShaAndHandsOutCopies(t *testing.T) {
	sha := strings.Repeat("d", 40)
	p, srv := newPodStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/bundle") && r.URL.Query().Get("prefix") == "odd/":
			writeJSON(w, 200, `{"commitSha":"not-a-sha","files":{"x":"1"}}`)
		case strings.HasSuffix(r.URL.Path, "/bundle"):
			writeJSON(w, 200, `{"commitSha":"`+sha+`","files":{"specs/a.md":"# A"}}`)
		case strings.HasSuffix(r.URL.Path, "/tree"):
			writeJSON(w, 200, `{"commitSha":"`+sha+`","entries":[{"path":"a","sha":"`+strings.Repeat("1", 40)+`","size":1}]}`)
		case strings.Contains(r.URL.Path, "/files/"):
			writeJSON(w, 200, `{"commitSha":"`+sha+`","path":"a","sha":"`+strings.Repeat("1", 40)+`","content":"QQ=="}`)
		case strings.HasSuffix(r.URL.Path, "/head"):
			writeJSON(w, 200, `{"sha":"`+sha+`"}`)
		}
	})
	a := New(Config{Endpoints: fixedTarget(srv.URL, "ou"), Tokens: &countingTokens{}})
	ctx := context.Background()
	ref := acmeGreeter

	for range 2 {
		_, _, _ = a.ReadBundle(ctx, ref, "", sourcecontrol.BundleFilter{Prefix: "odd/"})
	}
	files, _, _ := a.ReadBundle(ctx, ref, sha, sourcecontrol.BundleFilter{Prefix: "specs/"})
	files["specs/a.md"] = "mutated"
	again, _, _ := a.ReadBundle(ctx, ref, sha, sourcecontrol.BundleFilter{Prefix: "specs/"})
	if again["specs/a.md"] != "# A" {
		t.Fatalf("cached bundle was mutated through a returned map: %v", again)
	}
	for range 2 {
		if _, got, err := a.List(ctx, ref, sha); err != nil || got != sha {
			t.Fatalf("List = %s, %v", got, err)
		}
		content, _, err := a.ReadFile(ctx, ref, sha, "a")
		if err != nil || string(content) != "A" {
			t.Fatalf("ReadFile = %q, %v", content, err)
		}
		content[0] = 'Z'
		if _, err := a.Head(ctx, ref, sha); err != nil {
			t.Fatal(err)
		}
	}
	var bundles, trees, files2, heads int
	for _, r := range p.requests() {
		switch {
		case strings.HasSuffix(r.Path, "/bundle"):
			bundles++
		case strings.HasSuffix(r.Path, "/tree"):
			trees++
		case strings.Contains(r.Path, "/files/"):
			files2++
		case strings.HasSuffix(r.Path, "/head"):
			heads++
		}
	}
	if bundles != 3 || trees != 1 || files2 != 1 || heads != 2 {
		t.Fatalf("bundle=%d tree=%d file=%d head=%d, want 3 1 1 2 (non-sha never stored; head never cached)", bundles, trees, files2, heads)
	}
}

func TestReadCache_EvictsLeastRecentlyUsedByBytes(t *testing.T) {
	c := newReadCache(100)
	c.put("a", "A", 40)
	c.put("b", "B", 40)
	if _, ok := c.get("a"); !ok { // a is now the most recent
		t.Fatal("a missing")
	}
	c.put("c", "C", 40) // over 100: evicts b
	if _, ok := c.get("b"); ok {
		t.Fatal("b should be evicted")
	}
	if _, ok := c.get("a"); !ok {
		t.Fatal("a should stay")
	}
	c.put("huge", "H", 101) // larger than the cache: never stored
	if _, ok := c.get("huge"); ok {
		t.Fatal("an entry larger than the cache must not be stored")
	}
	if _, ok := c.get("c"); !ok {
		t.Fatal("c should stay")
	}
	c.put("c", "C2", 10) // replace keeps the size accounting right
	if v, _ := c.get("c"); v != "C2" || c.used != 52 {
		t.Fatalf("c=%v used=%d, want C2 and 52 (a: 40+1, c: 10+1)", v, c.used)
	}
}
