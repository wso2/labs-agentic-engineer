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

// The project skill mirror: the BFF is the single writer of `.claude/skills/`
// inside each PROJECT repo — a copy of the coding-relevant slice of the org's
// skill library, so a build (and a developer who clones the repo) has the
// guidance locally. docs/design/draft/2026-08-02-project-skill-mirror-plan.md.
//
// copied = (skill has audience "coding" AND skill.Enabled) OR the skill is
// pinned by a component. The pin union is a drift guard: a skill disabled
// AFTER a component pinned it still lands, so an admin toggle never breaks a
// build — it can only arise through drift, since a disabled skill is absent
// from the design agent's catalog and so cannot be newly pinned.
//
// `files.Apply` (internals.go validatePath) cannot write `.claude/` — only
// specs/ is in scope for that surface, and the write path is never widened.
// The mirror instead drives Workspace().Mutate directly, modelled on
// commitFiles (repo_store.go): deletes staged before writes, one commit.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// claudeSkillsDir is the root of the mirrored tree inside a project repo, with
// no trailing slash — Walk/Delete match it (or anything nested under it) the
// same way commitFiles' deletePrefixes do.
const claudeSkillsDir = ".claude/skills"

// designComponentsPrefix scopes the component design.json walk resolvePinnedSkills
// reads pins from — the same repo path design_json.go's codec targets.
const designComponentsPrefix = "specs/design/components/"

// designJSONSuffix is the per-component design file name, joined onto its
// directory under designComponentsPrefix.
const designJSONSuffix = "/design.json"

// syncProjectSkillsMessage is SyncProjectSkills' commit message.
const syncProjectSkillsMessage = "chore(skills): refresh .claude/skills from the org library"

// desiredMirror is the PURE computation of the `.claude/skills/` tree for one
// project: every enabled coding-audience skill in the org library, plus
// anything a component pinned (pinned takes precedence over Enabled/Audience —
// the drift guard). Paths are `.claude/skills/<name>/SKILL.md` plus each
// reference at `.claude/skills/<name>/<refPath>`. The result is deterministic
// in content (map iteration order does not affect the byte content written
// to any given path), which is what lets Mutate's diff-first behaviour
// recognise an up-to-date repo and commit nothing.
func desiredMirror(lib []Skill, pinned map[string]bool) map[string][]byte {
	out := map[string][]byte{}
	for _, sk := range lib {
		copied := (sk.Enabled && audienceIncludesCoding(sk.Audience)) || pinned[sk.Name]
		if !copied {
			continue
		}
		dir := claudeSkillsDir + "/" + sk.Name
		out[dir+"/SKILL.md"] = []byte(sk.SkillMD)
		for refPath, content := range sk.References {
			out[dir+"/"+refPath] = []byte(content)
		}
	}
	return out
}

// audienceIncludesCoding reports whether audience names the coding agent. A
// nil/empty list defaults to BOTH audiences (the same permissive default
// frontmatterAudience already applies when parsing a SKILL.md) — repeated
// here defensively so desiredMirror's copy rule is correct even if handed a
// Skill whose Audience was never round-tripped through frontmatter parsing
// (e.g. constructed directly in a test or a future caller).
func audienceIncludesCoding(audience []string) bool {
	if len(audience) == 0 {
		return true
	}
	for _, a := range audience {
		if a == SkillAudienceCoding {
			return true
		}
	}
	return false
}

// resolvePinnedSkills reads every component's `skillsPinned` at the project
// repo ref's tree and returns their union as a set. This is a plain
// Workspace read (its own flock), always called BEFORE the SyncProjectSkills
// Mutate opens — Workspace forbids nested calls (a read's flock and Mutate's
// exclusive lock would self-deadlock on the same repo).
//
// No design yet (a brand-new project repo has no specs/design/ tree at all)
// reads back as zero files, hence zero pins — not an error. A malformed
// component design.json is skipped with a warning rather than aborting the
// whole sync: one bad file must not block every OTHER project's skill guidance
// from refreshing (that specific component simply loses its pins for this
// pass — it re-establishes them once it is next saved).
func (s *SkillService) resolvePinnedSkills(ctx context.Context, ref sourcecontrol.WorkspaceRef) (map[string]bool, error) {
	keep := func(rel string) bool {
		return strings.HasPrefix(rel, designComponentsPrefix) && strings.HasSuffix(rel, designJSONSuffix)
	}
	files, _, err := s.git.Workspace().ReadBundle(ctx, ref, "", keep)
	if err != nil {
		return nil, fmt.Errorf("read component designs: %w", err)
	}
	pinned := map[string]bool{}
	for p, raw := range files {
		rel := strings.TrimPrefix(p, designComponentsPrefix)
		name := strings.TrimSuffix(rel, designJSONSuffix)
		if name == "" || strings.Contains(name, "/") {
			continue // not a direct components/<name>/design.json (keep already filters; defensive)
		}
		comp, perr := parseComponentDesignJSON(name, raw)
		if perr != nil {
			slog.WarnContext(ctx, "skills: skipping unparseable component design.json for pin resolution", "component", name, "error", perr)
			continue
		}
		for _, pinnedName := range comp.SkillsPinned {
			pinned[pinnedName] = true
		}
	}
	return pinned, nil
}

// SyncProjectSkills refreshes `.claude/skills/` in the project repo to match
// the org's current skill library (docs/design/draft/2026-08-02-project-skill-mirror-plan.md).
// Order matters: the org library is resolved FIRST via ListForMirror — an
// error-surfacing read — entirely OUTSIDE the project repo's Mutate closure,
// then the component pins, THEN the desired tree is computed, and only then
// does the single Mutate open. A failing library read aborts here with no
// writes and no deletes attempted: pruning on a degraded read would delete a
// build's guidance because GitHub blipped, which is far worse than serving a
// stale copy for one more cycle.
//
// The commit is diff-first for content (Mutate reports Changed:false and
// commits nothing when the tree is already current) but pruning is explicit:
// the Mutate closure walks the base tree's `.claude/skills/` and stages a
// Delete for every path absent from the desired set, mirroring commitFiles'
// prefix-delete-then-write shape.
//
// Callers are best-effort: a mirror failure must never fail project creation,
// a publish, or a dispatch — this method only computes and commits;
// logging-and-continuing is the caller's job. Three call sites, each reaching
// it through its own narrow port so no feature grows a spec edge: the project
// seed (projects.skillMirror, async so GitHub repo creation stays out of the
// create latency), the pre-tag build step (build.SkillMirror, so the version
// tag captures the guidance the build was designed against), and milestone
// dispatch (codingagent.SkillMirror, so the clone the agent works in is
// current).
func (s *SkillService) SyncProjectSkills(ctx context.Context, orgID, projectID string) error {
	if s == nil || s.git == nil || s.repos == nil || orgID == "" || projectID == "" {
		return fmt.Errorf("skills: service not configured for project skill sync")
	}

	lib, err := s.ListForMirror(ctx, orgID)
	if err != nil {
		return fmt.Errorf("resolve org skill library: %w", err)
	}

	repo, err := s.repos.GetRepo(ctx, orgID, projectID)
	if err != nil {
		return fmt.Errorf("get project repo: %w", err)
	}
	ref, err := sourcecontrol.ResolveWorkspaceRef(ctx, s.git.Resolver(), orgID, repo)
	if err != nil {
		return fmt.Errorf("resolve project workspace ref: %w", err)
	}

	pinned, err := s.resolvePinnedSkills(ctx, ref)
	if err != nil {
		return fmt.Errorf("resolve component skill pins: %w", err)
	}

	desired := desiredMirror(lib, pinned)
	author, committer := s.git.ResolveSaveIdentities(ref.Cred)

	_, err = s.git.Workspace().Mutate(ctx, ref, func(tx sourcecontrol.Tx) error {
		// Deletes staged BEFORE writes (Tx is last-op-wins per path — see
		// commitFiles), so a path being rewritten this commit survives its own
		// prune walk.
		if walkErr := tx.Base().Walk(claudeSkillsDir, func(rel, _ string) error {
			if rel != claudeSkillsDir && !strings.HasPrefix(rel, claudeSkillsDir+"/") {
				return nil // Walk matches by raw string prefix; guard the dir boundary explicitly.
			}
			if _, ok := desired[rel]; !ok {
				tx.Delete(rel)
			}
			return nil
		}); walkErr != nil {
			return fmt.Errorf("walk %q for prune: %w", claudeSkillsDir, walkErr)
		}
		for p, data := range desired {
			tx.Write(p, data)
		}
		return nil
	}, sourcecontrol.CommitOpts{Message: syncProjectSkillsMessage, Author: author, Committer: committer})
	return err
}
