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

package design

import (
	"log/slog"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/wso2/aep/aep-api/internal/clients/agents"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	embedskills "github.com/wso2/aep/aep-api/skills"
)

// The high-level-architecture skill is embedded (never org-repo-sourced): it is
// design-authoring guidance (component decomposition + the unified dependency
// model), not a design-attachable per-component coding skill, so it is NOT
// bootstrapped into the `skills` table the way the architect's four stack
// builtins are. It is loaded once from the embedded FS and pushed on every
// architect call. This mirrors how the task-planner always has its
// task-breakdown skill present (embedded, pushed directly on the wire) rather
// than resolved from the org catalogue. See ADR-0005 and
// skills/high-level-architecture/SKILL.md.
var (
	highLevelArchOnce  sync.Once
	highLevelArchSkill *agents.SkillRecord
)

// loadHighLevelArchitectureSkill reads + parses the embedded
// high-level-architecture SKILL.md into a wire SkillRecord (name/description
// from the frontmatter; Body is the full SKILL.md, matching the inlining the
// architect prompt does for platform skills). A parse failure logs and returns
// nil so the architect degrades to its built-in prompt guidance rather than
// failing the stream. Mirrors loadTaskBreakdownSkill in the task feature.
func loadHighLevelArchitectureSkill() *agents.SkillRecord {
	highLevelArchOnce.Do(func() {
		raw, err := embedskills.ArchitectFS.ReadFile(embedskills.HighLevelArchitectureSkillPath)
		if err != nil {
			slog.Error("architect: embedded high-level-architecture skill read failed", "error", err)
			return
		}
		fm, _, err := artifacts.SplitFrontmatter(string(raw))
		if err != nil {
			slog.Error("architect: high-level-architecture skill frontmatter split failed", "error", err)
			return
		}
		var meta struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
		}
		if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
			slog.Error("architect: high-level-architecture skill frontmatter decode failed", "error", err)
			return
		}
		if strings.TrimSpace(meta.Name) == "" {
			slog.Error("architect: high-level-architecture skill missing frontmatter name")
			return
		}
		highLevelArchSkill = &agents.SkillRecord{
			Name:        strings.TrimSpace(meta.Name),
			Description: strings.TrimSpace(meta.Description),
			Body:        string(raw),
		}
	})
	return highLevelArchSkill
}
