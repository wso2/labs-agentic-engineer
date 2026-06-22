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

// Shared value types + pure SKILL.md parsing helpers. The read/write surface
// itself is repo-backed and lives in repo_store.go + reconcile.go (the per-org
// GitHub skills repo is the single source of truth — there is no `skills`
// table). See docs/design/skills-repo-storage.md.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// newSkillValue assembles the in-memory Skill from validated mutation input so
// Create/Update can return the just-written skill WITHOUT a post-commit
// read-back. A read-back through the repo store can transiently return
// (nil, nil) on a GitHub blip (the cache was just evicted by the write), which
// would make the success path hand the controller a nil *Skill and panic.
// Every field here is derivable from what we just committed.
func newSkillValue(orgID, kind, name, skillMD string, refs References, fm skillFrontmatter) *Skill {
	return &Skill{
		OrgID:         orgID,
		Name:          name,
		Kind:          kind,
		Description:   strings.TrimSpace(fm.Description),
		SkillMD:       skillMD,
		References:    map[string]string(refs),
		Version:       versionFromMetadata(fm),
		ContentSHA:    contentSHA(skillMD, refs),
		License:       fm.License,
		Compatibility: fm.Compatibility,
		UpdatedAt:     time.Now().UTC(),
	}
}

// References is the map of optional reference filenames → body for a skill
// (e.g. `references/examples.md`).
type References map[string]string

// Skill re-exports models.Skill — the canonical shared value type lives in
// models so the task feature can reference resolved skills without importing
// the skills package.
type Skill = models.Skill

// SkillSummary is the lightweight projection used in catalogue listings —
// no body, no references.
type SkillSummary struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Version     int    `json:"version"`
	Description string `json:"description"`
	ContentSHA  string `json:"contentSha"`
	Editable    bool   `json:"editable"`
}

// ---- frontmatter parsing ----------------------------------------------------

// skillFrontmatter is the YAML frontmatter shape accepted on SKILL.md.
// Spec-clean AgentSkills: name, description, optional license,
// compatibility, allowed-tools. Platform extensions under metadata.asdlc.*
type skillFrontmatter struct {
	Name          string                 `yaml:"name"`
	Description   string                 `yaml:"description"`
	License       string                 `yaml:"license,omitempty"`
	Compatibility string                 `yaml:"compatibility,omitempty"`
	AllowedTools  any                    `yaml:"allowed-tools,omitempty"`
	Metadata      map[string]interface{} `yaml:"metadata,omitempty"`
}

// parseSkillMD splits frontmatter from body and decodes it. Returns the
// decoded frontmatter, the body, and any parse error.
func parseSkillMD(content string) (skillFrontmatter, string, error) {
	fm, body, err := artifacts.SplitFrontmatter(content)
	if err != nil {
		return skillFrontmatter{}, "", fmt.Errorf("split frontmatter: %w", err)
	}
	if fm == "" {
		return skillFrontmatter{}, "", fmt.Errorf("SKILL.md missing frontmatter")
	}
	var s skillFrontmatter
	if err := yaml.Unmarshal([]byte(fm), &s); err != nil {
		return skillFrontmatter{}, "", fmt.Errorf("decode frontmatter: %w", err)
	}
	if strings.TrimSpace(s.Name) == "" {
		return skillFrontmatter{}, "", fmt.Errorf("frontmatter missing name")
	}
	if strings.TrimSpace(s.Description) == "" {
		return skillFrontmatter{}, "", fmt.Errorf("frontmatter missing description")
	}
	return s, body, nil
}

// versionFromMetadata pulls metadata.asdlc.version out of frontmatter
// (stored as a string-as-int by the spec) and returns the integer
// version. Defaults to 1 when absent.
func versionFromMetadata(s skillFrontmatter) int {
	if s.Metadata == nil {
		return 1
	}
	// Flat dotted-key form — the documented AgentSkills string→string
	// representation: `metadata: { "asdlc.version": "2" }`.
	if v, ok := s.Metadata["asdlc.version"]; ok {
		return coerceVersion(v)
	}
	// Nested form — `metadata: { asdlc: { version: "2" } }`.
	if asdlcAny, ok := s.Metadata["asdlc"]; ok {
		if asdlcMap, ok := asdlcAny.(map[string]interface{}); ok {
			if verAny, ok := asdlcMap["version"]; ok {
				return coerceVersion(verAny)
			}
		}
	}
	return 1
}

// coerceVersion maps an int/float/string YAML scalar to a positive version
// integer, defaulting to 1.
func coerceVersion(v any) int {
	switch t := v.(type) {
	case int:
		if t > 0 {
			return t
		}
	case int64:
		if t > 0 {
			return int(t)
		}
	case float64:
		if t > 0 {
			return int(t)
		}
	case string:
		var n int
		_, _ = fmt.Sscanf(t, "%d", &n)
		if n > 0 {
			return n
		}
	}
	return 1
}

// contentSHA computes a deterministic hash over the canonical concat of
// the SKILL.md body + sorted reference filenames + their contents.
func contentSHA(skillMD string, references map[string]string) string {
	h := sha256.New()
	h.Write([]byte(skillMD))
	keys := make([]string, 0, len(references))
	for k := range references {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	for _, k := range keys {
		h.Write([]byte{'\x00'})
		h.Write([]byte(k))
		h.Write([]byte{'\x00'})
		h.Write([]byte(references[k]))
	}
	return hex.EncodeToString(h.Sum(nil))
}
