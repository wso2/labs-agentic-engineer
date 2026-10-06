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

// Package skills is the org skill library as the pod reads it, and the
// project skill mirror built from it.
//
// KNOWN DUPLICATE: the catalog parse here (layout, frontmatter, manifest) is
// copied from aep-api's internal/spec (repo_store.go loadCatalog and
// parseBundleEntries, skill_service.go's frontmatter, skill_manifest.go).
// aep-api keeps its own parse for the skills library until a later phase
// moves the library; until then a change to either parse must be made in
// both.
package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// Skill is one catalog skill, with the fields the mirror needs.
type Skill struct {
	Name string
	// Kind is platform | org | imported (frontmatter metadata.aep.kind, or
	// the legacy kind directory); it only breaks a same-name tie.
	Kind    string
	SkillMD string
	// References holds every auxiliary file relative to the skill dir.
	References map[string]string
	// Enabled is the inverse of the skill's skills-manifest.json Disabled
	// flag (no entry, or no manifest, is enabled).
	Enabled bool
	// Audience is metadata.aep.audience filtered to design | coding; empty
	// in the source means both.
	Audience []string
}

// Skill audiences and kinds (aep-api spec/skill.go).
const (
	AudienceDesign = "design"
	AudienceCoding = "coding"

	kindPlatform = "platform"
	kindOrg      = "org"
	kindImported = "imported"
)

const (
	skillsRootDir      = "skills"
	skillFileName      = "SKILL.md"
	skillsManifestPath = "skills-manifest.json"
)

// legacyKindDirs maps the retired kind path segments (skills/<kindDir>/<name>/)
// to the current kinds; such repos still parse until reconcile migrates them.
var legacyKindDirs = map[string]string{
	"builtin":  kindOrg,
	"flow":     kindPlatform,
	"custom":   kindOrg,
	"imported": kindImported,
}

// BundleReader reads a repository's files at a commit.
type BundleReader interface {
	ReadBundle(ctx context.Context, ref repo.RepoRef, at string, keep func(rel string) bool) (files map[string]string, headSHA string, err error)
}

// LoadCatalog reads the skills repository at its default-branch tip (fetched)
// and parses its catalog: skills/ in both layouts plus skills-manifest.json,
// from the same commit. A read failure is an error, never an empty library:
// the mirror prunes on what this answers.
func LoadCatalog(ctx context.Context, r BundleReader, ref repo.RepoRef) ([]Skill, error) {
	keep := func(rel string) bool { return rel == skillsManifestPath || isCatalogPath(rel) }
	files, _, err := r.ReadBundle(ctx, ref, "", keep)
	if err != nil {
		return nil, fmt.Errorf("read skills bundle: %w", err)
	}
	manifest := parseSkillsManifest([]byte(files[skillsManifestPath]))
	delete(files, skillsManifestPath)
	out := parseBundleEntries(ctx, files)
	for i := range out {
		out[i].Enabled = !manifest[out[i].Name].Disabled
	}
	return out, nil
}

// isCatalogPath keeps every blob under skills/<...>/ at depth ≥ 3 with no
// dot-led segment; parseBundleEntries resolves the layout.
func isCatalogPath(rel string) bool {
	parts := strings.Split(rel, "/")
	if len(parts) < 3 || parts[0] != skillsRootDir {
		return false
	}
	for _, p := range parts[1:] {
		if strings.HasPrefix(p, ".") {
			return false
		}
	}
	return true
}

// manifestEntry is the one skills-manifest.json field the mirror reads.
type manifestEntry struct {
	Disabled bool `json:"disabled,omitempty"`
}

// parseSkillsManifest decodes the manifest tolerantly: absent or corrupt is
// empty (every skill enabled), with a warning for corrupt.
func parseSkillsManifest(raw []byte) map[string]manifestEntry {
	if len(raw) == 0 {
		return map[string]manifestEntry{}
	}
	var m map[string]manifestEntry
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		slog.Warn("skills.manifest_unparseable")
		return map[string]manifestEntry{}
	}
	return m
}

// parseBundleEntries turns a path → content bundle into the sorted, deduped
// skill set. Flat skills (skills/<name>/SKILL.md) read their kind from
// frontmatter; legacy ones (skills/<kindDir>/<name>/SKILL.md) from the dir.
// A same-name tie goes to the higher kindRank, then to the flat copy. A
// SKILL.md that does not parse is skipped with a warning.
func parseBundleEntries(ctx context.Context, files map[string]string) []Skill {
	type key struct{ legacyDir, name string }
	bodies := map[key]string{}
	refs := map[key]map[string]string{}

	// Pass 1: SKILL.md bodies (they disambiguate the ambiguous aux shape).
	for path, content := range files {
		parts := strings.Split(path, "/")
		if len(parts) < 3 || parts[0] != skillsRootDir {
			continue
		}
		switch {
		case len(parts) == 3 && parts[2] == skillFileName:
			bodies[key{"", parts[1]}] = content
		case len(parts) == 4 && legacyKindDirs[parts[1]] != "" && parts[3] == skillFileName:
			bodies[key{parts[1], parts[2]}] = content
		}
	}
	// Pass 2: aux files, keyed by their path under the skill root.
	// skills/<x>/... is a flat skill's file unless <x> is a legacy kind dir
	// with no flat body of that name.
	addRef := func(k key, refKey, content string) {
		if refs[k] == nil {
			refs[k] = map[string]string{}
		}
		refs[k][refKey] = content
	}
	for path, content := range files {
		parts := strings.Split(path, "/")
		if len(parts) < 3 || parts[0] != skillsRootDir {
			continue
		}
		_, flatBodyExists := bodies[key{"", parts[1]}]
		if legacyKindDirs[parts[1]] != "" && !flatBodyExists {
			if len(parts) < 4 {
				continue
			}
			k := key{parts[1], parts[2]}
			if refKey := strings.Join(parts[3:], "/"); refKey != skillFileName {
				addRef(k, refKey, content)
			}
			continue
		}
		k := key{"", parts[1]}
		if _, ok := bodies[k]; !ok {
			continue
		}
		if refKey := strings.Join(parts[2:], "/"); refKey != skillFileName {
			addRef(k, refKey, content)
		}
	}

	type entry struct {
		Skill
		legacyDir string
	}
	deduped := map[string]entry{}
	for k := range bodies {
		fm, err := parseSkillMD(bodies[k])
		if err != nil {
			slog.WarnContext(ctx, "skills.skill_unparseable", "name", k.name, "legacyDir", k.legacyDir)
			continue
		}
		kind := legacyKindDirs[k.legacyDir]
		if k.legacyDir == "" {
			kind = frontmatterKind(fm)
		}
		if existing, ok := deduped[k.name]; ok {
			if kindRank(existing.Kind) > kindRank(kind) {
				continue
			}
			if kindRank(existing.Kind) == kindRank(kind) && existing.legacyDir == "" {
				continue // same kind in both layouts: flat wins
			}
		}
		r := refs[k]
		if r == nil {
			r = map[string]string{}
		}
		deduped[k.name] = entry{
			Skill: Skill{
				Name: k.name, Kind: kind, SkillMD: bodies[k], References: r,
				Enabled: true, Audience: frontmatterAudience(fm),
			},
			legacyDir: k.legacyDir,
		}
	}

	out := make([]Skill, 0, len(deduped))
	for _, e := range deduped {
		out = append(out, e.Skill)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return kindRank(out[i].Kind) < kindRank(out[j].Kind)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// kindRank orders kinds for dedup: an imported skill owns its name over a
// same-named platform or org one.
func kindRank(kind string) int {
	switch kind {
	case kindOrg:
		return 0
	case kindPlatform:
		return 1
	case kindImported:
		return 2
	default:
		return 3
	}
}
