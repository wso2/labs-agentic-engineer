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

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SKILL.md frontmatter, copied from aep-api (spec/skill_service.go and
// SplitFrontmatter in spec/artifact_store.go): see the package's known
// duplicate note.

// frontmatter is the part of a SKILL.md's YAML frontmatter the catalog reads.
type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Metadata    struct {
		Aep struct {
			Kind     string   `yaml:"kind,omitempty"`
			Audience []string `yaml:"audience,omitempty"`
		} `yaml:"aep,omitempty"`
	} `yaml:"metadata,omitempty"`
}

// parseSkillMD decodes a SKILL.md's frontmatter; it must exist and name a
// non-blank name and description.
func parseSkillMD(content string) (frontmatter, error) {
	raw, err := splitFrontmatter(content)
	if err != nil {
		return frontmatter{}, err
	}
	if raw == "" {
		return frontmatter{}, errors.New("SKILL.md missing frontmatter")
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(raw), &fm); err != nil {
		return frontmatter{}, fmt.Errorf("decode frontmatter: %w", err)
	}
	if strings.TrimSpace(fm.Name) == "" {
		return frontmatter{}, errors.New("frontmatter missing name")
	}
	if strings.TrimSpace(fm.Description) == "" {
		return frontmatter{}, errors.New("frontmatter missing description")
	}
	return fm, nil
}

// splitFrontmatter returns the YAML between a leading `---` line and the
// next `\n---`, trimmed; "" when content does not open with one (leading
// whitespace and a BOM allowed).
func splitFrontmatter(content string) (string, error) {
	trimmed := strings.TrimPrefix(strings.TrimLeft(content, " \t\r\n"), "\ufeff")
	if !strings.HasPrefix(trimmed, "---") {
		return "", nil
	}
	rest := strings.TrimLeft(trimmed[3:], " \t")
	if !strings.HasPrefix(rest, "\n") && !strings.HasPrefix(rest, "\r\n") {
		return "", nil
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", errors.New("frontmatter: unterminated --- block")
	}
	return strings.TrimSpace(rest[:end]), nil
}

// frontmatterKind is metadata.aep.kind when it names a kind, else org (the
// retired "custom" included).
func frontmatterKind(fm frontmatter) string {
	switch k := strings.TrimSpace(fm.Metadata.Aep.Kind); k {
	case kindPlatform, kindOrg, kindImported:
		return k
	default:
		return kindOrg
	}
}

// frontmatterAudience is metadata.aep.audience filtered to design and
// coding; empty (or naming neither) is both. It must agree with the TS
// agents' derivation, which reads the same frontmatter.
func frontmatterAudience(fm frontmatter) []string {
	out := make([]string, 0, len(fm.Metadata.Aep.Audience))
	for _, a := range fm.Metadata.Aep.Audience {
		switch a = strings.TrimSpace(a); a {
		case AudienceDesign, AudienceCoding:
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return []string{AudienceDesign, AudienceCoding}
	}
	return out
}
