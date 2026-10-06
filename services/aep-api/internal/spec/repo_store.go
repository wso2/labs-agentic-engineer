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
//   - writes are one Git.Commit to `main` through the same pod, planned from
//     a fresh read and re-planned on a baseSha conflict (commitFiles,
//     sourcecontrol.CommitRetrying).
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
	"maps"
	"slices"
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
	// git is the org's AE Studio pod: every skills-repo and project-repo
	// read, and every skills-repo commit.
	git sourcecontrol.Git
	// mirror is the pod's mirror-skills: the project .claude/skills refresh.
	mirror SkillMirrorPort
	repos  sourcecontrol.RepoService
	// library is the platform skill source read at reconcile time — os.DirFS
	// over the on-disk library (config.SkillsDir) in production, a test fs.FS in
	// tests. Rooted at the library directory itself ("<name>/SKILL.md"), so
	// callers read from ".".
	library fs.FS
	// provLocks serialises first-time provisioning per org. The commits'
	// baseSha preconditions make concurrent WRITES safe, but they cannot make
	// the first page load deterministic: EnsureBareRepo creates the GitHub repo
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

// SkillMirrorPort is the pod's mirror-skills op SyncProjectSkills drives.
type SkillMirrorPort = sourcecontrol.SkillsMirrorOps

// NewSkillService wires the repo-backed store. `git` is the Git port every
// read and commit goes through; `mirror` refreshes a project's
// .claude/skills; `repos` provisions/looks up the per-org skills repo row;
// `library` is the platform skill source read during seed/reconcile
// (os.DirFS(config.SkillsDir) in production, a test fs.FS in tests). Any may
// be nil in degraded/test boot (reads then return empty; a nil library seeds
// nothing).
func NewSkillService(git sourcecontrol.Git, mirror SkillMirrorPort, repos sourcecontrol.RepoService, library fs.FS) *SkillService {
	return &SkillService{git: git, mirror: mirror, repos: repos, library: library}
}

// configured reports whether the store has everything a read needs.
func (s *SkillService) configured() bool {
	return s != nil && s.git != nil && s.repos != nil
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
// kind checks; Enabled comes straight through from the catalog's own
// manifest cross-reference (loadCatalog), so a single catalog read still
// covers everything this projection needs.
//
// Unlike catalog(), a read failure is RETURNED: the skills page is a person
// asking, and an empty list would tell them their library is gone when AE
// Studio is only restarting or GitHub is not connected.
func (s *SkillService) ListSummaries(ctx context.Context, orgID string) ([]SkillSummary, error) {
	skills, err := s.readCatalog(ctx, orgID)
	if err != nil {
		return nil, err
	}
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
// same posture as the catalog; the console shows its connect-GitHub
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
	skills, err := s.readCatalog(ctx, orgID)
	if err != nil {
		slog.WarnContext(ctx, "skills: read catalog failed — serving empty", "org", orgID, "error", err)
		return nil
	}
	return skills
}

// readCatalog is the org's skills at the branch tip, or the failure that
// prevented the read. An unconfigured store or an empty org reads as empty.
func (s *SkillService) readCatalog(ctx context.Context, orgID string) ([]Skill, error) {
	if !s.configured() || orgID == "" {
		return nil, nil
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

// loadEntriesAndManifest reads skills/ through the pod (one Git.ReadBundle,
// kept to the catalog layout, parsed in-memory — with per-entry layout info
// for the reconciler) plus the skills-manifest.json baseline, both at one
// commit so entries and manifest are a consistent snapshot. That commit is
// the branch tip the pod's mirror holds (Head, Local: every skills write goes
// through the pod, so the mirror has it), and reading at its sha lets the
// adapter's read cache serve an unchanged library without re-transferring it. Every file comes back
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
	sha, err := s.git.Head(ctx, ref, "", sourcecontrol.Local())
	if err != nil {
		return nil, nil, false, fmt.Errorf("resolve skills head: %w", err)
	}
	tree, _, err := s.git.ReadBundle(ctx, ref, sha, sourcecontrol.BundleFilter{Prefix: skillsRootDir + "/"})
	if err != nil {
		return nil, nil, false, fmt.Errorf("read skills bundle: %w", err)
	}
	files := make(map[string]string, len(tree))
	for p, c := range tree {
		if isCatalogPath(p) {
			files[p] = c
		}
	}
	manifestFile, _, err := s.git.ReadBundle(ctx, ref, sha, sourcecontrol.BundleFilter{Paths: []string{skillsManifestPath}})
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
// repo's default branch in a single commit through Git.Commit, planned from
// the tree it reads at the tip and re-planned when a concurrent commit moved
// a path first (sourcecontrol.CommitRetrying). §9.
//
// Each attempt lists the tip: every write carries the path's blob sha there
// as its baseSha ("" when absent), and each delete prefix expands into the
// exact paths under it (the path itself or anything below it as a dir). A
// path that is also written is not deleted, so a reference rewritten under a
// pruning update survives its own prefix delete.
//
// manifestFn, when non-nil, is the retry-safe manifest merge: on EVERY
// attempt it re-reads skills-manifest.json at the commit that attempt listed
// and applies this operation's delta (upsert one entry / drop one entry /
// reconcile's computed set+delete). This is the fix for the lost-update
// hazard — a pre-rendered manifest would, on a retry, silently clobber any
// entry a concurrent commit added; re-reading + re-merging per attempt folds
// the concurrent entry in instead (the manifest's baseSha is what detects the
// race). The rendered manifest is written in the SAME commit as the file
// writes/deletes (the same-commit invariant), and only when the merge
// actually changes the bytes (so a no-op delta never churns the manifest, and
// a delete of an absent entry never conjures an empty manifest file). The
// file writes themselves are the caller's, planned once; only their baseSha
// is re-read per attempt.
func (s *SkillService) commitFiles(ctx context.Context, orgID string, repo *sourcecontrol.GitRepository, message string, writes map[string][]byte, deletePrefixes []string, manifestFn func(SkillsManifest) SkillsManifest) error {
	ref, err := sourcecontrol.RefForRow(orgID, repo)
	if err != nil {
		return fmt.Errorf("resolve skills repository: %w", err)
	}
	_, err = sourcecontrol.CommitRetrying(ctx, s.git, ref, func(ctx context.Context) (sourcecontrol.CommitRequest, error) {
		entries, commit, err := s.git.List(ctx, ref, "")
		if err != nil {
			return sourcecontrol.CommitRequest{}, fmt.Errorf("list skills repository: %w", err)
		}
		current := make(map[string]string, len(entries))
		for _, e := range entries {
			current[e.Path] = e.SHA
		}
		req := sourcecontrol.CommitRequest{Message: message}
		for _, p := range slices.Sorted(maps.Keys(writes)) {
			req.Writes = append(req.Writes, sourcecontrol.FileWrite{Path: p, Content: string(writes[p]), BaseSHA: current[p]})
		}
		if manifestFn != nil {
			raw, _, rerr := s.git.ReadFile(ctx, ref, commit, skillsManifestPath)
			if rerr != nil && !errors.Is(rerr, sourcecontrol.ErrPathNotFound) {
				return sourcecontrol.CommitRequest{}, fmt.Errorf("read manifest baseline: %w", rerr)
			}
			base := parseSkillsManifest(raw)
			renderedBase := renderSkillsManifest(base)
			merged := renderSkillsManifest(manifestFn(base)) // manifestFn may mutate base in place
			if !bytes.Equal(renderedBase, merged) {
				req.Writes = append(req.Writes, sourcecontrol.FileWrite{Path: skillsManifestPath, Content: string(merged), BaseSHA: current[skillsManifestPath]})
			}
		}
		written := make(map[string]bool, len(req.Writes))
		for _, w := range req.Writes {
			written[w.Path] = true
		}
		for _, e := range entries {
			if !written[e.Path] && underAnyPrefix(e.Path, deletePrefixes) {
				req.Deletes = append(req.Deletes, sourcecontrol.FileDelete{Path: e.Path, BaseSHA: e.SHA})
			}
		}
		return req, nil
	})
	return err
}

// underAnyPrefix reports whether path is one of prefixes or lies below one of
// them as a directory.
func underAnyPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
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
	err = s.commitFiles(ctx, orgID, repo, message, writes, deletes, manifestFn)
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
	err = s.commitFiles(ctx, orgID, repo, message, nil, append([]string{skillRepoDir(name)}, legacySkillDirs(name)...), manifestFn)
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
