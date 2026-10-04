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

// The project skill mirror: `.claude/skills/` inside each PROJECT repo — a
// copy of the coding-relevant slice of the org's skill library, so a build
// (and a developer who clones the repo) has the guidance locally.
// docs/design/draft/2026-08-02-project-skill-mirror-plan.md.
//
// copied = (skill has audience "coding" AND skill.Enabled) OR the skill is
// pinned by a component. The pin union is a drift guard: a skill disabled
// AFTER a component pinned it still lands, so an admin toggle never breaks a
// build — it can only arise through drift, since a disabled skill is absent
// from the design agent's catalog and so cannot be newly pinned.
//
// The org's AE Studio pod owns the copy (its mirror-skills op reads the
// skills library, applies the rule above, and writes and prunes
// `.claude/skills/` in one commit); aep-api resolves the two repositories and
// the component pins.

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// designComponentsPrefix scopes the component design.json walk resolvePinnedSkills
// reads pins from — the same repo path design_json.go's codec targets.
const designComponentsPrefix = "specs/design/components/"

// designJSONSuffix is the per-component design file name, joined onto its
// directory under designComponentsPrefix.
const designJSONSuffix = "/design.json"

// resolvePinnedSkills reads every component's `skillsPinned` at the project
// repository's fetched tip through the pod and returns their union, sorted.
//
// No design yet (a brand-new project repo has no specs/design/ tree at all)
// reads back as zero files, hence zero pins — not an error. A malformed
// component design.json is skipped with a warning rather than aborting the
// whole sync: one bad file must not block every OTHER project's skill guidance
// from refreshing (that specific component simply loses its pins for this
// pass — it re-establishes them once it is next saved).
func (s *SkillService) resolvePinnedSkills(ctx context.Context, ref sourcecontrol.RepoRef) ([]string, error) {
	files, _, err := s.git.ReadBundle(ctx, ref, "", sourcecontrol.BundleFilter{Prefix: designComponentsPrefix, Exts: []string{designJSONSuffix}})
	if err != nil {
		return nil, fmt.Errorf("read component designs: %w", err)
	}
	pinned := map[string]bool{}
	for p, raw := range files {
		rel := strings.TrimPrefix(p, designComponentsPrefix)
		name := strings.TrimSuffix(rel, designJSONSuffix)
		if name == "" || strings.Contains(name, "/") {
			continue // a design.json nested deeper than components/<name>/
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
	return slices.Sorted(maps.Keys(pinned)), nil
}

// SyncProjectSkills refreshes `.claude/skills/` in the project repo to match
// the org's current skill library
// (docs/design/draft/2026-08-02-project-skill-mirror-plan.md) through the
// pod's mirror-skills. The skills repository is provisioned first if the org
// has none yet (ensureSkillsRepo, the same create-and-seed every skills read
// does), so a first project mirrors the seeded library rather than failing on
// a missing repository. The component pins are read next; a failing read
// aborts before any write. The pod reads the library itself and refuses to
// prune on a degraded read, so a GitHub blip never deletes a build's
// guidance.
//
// Callers are best-effort: a mirror failure must never fail project creation,
// a publish, or a dispatch — this method only resolves and mirrors;
// logging-and-continuing is the caller's job. Three call sites, each reaching
// it through its own narrow port so no feature grows a spec edge: the project
// seed (projects.skillMirror, async so GitHub repo creation stays out of the
// create latency), the pre-tag build step (build.SkillMirror, so the version
// tag captures the guidance the build was designed against), and milestone
// dispatch (codingagent.SkillMirror, so the clone the agent works in is
// current).
func (s *SkillService) SyncProjectSkills(ctx context.Context, orgID, projectID string) error {
	if !s.configured() || s.mirror == nil || orgID == "" || projectID == "" {
		return fmt.Errorf("skills: service not configured for project skill sync")
	}
	skillsRow, err := s.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return fmt.Errorf("ensure skills repo: %w", err)
	}
	skillsRef, err := sourcecontrol.RefForRow(orgID, skillsRow)
	if err != nil {
		return fmt.Errorf("resolve skills repository: %w", err)
	}
	repo, err := s.repos.GetRepo(ctx, orgID, projectID)
	if err != nil {
		return fmt.Errorf("get project repo: %w", err)
	}
	projectRef, err := sourcecontrol.RefForRow(orgID, repo)
	if err != nil {
		return fmt.Errorf("resolve project repository: %w", err)
	}
	pinned, err := s.resolvePinnedSkills(ctx, projectRef)
	if err != nil {
		return fmt.Errorf("resolve component skill pins: %w", err)
	}
	if _, err := s.mirror.MirrorSkills(ctx, projectRef, skillsRef, pinned); err != nil {
		return fmt.Errorf("mirror skills: %w", err)
	}
	return nil
}
