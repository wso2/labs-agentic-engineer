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

package skills

// Repo-backed skills store. The per-org `org-skills` GitHub repo is the
// single source of truth (docs/design/skills-repo-storage.md); the store
// reads and writes it through the Workspace port over the shared-volume
// mirror (docs/design/shared-volume-clone-architecture.md, Phase 1):
//
//   - reads are one ReadBundle at the branch tip (fetch + local plumbing —
//     REST-parity freshness; no cache tier);
//   - writes are one Mutate commit to `main`; Mutate owns the CAS retry
//     (no per-feature retry wrapper).
//
// The embedded library (platform + org kinds) is seeded + content-reconciled
// from the vendored container files (reconcile.go). The exported read surface
// (Resolve/List/ListSummaries) is IDENTICAL to the previous store, so the
// architect and tech-lead resolvers consume it unchanged.

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/models"
)

const (
	// SkillsRepoName is the stable per-org GitHub repo name. §10.1.
	SkillsRepoName = "org-skills"
	// SkillsRepoProject is the sentinel project_id under which the skills repo
	// row lives in git_repositories (distinguishes it from project repos). §10.1.
	SkillsRepoProject = models.SkillsRepoSentinelProjectID

	skillsRootDir = "skills"
	skillFileName = "SKILL.md"
	refsPrefix    = "references/"
)

// legacyKindDirs maps the RETIRED kind path-segments (skills/<kindDir>/<name>/,
// the pre-flat layout) to the current kind vocabulary. Repos that predate the
// flat layout still parse through this table until their first reconcile
// migrates them (docs/design/skills-unified-library-migration.md §4). In the
// flat layout (skills/<name>/) a skill's kind lives in its frontmatter
// (`metadata.aep.kind`; absent → org).
var legacyKindDirs = map[string]string{
	"builtin":  models.SkillKindOrg,
	"flow":     models.SkillKindPlatform,
	"custom":   models.SkillKindCustom,
	"imported": models.SkillKindImported,
}

// SkillService is the repo-backed read/reconcile surface for skills. It also
// holds the low-level git read/write primitives that the mutation + import
// services and the reconciler compose with.
type SkillService struct {
	git   gitrepo.GitOpsService
	repos gitrepo.RepoService
	// provLocks serialises first-time provisioning per org. The gitfs flock +
	// origin push-CAS make concurrent WRITES safe, but they cannot make the
	// first page load deterministic: EnsureBareRepo creates the GitHub repo
	// and inserts the row BEFORE the seed commit lands, so an unguarded
	// concurrent reader could observe the row and list a still-empty repo (and
	// two concurrent provisions would both hit the GitHub create API —
	// adopt-on-conflict keeps that correct, but noisy). Uncontended ~1ms row
	// lookup once the repo exists.
	provLocks sync.Map // orgID → *sync.Mutex
}

func (s *SkillService) orgLock(orgID string) *sync.Mutex {
	v, _ := s.provLocks.LoadOrStore(orgID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// NewSkillService wires the repo-backed store. `git` provides the Workspace
// engine, credential resolver, and save identities; `repos` provisions/looks
// up the per-org skills repo row. Either may be nil in degraded/test boot
// (reads then return empty).
func NewSkillService(git gitrepo.GitOpsService, repos gitrepo.RepoService) *SkillService {
	return &SkillService{git: git, repos: repos}
}

// ---- read surface (unchanged contract) -------------------------------------

// findByName returns a copy of the first skill whose name matches, or nil.
// The copy avoids aliasing the range variable in the returned pointer.
func findByName(skills []Skill, name string) *Skill {
	for _, sk := range skills {
		if sk.Name == name {
			out := sk
			return &out
		}
	}
	return nil
}

// Resolve returns a single skill by name visible to the org — every kind,
// platform included (the skills page shows platform skills read-only so org
// admins can inspect the generation-flow guidance). A custom/imported skill
// owns its name outright (legacy shadow repos resolve user-kind-wins until
// migrated). Mutation guards, not visibility, enforce read-only-ness.
func (s *SkillService) Resolve(ctx context.Context, orgID, name string) (*Skill, error) {
	return findByName(s.catalog(ctx, orgID), name), nil
}

// resolveFresh resolves a single skill of ANY kind (platform included — a
// same-named custom/imported skill would shadow the platform skill in the
// deduped catalog and duplicate it in snapshots, so platform names stay
// reserved) with read errors SURFACED rather than degraded. Used by the create/import
// collision checks so a transient read failure yields an error rather than a
// phantom "no collision". This narrows — but cannot eliminate — the
// cross-replica TOCTOU window: git has no unique constraint
// (docs/design/skills-repo-storage.md §9).
func (s *SkillService) resolveFresh(ctx context.Context, orgID, name string) (*Skill, error) {
	if s == nil || s.git == nil || s.repos == nil || orgID == "" {
		return nil, nil
	}
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return nil, err
	}
	skills, err := s.loadCatalog(ctx, orgID, repo)
	if err != nil {
		return nil, err
	}
	return findByName(skills, name), nil
}

// List returns every skill visible to the org (including platform skills —
// the internal catalog), sorted by kind then name. Callers that feed
// user-facing or per-turn surfaces filter kinds themselves (ListSummaries
// hides platform skills).
func (s *SkillService) List(ctx context.Context, orgID string) ([]Skill, error) {
	return s.catalog(ctx, orgID), nil
}

// ListSummaries is the skills-page projection: every kind, projected to
// (name, kind, description, ...). Platform skills list READ-ONLY (the page
// shows the generation-flow guidance for inspection); only user-owned kinds
// are editable — org + platform are reconcile-managed.
func (s *SkillService) ListSummaries(ctx context.Context, orgID string) ([]SkillSummary, error) {
	skills := s.catalog(ctx, orgID)
	out := make([]SkillSummary, 0, len(skills))
	for _, sk := range skills {
		out = append(out, SkillSummary{
			Name:        sk.Name,
			Kind:        sk.Kind,
			Description: sk.Description,
			ContentSHA:  sk.ContentSHA,
			Editable:    sk.Kind == models.SkillKindCustom || sk.Kind == models.SkillKindImported,
		})
	}
	return out, nil
}

// ---- catalog loading ---------------------------------------------------------

// catalog returns the org's skills at the branch tip, degrading to empty on
// any git/provisioning failure so a transient outage never fails a design/task
// run. §12.
func (s *SkillService) catalog(ctx context.Context, orgID string) []Skill {
	if s == nil || s.git == nil || s.repos == nil || orgID == "" {
		return nil
	}
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		slog.WarnContext(ctx, "skills: ensure repo failed — serving empty", "org", orgID, "error", err)
		return nil
	}
	skills, err := s.loadCatalog(ctx, orgID, repo)
	if err != nil {
		slog.WarnContext(ctx, "skills: load catalog failed — serving empty", "org", orgID, "error", err)
		return nil
	}
	return skills
}

// loadCatalogEntries reads skills/ at the branch tip: one Workspace.ReadBundle
// (fetch + local ls-tree/cat-file) filtered to the catalog layout, parsed
// in-memory — with per-entry layout info for the reconciler. Branch-tip reads
// always revalidate origin, so freshness matches the retired REST walk without
// any cache.
func (s *SkillService) loadCatalogEntries(ctx context.Context, orgID string, repo *models.GitRepository) ([]catalogEntry, error) {
	ref, err := gitrepo.ResolveWorkspaceRef(ctx, s.git.Resolver(), orgID, repo)
	if err != nil {
		return nil, err
	}
	files, _, err := s.git.Workspace().ReadBundle(ctx, ref, "", isCatalogPath)
	if err != nil {
		return nil, fmt.Errorf("read skills bundle: %w", err)
	}
	return parseBundleEntries(ctx, files), nil
}

// loadCatalog is loadCatalogEntries projected to the Skill catalog shape.
func (s *SkillService) loadCatalog(ctx context.Context, orgID string, repo *models.GitRepository) ([]Skill, error) {
	entries, err := s.loadCatalogEntries(ctx, orgID, repo)
	if err != nil {
		return nil, err
	}
	out := make([]Skill, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Skill)
	}
	return out, nil
}

// isCatalogPath keeps exactly the blobs the catalog is parsed from — both
// layouts (§4.1):
//
//	flat:   skills/<name>/SKILL.md, skills/<name>/references/*.md
//	legacy: skills/<kindDir>/<name>/SKILL.md, skills/<kindDir>/<name>/references/*.md
func isCatalogPath(rel string) bool {
	parts := strings.Split(rel, "/")
	if len(parts) < 3 || parts[0] != skillsRootDir {
		return false
	}
	switch len(parts) {
	case 3: // flat SKILL.md
		return parts[2] == skillFileName
	case 4: // flat reference OR legacy SKILL.md
		if parts[2] == "references" && strings.HasSuffix(parts[3], ".md") {
			return true
		}
		return legacyKindDirs[parts[1]] != "" && parts[3] == skillFileName
	case 5: // legacy reference
		return legacyKindDirs[parts[1]] != "" && parts[3] == "references" && strings.HasSuffix(parts[4], ".md")
	default:
		return false
	}
}

// catalogEntry is one parsed skill plus where it was found: legacyDir is the
// retired kind path-segment ("" for the flat layout). The reconciler uses the
// location to migrate legacy repos; everything else consumes the Skill.
type catalogEntry struct {
	Skill
	legacyDir string
}

// parseBundleEntries turns a path→content bundle into the sorted, deduped
// entry set. Same shape rules as isCatalogPath (re-checked defensively — the
// keep filter is an optimization, not the contract). Flat skills read their
// kind from frontmatter (absent → org); legacy paths map through
// legacyKindDirs. Dedup by name: the higher kindRank wins (user kinds beat
// platform-shipped ones — the legacy shadow semantics), and on a same-kind
// tie the flat copy wins (a transient dual-layout state).
func parseBundleEntries(ctx context.Context, files map[string]string) []catalogEntry {
	// key.legacyDir = "" for flat entries.
	type key struct{ legacyDir, name string }
	bodies := map[key]string{}
	refs := map[key]map[string]string{}

	// Pass 1: SKILL.md bodies (they disambiguate the ambiguous ref shape below).
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
	// Pass 2: references. skills/<x>/references/*.md is a FLAT ref unless <x>
	// is a legacy kind dir without a flat body (then it is legacy junk to skip).
	addRef := func(k key, refKey, content string) {
		if refs[k] == nil {
			refs[k] = map[string]string{}
		}
		refs[k][refKey] = content
	}
	for path, content := range files {
		parts := strings.Split(path, "/")
		if len(parts) < 4 || parts[0] != skillsRootDir {
			continue
		}
		switch {
		case len(parts) == 4 && parts[2] == "references" && strings.HasSuffix(parts[3], ".md"):
			k := key{"", parts[1]}
			if _, ok := bodies[k]; ok {
				addRef(k, refsPrefix+parts[3], content)
			}
		case len(parts) == 5 && legacyKindDirs[parts[1]] != "" && parts[3] == "references" && strings.HasSuffix(parts[4], ".md"):
			addRef(key{parts[1], parts[2]}, refsPrefix+parts[4], content)
		}
	}

	deduped := map[string]catalogEntry{}
	for k := range bodies {
		kind := legacyKindDirs[k.legacyDir]
		fm, _, err := parseSkillMD(bodies[k])
		if err != nil {
			slog.WarnContext(ctx, "skills: skipping unparseable SKILL.md", "legacyDir", k.legacyDir, "name", k.name, "error", err)
			continue
		}
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
		deduped[k.name] = catalogEntry{
			Skill: Skill{
				Name:          k.name,
				Kind:          kind,
				Description:   strings.TrimSpace(fm.Description),
				SkillMD:       bodies[k],
				References:    r,
				ContentSHA:    contentSHA(bodies[k], r),
				License:       fm.License,
				Compatibility: fm.Compatibility,
			},
			legacyDir: k.legacyDir,
		}
	}

	out := make([]catalogEntry, 0, len(deduped))
	for _, e := range deduped {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return kindRank(out[i].Kind) < kindRank(out[j].Kind)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// parseBundle is parseBundleEntries projected to the Skill catalog shape.
func parseBundle(ctx context.Context, files map[string]string) []Skill {
	entries := parseBundleEntries(ctx, files)
	out := make([]Skill, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Skill)
	}
	return out
}

// ---- low-level write primitive ---------------------------------------------

// commitFiles applies a set of blob writes + path/prefix deletes to the skills
// repo's default branch in a single commit through Workspace.Mutate, which
// owns the bounded fast-forward CAS retry (design D5). §9.
func (s *SkillService) commitFiles(ctx context.Context, orgID string, repo *models.GitRepository, message string, writes map[string][]byte, deletePrefixes []string) (string, error) {
	ref, err := gitrepo.ResolveWorkspaceRef(ctx, s.git.Resolver(), orgID, repo)
	if err != nil {
		return "", err
	}
	author, committer := s.git.ResolveSaveIdentities(ref.Cred)

	res, err := s.git.Workspace().Mutate(ctx, ref, func(tx gitrepo.Tx) error {
		// Deletes are staged BEFORE writes: Tx is last-op-wins per path, so a
		// path also being written this commit survives its own prefix delete
		// (e.g. a reference file being replaced under a pruning update).
		for _, prefix := range deletePrefixes {
			if err := tx.Base().Walk(prefix, func(rel, _ string) error {
				// Walk matches by raw string prefix; keep the exact historical
				// semantics — the path itself or anything under it as a dir.
				if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
					tx.Delete(rel)
				}
				return nil
			}); err != nil {
				return fmt.Errorf("walk %q for delete: %w", prefix, err)
			}
		}
		for p, data := range writes {
			tx.Write(p, data)
		}
		return nil
	}, gitrepo.CommitOpts{Message: message, Author: author, Committer: committer})
	if err != nil {
		return "", err
	}
	// Changed=false (nothing staged / identical content) returns the unchanged
	// tip — same contract as the retired REST path.
	return res.CommitSHA, nil
}

// writeSkillFiles commits a skill's SKILL.md + references under the flat
// skills/<name>/. When pruneStaleRefs is set (updates), reference files
// present in the repo but absent from the new set are removed in the same
// commit (the SKILL.md path and any reference being rewritten are protected
// from the delete). Creates/imports pass pruneStaleRefs=false: there is nothing
// to prune for a brand-new skill. Any retired-layout directories of the same
// name are cleaned up in the same commit (no-ops on migrated repos). Used by
// the mutation + import services and the reconciler. §9.
func (s *SkillService) writeSkillFiles(ctx context.Context, orgID, name, skillMD string, references map[string]string, message string, pruneStaleRefs bool) error {
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return err
	}
	writes := map[string][]byte{skillRepoPath(name): []byte(skillMD)}
	for refKey, content := range references {
		writes[skillRefPath(name, refKey)] = []byte(content)
	}
	deletes := legacySkillDirs(name)
	if pruneStaleRefs {
		// Sweep the references/ subtree so removed refs don't linger; commitFiles
		// stages writes after deletes, so rewritten refs win.
		deletes = append(deletes, skillRepoDir(name)+"/"+strings.TrimSuffix(refsPrefix, "/"))
	}
	_, err = s.commitFiles(ctx, orgID, repo, message, writes, deletes)
	return err
}

// deleteSkillDir removes a skill's whole directory (plus any retired-layout
// copies of the name) in one commit. §9.
func (s *SkillService) deleteSkillDir(ctx context.Context, orgID, name, message string) error {
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return err
	}
	_, err = s.commitFiles(ctx, orgID, repo, message, nil, append([]string{skillRepoDir(name)}, legacySkillDirs(name)...))
	return err
}

// ---- helpers ---------------------------------------------------------------

// kindRank orders org < platform < custom < imported. The dedup rule keeps the
// HIGHER rank, so a custom/imported skill owns its name over a same-named
// platform-shipped skill (the legacy shadow semantics, "org wins").
func kindRank(kind string) int {
	switch kind {
	case models.SkillKindOrg:
		return 0
	case models.SkillKindPlatform:
		return 1
	case models.SkillKindCustom:
		return 2
	case models.SkillKindImported:
		return 3
	default:
		return 4
	}
}

// skillRepoDir is the canonical FLAT directory for a skill (the layout is
// defined here once; the file/ref paths below compose from it) — no kind
// segment; the kind lives in frontmatter. §3.3.
func skillRepoDir(name string) string {
	return skillsRootDir + "/" + name
}

// skillRepoPath is the canonical repo path for a skill's SKILL.md.
func skillRepoPath(name string) string {
	return skillRepoDir(name) + "/" + skillFileName
}

// skillRefPath maps a "references/foo.md" key to its repo path.
func skillRefPath(name, refKey string) string {
	return skillRepoDir(name) + "/" + refKey
}

// legacySkillDirs returns every retired-layout directory a skill of this name
// could occupy (skills/<kindDir>/<name>). Writes and deletes stage these as
// cleanup prefixes so a mutation against a not-yet-migrated repo self-heals
// the touched name in the same commit (a delete of an absent prefix is a
// no-op). §4.
func legacySkillDirs(name string) []string {
	out := make([]string, 0, len(legacyKindDirs))
	for dir := range legacyKindDirs {
		out = append(out, skillsRootDir+"/"+dir+"/"+name)
	}
	sort.Strings(out)
	return out
}
