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

// UNIT tier: the skills-manifest.json parse/render pair. Parsing is tolerant
// (a missing or corrupt manifest must never brick reads — spec §3); rendering
// is deterministic (sorted keys, trailing newline) so reconcile commits diff
// cleanly.
package spec

import (
	"bytes"
	"strings"
	"testing"
)

func TestSkillsManifest_ParseRenderRoundTrip(t *testing.T) {
	t.Parallel()
	m := SkillsManifest{
		"go":            {Origin: ManifestOriginPlatform, BaseHash: "ab12"},
		"agent-browser": {Origin: ManifestOriginImported, Source: "vercel-labs/agent-browser", BaseHash: "cd34"},
	}
	raw := renderSkillsManifest(m)
	got := parseSkillsManifest(raw)
	if len(got) != 2 || got["go"].BaseHash != "ab12" || got["agent-browser"].Source != "vercel-labs/agent-browser" {
		t.Fatalf("round trip lost data: %#v", got)
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatalf("render must end with a newline: %q", raw)
	}
	// Determinism: two renders of the same map are byte-identical.
	if !bytes.Equal(raw, renderSkillsManifest(m)) {
		t.Fatal("render is not deterministic")
	}
}

func TestSkillsManifest_ParseTolerant(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string][]byte{
		"nil":        nil,
		"empty":      {},
		"corrupt":    []byte("{not json"),
		"wrongShape": []byte(`[1,2,3]`),
	} {
		if got := parseSkillsManifest(raw); got == nil || len(got) != 0 {
			t.Fatalf("%s: want empty non-nil manifest, got %#v", name, got)
		}
	}
}

func TestDecideReconcile(t *testing.T) {
	t.Parallel()
	plat := func(base string) *ManifestEntry {
		return &ManifestEntry{Origin: ManifestOriginPlatform, BaseHash: base}
	}
	cases := []struct {
		name       string
		embedded   string
		repo       string
		repoExists bool
		entry      *ManifestEntry
		want       reconcileAction
	}{
		// No repo copy at all → seed (fresh org, or org deleted a platform copy).
		{"absent", "E1", "", false, nil, actionSeed},
		{"absent despite entry", "E1", "", false, plat("E1"), actionSeed},
		// Pre-manifest repo copy: no entry means the manifest is being created
		// now, so the platform is authoritative — BOTH the matching and the
		// diverged copy adopt the shipped content. The diverged case used to be
		// an override, which fabricated a baseline matching neither side and
		// surfaced later as an unresolvable conflict on untouched skills.
		{"backfill clean", "E1", "E1", true, nil, actionBackfill},
		{"backfill divergent adopts the shipped content", "E1", "X9", true, nil, actionBackfill},
		// Steady state, entry present.
		{"nothing moved", "E1", "E1", true, plat("E1"), actionSkip},
		{"platform moved, org clean", "E2", "E1", true, plat("E1"), actionRefresh},
		{"org moved, platform not", "E1", "X9", true, plat("E1"), actionOverride},
		{"both moved", "E2", "X9", true, plat("E1"), actionConflict},
		// Edit-then-revert returns to clean automatically (spec §3).
		{"reverted org edit", "E2", "E1", true, plat("E1"), actionRefresh},
		// Both moved but CONVERGED (org manually adopted the new platform
		// content) → auto-resolve: stamp the base, no conflict.
		{"converged", "E2", "E2", true, plat("E1"), actionBackfill},
		// A non-platform entry never participates (imported name — defensive).
		{"imported entry", "E1", "X9", true, &ManifestEntry{Origin: ManifestOriginImported, BaseHash: "X9"}, actionSkip},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := decideReconcile(c.embedded, c.repo, c.repoExists, c.entry); got != c.want {
				t.Fatalf("decideReconcile(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

func TestSkillsManifest_ReadsLegacyKindField(t *testing.T) {
	t.Parallel()
	// A manifest written before the rename uses "kind"; it must read as origin.
	raw := []byte(`{"go":{"kind":"platform","baseHash":"ab12"}}`)
	m := parseSkillsManifest(raw)
	if m["go"].Origin != ManifestOriginPlatform || m["go"].BaseHash != "ab12" {
		t.Fatalf("legacy kind field not read as origin: %#v", m["go"])
	}
}

func TestSkillsManifest_RendersOriginField(t *testing.T) {
	t.Parallel()
	raw := renderSkillsManifest(SkillsManifest{"go": {Origin: ManifestOriginPlatform, BaseHash: "ab12"}})
	if !strings.Contains(string(raw), `"origin": "platform"`) || strings.Contains(string(raw), `"kind"`) {
		t.Fatalf("render must emit origin, not kind: %s", raw)
	}
}
