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

// Repo-backed skills store. The per-org `org-skills` GitHub repo is the
// single source of truth (docs/design/skills-repo-storage.md):
//
//   - reads go through the org's AE Studio pod (sourcecontrol.Git): the
//     library at the fetched branch tip, then the manifest at the sha that
//     read answered, so both are one snapshot;
//   - writes are one Workspace.Mutate commit to `main` over the shared-volume
//     mirror until Task 4.16 moves them to Git.Commit; Mutate owns the CAS
//     retry (no per-feature retry wrapper).
//
// The platform library (platform + org kinds) is seeded + content-reconciled
// from the on-disk skill library (config.SkillsDir, injected as an fs.FS;
// reconcile.go). The exported read surface
// (Resolve/List/ListSummaries) is IDENTICAL to the previous store, so the
// architect and tech-lead resolvers consume it unchanged.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

const (
	// SkillsRepoName is the stable per-org GitHub repo name. §10.1.
	SkillsRepoName = "org-skills"
	// SkillsRepoProject is the sentinel project_id under which the skills repo
	// row lives in git_repositories (distinguishes it from project repos). §10.1.
	SkillsRepoProject = SkillsRepoSentinelProjectID

	skillsRootDir = "skills"
	skillFileName = "SKILL.md"
	refsPrefix    = "references/"
	// skillOverlaysDir names the one subdirectory of an authored skill that is
	// NOT skill content: the coding runner's mode overlays (compose-time input
	// for a session it assembles — ADR-0004 in runners/remote-worker). loadLibrary
	// skips it, so it never reaches an org's skills repo or a ContentSHA.
	skillOverlaysDir = "overlays"
)

// legacyKindDirs maps the RETIRED kind path-segments (skills/<kindDir>/<name>/,
// the pre-flat layout) to the current kind vocabulary. Repos that predate the
// flat layout still parse through this table until their first reconcile
// migrates them. In the flat layout (skills/<name>/) a skill's kind lives in
// its frontmatter (`metadata.aep.kind`; absent → org).
var legacyKindDirs = map[string]string{
	"builtin":  SkillKindOrg,
	"flow":     SkillKindPlatform,
	"custom":   SkillKindOrg,
	"imported": SkillKindImported,
}

// legacyUserDirs are the legacy dirs that were never platform output —
// "custom" (now folded into the org kind) and "imported". Reconcile's legacy
// migration keys off this set, not the mapped kind, to tell a user-authored
// legacy skill (must survive migration) apart from a retired platform one
// living in "builtin"/"flow" (must fall to the wholesale legacy-dir purge):
// since the fold, both the user and platform legacy dirs can map to the same
// kind (org), so kind alone no longer carries that distinction.
var legacyUserDirs = map[string]bool{
	"custom":   true,
	"imported": true,
}

// SkillService is the repo-backed read/reconcile surface for skills. It also
// holds the low-level git read/write primitives that the mutation + import
// services and the reconciler compose with.
type SkillService struct {
	// git commits through the workspace mirror until Task 4.16 moves the
	// writes to Git.Commit.
	git sourcecontrol.GitOpsService
	// reads is the org's AE Studio pod: every skills-repo and project-repo
	// read.
	reads sourcecontrol.Git
	repos sourcecontrol.RepoService
	// library is the platform skill source read at reconcile time — os.DirFS
	// over the on-disk library (config.SkillsDir) in production, a test fs.FS in
	// tests. Rooted at the library directory itself ("<name>/SKILL.md"), so
	// callers read from ".".
	library fs.FS
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
// engine, credential resolver, and save identities for the writes; `reads` is
// the Git port every read goes through; `repos` provisions/looks up the
// per-org skills repo row; `library` is the platform skill source read during
// seed/reconcile (os.DirFS(config.SkillsDir) in production, a test fs.FS in
// tests). Any may be nil in degraded/test boot (reads then return empty; a
// nil library seeds nothing).
func NewSkillService(git sourcecontrol.GitOpsService, reads sourcecontrol.Git, repos sourcecontrol.RepoService, library fs.FS) *SkillService {
	return &SkillService{git: git, reads: reads, repos: repos, library: library}
}

// configured reports whether the store has everything a read needs.
func (s *SkillService) configured() bool {
	return s != nil && s.git != nil && s.reads != nil && s.repos != nil
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
	if !s.configured() || orgID == "" {
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

// ListForMirror is List with read errors SURFACED rather than degraded to
// empty — the same "same read, errors surfaced" shape as resolveFresh, for the
// one caller that must NOT treat a git outage as "the org library is empty":
// the project-skill mirror (skill_mirror.go). List/catalog degrade to nil on
// any failure (§12) because a design/task run reading a stale-but-nonempty
// catalog is far better than failing the run outright; the mirror's pruning
// step has the opposite failure mode — an empty read would delete every
// project's copy of every skill — so it must be able to tell "the library is
// genuinely empty" apart from "the read failed".
func (s *SkillService) ListForMirror(ctx context.Context, orgID string) ([]Skill, error) {
	if !s.configured() || orgID == "" {
		return nil, fmt.Errorf("skills: service not configured for org %q", orgID)
	}
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("ensure skills repo: %w", err)
	}
	skills, err := s.loadCatalog(ctx, orgID, repo)
	if err != nil {
		return nil, fmt.Errorf("load skills catalog: %w", err)
	}
	return skills, nil
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
// shows the generation-flow guidance for inspection); org + imported are
// editable and deletable per SkillEditable/SkillDeletable — both are pure
// kind checks; Enabled comes straight through from catalog()'s own
// manifest cross-reference (loadCatalog), so a single catalog() read still
// covers everything this projection needs.
func (s *SkillService) ListSummaries(ctx context.Context, orgID string) ([]SkillSummary, error) {
	skills := s.catalog(ctx, orgID)
	out := make([]SkillSummary, 0, len(skills))
	for _, sk := range skills {
		out = append(out, SkillSummary{
			Name:        sk.Name,
			Kind:        sk.Kind,
			Description: sk.Description,
			ContentSHA:  sk.ContentSHA,
			Editable:    SkillEditable(sk.Kind),
			Deletable:   SkillDeletable(sk.Kind),
			Enabled:     sk.Enabled,
		})
	}
	return out, nil
}

// RepoWebURL returns the HTML URL of the org's skills repo — the stored clone
// URL with any ".git" suffix trimmed (the contract SkillSummaryList.repoUrl;
// it powers the console Import dialog's via-pull-request link). Provisions the
// repo on first touch like every read, and degrades to "" on any failure —
// same posture as the catalog (§12); the console shows its connect-GitHub
// guidance for an empty URL.
func (s *SkillService) RepoWebURL(ctx context.Context, orgID string) string {
	if !s.configured() || orgID == "" {
		return ""
	}
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		slog.WarnContext(ctx, "skills: resolve repo url failed — serving empty", "org", orgID, "error", err)
		return ""
	}
	return strings.TrimSuffix(repo.RepoURL, ".git")
}

// ---- catalog loading ---------------------------------------------------------

// catalog returns the org's skills at the branch tip, degrading to empty on
// any git/provisioning failure so a transient outage never fails a design/task
// run. §12.
func (s *SkillService) catalog(ctx context.Context, orgID string) []Skill {
	if !s.configured() || orgID == "" {
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

// loadEntriesAndManifest reads skills/ at the fetched branch tip through the
// pod (one Git.ReadBundle, kept to the catalog layout, parsed in-memory —
// with per-entry layout info for the reconciler) plus the
// skills-manifest.json baseline, read at the commit sha that bundle answered
// so entries and manifest are a consistent snapshot. Every file comes back
// byte for byte (the bundle is base64 on the wire), so a binary aux file the
// reconciler writes back is never mangled. The manifest is tolerant-parsed
// (absent/corrupt → empty). The
// returned manifestPresent reports whether the manifest FILE existed at all —
// distinct from an empty parse — so a caller can tell a pre-manifest (or
// manually deleted) repo apart from one whose manifest is simply empty, and
// lazily backfill it (see UpdatesAvailable).
func (s *SkillService) loadEntriesAndManifest(ctx context.Context, orgID string, repo *sourcecontrol.GitRepository) ([]catalogEntry, SkillsManifest, bool, error) {
	ref, err := sourcecontrol.RefForRow(orgID, repo)
	if err != nil {
		return nil, nil, false, fmt.Errorf("resolve skills repository: %w", err)
	}
	tree, sha, err := s.reads.ReadBundle(ctx, ref, "", sourcecontrol.BundleFilter{Prefix: skillsRootDir + "/"})
	if err != nil {
		return nil, nil, false, fmt.Errorf("read skills bundle: %w", err)
	}
	files := make(map[string]string, len(tree))
	for p, c := range tree {
		if isCatalogPath(p) {
			files[p] = c
		}
	}
	manifestFile, _, err := s.reads.ReadBundle(ctx, ref, sha, sourcecontrol.BundleFilter{Paths: []string{skillsManifestPath}})
	if err != nil {
		return nil, nil, false, fmt.Errorf("read skills manifest: %w", err)
	}
	raw, manifestPresent := manifestFile[skillsManifestPath]
	return parseBundleEntries(ctx, files), parseSkillsManifest([]byte(raw)), manifestPresent, nil
}

// loadCatalog is loadEntriesAndManifest projected to the Skill catalog shape,
// with each skill's Enabled cross-referenced against its skills-manifest.json
// entry (absent entry, or no manifest at all, means enabled — see
// ManifestEntry.Disabled).
func (s *SkillService) loadCatalog(ctx context.Context, orgID string, repo *sourcecontrol.GitRepository) ([]Skill, error) {
	entries, manifest, _, err := s.loadEntriesAndManifest(ctx, orgID, repo)
	if err != nil {
		return nil, err
	}
	out := make([]Skill, 0, len(entries))
	for _, e := range entries {
		sk := e.Skill
		sk.Enabled = !manifest[sk.Name].Disabled
		out = append(out, sk)
	}
	return out, nil
}

// isCatalogPath keeps exactly the blobs the catalog is parsed from — both
// layouts (§4.1), every aux file (standard structure: scripts/, references/,
// assets/, and any extras) alongside SKILL.md, skipping only dotfiles:
//
//	flat:   skills/<name>/SKILL.md, skills/<name>/<any aux file, any depth>
//	legacy: skills/<kindDir>/<name>/SKILL.md, skills/<kindDir>/<name>/<any aux file, any depth>
func isCatalogPath(rel string) bool {
	parts := strings.Split(rel, "/")
	if len(parts) < 3 || parts[0] != skillsRootDir {
		return false
	}
	if hasDotSegment(parts[1:]) {
		return false
	}
	if len(parts) == 3 {
		// skills/<name>/SKILL.md is ALWAYS a flat skill's body: depth alone
		// discriminates the two layouts, never <name>. A flat skill literally
		// named "custom"/"builtin"/"flow"/"imported" (legacyKindDirs' keys) must
		// keep at depth 3 — routing it into the legacy branch below silently
		// dropped it from the catalog.
		return true
	}
	// len(parts) >= 4: skills/<x>/<y>/... is ambiguous when <x> is a legacy
	// kind dir name — it could be legacy (skills/<kindDir>/<name>/<refKey...>)
	// or a flat skill literally named <x> with an aux file nested under a
	// subdirectory named <y> (skills/<name>/<subdir>/...). reservedSkillNames
	// (skill_mutation_service.go) keeps user-created skills out of the
	// legacyKindDirs vocabulary, so in practice this only touches
	// platform-shipped names. This is a keep filter, not the discriminator —
	// both interpretations need the blob, so we keep it either way and let
	// parseBundleEntries (which sees the whole path set) resolve the
	// ambiguity.
	return true
}

// hasDotSegment reports whether any path segment (file or directory) starts
// with a dot — the same skip rule loadLibrary applies to the on-disk library.
func hasDotSegment(parts []string) bool {
	for _, p := range parts {
		if strings.HasPrefix(p, ".") {
			return true
		}
	}
	return false
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
	// Pass 2: every aux file (standard structure: scripts/, references/,
	// assets/, any extras — no extension filter), keyed by its path relative to
	// the skill root. skills/<x>/... is a FLAT aux file unless <x> is a legacy
	// kind dir without a flat body (then it is legacy junk to skip).
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
		// len(parts) >= 4 under a legacy kind dir name is ambiguous — see
		// isCatalogPath. Resolve it for free here (pass 1 already ran): if a
		// flat SKILL.md body exists for parts[1], these are that flat skill's
		// aux files, not legacy junk, so the flat interpretation wins and no
		// aux file is ever silently dropped.
		_, flatBodyExists := bodies[key{"", parts[1]}]
		if legacyKindDirs[parts[1]] != "" && !flatBodyExists {
			// legacy: skills/<kindDir>/<name>/<refKey...> — needs at least the
			// kind dir + name + one path segment past the skill root.
			if len(parts) < 4 {
				continue
			}
			k := key{parts[1], parts[2]}
			refKey := strings.Join(parts[3:], "/")
			if refKey == skillFileName {
				continue
			}
			addRef(k, refKey, content)
			continue
		}
		// flat: skills/<name>/<refKey...>
		k := key{"", parts[1]}
		if _, ok := bodies[k]; !ok {
			continue
		}
		refKey := strings.Join(parts[2:], "/")
		if refKey == skillFileName {
			continue
		}
		addRef(k, refKey, content)
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
				// Enabled defaults true here (no manifest is in scope for this
				// pure parse); loadCatalog cross-references skills-manifest.json
				// and flips it for any entry the org disabled.
				Enabled:  true,
				Audience: frontmatterAudience(fm),
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

// ---- low-level write primitive ---------------------------------------------

// commitFiles applies a set of blob writes + path/prefix deletes to the skills
// repo's default branch in a single commit through Workspace.Mutate, which
// owns the bounded fast-forward CAS retry (design D5). §9.
//
// manifestFn, when non-nil, is the retry-safe manifest merge: it runs INSIDE
// the CAS closure on EVERY attempt, re-reading skills-manifest.json from the
// attempt's current base (tx.Base()) and applying this operation's delta
// (upsert one entry / drop one entry / reconcile's computed set+delete). This
// is the fix for the lost-update hazard — a pre-rendered manifest captured
// outside the closure would, on a non-fast-forward retry, silently clobber any
// entry a concurrent commit added; re-reading + re-merging per attempt folds
// the concurrent entry in instead. The rendered manifest is staged in the SAME
// commit as the file writes/deletes below (the same-commit invariant), and
// only when the merge actually changes the bytes (so a no-op delta never
// churns the manifest, and a delete of an absent entry never conjures an empty
// manifest file).
func (s *SkillService) commitFiles(ctx context.Context, orgID string, repo *sourcecontrol.GitRepository, message string, writes map[string][]byte, deletePrefixes []string, manifestFn func(SkillsManifest) SkillsManifest) (string, error) {
	ref, err := sourcecontrol.ResolveWorkspaceRef(ctx, s.git.Resolver(), orgID, repo)
	if err != nil {
		return "", err
	}
	author, committer := s.git.ResolveSaveIdentities(ref.Cred)

	res, err := s.git.Workspace().Mutate(ctx, ref, func(tx sourcecontrol.Tx) error {
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
		// Manifest merge, re-read + re-applied against THIS attempt's base so
		// the CAS retry loop never loses a concurrently-added entry. NOTE the
		// scope boundary: only the manifest is made retry-safe here. The file
		// writes/deletes above were planned by the caller against the base it
		// pre-read; a competing commit could in theory invalidate that plan
		// too, but that hazard pre-dates the shared manifest and full
		// re-planning inside the closure is out of scope — ONLY the manifest
		// merge is folded per attempt.
		if manifestFn != nil {
			raw, _, rerr := tx.Base().Read(skillsManifestPath)
			if rerr != nil && !errors.Is(rerr, sourcecontrol.ErrPathNotFound) {
				return fmt.Errorf("read manifest baseline: %w", rerr)
			}
			base := parseSkillsManifest(raw)
			renderedBase := renderSkillsManifest(base)
			merged := renderSkillsManifest(manifestFn(base)) // manifestFn may mutate base in place
			if !bytes.Equal(renderedBase, merged) {
				tx.Write(skillsManifestPath, merged)
			}
		}
		return nil
	}, sourcecontrol.CommitOpts{Message: message, Author: author, Committer: committer})
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
// name are cleaned up in the same commit (no-ops on migrated repos). When
// entry is non-nil the skill's skills-manifest.json entry is written in the
// SAME commit (imports stamp provenance; custom create/update pass nil —
// org-authored skills never get an entry). Used by the mutation + import
// services and the reconciler. §9.
func (s *SkillService) writeSkillFiles(ctx context.Context, orgID, name, skillMD string, references map[string]string, message string, pruneStaleRefs bool, entry *ManifestEntry) error {
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return err
	}
	writes := map[string][]byte{skillRepoPath(name): []byte(skillMD)}
	for refKey, content := range references {
		writes[skillRefPath(name, refKey)] = []byte(content)
	}
	// The manifest entry (imports only) is upserted INSIDE the commit closure
	// so a concurrent import's entry is never clobbered on a CAS retry — see
	// commitFiles. Capture entry by value; the closure runs per attempt.
	var manifestFn func(SkillsManifest) SkillsManifest
	if entry != nil {
		e := *entry
		manifestFn = func(m SkillsManifest) SkillsManifest {
			// The upsert replaces the WHOLE entry, but availability is org
			// intent and independent of the baseline this write records: a
			// converging console edit or a re-import must not silently
			// re-enable a skill the org switched off. Carry the prior flag here
			// so no caller has to remember to. Re-enabling is a manifest-only
			// write (SetEnabled), which never routes through this path.
			next := e
			if prior, ok := m[name]; ok && prior.Disabled {
				next.Disabled = true
			}
			m[name] = next
			return m
		}
	}
	deletes := legacySkillDirs(name)
	if pruneStaleRefs {
		// Sweep the references/ subtree so removed refs don't linger; commitFiles
		// stages writes after deletes, so rewritten refs win.
		deletes = append(deletes, skillRepoDir(name)+"/"+strings.TrimSuffix(refsPrefix, "/"))
	}
	_, err = s.commitFiles(ctx, orgID, repo, message, writes, deletes, manifestFn)
	return err
}

// deleteSkillDir removes a skill's whole directory (plus any retired-layout
// copies of the name) in one commit. If the name has a skills-manifest.json
// entry (an imported skill), it is dropped in the SAME commit so the
// manifest never outlives the files it describes. §9.
func (s *SkillService) deleteSkillDir(ctx context.Context, orgID, name, message string) error {
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return err
	}
	// TOMBSTONE the name's manifest entry (if any) INSIDE the commit closure so
	// a concurrent commit's entries survive the CAS retry — see commitFiles.
	// The entry deliberately outlives its files: it is the only record that
	// this org threw the skill away, which is what stops reconcile handing it
	// back on the next sync. A name with NO entry is left untouched — it is
	// org-authored, and inventing a tombstone for it would both conjure an
	// empty manifest file and permanently block a future platform default of
	// the same name. commitFiles stages the manifest only when these bytes
	// actually change, so that case stays a clean no-op.
	manifestFn := func(m SkillsManifest) SkillsManifest {
		if e, ok := m[name]; ok {
			e.Removed = true
			e.BaseHash = "" // no live copy to compare against any more
			m[name] = e
		}
		return m
	}
	_, err = s.commitFiles(ctx, orgID, repo, message, nil, append([]string{skillRepoDir(name)}, legacySkillDirs(name)...), manifestFn)
	return err
}

// ---- helpers ---------------------------------------------------------------

// kindRank orders org < platform < imported. The dedup rule keeps the HIGHER
// rank, so an imported skill owns its name over a same-named platform-shipped
// skill (the legacy shadow semantics, "org wins"). The retired custom kind
// shared org's rank and folds into it.
func kindRank(kind string) int {
	switch kind {
	case SkillKindOrg:
		return 0
	case SkillKindPlatform:
		return 1
	case SkillKindImported:
		return 2
	default:
		return 3
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
