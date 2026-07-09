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

// Provisioning + embedded-skill reconciliation. The embedded library ships in
// the BFF container (the shipping vehicle; platform + org kinds, kind in
// frontmatter) and is seeded + reconciled into each org's skills repo (the
// live store) under the FLAT layout skills/<name>/
// (docs/design/skills-unified-library-migration.md; skills-repo-storage.md §6
// reconcile, §10 provisioning). Reconcile is content-hash based: absent →
// seed; embedded content SHA ≠ repo content SHA → overwrite (replacing the
// whole skill dir, so stale references never linger); else leave. A name owned
// by a user kind (custom/imported) is SKIPPED — the user copy wins. Retired
// names the embed no longer ships are purged. Reconcile also MIGRATES
// legacy-layout repos (skills/<kindDir>/<name>/) in the same single commit:
// user skills move to their flat dir with the kind stamped into frontmatter,
// embedded skills are rewritten flat, and the retired kind dirs are removed
// (§4).

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"strings"

	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/models"
	embedskills "github.com/wso2/aep/aep-api/skills"
)

// SkillUpdate is one row of the "updates available" badge: an org-kind skill
// whose embedded content differs from (or is absent from) the org's repo. §6.3.
type SkillUpdate struct {
	Name string `json:"name"`
}

// ensureSkillsRepo idempotently provisions the org's skills repo, seeding
// built-ins + flow skills on first creation (lazy self-heal on the read path).
// §10.3.
//
// The per-org lock is held across the WHOLE function — deliberately, not just
// the provision branch. EnsureBareRepo creates the repo ROW before the seed
// commit lands, so a concurrent reader that slipped past the lock could see
// the row and read a still-empty repo (empty skills on first load). The gitfs
// flock + push-CAS don't close that window (they arbitrate writes, not
// first-load ordering), and the GitHub repo creation itself runs under no
// flock at all — this narrow in-process guard covers exactly that first-time
// step. In steady state it wraps only a ~1ms GetRepo at design/task QPS.
func (s *SkillService) ensureSkillsRepo(ctx context.Context, orgID string) (*models.GitRepository, error) {
	mu := s.orgLock(orgID)
	mu.Lock()
	defer mu.Unlock()

	repo, err := s.repos.GetRepo(ctx, orgID, SkillsRepoProject)
	if err == nil {
		return repo, nil
	}
	if !errors.Is(err, gitrepo.ErrRepoNotFound) {
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

// Reconcile content-reconciles the embedded library for an org
// (seed/overwrite/purge/skip + legacy-layout migration). Used by project
// creation and the admin "Sync built-in skills" action. §6.
func (s *SkillService) Reconcile(ctx context.Context, orgID string) (int, error) {
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return 0, err
	}
	return s.reconcileEmbedded(ctx, orgID, repo)
}

// isUserKind reports whether a kind is user-owned (never touched by reconcile).
func isUserKind(kind string) bool {
	return kind == models.SkillKindCustom || kind == models.SkillKindImported
}

// reconcileEmbedded drives the whole repo to the desired flat state in ONE
// commit:
//
//   - every embedded skill absent from the repo, content-drifted, or still in
//     a legacy dir is (re)written flat — unless a user kind owns the name
//     (the user copy wins, the embedded skill is skipped);
//   - user skills found in legacy dirs are moved to their flat dir with the
//     kind stamped into frontmatter (§4);
//   - platform/org-kind skills the embed no longer ships are purged (without
//     this a retired skill would linger in every org repo forever and keep
//     getting inlined into agent prompts);
//   - the retired legacy kind dirs are removed wholesale (writes land at flat
//     paths, so the staged prefix deletes only ever remove old copies).
//
// A rewritten skill's flat directory is replaced wholesale (delete staged
// before the writes) so references removed by the new content never linger.
// Returns the number of skills written + migrated + purged. §6.2.
func (s *SkillService) reconcileEmbedded(ctx context.Context, orgID string, repo *models.GitRepository) (int, error) {
	embedded, err := loadEmbeddedLibrary()
	if err != nil {
		return 0, err
	}
	entries, err := s.loadCatalogEntries(ctx, orgID, repo)
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

	stageWrite := func(name, skillMD string, refs map[string]string) {
		writes[skillRepoPath(name)] = []byte(skillMD)
		for refKey, content := range refs {
			writes[skillRefPath(name, refKey)] = []byte(content)
		}
	}

	for _, b := range embedded {
		embeddedNames[b.Name] = true
		cur, ok := current[b.Name]
		if ok && isUserKind(cur.Kind) {
			continue // the user copy owns this name — never overwrite it
		}
		if ok && cur.legacyDir == "" && cur.ContentSHA == b.ContentSHA {
			continue
		}
		written++
		// Replace the whole flat dir: the delete is staged first, the new
		// files win (legacy copies fall to the wholesale prefix deletes below).
		deletes = append(deletes, skillRepoDir(b.Name))
		stageWrite(b.Name, b.SkillMD, b.References)
	}

	for name, cur := range current {
		if isUserKind(cur.Kind) {
			if cur.legacyDir != "" {
				// Migrate: move to the flat dir with the kind stamped so the
				// file stays self-describing under the flat layout. Content
				// that already round-tripped the parser cannot realistically
				// fail the stamp; if it somehow does, move it unstamped (it
				// would read as org) rather than lose it.
				migrated++
				stamped, serr := stampFrontmatterKind(cur.SkillMD, cur.Kind)
				if serr != nil {
					slog.WarnContext(ctx, "skills: migrate stamp failed — moving unstamped", "org", orgID, "name", name, "error", serr)
					stamped = cur.SkillMD
				}
				stageWrite(name, stamped, cur.References)
			}
			continue
		}
		if !embeddedNames[name] {
			purged++
			if cur.legacyDir == "" {
				deletes = append(deletes, skillRepoDir(name))
			} // legacy copies fall to the wholesale prefix deletes below
		}
	}

	if hasLegacy {
		// Remove the retired kind dirs wholesale — every preserved skill was
		// rewritten at its flat path in this same commit.
		for dir := range legacyKindDirs {
			deletes = append(deletes, skillsRootDir+"/"+dir)
		}
	}

	changed := written + migrated + purged
	if changed == 0 {
		return 0, nil
	}
	msg := fmt.Sprintf("chore(skills): reconcile embedded library (%d written, %d migrated, %d retired)", written, migrated, purged)
	if _, err := s.commitFiles(ctx, orgID, repo, msg, writes, deletes); err != nil {
		return 0, err
	}
	slog.InfoContext(ctx, "skills: reconciled embedded skills", "org", orgID, "written", written, "migrated", migrated, "purged", purged)
	return changed, nil
}

// UpdatesAvailable returns the embedded skills (org + platform — both list on
// the skills page now) whose embedded content differs from (or is missing
// from) the org's repo — the data behind the badge. Names owned by a user
// kind are skipped — reconcile will never overwrite them, so a badge entry
// would be permanently stuck. §6.3.
func (s *SkillService) UpdatesAvailable(ctx context.Context, orgID string) ([]SkillUpdate, error) {
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return nil, err
	}
	embedded, err := loadEmbeddedLibrary()
	if err != nil {
		return nil, err
	}
	entries, err := s.loadCatalogEntries(ctx, orgID, repo)
	if err != nil {
		return nil, err
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
		if !ok || cur.ContentSHA != b.ContentSHA {
			out = append(out, SkillUpdate{Name: b.Name})
		}
	}
	return out, nil
}

// loadEmbeddedLibrary reads the whole vendored skill library
// (embedded/<name>/SKILL.md + optional references/*.md — see skills/embed.go)
// into the canonical Skill shape. Each skill's kind comes from its frontmatter
// (`metadata.aep.kind`; absent → org); the embedded source may only carry the
// platform-shipped kinds — anything else is coerced to org with a warning.
// Entries without a parseable SKILL.md whose frontmatter name matches the
// directory are skipped with a warning — a bad bundled file must never break
// provisioning.
func loadEmbeddedLibrary() ([]Skill, error) {
	const root = "embedded"
	fsys := fs.FS(embedskills.LibraryFS)
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("read embedded skills dir: %w", err)
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
		if kind != models.SkillKindPlatform && kind != models.SkillKindOrg {
			slog.Warn("skills: embedded skill carries a user kind — coerced to org", "name", name, "kind", kind)
			kind = models.SkillKindOrg
		}
		refs := map[string]string{}
		refDir := path.Join(root, name, "references")
		if refEntries, err := fs.ReadDir(fsys, refDir); err == nil {
			for _, r := range refEntries {
				if r.IsDir() || !strings.HasSuffix(r.Name(), ".md") {
					continue
				}
				data, err := fs.ReadFile(fsys, path.Join(refDir, r.Name()))
				if err != nil {
					slog.Warn("skills: embedded reference read failed", "name", name, "ref", r.Name(), "error", err)
					continue
				}
				refs[refsPrefix+r.Name()] = string(data)
			}
		}
		out = append(out, Skill{
			Name:        name,
			Kind:        kind,
			Description: strings.TrimSpace(fm.Description),
			SkillMD:     string(raw),
			References:  refs,
			ContentSHA:  contentSHA(string(raw), refs),
		})
	}
	return out, nil
}
