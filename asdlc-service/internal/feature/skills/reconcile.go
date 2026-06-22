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

// Provisioning + built-in reconciliation. Built-ins ship embedded in the BFF
// container (the shipping vehicle) and are seeded + version-reconciled into
// each org's skills repo (the live store). docs/design/skills-repo-storage.md
// §6 (reconcile) and §10 (provisioning). User-modification protection is
// deferred (§6.4) — reconcile is purely version-based: absent → seed;
// embed.version > repo.version → overwrite; else leave.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"strings"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
	"github.com/wso2/asdlc/asdlc-service/models"
	embedskills "github.com/wso2/asdlc/asdlc-service/skills"
)

// SkillUpdate is one row of the "updates available" badge: a built-in whose
// embedded version is newer than (or absent from) the org's repo. §6.3.
type SkillUpdate struct {
	Name            string `json:"name"`
	RepoVersion     int    `json:"repoVersion"` // -1 when the skill is absent in the repo
	EmbeddedVersion int    `json:"embeddedVersion"`
}

// ensureSkillsRepo idempotently provisions the org's skills repo, seeding
// built-ins on first creation (lazy self-heal on the read path). §10.3.
//
// The per-org lock is held across the WHOLE function — deliberately, not just
// the provision branch. EnsureBareRepo creates the repo ROW before
// reconcileBuiltins seeds it, so a concurrent reader that slipped past the lock
// could see the row and read a still-empty repo (empty skills on first load).
// Holding the lock across seeding makes the list + updates-badge requests that
// fire together on page load wait for the seed, so first load is deterministic.
// In steady state this lock wraps only a ~1ms GetRepo at design/task QPS — the
// serialization cost is negligible and worth the first-load guarantee.
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
	// First time for this org — create the bare repo and seed built-ins.
	repo, err = s.repos.EnsureBareRepo(ctx, orgID, SkillsRepoProject, SkillsRepoName)
	if err != nil {
		return nil, err
	}
	if n, serr := s.reconcileBuiltins(ctx, orgID, repo); serr != nil {
		slog.WarnContext(ctx, "skills: seed built-ins failed (repo provisioned)", "org", orgID, "error", serr)
	} else {
		slog.InfoContext(ctx, "skills: seeded built-ins into new repo", "org", orgID, "count", n)
	}
	return repo, nil
}

// EnsureProvisioned ensures the org's skills repo exists (+ seeds built-ins on
// first creation). Called eagerly on project creation. §6.3/§10.2.
func (s *SkillService) EnsureProvisioned(ctx context.Context, orgID string) error {
	if s == nil || s.repos == nil || orgID == "" {
		return nil
	}
	_, err := s.ensureSkillsRepo(ctx, orgID)
	return err
}

// Reconcile version-reconciles built-ins for an org (seed/overwrite/skip).
// Used by project creation and the admin "Sync built-in skills" action. §6.
func (s *SkillService) Reconcile(ctx context.Context, orgID string) (int, error) {
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return 0, err
	}
	return s.reconcileBuiltins(ctx, orgID, repo)
}

// reconcileBuiltins writes every embedded built-in that is absent from the repo
// or whose embedded version is newer, in a single commit. Returns the number
// of built-ins written. §6.2.
func (s *SkillService) reconcileBuiltins(ctx context.Context, orgID string, repo *models.GitRepository) (int, error) {
	embedded, err := loadEmbeddedBuiltins()
	if err != nil {
		return 0, err
	}
	current, err := s.repoBuiltinVersions(ctx, orgID, repo)
	if err != nil {
		return 0, err
	}

	embeddedNames := make(map[string]bool, len(embedded))
	writes := map[string][]byte{}
	for _, b := range embedded {
		embeddedNames[b.Name] = true
		cur, ok := current[b.Name]
		if !ok || b.Version > cur {
			writes[skillRepoPath("builtin", b.Name)] = []byte(b.SkillMD)
		}
	}
	// Purge built-ins the platform embed no longer ships. The old SkillBootstrap
	// did this via `DELETE FROM skills WHERE kind='builtin' AND name NOT IN (...)`;
	// without it a retired built-in would linger in every org repo forever and
	// keep getting inlined into the architect/tech-lead prompts. §6.2.
	var deletes []string
	for name := range current {
		if !embeddedNames[name] {
			deletes = append(deletes, skillRepoDir("builtin", name))
		}
	}

	changed := len(writes) + len(deletes)
	if changed == 0 {
		return 0, nil
	}
	msg := fmt.Sprintf("chore(skills): reconcile built-ins (%d written, %d retired)", len(writes), len(deletes))
	if _, err := s.commitFiles(ctx, orgID, repo, msg, writes, deletes); err != nil {
		return 0, err
	}
	slog.InfoContext(ctx, "skills: reconciled built-ins", "org", orgID, "written", len(writes), "purged", len(deletes))
	return changed, nil
}

// UpdatesAvailable returns the built-ins whose embedded version is newer than
// (or missing from) the org's repo — the data behind the badge. §6.3.
func (s *SkillService) UpdatesAvailable(ctx context.Context, orgID string) ([]SkillUpdate, error) {
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return nil, err
	}
	embedded, err := loadEmbeddedBuiltins()
	if err != nil {
		return nil, err
	}
	current, err := s.repoBuiltinVersions(ctx, orgID, repo)
	if err != nil {
		return nil, err
	}
	var out []SkillUpdate
	for _, b := range embedded {
		cur, ok := current[b.Name]
		switch {
		case !ok:
			out = append(out, SkillUpdate{Name: b.Name, RepoVersion: -1, EmbeddedVersion: b.Version})
		case b.Version > cur:
			out = append(out, SkillUpdate{Name: b.Name, RepoVersion: cur, EmbeddedVersion: b.Version})
		}
	}
	return out, nil
}

// repoBuiltinVersions reads the current built-in name→version map from the repo
// at HEAD (via the cache — embed versions are fixed and ≤30s staleness is
// acceptable, same as reads; a stale "absent" just triggers a no-op CAS-retried
// rewrite). The post-commit cache evict in commitFiles keeps writes consistent.
func (s *SkillService) repoBuiltinVersions(ctx context.Context, orgID string, repo *models.GitRepository) (map[string]int, error) {
	skills, err := s.loadCatalog(ctx, orgID, repo, false)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, sk := range skills {
		if sk.Kind == "builtin" {
			out[sk.Name] = sk.Version
		}
	}
	return out, nil
}

// loadEmbeddedBuiltins reads the bundled builtin/<name>/SKILL.md files from the
// embedded FS into the canonical Skill shape. The embed is the platform's
// shipping vehicle for built-ins; the repo is the live store.
func loadEmbeddedBuiltins() ([]Skill, error) {
	entries, err := fs.ReadDir(embedskills.BuiltinFS, "builtin")
	if err != nil {
		return nil, fmt.Errorf("read embedded builtin dir: %w", err)
	}
	out := make([]Skill, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		raw, err := fs.ReadFile(embedskills.BuiltinFS, path.Join("builtin", name, skillFileName))
		if err != nil {
			slog.Warn("skills: embedded builtin read failed", "name", name, "error", err)
			continue
		}
		fm, _, err := parseSkillMD(string(raw))
		if err != nil {
			slog.Warn("skills: embedded builtin parse failed", "name", name, "error", err)
			continue
		}
		if fm.Name != name {
			slog.Warn("skills: embedded builtin name mismatch", "dir", name, "frontmatter", fm.Name)
			continue
		}
		out = append(out, Skill{
			Name:        name,
			Kind:        "builtin",
			Description: strings.TrimSpace(fm.Description),
			SkillMD:     string(raw),
			References:  map[string]string{},
			Version:     versionFromMetadata(fm),
			ContentSHA:  contentSHA(string(raw), nil),
		})
	}
	return out, nil
}
