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

// Skill is the resolved shape that flows from the `skills` table to the
// architect input, the tech-lead input, the runner pull endpoint, and the
// console. Mirrors the row schema 1:1 plus a few derived fields.
//
// Lives in models (the shared value-type layer) so both the skills feature and
// the task feature (which snapshots resolved skills per design version) can
// reference it without crossing a feature boundary. The skills package keeps a
// `type Skill = models.Skill` alias.
type Skill struct {
	OrgID         string            `json:"orgId"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind"` // builtin | custom | imported
	Description   string            `json:"description"`
	SkillMD       string            `json:"skillMd"`
	References    map[string]string `json:"references"`
	Version       int               `json:"version"`
	ContentSHA    string            `json:"contentSha"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

// MaterializedName / PrefixedID are the pure skill-naming helpers shared by the
// skills feature and task's skill snapshotting.
func MaterializedName(kind, name string) string { return kind + "-" + name }
func PrefixedID(kind, name string) string       { return kind + "/" + name }

// SkillsRepoSentinelProjectID is the reserved git_repositories.project_id under
// which the per-org skills repo row lives (so it is distinguishable from real
// project repos). Defined in models — a neutral package — so both the skills
// feature and gitrepo (which skips it from clone pre-warm; the skills repo is
// API-read-only, never cloned) reference one constant.
// See docs/design/skills-repo-storage.md §10.1.
const SkillsRepoSentinelProjectID = "_skills"
