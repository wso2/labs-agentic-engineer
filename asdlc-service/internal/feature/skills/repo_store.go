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
// single source of truth (docs/design/skills-repo-storage.md). The old
// Postgres `skills` table is gone; reads walk the repo tree at HEAD via the
// GitHub API and an in-memory, HEAD-SHA-revalidated cache. Writes commit
// directly to `main`. Built-ins are seeded + version-reconciled from the
// embedded container files.
//
// The exported read surface (Resolve/List/ListSummaries/ResolveMany) is
// IDENTICAL to the previous DB-backed SkillService, so the architect and
// tech-lead resolvers consume it unchanged.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wso2/asdlc/asdlc-service/internal/credentials"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
	"github.com/wso2/asdlc/asdlc-service/models"
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

	// cacheSoftTTL is the window during which a cached catalog is served
	// without even revalidating HEAD. Past it, we re-check the HEAD sha and
	// rebuild only on a change. §7.3.
	cacheSoftTTL = 30 * time.Second

	casAttempts = 4
)

// validKinds is the set of kind path-segments under skills/. §8.
var validKinds = map[string]bool{"builtin": true, "custom": true, "imported": true}

// SkillService is the repo-backed read/reconcile surface for skills. It also
// holds the low-level git read/write primitives that the mutation + import
// services and the reconciler compose with.
type SkillService struct {
	git   gitrepo.GitOpsService
	repos gitrepo.RepoService
	cache *catalogCache
	// provLocks serialises first-time provisioning per org so the skills list
	// + updates-badge requests that fire together on page load don't both
	// create the repo / double-seed. Uncontended once the repo row exists.
	provLocks sync.Map // orgID → *sync.Mutex
}

func (s *SkillService) orgLock(orgID string) *sync.Mutex {
	v, _ := s.provLocks.LoadOrStore(orgID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// NewSkillService wires the repo-backed store. `git` provides the credential
// resolver + GitHub client; `repos` provisions/looks up the per-org skills
// repo row. Either may be nil in degraded/test boot (reads then return empty).
func NewSkillService(git gitrepo.GitOpsService, repos gitrepo.RepoService) *SkillService {
	return &SkillService{git: git, repos: repos, cache: newCatalogCache()}
}

// ---- read surface (unchanged contract) -------------------------------------

// findByName returns a copy of the first skill whose name matches, or nil. The
// copy avoids aliasing the range variable in the returned pointer.
func findByName(skills []Skill, name string) *Skill {
	for _, sk := range skills {
		if sk.Name == name {
			out := sk
			return &out
		}
	}
	return nil
}

// Resolve returns a single skill by name visible to the org. A custom/imported
// skill shadows a builtin of the same name (org wins), matching the prior
// DB-backed union semantics.
func (s *SkillService) Resolve(ctx context.Context, orgID, name string) (*Skill, error) {
	return findByName(s.catalog(ctx, orgID), name), nil
}

// resolveFresh resolves a single skill bypassing the soft-TTL cache (it still
// reuses the cache when HEAD is unchanged). Used by the create-collision check
// so a same-named skill just created on this replica is seen, and so a
// transient read failure surfaces as an error rather than a phantom
// "no collision". This narrows — but cannot eliminate — the cross-replica
// TOCTOU window: git has no unique constraint (docs/design/skills-repo-storage.md §9).
func (s *SkillService) resolveFresh(ctx context.Context, orgID, name string) (*Skill, error) {
	if s == nil || s.git == nil || s.repos == nil || orgID == "" {
		return nil, nil
	}
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return nil, err
	}
	skills, err := s.loadCatalog(ctx, orgID, repo, false)
	if err != nil {
		return nil, err
	}
	return findByName(skills, name), nil
}

// List returns every skill visible to the org, sorted by kind then name.
func (s *SkillService) List(ctx context.Context, orgID string) ([]Skill, error) {
	return s.catalog(ctx, orgID), nil
}

// ListSummaries is List() projected to (name, kind, version, description, ...).
func (s *SkillService) ListSummaries(ctx context.Context, orgID string) ([]SkillSummary, error) {
	skills := s.catalog(ctx, orgID)
	out := make([]SkillSummary, 0, len(skills))
	for _, sk := range skills {
		out = append(out, SkillSummary{
			Name:        sk.Name,
			Kind:        sk.Kind,
			Version:     sk.Version,
			Description: sk.Description,
			ContentSHA:  sk.ContentSHA,
			Editable:    sk.Kind != "builtin",
		})
	}
	return out, nil
}

// ResolveMany fans Resolve over names, preserving order; missing names are
// omitted (the caller may compare lengths to detect drops).
func (s *SkillService) ResolveMany(ctx context.Context, orgID string, names []string) ([]Skill, error) {
	byName := make(map[string]Skill)
	for _, sk := range s.catalog(ctx, orgID) {
		byName[sk.Name] = sk
	}
	out := make([]Skill, 0, len(names))
	for _, n := range names {
		if sk, ok := byName[n]; ok {
			out = append(out, sk)
			continue
		}
		slog.WarnContext(ctx, "skill resolve missing", "orgID", orgID, "name", n)
	}
	return out, nil
}

// ---- catalog loading + cache -----------------------------------------------

// catalog returns the org's skills at HEAD, degrading to stale cache (then to
// empty) on any GitHub/provisioning failure so a transient outage never fails
// a design/task run. §12.
func (s *SkillService) catalog(ctx context.Context, orgID string) []Skill {
	if s == nil || s.git == nil || s.repos == nil || orgID == "" {
		return nil
	}
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		slog.WarnContext(ctx, "skills: ensure repo failed — serving stale/empty", "org", orgID, "error", err)
		return s.cache.stale(orgID)
	}
	skills, err := s.loadCatalog(ctx, orgID, repo, true)
	if err != nil {
		slog.WarnContext(ctx, "skills: load catalog failed — serving stale/empty", "org", orgID, "error", err)
		return s.cache.stale(orgID)
	}
	return skills
}

// loadCatalog reads skills/ at HEAD, with a soft-TTL + HEAD-SHA revalidation
// cache to keep GitHub calls cheap. §7.2/§7.3.
//
// useSoftTTL=true (reads) serves the cache without touching GitHub for ≤30s.
// useSoftTTL=false (reconcile / updates-badge) skips that fast-path so it always
// revalidates HEAD — but still reuses the cache when HEAD is unchanged, so it
// stays cheap (one GetRef) instead of re-walking the whole tree every call.
func (s *SkillService) loadCatalog(ctx context.Context, orgID string, repo *models.GitRepository, useSoftTTL bool) ([]Skill, error) {
	// Fast path: within the soft TTL, serve cache without touching GitHub.
	if useSoftTTL {
		if cached, ok := s.cache.fresh(orgID); ok {
			return cached, nil
		}
	}

	owner, name := models.OwnerRepoFromURL(repo.RepoURL)
	if owner == "" || name == "" {
		return nil, fmt.Errorf("cannot derive owner/repo from %q", repo.RepoURL)
	}
	cred, err := s.git.Resolver().Resolve(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("resolve credential: %w", err)
	}
	gh := s.git.GitHubClient()
	branch := defaultBranch(repo)

	headSHA, err := gh.GetRef(ctx, owner, name, cred, "heads/"+branch)
	if err != nil {
		return nil, fmt.Errorf("get head ref: %w", err)
	}
	// HEAD unchanged since last build → reuse cache, extend its freshness.
	if cached, ok := s.cache.matching(orgID, headSHA); ok {
		s.cache.touch(orgID)
		return cached, nil
	}

	commit, err := gh.GetCommit(ctx, owner, name, cred, headSHA)
	if err != nil {
		return nil, fmt.Errorf("get head commit: %w", err)
	}
	tree, err := gh.GetTree(ctx, owner, name, cred, commit.TreeSHA, true)
	if err != nil {
		return nil, fmt.Errorf("get head tree: %w", err)
	}

	skills, err := s.parseTree(ctx, owner, name, cred, tree)
	if err != nil {
		return nil, err
	}
	s.cache.put(orgID, headSHA, skills)
	return skills, nil
}

// parseTree turns a recursive tree listing into the sorted, deduped skill set.
// Files: skills/<kind>/<name>/SKILL.md and skills/<kind>/<name>/references/*.md.
func (s *SkillService) parseTree(ctx context.Context, owner, repo string, cred credentials.Credential, tree *gitrepo.TreeObject) ([]Skill, error) {
	type key struct{ kind, name string }
	bodies := map[key]string{}
	refs := map[key]map[string]string{}

	gh := s.git.GitHubClient()
	for _, e := range tree.Entries {
		if e.Type != "blob" {
			continue
		}
		parts := strings.Split(e.Path, "/")
		if len(parts) < 4 || parts[0] != skillsRootDir || !validKinds[parts[1]] {
			continue
		}
		k := key{kind: parts[1], name: parts[2]}
		rest := strings.Join(parts[3:], "/")
		switch {
		case rest == skillFileName:
			data, err := gh.GetBlob(ctx, owner, repo, cred, e.SHA)
			if err != nil {
				return nil, fmt.Errorf("get blob %s: %w", e.Path, err)
			}
			bodies[k] = string(data)
		case strings.HasPrefix(rest, refsPrefix) && strings.HasSuffix(rest, ".md"):
			data, err := gh.GetBlob(ctx, owner, repo, cred, e.SHA)
			if err != nil {
				return nil, fmt.Errorf("get blob %s: %w", e.Path, err)
			}
			if refs[k] == nil {
				refs[k] = map[string]string{}
			}
			refs[k][rest] = string(data)
		}
	}

	// Dedup by name; a custom/imported skill shadows a same-named builtin
	// ("org wins") via kind precedence, so map iteration order doesn't matter.
	deduped := map[string]Skill{}
	for k := range bodies {
		if existing, ok := deduped[k.name]; ok && kindRank(existing.Kind) >= kindRank(k.kind) {
			continue
		}
		r := refs[k]
		if r == nil {
			r = map[string]string{}
		}
		fm, _, err := parseSkillMD(bodies[k])
		if err != nil {
			slog.WarnContext(ctx, "skills: skipping unparseable SKILL.md", "kind", k.kind, "name", k.name, "error", err)
			continue
		}
		deduped[k.name] = Skill{
			Name:          k.name,
			Kind:          k.kind,
			Description:   strings.TrimSpace(fm.Description),
			SkillMD:       bodies[k],
			References:    r,
			Version:       versionFromMetadata(fm),
			ContentSHA:    contentSHA(bodies[k], r),
			License:       fm.License,
			Compatibility: fm.Compatibility,
		}
	}

	out := make([]Skill, 0, len(deduped))
	for _, sk := range deduped {
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return kindRank(out[i].Kind) < kindRank(out[j].Kind)
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// ---- low-level write primitive ---------------------------------------------

// commitFiles applies a set of blob writes + path deletes to the skills repo's
// default branch in a single commit, under a bounded fast-forward CAS retry.
// On success the org's catalog cache is evicted so the next read rebuilds. §9.
func (s *SkillService) commitFiles(ctx context.Context, orgID string, repo *models.GitRepository, message string, writes map[string][]byte, deletePrefixes []string) (string, error) {
	owner, name := models.OwnerRepoFromURL(repo.RepoURL)
	if owner == "" || name == "" {
		return "", fmt.Errorf("cannot derive owner/repo from %q", repo.RepoURL)
	}
	cred, err := s.git.Resolver().Resolve(ctx, orgID)
	if err != nil {
		return "", fmt.Errorf("resolve credential: %w", err)
	}
	gh := s.git.GitHubClient()
	author, committer := s.git.ResolveSaveIdentities(cred)
	branch := defaultBranch(repo)

	var commitSHA string
	err = retryCAS(ctx, casAttempts, func() error {
		headSHA, ferr := gh.GetRef(ctx, owner, name, cred, "heads/"+branch)
		if ferr != nil {
			return fmt.Errorf("get ref: %w", ferr)
		}
		commit, ferr := gh.GetCommit(ctx, owner, name, cred, headSHA)
		if ferr != nil {
			return fmt.Errorf("get commit: %w", ferr)
		}

		var entries []gitrepo.TreeEntry
		for p, data := range writes {
			blobSHA, berr := gh.CreateBlob(ctx, owner, name, cred, data)
			if berr != nil {
				return fmt.Errorf("create blob %s: %w", p, berr)
			}
			entries = append(entries, gitrepo.TreeEntry{Path: p, Mode: "100644", Type: "blob", SHA: blobSHA})
		}
		if len(deletePrefixes) > 0 {
			tree, terr := gh.GetTree(ctx, owner, name, cred, commit.TreeSHA, true)
			if terr != nil {
				return fmt.Errorf("get tree for delete: %w", terr)
			}
			for _, e := range tree.Entries {
				if e.Type != "blob" {
					continue
				}
				// Never delete a path we're also writing this commit — the
				// write must win (e.g. a reference file being replaced).
				if _, beingWritten := writes[e.Path]; beingWritten {
					continue
				}
				for _, prefix := range deletePrefixes {
					if e.Path == prefix || strings.HasPrefix(e.Path, prefix+"/") {
						// Empty SHA → serialised as sha:null → deletion.
						entries = append(entries, gitrepo.TreeEntry{Path: e.Path, Mode: "100644", Type: "blob"})
					}
				}
			}
		}
		if len(entries) == 0 {
			commitSHA = headSHA
			return nil
		}

		treeSHA, terr := gh.CreateTree(ctx, owner, name, cred, commit.TreeSHA, entries)
		if terr != nil {
			return fmt.Errorf("create tree: %w", terr)
		}
		newSHA, cerr := gh.CreateCommit(ctx, owner, name, cred, gitrepo.CreateCommitRequest{
			Message:   message,
			TreeSHA:   treeSHA,
			Parents:   []string{headSHA},
			Author:    author,
			Committer: committer,
		})
		if cerr != nil {
			return fmt.Errorf("create commit: %w", cerr)
		}
		if uerr := gh.UpdateRef(ctx, owner, name, cred, "heads/"+branch, newSHA, false); uerr != nil {
			return uerr
		}
		commitSHA = newSHA
		return nil
	})
	if err != nil {
		return "", err
	}
	s.cache.evict(orgID)
	return commitSHA, nil
}

// writeSkillFiles commits a skill's SKILL.md + references under
// skills/<kind>/<name>/. When pruneStaleRefs is set (updates), reference files
// present in the repo but absent from the new set are removed in the same
// commit (the SKILL.md path and any reference being rewritten are protected
// from the delete). Creates/imports pass pruneStaleRefs=false: there is nothing
// to prune for a brand-new skill, so we skip the recursive GetTree that the
// sweep would otherwise force on every write. Used by the mutation + import
// services and the reconciler. §9.
func (s *SkillService) writeSkillFiles(ctx context.Context, orgID, kind, name, skillMD string, references map[string]string, message string, pruneStaleRefs bool) error {
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return err
	}
	writes := map[string][]byte{skillRepoPath(kind, name): []byte(skillMD)}
	for refKey, content := range references {
		writes[skillRefPath(kind, name, refKey)] = []byte(content)
	}
	var deletes []string
	if pruneStaleRefs {
		// Sweep the references/ subtree so removed refs don't linger; commitFiles
		// skips deleting any path we're rewriting this commit.
		deletes = []string{skillRepoDir(kind, name) + "/" + strings.TrimSuffix(refsPrefix, "/")}
	}
	_, err = s.commitFiles(ctx, orgID, repo, message, writes, deletes)
	return err
}

// deleteSkillDir removes a skill's whole directory in one commit. §9.
func (s *SkillService) deleteSkillDir(ctx context.Context, orgID, kind, name, message string) error {
	repo, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return err
	}
	_, err = s.commitFiles(ctx, orgID, repo, message, nil, []string{skillRepoDir(kind, name)})
	return err
}

// ---- helpers ---------------------------------------------------------------

func defaultBranch(repo *models.GitRepository) string {
	if repo != nil && repo.DefaultBranch != "" {
		return repo.DefaultBranch
	}
	return "main"
}

// kindRank orders builtin < custom < imported (matches the prior `ORDER BY kind`).
func kindRank(kind string) int {
	switch kind {
	case "builtin":
		return 0
	case "custom":
		return 1
	case "imported":
		return 2
	default:
		return 3
	}
}

// skillRepoDir is the canonical directory for a skill (the layout is defined
// here once; the file/ref paths below compose from it). §8.
func skillRepoDir(kind, name string) string {
	return skillsRootDir + "/" + kind + "/" + name
}

// skillRepoPath is the canonical repo path for a skill's SKILL.md. §8.
func skillRepoPath(kind, name string) string {
	return skillRepoDir(kind, name) + "/" + skillFileName
}

// skillRefPath maps a "references/foo.md" key to its repo path.
func skillRefPath(kind, name, refKey string) string {
	return skillRepoDir(kind, name) + "/" + refKey
}

// retryCAS retries fn on a non-fast-forward ref update (concurrent writer),
// re-running the whole read-modify-write so base_tree is fresh. Other errors
// abort immediately. §9.
func retryCAS(ctx context.Context, attempts int, fn func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if !errors.Is(err, gitrepo.ErrRefNotFastForward) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(50*(i+1)) * time.Millisecond):
		}
	}
	return fmt.Errorf("skills commit: %w (after %d attempts)", err, attempts)
}

// ---- catalog cache ---------------------------------------------------------

type catalogEntry struct {
	headSHA   string
	skills    []Skill
	fetchedAt time.Time
}

type catalogCache struct {
	mu      sync.Mutex
	entries map[string]catalogEntry // keyed by orgID
}

func newCatalogCache() *catalogCache {
	return &catalogCache{entries: map[string]catalogEntry{}}
}

// fresh returns the cached catalog when it was fetched within the soft TTL.
func (c *catalogCache) fresh(orgID string) ([]Skill, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[orgID]
	if !ok {
		return nil, false
	}
	if time.Since(e.fetchedAt) < cacheSoftTTL {
		return e.skills, true
	}
	return nil, false
}

// matching returns the cached catalog if its HEAD sha matches (content unchanged).
func (c *catalogCache) matching(orgID, headSHA string) ([]Skill, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[orgID]
	if ok && e.headSHA == headSHA {
		return e.skills, true
	}
	return nil, false
}

// touch resets the freshness clock without changing content (HEAD revalidated).
func (c *catalogCache) touch(orgID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[orgID]; ok {
		e.fetchedAt = time.Now()
		c.entries[orgID] = e
	}
}

func (c *catalogCache) put(orgID, headSHA string, skills []Skill) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[orgID] = catalogEntry{headSHA: headSHA, skills: skills, fetchedAt: time.Now()}
}

// stale returns the last-known catalog regardless of TTL (degrade path), or nil.
func (c *catalogCache) stale(orgID string) []Skill {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[orgID]; ok {
		return e.skills
	}
	return nil
}

func (c *catalogCache) evict(orgID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, orgID)
}
