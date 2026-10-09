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

// Provisioning + platform-skill reconciliation. The platform skill library
// ships in the BFF container as on-disk files (config.SkillsDir, read at
// runtime; platform + org kinds, kind in frontmatter) and is seeded +
// reconciled into each org's skills repo (the live store) under the FLAT
// layout skills/<name>/. Every shipped default — platform-kind and org-kind
// alike — is seeded on first creation AND on every ongoing sync, so adding a
// skill to the library reaches EXISTING orgs, not just new ones. The single
// exception is a name the org DELETED: delete leaves a manifest tombstone
// (ManifestEntry.Removed) and reconcile never hands a tombstoned name back.
// That tombstone is what separates "the org threw this away" from "this org
// has never been offered it"; a name with no entry at all is the latter.
// Reconcile is a THREE-WAY compare per skill
// against skills-manifest.json (§3), the org's baseline of what a
// platform-managed skill was last handed at: did the org's copy move off
// the baseline, and did the platform's move off the baseline? absent repo
// copy → seed (subject to the ownership gate above); org clean + platform
// moved → refresh (and advance the baseline); org moved + platform clean →
// leave the override alone (never touched); both moved → leave it alone as
// a conflict, surfaced by /updates, UNLESS the two copies converged on
// identical content, which auto-resolves clean; a pre-manifest repo copy is
// backfilled (baseline stamped so future compares work, a divergent copy
// treated as an override and never clobbered). The manifest is rewritten in
// the SAME commit as any file changes it reflects. A name owned by the user
// kind (imported) never carries a platform manifest entry, so it is
// skipped outright — the user copy always wins. Names the manifest still
// tracks as platform-shipped that the embed no longer ships are purged: a
// clean copy's files are deleted; an overridden copy's files are kept and
// just the entry dropped (divergence is ownership — it becomes a plain org
// skill). A name with no manifest entry at all is org-authored and reconcile
// never touches it. Reconcile also MIGRATES legacy-layout repos
// (skills/<kindDir>/<name>/) in the same single commit: user skills move to
// their flat dir with the kind stamped into frontmatter, embedded skills are
// rewritten flat (a divergent legacy copy is treated as a pre-manifest
// override, per the backfill rule above), and the retired kind dirs are
// removed (§4).

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"strings"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// SkillUpdate is one row of the "updates available" view: a platform-shipped
// skill whose state differs from the org's baseline. State semantics: "update"
// = clean copy, platform moved (sync refreshes it); "overridden" = org moved
// (sync never touches it); "conflict" = both moved (review required). §6.3 +
// skills-experience spec §3.
type SkillUpdate struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// ensureSkillsRepo idempotently provisions the org's skills repo, seeding
// built-ins + flow skills on first creation (lazy self-heal on the read path).
// §10.3.
//
// The per-org lock is held across the WHOLE function — deliberately, not just
// the provision branch. EnsureBareRepo creates the repo ROW before the seed
// commit lands, so a concurrent reader that slipped past the lock could see
// the row and read a still-empty repo (empty skills on first load). The
// commits' baseSha preconditions don't close that window (they arbitrate
// writes, not first-load ordering), and the GitHub repo creation itself runs
// under no lock at all — this narrow in-process guard covers exactly that
// first-time step. In steady state it wraps only a ~1ms GetRepo at design/task QPS.
func (s *SkillService) ensureSkillsRepo(ctx context.Context, orgID string) (*sourcecontrol.GitRepository, error) {
	mu := s.orgLock(orgID)
	mu.Lock()
	defer mu.Unlock()

	repo, err := s.repos.GetRepo(ctx, orgID, SkillsRepoProject)
	if err == nil {
		return repo, nil
	}
	if !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		return nil, err
	}
	// First time for this org — create the bare repo and seed the embedded skills.
	repo, err = s.repos.EnsureBareRepo(ctx, orgID, SkillsRepoProject, SkillsRepoName)
	if err != nil {
		return nil, err
	}
	if n, serr := s.reconcileEmbedded(ctx, orgID, repo); serr != nil {
		slog.WarnContext(ctx, "skills: seed embedded skills failed (repo provisioned)", "org", orgID, "error", serr)
	} else {
		slog.InfoContext(ctx, "skills: seeded embedded skills into new repo", "org", orgID, "count", n)
	}
	return repo, nil
}

// EnsureProvisioned ensures the org's skills repo exists (+ seeds the embedded
// skills on first creation). Called eagerly on project creation. §6.3/§10.2.
func (s *SkillService) EnsureProvisioned(ctx context.Context, orgID string) error {
	if s == nil || s.repos == nil || orgID == "" {
		return nil
	}
	_, err := s.ensureSkillsRepo(ctx, orgID)
	return err
}

// Reconcile drives the three-way reconcile of the embedded library for an org
// (seed/refresh/backfill/override/conflict/purge + legacy-layout migration).
// Used by project creation and the admin "Sync built-in skills" action. §6.
func (s *SkillService) Reconcile(ctx context.Context, orgID string) (int, error) {
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return 0, err
	}
	return s.reconcileEmbedded(ctx, orgID, repo)
}

// isUserKind reports whether a kind is user-owned (never touched by
// reconcile). Org is no longer a user-owned kind here: a platform-seeded org
// skill and a user-authored one share the org kind, and reconcile tells them
// apart via the skills-manifest.json baseline (an org skill with no manifest
// entry is org-authored and reconcile's embedded-skill loop never visits an
// unrelated name, so it is never touched either).
func isUserKind(kind string) bool {
	return kind == SkillKindImported
}

// reconcileEmbedded drives the whole repo to the desired flat state in ONE
// commit, deciding each embedded skill's fate with the three-way compare
// (decideReconcile, §3) against its skills-manifest.json baseline. Seeding is
// NOT gated on kind: every shipped default the org does not already have is
// seeded, on first creation and on every ongoing sync alike. The one thing
// that keeps a default out is a tombstone — the manifest entry a delete
// leaves behind — so an org's removal sticks while a newly shipped default
// still lands.
//
//   - every embedded skill absent from the repo, or clean with the platform
//     moved, is (re)written flat and its baseline advanced — unless a user
//     kind owns the name (the user copy wins, the embedded skill is
//     skipped), or the org deleted the name (tombstoned — see above);
//   - a repo copy the org diverged (with the platform not moving, or a
//     conflict where both moved) is left alone — reconcile never clobbers an
//     org edit;
//   - a pre-manifest repo copy (no entry yet — first reconcile after this
//     migration, or a legacy-dir copy) is backfilled: the baseline is stamped
//     so future reconciles compare correctly, but a divergent copy's content
//     is never rewritten;
//   - user skills found in legacy dirs are moved to their flat dir with the
//     kind stamped into frontmatter (§4);
//   - names the manifest still tracks as platform-shipped that the embed no
//     longer ships are purged: a clean copy's files are deleted (without this
//     a retired skill would linger in every org repo forever and keep
//     getting inlined into agent prompts); an overridden copy's files are
//     kept and its entry dropped (the org now owns the name outright);
//   - the retired legacy kind dirs are removed wholesale (writes land at flat
//     paths, so the staged prefix deletes only ever remove old copies).
//
// The manifest is rewritten in the same commit as any file changes it
// reflects. A rewritten skill's flat directory is replaced wholesale (delete
// staged before the writes) so references removed by the new content never
// linger. Returns the number of skills written + migrated + purged (the
// manifest-only stamp of a backfill is not counted, but still committed).
// §6.2.
func (s *SkillService) reconcileEmbedded(ctx context.Context, orgID string, repo *sourcecontrol.GitRepository) (int, error) {
	embedded, err := loadLibrary(s.library)
	if err != nil {
		return 0, err
	}
	entries, manifest, _, err := s.loadEntriesAndManifest(ctx, orgID, repo)
	if err != nil {
		return 0, err
	}
	current := map[string]catalogEntry{}
	hasLegacy := false
	for _, e := range entries {
		current[e.Name] = e
		if e.legacyDir != "" {
			hasLegacy = true
		}
	}

	embeddedNames := make(map[string]bool, len(embedded))
	writes := map[string][]byte{}
	var deletes []string
	written, migrated, purged := 0, 0, 0
	manifestDirty := false
	// manifestSet/manifestDelete are the manifest DELTA reconcile computes
	// against the pre-read `manifest`. They are re-applied on every commit
	// attempt (commitFiles' manifestFn) against that attempt's read, so a
	// concurrent import/delete that advanced the manifest is merged in rather
	// than clobbered on a retry. The `manifest` map itself is still mutated
	// in place below purely to keep the pre-read decision logic
	// (entry lookups, dirty accounting) unchanged.
	manifestSet := map[string]ManifestEntry{}
	manifestDelete := map[string]bool{}

	stageWrite := func(name, skillMD string, refs map[string]string) {
		writes[skillRepoPath(name)] = []byte(skillMD)
		for refKey, content := range refs {
			writes[skillRefPath(name, refKey)] = []byte(content)
		}
	}
	setBase := func(name, sha string) {
		// Disabled is org intent (ADR-0014), not baseline state: carry the
		// prior entry's flag through so a platform refresh — which advances
		// BaseHash and may rewrite content — never silently re-enables a
		// skill the org switched off.
		want := ManifestEntry{Origin: ManifestOriginPlatform, BaseHash: sha, Disabled: manifest[name].Disabled}
		if manifest[name] != want {
			manifest[name] = want
			manifestDirty = true
		}
		manifestSet[name] = want // record the delta op (idempotent per attempt)
	}

	for _, b := range embedded {
		embeddedNames[b.Name] = true
		cur, ok := current[b.Name]
		if ok && isUserKind(cur.Kind) {
			continue // an imported copy owns this name — never touch it
		}
		// A skill the org DELETED never comes back: its manifest tombstone says
		// so. A name with NO entry has never been offered to this org — a
		// default added to the library after the org was created — so it seeds
		// like any other, which is what lets a new org-kind default reach
		// EXISTING orgs and not just new ones. `setBase` re-stamps a fresh
		// entry on every write, so seeding or refreshing clears a tombstone.
		if !ok && manifest[b.Name].Removed {
			continue
		}
		var entry *ManifestEntry
		if e, has := manifest[b.Name]; has {
			entry = &e
		}
		// Legacy-dir copies predate the manifest: treat as pre-manifest (nil
		// entry) so backfill semantics apply; the rewrite below re-flattens.
		if ok && cur.legacyDir != "" && entry == nil {
			if cur.ContentSHA == b.ContentSHA {
				written++ // legacy but clean: rewrite flat at embed content
				deletes = append(deletes, skillRepoDir(b.Name))
				stageWrite(b.Name, b.SkillMD, b.References)
				setBase(b.Name, b.ContentSHA)
			} else {
				// Divergent legacy copy of an embedded name: keep content,
				// move flat via the user-skill migration path below is NOT
				// available (kind is platform/org) — rewrite flat preserving
				// the org's content, stamp as override.
				written++
				deletes = append(deletes, skillRepoDir(b.Name))
				stageWrite(b.Name, cur.SkillMD, cur.References)
				setBase(b.Name, b.ContentSHA)
			}
			continue
		}
		switch decideReconcile(b.ContentSHA, cur.ContentSHA, ok, entry) {
		case actionSeed, actionRefresh:
			written++
			deletes = append(deletes, skillRepoDir(b.Name))
			stageWrite(b.Name, b.SkillMD, b.References)
			setBase(b.Name, b.ContentSHA)
		case actionBackfill:
			// Pre-manifest copy: adopt the shipped content and stamp the
			// baseline from the same bytes, so copy and baseline agree from
			// here on. Writing is what makes the migration honest — stamping
			// alone left a baseline describing content the org did not have.
			written++
			deletes = append(deletes, skillRepoDir(b.Name))
			stageWrite(b.Name, b.SkillMD, b.References)
			setBase(b.Name, b.ContentSHA)
		case actionOverride, actionConflict:
			// Org-owned divergence: never write files, never move the base.
			// These arise only with an existing entry — i.e. a divergence that
			// appeared AFTER a baseline was agreed, which is a real org edit.
		case actionSkip:
		}
	}

	// Legacy migration of user-authored skills living in a legacy USER dir
	// (skills/custom/<name>/, skills/imported/<name>/) that the embedded loop
	// above didn't already claim. isUserKind(cur.Kind) alone can no longer
	// select these: org is no longer user-owned by kind, and the retired
	// "custom" dir now folds to the SAME kind (org) as the retired "builtin"
	// dir, so kind can't tell a user-authored legacy skill (must survive
	// migration) from a retired platform one (must fall to the wholesale
	// legacy-dir purge below, same as pre-fold). The legacy dir NAME still
	// carries that distinction — legacyUserDirs are the dirs that were never
	// platform output.
	for name, cur := range current {
		if legacyUserDirs[cur.legacyDir] && !embeddedNames[name] {
			migrated++
			stamped, serr := stampFrontmatterKind(cur.SkillMD, cur.Kind)
			if serr != nil {
				slog.WarnContext(ctx, "skills: migrate stamp failed — moving unstamped", "org", orgID, "name", name, "error", serr)
				stamped = cur.SkillMD
			}
			stageWrite(name, stamped, cur.References)
		}
	}

	// Purge: ONLY names the manifest says are platform-shipped and the embed
	// no longer ships. Clean copy → delete; overridden copy → keep the files,
	// drop the entry (divergence = ownership, it becomes a plain org skill).
	// Names with no manifest entry are org-authored and never touched.
	for name, entry := range manifest {
		if entry.Origin != ManifestOriginPlatform || embeddedNames[name] {
			continue
		}
		purged++
		manifestDirty = true
		delete(manifest, name)
		manifestDelete[name] = true // record the delta op
		if cur, ok := current[name]; ok && cur.ContentSHA == entry.BaseHash {
			if cur.legacyDir == "" {
				deletes = append(deletes, skillRepoDir(name))
			} // legacy copies fall to the wholesale prefix deletes below
		}
	}

	if hasLegacy {
		for dir := range legacyKindDirs {
			deletes = append(deletes, skillsRootDir+"/"+dir)
		}
	}

	changed := written + migrated + purged
	if changed == 0 && !manifestDirty {
		return 0, nil
	}
	// The manifest is merged in the SAME commit as the file changes, but via
	// the per-attempt delta (commitFiles' manifestFn) so a concurrent writer's
	// entries survive a retry. Only the manifest is re-merged; the file
	// writes/deletes above were planned against the pre-read state (see
	// commitFiles).
	manifestFn := func(m SkillsManifest) SkillsManifest {
		for name, entry := range manifestSet {
			m[name] = entry
		}
		for name := range manifestDelete {
			delete(m, name)
		}
		return m
	}
	msg := fmt.Sprintf("chore(skills): reconcile embedded library (%d written, %d migrated, %d retired)", written, migrated, purged)
	if err := s.commitFiles(ctx, orgID, repo, msg, writes, deletes, manifestFn); err != nil {
		return 0, err
	}
	slog.InfoContext(ctx, "skills: reconciled embedded skills", "org", orgID, "written", written, "migrated", migrated, "purged", purged)
	return changed, nil
}

// UpdatesAvailable evaluates the same three-way compare as reconcile,
// read-only. Names owned by a user kind or with an imported manifest entry
// are skipped; a pre-manifest divergent copy reports "overridden" (the
// migration posture: never suggest a clobber).
func (s *SkillService) UpdatesAvailable(ctx context.Context, orgID string) ([]SkillUpdate, error) {
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return nil, err
	}
	embedded, err := loadLibrary(s.library)
	if err != nil {
		return nil, err
	}
	entries, manifest, manifestPresent, err := s.loadEntriesAndManifest(ctx, orgID, repo)
	if err != nil {
		return nil, err
	}

	// Lazy manifest backfill — self-heal on the read path, mirroring how
	// ensureSkillsRepo lazily provisions the repo itself. A repo with NO
	// manifest file (a team that predates the manifest, or one a user deleted)
	// has no baselines: every platform-shipped skill would read as a
	// pre-manifest override, and there is no reconcile-write to CREATE the
	// manifest unless the org happens to see an "update" row and clicks Sync —
	// which a clean pre-manifest repo never shows. So stamp it once, here, the
	// first time updates are read. Reconcile is idempotent, so once the file
	// exists this never runs again. Gated on a non-empty embedded library:
	// with nothing to stamp Reconcile writes no manifest, and re-triggering
	// every read would be a pointless no-op loop. Best-effort — a failed
	// backfill must NOT fail the read; it degrades to the pre-manifest posture
	// (the same rows this method returned before the hook).
	if !manifestPresent && len(embedded) > 0 {
		if _, rerr := s.Reconcile(ctx, orgID); rerr != nil {
			slog.WarnContext(ctx, "skills: lazy manifest backfill failed — serving pre-manifest updates", "org", orgID, "error", rerr)
		} else if entries, manifest, _, err = s.loadEntriesAndManifest(ctx, orgID, repo); err != nil {
			return nil, err
		}
	}

	current := map[string]catalogEntry{}
	for _, e := range entries {
		current[e.Name] = e
	}
	var out []SkillUpdate
	for _, b := range embedded {
		cur, ok := current[b.Name]
		if ok && isUserKind(cur.Kind) {
			continue
		}
		// Org-kind defaults are opt-in on ongoing sync: an ABSENT org skill
		// (org-deleted, or a newly-shipped default not yet added) is NOT an
		// available update — Reconcile won't seed it (mirrors the same
		// org-kind-absent skip in reconcileEmbedded). Surfacing it here would
		// advertise an update that a subsequent Sync just no-ops. It belongs
		// to the separate "available to add" surface, not the updates badge.
		// Platform-kind absents ARE always managed, so they still report.
		if b.Kind == SkillKindOrg && !ok {
			continue
		}
		var entry *ManifestEntry
		if e, has := manifest[b.Name]; has {
			entry = &e
		}
		switch decideReconcile(b.ContentSHA, cur.ContentSHA, ok, entry) {
		case actionSeed, actionRefresh:
			out = append(out, SkillUpdate{Name: b.Name, State: "update"})
		case actionOverride:
			out = append(out, SkillUpdate{Name: b.Name, State: "overridden"})
		case actionConflict:
			out = append(out, SkillUpdate{Name: b.Name, State: "conflict"})
		}
	}
	return out, nil
}

// embeddedContentSHA returns the platform library's current contentSHA for a
// skill name, and whether the platform ships a skill by that name. The
// mutation path uses it to detect when a console edit has converged an org
// copy back onto the platform's current version, so the manifest baseline can
// advance in the same commit (SkillMutationService.Update) rather than drift
// stale and provoke a future false conflict.
func (s *SkillService) embeddedContentSHA(name string) (string, bool, error) {
	embedded, err := loadLibrary(s.library)
	if err != nil {
		return "", false, err
	}
	for _, b := range embedded {
		if b.Name == name {
			return b.ContentSHA, true, nil
		}
	}
	return "", false, nil
}

// loadLibrary reads the whole platform skill library (fsys rooted at the
// library dir, i.e. <name>/SKILL.md + every aux file, standard structure:
// scripts/, references/, assets/, and any extras) into the canonical Skill
// shape. In production fsys is os.DirFS(config.SkillsDir); in tests it is an
// injected fs.FS. Each skill's kind comes from its frontmatter
// (`metadata.aep.kind`; absent → org); the library may only carry the
// platform-shipped kinds — anything else is coerced to org with a warning.
// Entries without a parseable SKILL.md whose frontmatter name matches the
// directory are skipped with a warning — a bad bundled file must never break
// provisioning. A nil fsys yields an empty library (seed nothing).
func loadLibrary(fsys fs.FS) ([]Skill, error) {
	if fsys == nil {
		return nil, nil
	}
	const root = "."
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("read skills library dir: %w", err)
	}
	out := make([]Skill, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		raw, err := fs.ReadFile(fsys, path.Join(root, name, skillFileName))
		if err != nil {
			slog.Warn("skills: embedded skill read failed", "name", name, "error", err)
			continue
		}
		fm, _, err := parseSkillMD(string(raw))
		if err != nil {
			slog.Warn("skills: embedded skill parse failed", "name", name, "error", err)
			continue
		}
		if fm.Name != name {
			slog.Warn("skills: embedded skill name mismatch", "dir", name, "frontmatter", fm.Name)
			continue
		}
		kind := frontmatterKind(fm)
		if kind != SkillKindPlatform && kind != SkillKindOrg {
			slog.Warn("skills: embedded skill carries a user kind — coerced to org", "name", name, "kind", kind)
			kind = SkillKindOrg
		}
		refs := map[string]string{}
		skillRoot := path.Join(root, name)
		walkErr := fs.WalkDir(fsys, skillRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			base := path.Base(p)
			if strings.HasPrefix(base, ".") {
				if d.IsDir() && p != skillRoot {
					return fs.SkipDir
				}
				return nil
			}
			// `overlays/` is COMPOSE-TIME input, not skill content: the coding
			// runner assembles its base plugin out of this same library and
			// applies skills/aep/overlays/local.md to get the playground's
			// local-mode workflow (ADR-0004, runners/remote-worker). Seeding it
			// would put playground-only prose in every org's skills repo, count
			// it in ContentSHA, and hand it to any agent that loads the skill.
			if d.IsDir() && base == skillOverlaysDir {
				return fs.SkipDir
			}
			if d.IsDir() {
				return nil
			}
			rel := strings.TrimPrefix(p, skillRoot+"/")
			if rel == skillFileName { // SKILL.md is the body, not an aux file
				return nil
			}
			data, rerr := fs.ReadFile(fsys, p)
			if rerr != nil {
				slog.Warn("skills: embedded aux file read failed", "name", name, "file", rel, "error", rerr)
				return nil
			}
			refs[rel] = string(data)
			return nil
		})
		if walkErr != nil {
			slog.Warn("skills: embedded skill walk failed", "name", name, "error", walkErr)
		}
		out = append(out, Skill{
			Name:        name,
			Kind:        kind,
			Description: strings.TrimSpace(fm.Description),
			SkillMD:     string(raw),
			References:  refs,
			ContentSHA:  contentSHA(string(raw), refs),
			Audience:    frontmatterAudience(fm),
		})
	}
	return out, nil
}
