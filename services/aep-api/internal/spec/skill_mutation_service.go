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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// Skill mutation sentinels — controllers map these to HTTP status codes.
var (
	// ErrSkillNotEditable is returned for PUT/DELETE against a platform-shipped
	// (org-kind) skill.
	ErrSkillNotEditable = errors.New("skill is read-only")
	// ErrSkillNotFound is returned when a name maps to no editable row.
	ErrSkillNotFound = errors.New("skill not found")
	// ErrSkillNameCollision is returned when a create reuses a visible name.
	ErrSkillNameCollision = errors.New("skill name already in use")
	// ErrSkillRequired is returned when a PATCH tries to disable a skill the
	// coding runner cannot start without. See RequiredSkills.
	ErrSkillRequired = errors.New("skill is required by every coding run and cannot be disabled")
)

// RequiredSkills names the skills the coding runner reads out of the project
// mirror on EVERY run, whatever the design pinned. `aep` is the run's procedure
// and `acceptance-run` replaces its run section for a validation task.
//
// Availability is deliberately not gated on `editable` — an org admin may
// withhold a read-only platform skill from their own library, and that is the
// right default. These two are the exception, because the mirror is the only
// place a run gets its workflow from: disabling `aep` would stop it being
// copied, and every build in the org would then refuse to start (the runner
// fails fast rather than improvise — `requireWorkflowBodies`).
//
// Refused here rather than force-copied in desiredMirror on purpose. Copying a
// disabled skill anyway would leave the console showing `aep` as off while every
// build loaded it, which makes the flag a lie; a refusal tells the admin why.
var RequiredSkills = map[string]bool{
	"aep":            true,
	"acceptance-run": true,
}

// maxSkillBytes caps total skill size (SKILL.md + references). Matches the
// design's 400 KB ceiling.
const maxSkillBytes = 400 * 1024

// reservedSkillNames cannot be used by custom/imported skills — `aep` is the
// base plugin's name; the kind words keep the flat-vs-legacy repo layout
// parser unambiguous while legacy trees still exist.
var reservedSkillNames = map[string]bool{
	"aep":      true,
	"platform": true, "org": true, "custom": true, "imported": true,
	"builtin": true, "flow": true,
}

// reservedSkillPrefixes are used at materialisation time (`<kind>-<name>`);
// custom/imported names must not collide with them. builtin- stays reserved
// so legacy materialisations can never be spoofed.
var reservedSkillPrefixes = []string{"platform-", "org-", "builtin-", "custom-", "imported-"}

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

// CreateSkillInput is the POST body for a new org skill. References is
// OPTIONAL — a skill without reference files is legitimate, and the console's
// create dialog sends only {name, skillMd} (absent ≡ {}).
type CreateSkillInput struct {
	Name       string            `json:"name"`
	SkillMD    string            `json:"skillMd"`
	References map[string]string `json:"references,omitempty"`
}

// UpdateSkillInput is the PUT body for an editable skill (org or imported).
// References is OPTIONAL (absent ≡ {}); an update without it prunes any
// existing reference files.
type UpdateSkillInput struct {
	SkillMD    string            `json:"skillMd"`
	References map[string]string `json:"references,omitempty"`
}

// SkillMutationService owns the org-editable write surface: create/update/
// delete for org and imported skills, and the read-only guard for platform
// skills. Delete additionally consults the reconcile-owned manifest — a
// deletable (user-authored) org name is indistinguishable at the kind level
// from a platform-seeded one, so the manifest's origin entry is what tells
// them apart; a platform-seeded org skill stays undeletable even though it is
// editable. Writes commit directly to the org's skills repo on `main`. See
// docs/design/skills-repo-storage.md §9.
type SkillMutationService struct {
	skills *SkillService
}

func NewSkillMutationService(skills *SkillService) *SkillMutationService {
	return &SkillMutationService{skills: skills}
}

// Create validates and commits a new kind=org skill for the org.
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

	// Stamp the kind into the stored file — the flat layout has no kind dirs,
	// so an unstamped SKILL.md would read back as org anyway, but stamping
	// makes the file self-describing and defeats a spoofed platform marker. A
	// newly authored skill is an org skill (reconcile never touches it: the
	// embedded-skill loop only visits names it ships, and this name has no
	// manifest entry).
	stamped, err := stampFrontmatterKind(in.SkillMD, SkillKindOrg)
	if err != nil {
		return nil, validationErr("FRONTMATTER_INVALID", err.Error(), "skillMd")
	}

	refs := normalizeRefs(in.References)
	msg := fmt.Sprintf("feat(skills): add org skill %q\n\nby %s", name, actor)
	if err := m.skills.writeSkillFiles(ctx, orgID, name, stamped, refs, msg, false, nil); err != nil {
		return nil, fmt.Errorf("commit org skill %q: %w", name, err)
	}
	slog.InfoContext(ctx, "skill created", "orgID", orgID, "name", name, "actor", actor)
	// Return the just-written skill from validated input — no read-back (a
	// transient post-commit read could nil-panic the handler on a real success).
	return newSkillValue(orgID, SkillKindOrg, name, stamped, refs, fm, true), nil
}

// Update rewrites an existing editable skill (org or imported) in place,
// preserving its kind. Returns ErrSkillNotEditable for platform skills,
// ErrSkillNotFound when the name resolves to no row at all.
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
	if !SkillEditable(existing.Kind) {
		return nil, ErrSkillNotEditable // platform is reconcile-managed
	}

	fm, _, err := parseAndValidateSkillMD(in.SkillMD, in.References)
	if err != nil {
		return nil, err
	}
	if fm.Name != name {
		return nil, validationErr("NAME_IMMUTABLE",
			"cannot rename a skill via update; frontmatter name must match the existing name", "name")
	}

	// Preserve the existing kind — editing an org skill (platform-seeded or
	// user-authored) keeps it org, editing an imported skill keeps it imported.
	stamped, err := stampFrontmatterKind(in.SkillMD, existing.Kind)
	if err != nil {
		return nil, validationErr("FRONTMATTER_INVALID", err.Error(), "skillMd")
	}

	refs := normalizeRefs(in.References)

	// Convergence-persist: if this edit brings a platform-managed org skill's
	// content back to the platform's CURRENT embedded version, advance its
	// manifest baseline to that version in the same commit. Otherwise the
	// baseline stays frozen at the version the org last synced from, and the
	// next platform release would read the now-identical copy as an org edit
	// diverged from a stale base — a false conflict (skills-experience spec §3;
	// the console counterpart of reconcile's converged-copy backfill). A
	// divergent edit leaves the entry nil: the baseline stays frozen, which is
	// the correct "override" posture. Only org-kind skills the platform
	// actually ships can converge this way — a user-authored org skill (absent
	// from the library) or an imported skill keeps its own manifest posture
	// untouched.
	var manifestEntry *ManifestEntry
	if existing.Kind == SkillKindOrg {
		embeddedSHA, shipped, serr := m.skills.embeddedContentSHA(name)
		if serr != nil {
			return nil, fmt.Errorf("embedded lookup for %q: %w", name, serr)
		}
		if shipped && contentSHA(stamped, refs) == embeddedSHA {
			manifestEntry = &ManifestEntry{Origin: ManifestOriginPlatform, BaseHash: embeddedSHA}
		}
	}

	msg := fmt.Sprintf("chore(skills): update %s skill %q\n\nby %s", existing.Kind, name, actor)
	// pruneStaleRefs=true: an update may have removed reference files.
	if err := m.skills.writeSkillFiles(ctx, orgID, name, stamped, refs, msg, true, manifestEntry); err != nil {
		return nil, fmt.Errorf("commit update for %q: %w", name, err)
	}
	slog.InfoContext(ctx, "skill updated", "orgID", orgID, "name", name, "actor", actor)
	return newSkillValue(orgID, existing.Kind, name, stamped, refs, fm, existing.Enabled), nil
}

// Delete removes an editable skill (deletes the skill's directory and
// commits). Platform-kind skills return ErrSkillNotEditable — everything
// else, including a platform-seeded org skill (e.g. "go"), is deletable;
// reconcile (see reconcile.go) no longer re-seeds a deleted platform
// default, so the delete sticks. The prior IMPORTED_SKILL_IN_USE guard is
// dropped — with HEAD reads and no snapshots there are no rows to protect
// (docs/design/skills-repo-storage.md §9); an in-flight task simply reads
// HEAD without the deleted skill.
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
	if !SkillEditable(existing.Kind) {
		return ErrSkillNotEditable // platform is reconcile-managed
	}

	msg := fmt.Sprintf("chore(skills): delete %s skill %q\n\nby %s", existing.Kind, name, actor)
	if err := m.skills.deleteSkillDir(ctx, orgID, name, msg); err != nil {
		return fmt.Errorf("delete skill %q: %w", name, err)
	}
	slog.InfoContext(ctx, "skill deleted", "orgID", orgID, "name", name, "actor", actor)
	return nil
}

// SetEnabled flips a skill's manifest-level availability (ADR-0014) without
// touching a single SKILL.md byte — a manifest-only commit, unlike
// writeSkillFiles (which upserts a WHOLE entry and deliberately preserves any
// prior Disabled, so it can never CLEAR the flag). Applies to any visible
// kind — platform-shipped or org-authored, editable or not — because
// availability is an org-admin call, orthogonal to the content-edit guard
// that gates PUT/DELETE. Returns ErrSkillNotFound when the name resolves to
// no row at all, and ErrSkillRequired for the coding run's own workflow.
func (m *SkillMutationService) SetEnabled(ctx context.Context, orgID, actor, name string, enabled bool) (*Skill, error) {
	if m == nil || m.skills == nil {
		return nil, fmt.Errorf("skill mutation service: not configured")
	}
	if !enabled && RequiredSkills[name] {
		return nil, fmt.Errorf("%w: %s", ErrSkillRequired, name)
	}
	existing, err := m.skills.Resolve(ctx, orgID, name)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", name, err)
	}
	if existing == nil {
		return nil, ErrSkillNotFound
	}

	repo, err := m.skills.ensureSkillsRepo(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("ensure skills repo: %w", err)
	}
	// Re-read + re-applied against the CAS attempt's own base inside
	// commitFiles (same lost-update fix as writeSkillFiles/deleteSkillDir): a
	// concurrent commit's manifest entry is folded in, never clobbered. An
	// absent entry is left absent when enabling — that is already the enabled
	// state (ManifestEntry.Disabled's zero value), so there is nothing to
	// clear and no reason to manufacture an empty {origin:"",baseHash:""} row.
	manifestFn := func(mf SkillsManifest) SkillsManifest {
		e, has := mf[name]
		if !has && enabled {
			return mf
		}
		e.Disabled = !enabled
		mf[name] = e
		return mf
	}
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	msg := fmt.Sprintf("chore(skills): %s %s skill %q\n\nby %s", verb, existing.Kind, name, actor)
	if _, err := m.skills.commitFiles(ctx, orgID, repo, msg, nil, nil, manifestFn); err != nil {
		return nil, fmt.Errorf("%s skill %q: %w", verb, name, err)
	}
	slog.InfoContext(ctx, "skill availability changed", "orgID", orgID, "name", name, "actor", actor, "enabled", enabled)

	out := *existing
	out.Enabled = enabled
	return &out, nil
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
	for refPath, content := range references {
		if err := validateSkillRefPath(refPath); err != nil {
			return skillFrontmatter{}, "", validationErr("REFERENCE_PATH_INVALID", err.Error(), "references")
		}
		total += len(content)
	}
	if total > maxSkillBytes {
		return skillFrontmatter{}, "", validationErr("SIZE_EXCEEDED",
			fmt.Sprintf("total skill size %d bytes exceeds the %d-byte limit", total, maxSkillBytes), "")
	}
	return fm, body, nil
}

// validateSkillRefPath enforces the storage contract's aux-file path rule for
// user-facing writes (custom create/update, import): the SAME shape the
// loaders accept (repo_store.go's isCatalogPath/parseBundleEntries — any aux
// file, any depth, no extension filter, skipping only SKILL.md itself and
// dotfile segments), reused here as a hard validation error. The loaders can
// silently skip an unwanted blob when reading a repo; explicit user input on
// a write path must be rejected outright rather than silently dropped.
func validateSkillRefPath(refPath string) error {
	if refPath == skillFileName {
		return fmt.Errorf("reference key %q must not shadow %s", refPath, skillFileName)
	}
	if refPath == "" || strings.HasPrefix(refPath, "/") {
		return fmt.Errorf("reference key %q must be a relative path", refPath)
	}
	parts := strings.Split(refPath, "/")
	for _, seg := range parts {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("reference key %q must not contain path traversal or an empty segment", refPath)
		}
	}
	if hasDotSegment(parts) {
		return fmt.Errorf("reference key %q must not contain a dotfile segment", refPath)
	}
	return nil
}

// normalizeRefs returns a non-nil References map from the raw input.
func normalizeRefs(in map[string]string) References {
	out := References{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
