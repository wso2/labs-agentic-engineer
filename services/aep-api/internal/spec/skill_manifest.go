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

// skills-manifest.json — the per-org baseline memory (spec §3). A
// platform-managed sidecar at the org-skills repo ROOT recording, per
// non-org-authored skill, the content it was last handed at: platform-shipped
// skills get origin "platform" (whatever their frontmatter kind — the manifest
// origin is provenance, the frontmatter kind is a human ownership label),
// imports get origin "imported" + their source. baseHash is contentSHA output
// (bare hex). An entry also OUTLIVES its files as a tombstone (removed:true)
// when the org deletes the skill, so "the org threw this away" stays
// distinguishable from "never offered". A
// skill with NO entry is org-authored or never-offered: reconcile never
// touches an org-authored name, and seeds a never-offered default. Parsing
// is tolerant — a corrupt manifest must never brick reads; the next
// reconcile rewrites it. Rendering is deterministic (encoding/json sorts map
// keys) so commits diff cleanly.

import (
	"encoding/json"
	"log/slog"
)

// skillsManifestPath is the manifest's repo-relative path — at the repo root,
// deliberately OUTSIDE skills/ so isCatalogPath never surfaces it as a skill.
const skillsManifestPath = "skills-manifest.json"

// Manifest provenance origins. Distinct vocabulary from the frontmatter skill
// kinds: "platform" covers every embedded-library skill (frontmatter platform
// AND org kinds alike).
const (
	ManifestOriginPlatform = "platform"
	ManifestOriginImported = "imported"
)

// ManifestEntry is one skill's baseline: what it is, where it came from, and
// the contentSHA it was last handed at. Removed marks a TOMBSTONE — the org
// deleted this name, so it is never handed back (see the Removed doc below).
type ManifestEntry struct {
	Origin   string `json:"origin"`
	Source   string `json:"source,omitempty"`
	BaseHash string `json:"baseHash"`

	// Disabled withholds the skill from the agents (catalog + project-repo
	// copies) without touching SKILL.md — availability must never alter
	// contentSHA, or a disabled skill would read as a divergence from the
	// platform baseline and surface as a pending platform update. Stored as the
	// NEGATIVE so an entry written before this field existed, and every newly
	// seeded entry, is enabled by its zero value. ADR-0014. Distinct from
	// Removed below: disabled means the files are present but withheld,
	// removed means the org threw the files away.
	Disabled bool `json:"disabled,omitempty"`

	// Removed records that the org deleted this skill. The entry outlives the
	// files precisely so reconcile can tell "the org threw this away" apart
	// from "this org has never been offered it" — a name with NO entry is the
	// latter, and gets seeded like any other default. Without the tombstone
	// the two states are identical (delete used to drop the entry), which is
	// why seeding an absent org-kind default had to be refused wholesale and
	// no new org-kind default could ever reach an existing org.
	Removed bool `json:"removed,omitempty"`
}

// UnmarshalJSON tolerates the pre-rename "kind" field (manifests written
// before the origin rename) by mapping it onto Origin when "origin" is
// absent.
func (e *ManifestEntry) UnmarshalJSON(b []byte) error {
	var raw struct {
		Origin   string `json:"origin"`
		Kind     string `json:"kind"`
		Source   string `json:"source"`
		BaseHash string `json:"baseHash"`
		Disabled bool   `json:"disabled"`
		Removed  bool   `json:"removed"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	e.Origin = raw.Origin
	if e.Origin == "" {
		e.Origin = raw.Kind // legacy
	}
	e.Source = raw.Source
	e.BaseHash = raw.BaseHash
	e.Disabled = raw.Disabled
	e.Removed = raw.Removed
	return nil
}

// SkillsManifest maps skill name → baseline entry.
type SkillsManifest map[string]ManifestEntry

// parseSkillsManifest decodes raw bytes into a manifest. Nil/empty/corrupt
// input yields an empty (non-nil) manifest with a warning — never an error.
func parseSkillsManifest(raw []byte) SkillsManifest {
	if len(raw) == 0 {
		return SkillsManifest{}
	}
	var m SkillsManifest
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		slog.Warn("skills: manifest unparseable — treating as empty", "error", err)
		return SkillsManifest{}
	}
	return m
}

// renderSkillsManifest encodes the manifest deterministically: 2-space
// indent, sorted keys (encoding/json sorts map keys), trailing newline.
func renderSkillsManifest(m SkillsManifest) []byte {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		// A map[string]struct of strings cannot fail to marshal; guard anyway.
		slog.Warn("skills: manifest render failed", "error", err)
		return []byte("{}\n")
	}
	return append(raw, '\n')
}

// reconcileAction is the per-skill outcome of the three-way compare (spec §3):
// two questions — did the org change their copy (repo vs baseHash)? did the
// platform change theirs (embedded vs baseHash)? — four answers, plus the
// seed and pre-manifest backfill cases.
type reconcileAction int

const (
	actionSkip     reconcileAction = iota // nothing to do
	actionSeed                            // no repo copy: write files + stamp baseHash
	actionRefresh                         // org clean, platform moved: write files + advance baseHash
	actionBackfill                        // pre-manifest copy: adopt the shipped content + stamp baseHash
	actionOverride                        // org moved, platform not: leave alone
	actionConflict                        // both moved: leave alone, surface for review
)

// decideReconcile is the pure three-way decision for ONE embedded skill.
// entry is the skill's manifest entry (nil = pre-manifest or org-authored);
// repoSHA is the org copy's contentSHA ("" when repoExists is false).
func decideReconcile(embeddedSHA, repoSHA string, repoExists bool, entry *ManifestEntry) reconcileAction {
	if !repoExists {
		return actionSeed
	}
	if entry == nil {
		// Migration backfill: no entry means the manifest is being created for
		// this skill RIGHT NOW, so there is no recorded agreement to reason
		// from — the platform is authoritative at that moment. Adopt the
		// shipped content and stamp the baseline from the same bytes.
		//
		// This supersedes #293's "a diverged pre-manifest copy is an override,
		// never clobbered". That protected a pre-manifest org edit, but it
		// could not tell one from a merely STALE copy, and it recorded the
		// EMBEDDED sha as the baseline of a copy the org did not have — a
		// third value matching neither side. Every later platform release then
		// read as "both moved" and surfaced an unresolvable conflict on skills
		// nobody had touched. A one-time adoption is visible in the org repo's
		// git history; a fabricated baseline is visible nowhere.
		//
		// Only the MIGRATION is affected: once an entry exists, a genuine org
		// edit is still actionOverride/actionConflict and is never clobbered.
		return actionBackfill
	}
	if entry.Origin != ManifestOriginPlatform {
		return actionSkip // imported-owned name: reconcile never manages it
	}
	orgMoved := repoSHA != entry.BaseHash
	platformMoved := embeddedSHA != entry.BaseHash
	switch {
	case !orgMoved && !platformMoved:
		return actionSkip
	case !orgMoved && platformMoved:
		return actionRefresh
	case orgMoved && !platformMoved:
		return actionOverride
	case repoSHA == embeddedSHA:
		// Both moved but converged (org manually adopted the new platform
		// content): auto-resolve — stamp the base, no conflict.
		return actionBackfill
	default:
		return actionConflict
	}
}
