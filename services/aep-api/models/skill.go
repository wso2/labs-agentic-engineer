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

package models

import "time"

// Skill is the resolved shape that flows from the `org-skills` repo to the
// architect input, the tech-lead input, and the console. Mirrors the stored
// SKILL.md 1:1 plus a few derived fields. (The coding runner no longer receives
// this shape over the wire — it clones `org-skills` and resolves applied skills
// locally.)
//
// Lives in models (the shared value-type layer) so the skills feature and its
// consumers can reference it without crossing a feature boundary. The skills
// package keeps a `type Skill = models.Skill` alias.
type Skill struct {
	OrgID         string            `json:"orgId"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind"` // platform | org | custom | imported
	Description   string            `json:"description"`
	SkillMD       string            `json:"skillMd"`
	References    map[string]string `json:"references"`
	ContentSHA    string            `json:"contentSha"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

// Skill kinds (docs/design/skills-unified-library-migration.md §3.2). A
// SKILL.md declares its kind in frontmatter `metadata.aep.kind`; absent means
// SkillKindOrg. platform + org are platform-shipped and reconciled from the
// embedded library; custom + imported are user-owned and stamped on write.
const (
	// SkillKindPlatform — generation-flow guidance; hidden from the skills
	// page and the updates badge (was kind "flow").
	SkillKindPlatform = "platform"
	// SkillKindOrg — the org-visible stack skills; read-only on the skills
	// page, feeds coding-runner skillsApplied (was kind "builtin").
	SkillKindOrg = "org"
	// SkillKindCustom — user-authored via create/update; editable.
	SkillKindCustom = "custom"
	// SkillKindImported — imported from an AgentSkills tarball; editable.
	SkillKindImported = "imported"
)

// SkillsRepoSentinelProjectID is the reserved git_repositories.project_id under
// which the per-org skills repo row lives (so it is distinguishable from real
// project repos). Defined in models — a neutral package — so both the skills
// feature and gitrepo (which skips it from clone pre-warm; the skills repo is
// API-read-only, never cloned) reference one constant.
// See docs/design/skills-repo-storage.md §10.1.
const SkillsRepoSentinelProjectID = "_skills"

// SkillsRepoDirName is the pinned on-disk directory leaf for the per-org
// skills repo on the shared workspace volume:
// repos/<orgId>/_skills/org-skills/ (design shared-volume-clone-architecture
// §4). It is deliberately NOT the row's repo_slug: the slug is owner-prefixed
// (lower(<owner>-<repo>)) and would change if the org reconnects under a
// different GitHub owner, while the agents service — which never receives a
// path — derives the skills snapshot dir structurally from this fixed leaf
// (services/agents/src/shared/snapshot-path.ts). One org, one skills repo,
// one constant.
const SkillsRepoDirName = "org-skills"
