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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// Skill mutation sentinels — controllers map these to HTTP status codes.
var (
	// ErrSkillNotEditable is returned for PUT/DELETE against a builtin.
	ErrSkillNotEditable = errors.New("skill is read-only (builtin)")
	// ErrSkillNotFound is returned when a name maps to no editable row.
	ErrSkillNotFound = errors.New("skill not found")
	// ErrSkillNameCollision is returned when a create reuses a visible name.
	ErrSkillNameCollision = errors.New("skill name already in use")
)

// maxSkillBytes caps total skill size (SKILL.md + references). Matches the
// design's 400 KB ceiling.
const maxSkillBytes = 400 * 1024

// reservedSkillNames cannot be used by custom/imported skills — `asdlc` is
// the base plugin's name.
var reservedSkillNames = map[string]bool{"asdlc": true}

// reservedSkillPrefixes are used at materialisation time; custom/imported
// names must not collide with them.
var reservedSkillPrefixes = []string{"builtin-", "custom-", "imported-"}

// skillNameRE is the AgentSkills kebab rule: lowercase alphanumeric
// segments joined by single hyphens; no leading/trailing/consecutive
// hyphen. Stricter than validate.Slug (which allows trailing/double
// hyphens) because the name is also the AgentSkills directory + frontmatter
// `name:`.
var skillNameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// maxCustomNameLen leaves room for the 9-char materialisation prefix
// (`imported-`) within AgentSkills' 64-char ceiling.
const maxCustomNameLen = 55

// SkillValidationIssue mirrors the design's { code, message, path } shape.
type SkillValidationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

// SkillValidationError carries one or more structured issues. The
// controller renders it as 400 with the issues array; nothing persists
// when this is returned.
type SkillValidationError struct {
	Issues []SkillValidationIssue
}

func (e *SkillValidationError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, i := range e.Issues {
		parts = append(parts, i.Code+": "+i.Message)
	}
	return "skill validation failed: " + strings.Join(parts, "; ")
}

func validationErr(code, message, path string) *SkillValidationError {
	return &SkillValidationError{Issues: []SkillValidationIssue{{Code: code, Message: message, Path: path}}}
}

// CreateSkillInput is the POST body for a custom skill.
type CreateSkillInput struct {
	Name       string            `json:"name"`
	SkillMD    string            `json:"skillMd"`
	References map[string]string `json:"references"`
}

// UpdateSkillInput is the PUT body for a custom skill.
type UpdateSkillInput struct {
	SkillMD    string            `json:"skillMd"`
	References map[string]string `json:"references"`
}

// SkillMutationService owns the org-editable write surface: create/update/
// delete for custom skills, delete for imported skills, and the read-only
// guard for builtins. Writes commit directly to the org's skills repo on
// `main`. See docs/design/skills-repo-storage.md §9.
type SkillMutationService struct {
	skills *SkillService
}

func NewSkillMutationService(skills *SkillService) *SkillMutationService {
	return &SkillMutationService{skills: skills}
}

// Create validates and commits a new kind=custom skill for the org.
func (m *SkillMutationService) Create(ctx context.Context, orgID, actor string, in CreateSkillInput) (*Skill, error) {
	if m == nil || m.skills == nil {
		return nil, fmt.Errorf("skill mutation service: not configured")
	}
	name := strings.TrimSpace(in.Name)
	if issues := validateSkillName(name); issues != nil {
		return nil, issues
	}
	fm, _, err := parseAndValidateSkillMD(in.SkillMD, in.References)
	if err != nil {
		return nil, err
	}
	if fm.Name != name {
		return nil, validationErr("NAME_MISMATCH",
			fmt.Sprintf("frontmatter name %q must equal the request name %q", fm.Name, name), "name")
	}

	// Collision: any visible skill (builtin or this org's custom/imported).
	// Use a fresh read so a same-named skill just created on this replica is
	// seen and a transient read failure surfaces (rather than a phantom
	// "no collision" that would silently overwrite). The cross-replica TOCTOU
	// window remains — git has no unique constraint (docs §9).
	existing, err := m.skills.resolveFresh(ctx, orgID, name)
	if err != nil {
		return nil, fmt.Errorf("collision check: %w", err)
	}
	if existing != nil {
		return nil, ErrSkillNameCollision
	}

	refs := normalizeRefs(in.References)
	msg := fmt.Sprintf("feat(skills): add custom skill %q\n\nby %s", name, actor)
	if err := m.skills.writeSkillFiles(ctx, orgID, "custom", name, in.SkillMD, refs, msg, false); err != nil {
		return nil, fmt.Errorf("commit custom skill %q: %w", name, err)
	}
	slog.InfoContext(ctx, "skill created", "orgID", orgID, "name", name, "actor", actor)
	// Return the just-written skill from validated input — no read-back (a
	// transient post-commit read could nil-panic the handler on a real success).
	return newSkillValue(orgID, "custom", name, in.SkillMD, refs, fm), nil
}

// Update rewrites an existing kind=custom skill. Returns ErrSkillNotEditable
// for builtins, ErrSkillNotFound when the name is not an editable custom row.
func (m *SkillMutationService) Update(ctx context.Context, orgID, actor, name string, in UpdateSkillInput) (*Skill, error) {
	if m == nil || m.skills == nil {
		return nil, fmt.Errorf("skill mutation service: not configured")
	}
	existing, err := m.skills.Resolve(ctx, orgID, name)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", name, err)
	}
	if existing == nil {
		return nil, ErrSkillNotFound
	}
	if existing.Kind == "builtin" {
		return nil, ErrSkillNotEditable
	}
	if existing.Kind != "custom" {
		// imported skills are replaced via re-import, not PUT.
		return nil, ErrSkillNotFound
	}

	fm, _, err := parseAndValidateSkillMD(in.SkillMD, in.References)
	if err != nil {
		return nil, err
	}
	if fm.Name != name {
		return nil, validationErr("NAME_IMMUTABLE",
			"cannot rename a skill via update; frontmatter name must match the existing name", "name")
	}

	refs := normalizeRefs(in.References)
	msg := fmt.Sprintf("chore(skills): update custom skill %q\n\nby %s", name, actor)
	// pruneStaleRefs=true: an update may have removed reference files.
	if err := m.skills.writeSkillFiles(ctx, orgID, "custom", name, in.SkillMD, refs, msg, true); err != nil {
		return nil, fmt.Errorf("commit update for %q: %w", name, err)
	}
	slog.InfoContext(ctx, "skill updated", "orgID", orgID, "name", name, "actor", actor)
	return newSkillValue(orgID, "custom", name, in.SkillMD, refs, fm), nil
}

// Delete removes a custom or imported skill (deletes the skill's directory and
// commits). Builtins return ErrSkillNotEditable. The prior
// IMPORTED_SKILL_IN_USE guard is dropped — with HEAD reads and no snapshots
// there are no rows to protect (docs/design/skills-repo-storage.md §9); an
// in-flight task simply reads HEAD without the deleted skill.
func (m *SkillMutationService) Delete(ctx context.Context, orgID, actor, name string) error {
	if m == nil || m.skills == nil {
		return fmt.Errorf("skill mutation service: not configured")
	}
	existing, err := m.skills.Resolve(ctx, orgID, name)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", name, err)
	}
	if existing == nil {
		return ErrSkillNotFound
	}
	if existing.Kind == "builtin" {
		return ErrSkillNotEditable
	}

	msg := fmt.Sprintf("chore(skills): delete %s skill %q\n\nby %s", existing.Kind, name, actor)
	if err := m.skills.deleteSkillDir(ctx, orgID, existing.Kind, name, msg); err != nil {
		return fmt.Errorf("delete skill %q: %w", name, err)
	}
	slog.InfoContext(ctx, "skill deleted", "orgID", orgID, "name", name, "actor", actor)
	return nil
}

// ---- shared validation helpers ---------------------------------------------

// validateSkillName enforces the AgentSkills kebab rule, the custom-name
// length cap, and reserved-name rules.
func validateSkillName(name string) *SkillValidationError {
	if name == "" {
		return validationErr("NAME_REQUIRED", "name is required", "name")
	}
	if len(name) > maxCustomNameLen {
		return validationErr("NAME_TOO_LONG",
			fmt.Sprintf("name must be ≤ %d chars (leaves room for the materialisation prefix)", maxCustomNameLen), "name")
	}
	if !skillNameRE.MatchString(name) {
		return validationErr("NAME_INVALID",
			"name must be lowercase kebab-case: alphanumeric segments joined by single hyphens, no leading/trailing/double hyphen", "name")
	}
	if reservedSkillNames[name] {
		return validationErr("NAME_RESERVED", fmt.Sprintf("%q is a reserved name", name), "name")
	}
	for _, p := range reservedSkillPrefixes {
		if strings.HasPrefix(name, p) {
			return validationErr("NAME_RESERVED",
				fmt.Sprintf("name must not start with the reserved prefix %q", p), "name")
		}
	}
	return nil
}

// parseAndValidateSkillMD parses the frontmatter, enforces description
// length, reference-key shape, and total size. Returns the parsed
// frontmatter + body, or a SkillValidationError.
func parseAndValidateSkillMD(skillMD string, references map[string]string) (skillFrontmatter, string, error) {
	if strings.TrimSpace(skillMD) == "" {
		return skillFrontmatter{}, "", validationErr("SKILL_MD_REQUIRED", "skillMd is required", "skillMd")
	}
	fm, body, err := parseSkillMD(skillMD)
	if err != nil {
		return skillFrontmatter{}, "", validationErr("FRONTMATTER_INVALID", err.Error(), "skillMd")
	}
	if n := len(strings.TrimSpace(fm.Description)); n < 1 || n > 1024 {
		return skillFrontmatter{}, "", validationErr("DESCRIPTION_LENGTH",
			"description must be 1–1024 chars", "description")
	}
	total := len(skillMD)
	for path, content := range references {
		if !strings.HasPrefix(path, "references/") || !strings.HasSuffix(path, ".md") {
			return skillFrontmatter{}, "", validationErr("REFERENCE_PATH_INVALID",
				fmt.Sprintf("reference key %q must be of the form references/<file>.md", path), "references")
		}
		if strings.Contains(path, "..") || strings.Contains(path, "//") {
			return skillFrontmatter{}, "", validationErr("REFERENCE_PATH_INVALID",
				fmt.Sprintf("reference key %q must not contain path traversal", path), "references")
		}
		total += len(content)
	}
	if total > maxSkillBytes {
		return skillFrontmatter{}, "", validationErr("SIZE_EXCEEDED",
			fmt.Sprintf("total skill size %d bytes exceeds the %d-byte limit", total, maxSkillBytes), "")
	}
	return fm, body, nil
}

// normalizeRefs returns a non-nil References map from the raw input.
func normalizeRefs(in map[string]string) References {
	out := References{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

// MarshalValidationIssues is a JSON helper for controllers rendering a
// SkillValidationError into the wire body.
func MarshalValidationIssues(e *SkillValidationError) []byte {
	b, _ := json.Marshal(map[string]any{"error": "Bad Request", "issues": e.Issues})
	return b
}
