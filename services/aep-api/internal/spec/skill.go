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

import (
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// Skill is the resolved shape that flows from the `org-skills` repo to the
// architect input, the tech-lead input, and the console. Mirrors the stored
// SKILL.md 1:1 plus a few derived fields. (The coding runner no longer receives
// this shape over the wire — it clones `org-skills` and resolves applied skills
// locally.)
//
// Skill is the resolved skill value type, owned by the spec domain (skills
// live under specs/). Cross-domain consumers reference it as spec.Skill.
type Skill struct {
	OrgID       string `json:"orgId"`
	Name        string `json:"name"`
	Kind        string `json:"kind"` // platform | org | imported
	Description string `json:"description"`
	SkillMD     string `json:"skillMd"`
	// References holds all auxiliary files relative to the skill dir —
	// scripts/, references/, assets/, and any extras; values are raw bytes.
	References    map[string]string `json:"references"`
	ContentSHA    string            `json:"contentSha"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	UpdatedAt     time.Time         `json:"updatedAt"`
	// Enabled mirrors the skills-manifest.json entry's Disabled flag, inverted
	// (see ManifestEntry.Disabled — stored negative so an absent entry means
	// enabled). Every constructor of a catalog-visible Skill must set this
	// explicitly; Go's bool zero value is false, the opposite of the
	// no-manifest-entry default.
	Enabled bool `json:"enabled"`
	// Audience names the agent(s) this skill's guidance is written for
	// (ADR-0013, mirrored from the TS agents service's SkillAudience): "design"
	// | "coding". Derived from frontmatter `metadata.aep.audience` by
	// frontmatterAudience — an empty/absent list means EVERY audience (the
	// permissive default), so an unmarked or org-authored skill stays visible
	// everywhere it always was. Ownership (Kind) and Audience are independent
	// axes: a skill can be the org's to edit while being coding-only to read.
	Audience []string `json:"audience"`
}

// Skill audiences. A SKILL.md declares its audience in frontmatter
// `metadata.aep.audience` as a string list; unrecognised values are dropped
// and an empty/absent list means every audience (see frontmatterAudience).
const (
	SkillAudienceDesign = "design"
	SkillAudienceCoding = "coding"
)

// Skill kinds. A SKILL.md declares its kind in frontmatter `metadata.aep.kind`;
// absent means
// SkillKindOrg. platform + org are platform-shipped and reconciled from the
// embedded library (org is also the kind a newly-authored skill is stamped
// with — a user-created skill and a platform-seeded one share the org kind;
// reconcile tells them apart via the skills-manifest.json baseline, not the
// kind); imported is user-owned and stamped on write. The retired
// SkillKindCustom kind has folded into org (frontmatterKind reads a stored
// "custom" back as org for back-compat).
const (
	// SkillKindPlatform — generation-flow guidance; hidden from the skills
	// page and the updates badge (was kind "flow").
	SkillKindPlatform = "platform"
	// SkillKindOrg — the org-visible stack skills, platform-seeded or
	// user-authored; feeds coding-runner skillsPinned (was kind "builtin").
	SkillKindOrg = "org"
	// SkillKindImported — imported from an AgentSkills tarball; editable.
	SkillKindImported = "imported"
)

// SkillEditable is the single editability rule, decoupled from ownership so
// platform-seeded skills can be edited without changing kind. Today: org +
// imported are editable, the AE-owned platform (agent-workflow/generation
// guidance) kind is read-only.
func SkillEditable(kind string) bool { return kind != SkillKindPlatform }

// SkillDeletable is the deletability rule. It currently equals SkillEditable
// — reconcile (see reconcile.go) no longer re-seeds a deleted platform
// default, so a platform-seeded org skill (e.g. "go") is deletable like any
// other org skill and simply stays gone. The rule is kept as its own named
// function, decoupled from SkillEditable, because the two axes CAN diverge:
// a future editable-but-managed platform skill would be editable without
// being deletable. Mirrors Delete's own guard (skill_mutation_service.go)
// exactly, so a summary/detail response never promises a delete the mutation
// service would then refuse.
func SkillDeletable(kind string) bool { return SkillEditable(kind) }

// SkillsRepoSentinelProjectID re-exports the skills repo row's reserved
// project id (sourcecontrol owns the git_repositories vocabulary); consumers
// reference it as spec.*. See docs/design/skills-repo-storage.md §10.1.
const SkillsRepoSentinelProjectID = sourcecontrol.SkillsRepoSentinelProjectID
