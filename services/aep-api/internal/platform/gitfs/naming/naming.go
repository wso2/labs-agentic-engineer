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

package naming

import (
	"regexp"
	"strings"
)

// The workspace-naming vocabulary — the single source of the conventions that
// map a repo row to its on-disk location under repos/<org>/<project>/<slug>/.
//
// This is a pure leaf beside the gitfs engine — it imports only stdlib, so a
// package can use the workspace-naming vocabulary WITHOUT pulling in the engine's
// secrets.Credential dependency (which would cycle via models). gitfs OWNS the
// workspace layout (the engine
// creates repos/, computes RepoRef paths). It was previously in models/, where
// the reaper (a gitfs sub-package), four features, the domain, and the
// composition root each reached for it — one convention re-derived across
// uncoupled packages, the §11.3 smell. Now they share one contract in the
// package that defines the layout it describes. models/ re-exports these for
// legacy callers until each becomes a domain.

// SkillsRepoSentinelProjectID is the reserved git_repositories.project_id under
// which the per-org skills repo row lives (so it is distinguishable from real
// project repos). See docs/design/skills-repo-storage.md §10.1.
const SkillsRepoSentinelProjectID = "_skills"

// SkillsRepoDirName is the pinned on-disk directory leaf for the per-org skills
// repo on the shared workspace volume: repos/<orgId>/_skills/org-skills/. It is
// deliberately NOT the row's repo_slug (which is owner-prefixed and would change
// if the org reconnects under a different GitHub owner); the agents service
// derives the skills snapshot dir structurally from this fixed leaf. One org,
// one skills repo, one constant.
const SkillsRepoDirName = "org-skills"

// repoURLPattern extracts `<owner>/<repo>` from a GitHub HTTPS URL. Matches both
// `https://github.com/owner/repo` and `.../repo.git`.
var repoURLPattern = regexp.MustCompile(`github\.com/([^/]+/[^/]+?)(?:\.git)?/?$`)

// slugForURL returns the canonical repo slug for a GitHub HTTPS URL — the
// `owner/repo` path lowercased with `/` replaced by `-`. Returns "" if the URL
// doesn't match the GitHub HTTPS pattern (caller decides whether to backfill or
// fail). Mirrors phase2.md §9.1.
func slugForURL(repoURL string) string {
	m := repoURLPattern.FindStringSubmatch(repoURL)
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(m[1], "/", "-"))
}

// WorkspaceSlug returns the on-disk directory leaf for a repo row on the shared
// workspace volume (design D6: a pure function of the row's identity). For the
// per-org skills repo the leaf is the pinned SkillsRepoDirName; for everything
// else it is repoSlug, backfilled from repoURL for pre-phase2 rows.
//
// A free function of (projectID, repoSlug, repoURL) rather than a method, so it
// does not require the GitRepository struct — which is exactly what lets the
// reaper compute a coordinate without importing the entity.
func WorkspaceSlug(projectID, repoSlug, repoURL string) string {
	if projectID == SkillsRepoSentinelProjectID {
		return SkillsRepoDirName
	}
	if repoSlug != "" {
		return repoSlug
	}
	return slugForURL(repoURL)
}
