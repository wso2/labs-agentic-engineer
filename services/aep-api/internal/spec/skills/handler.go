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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"sort"
	"unicode/utf8"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/platform/validate"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Handler serves the org-scoped skills catalogue (list/get/create/update/
// delete), the built-in updates badge + sync, and the multipart
// AgentSkills-tarball import. Every operation is org-scoped — the
// deny-by-default tenant gate bound the token org into the context, and the
// handlers pass it to the services as an explicit argument (the services also
// take it as the actor, exactly as the retired Huma handlers did).
type Handler struct {
	skills *spec.SkillService
	mut    *spec.SkillMutationService
	imp    *spec.SkillImportService
}

// New returns the slice's handler.
func New(skills *spec.SkillService, mut *spec.SkillMutationService, imp *spec.SkillImportService) *Handler {
	return &Handler{skills: skills, mut: mut, imp: imp}
}

// skillImportMaxUploadBytes caps the multipart upload the BFF hands to the
// import service — bounds memory on the import path (the service applies its
// own decompressed-payload budget). Mirrors the legacy upload ceiling
// (skills/skill_upload.go's importMaxUploadBytes, which retires with
// skill_huma.go).
const skillImportMaxUploadBytes = 4 << 20 // 4 MiB

func (h *Handler) ListSkills(ctx context.Context, _ gen.ListSkillsRequestObject) (gen.ListSkillsResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	summaries, err := h.skills.ListSummaries(ctx, org)
	if err != nil {
		return nil, mapSkillError(err)
	}
	// repoUrl is the org skills repo's HTML URL (contract
	// SkillSummaryList.repoUrl) — "" while the repo can't be provisioned
	// (e.g. GitHub not connected yet); the console keys its Import dialog's
	// via-pull-request guidance off it.
	out := gen.SkillSummaryList{
		Skills:  make([]gen.SkillSummary, 0, len(summaries)),
		RepoURL: h.skills.RepoWebURL(ctx, org),
	}
	for _, sum := range summaries {
		out.Skills = append(out.Skills, gen.SkillSummary{
			Name:        sum.Name,
			Kind:        sum.Kind,
			Description: sum.Description,
			ContentSha:  sum.ContentSHA,
			Editable:    sum.Editable,
			Deletable:   sum.Deletable,
			// The skills page renders its availability toggle from THIS list,
			// not from the per-skill detail — omitting it made every row read
			// as disabled, so a toggle click sent "enable" for a skill that
			// was already enabled and nothing appeared to happen.
			Enabled: sum.Enabled,
			// The console renders the toggle as unavailable rather than letting
			// the PATCH 409 — a control that only fails is worse than one that
			// explains itself. Served from the server's own list so the policy
			// lives in one place (spec.RequiredSkills), not in the UI.
			Required: spec.RequiredSkills[sum.Name],
		})
	}
	return gen.ListSkills200JSONResponse(out), nil
}

func (h *Handler) CreateSkill(ctx context.Context, request gen.CreateSkillRequestObject) (gen.CreateSkillResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.mut == nil {
		return nil, apierr.ServiceUnavailable("skill mutation not configured")
	}
	in := spec.CreateSkillInput{
		Name:       request.Body.Name,
		SkillMD:    request.Body.SkillMd,
		References: request.Body.References,
	}
	sk, err := h.mut.Create(ctx, org, org, in)
	if err != nil {
		return nil, mapSkillError(err)
	}
	// A freshly created skill can never be platform-seeded — Create's own
	// collision check already rejects any name a visible skill (including a
	// platform-seeded one) already uses — so it is always deletable.
	return gen.CreateSkill201JSONResponse(skillDetailBody(sk, true, true)), nil
}

func (h *Handler) ImportSkill(ctx context.Context, request gen.ImportSkillRequestObject) (gen.ImportSkillResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.imp == nil {
		return nil, apierr.ServiceUnavailable("skill import not configured")
	}
	file, err := multipartFormFilePart(request.Body, "file")
	if err != nil {
		return nil, err
	}
	// Bound the upload to the legacy ceiling, then REJECT an overage rather than
	// importing a prefix of it. io.LimitReader ends a capped read with io.EOF,
	// which is indistinguishable from the end of a small file, so capping alone
	// handed the importer a truncated tarball: the user got "not a valid gzip
	// stream" or "read tar: unexpected EOF" for what was only an oversized file.
	// The service's own importMaxBytes budget does not cover this — it bounds the
	// DECOMPRESSED payload, so a 4.1MiB upload never reaches it intact.
	// Reading one byte past the ceiling is what makes the overage visible; same
	// pattern as spec_collect.go's fetch.
	body, err := io.ReadAll(io.LimitReader(file, skillImportMaxUploadBytes+1))
	if err != nil {
		return nil, apierr.BadRequest("read upload: " + err.Error())
	}
	if len(body) > skillImportMaxUploadBytes {
		return nil, apierr.BadRequest(fmt.Sprintf(
			"skill archive exceeds the %d MiB upload limit", skillImportMaxUploadBytes>>20))
	}
	result, err := h.imp.Import(ctx, org, org, bytes.NewReader(body))
	if err != nil {
		return nil, mapSkillError(err)
	}
	return gen.ImportSkill201JSONResponse(gen.ImportResult{
		Name:          result.Name,
		Kind:          result.Kind,
		License:       result.License,
		Compatibility: result.Compatibility,
		Warnings:      result.Warnings,
	}), nil
}

func (h *Handler) ListSkillUpdates(ctx context.Context, _ gen.ListSkillUpdatesRequestObject) (gen.ListSkillUpdatesResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	updates, err := h.skills.UpdatesAvailable(ctx, org)
	if err != nil {
		return nil, mapSkillError(err)
	}
	// Non-nil so JSON marshals as [] not null — the console reads
	// updates.length directly.
	out := gen.SkillUpdateList{
		Updates: make([]gen.SkillUpdate, 0, len(updates)),
		Count:   int64(len(updates)),
	}
	for _, u := range updates {
		out.Updates = append(out.Updates, gen.SkillUpdate{Name: u.Name, State: gen.SkillUpdateState(u.State)})
	}
	return gen.ListSkillUpdates200JSONResponse(out), nil
}

func (h *Handler) SyncSkills(ctx context.Context, _ gen.SyncSkillsRequestObject) (gen.SyncSkillsResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	updated, err := h.skills.Reconcile(ctx, org)
	if err != nil {
		return nil, mapSkillError(err)
	}
	return gen.SyncSkills200JSONResponse(gen.SkillSyncOutput{
		Status:  "synced",
		Updated: int64(updated),
	}), nil
}

func (h *Handler) GetSkill(ctx context.Context, request gen.GetSkillRequestObject) (gen.GetSkillResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if err := requireSlug("name", request.Name); err != nil {
		return nil, err
	}
	sk, err := h.skills.Resolve(ctx, org, request.Name)
	if err != nil {
		return nil, mapSkillError(err)
	}
	if sk == nil {
		return nil, apierr.NotFound("skill not found")
	}
	return gen.GetSkill200JSONResponse(skillDetailBody(sk, skillEditable(sk.Kind), spec.SkillDeletable(sk.Kind))), nil
}

func (h *Handler) UpdateSkill(ctx context.Context, request gen.UpdateSkillRequestObject) (gen.UpdateSkillResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.mut == nil {
		return nil, apierr.ServiceUnavailable("skill mutation not configured")
	}
	if err := requireSlug("name", request.Name); err != nil {
		return nil, err
	}
	in := spec.UpdateSkillInput{
		SkillMD:    request.Body.SkillMd,
		References: request.Body.References,
	}
	sk, err := h.mut.Update(ctx, org, org, request.Name, in)
	if err != nil {
		return nil, mapSkillError(err)
	}
	// Update preserves kind, and deletability is a pure kind check.
	return gen.UpdateSkill200JSONResponse(skillDetailBody(sk, true, spec.SkillDeletable(sk.Kind))), nil
}

func (h *Handler) DeleteSkill(ctx context.Context, request gen.DeleteSkillRequestObject) (gen.DeleteSkillResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.mut == nil {
		return nil, apierr.ServiceUnavailable("skill mutation not configured")
	}
	if err := requireSlug("name", request.Name); err != nil {
		return nil, err
	}
	if err := h.mut.Delete(ctx, org, org, request.Name); err != nil {
		return nil, mapSkillError(err)
	}
	return gen.DeleteSkill200JSONResponse(map[string]any{
		"status": "deleted",
		"name":   request.Name,
	}), nil
}

// SetSkillEnabled flips a skill's availability for the org — withholding it
// from the agents, or restoring it — without touching a single SKILL.md byte
// (ADR-0014). It is a PATCH rather than part of UpdateSkill precisely because
// UpdateSkill rewrites content: an availability change that altered contentSHA
// would read as a divergence from the platform baseline and surface as a
// pending platform update.
//
// Deliberately NOT gated on editability. Availability is an org-admin call over
// its own library, orthogonal to who may edit a skill's text — so a read-only
// platform skill can still be switched off. The exception is spec.RequiredSkills:
// the coding run reads its workflow out of the project mirror, so disabling those
// would stop them being copied and every build in the org would refuse to start.
func (h *Handler) SetSkillEnabled(ctx context.Context, request gen.SetSkillEnabledRequestObject) (gen.SetSkillEnabledResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if h.mut == nil {
		return nil, apierr.ServiceUnavailable("skill mutation not configured")
	}
	if err := requireSlug("name", request.Name); err != nil {
		return nil, err
	}
	sk, err := h.mut.SetEnabled(ctx, org, org, request.Name, request.Body.Enabled)
	if err != nil {
		return nil, mapSkillError(err)
	}
	// Availability leaves kind untouched, so both derived flags are pure kind
	// checks, exactly as on the GET.
	return gen.SetSkillEnabled200JSONResponse(skillDetailBody(sk, skillEditable(sk.Kind), spec.SkillDeletable(sk.Kind))), nil
}

// skillDetailBody projects a resolved Skill + the derived editable/deletable
// flags onto the contract's SkillDetailBody (the full single-skill response).
// Binary aux files are pulled out of `references` and listed in
// `binaryReferences` (see splitBinaryReferences) so invalid-UTF-8 content is
// never mangled by the JSON encoder.
func skillDetailBody(sk *spec.Skill, editable, deletable bool) gen.SkillDetailBody {
	refs, binary := splitBinaryReferences(sk.References)
	return gen.SkillDetailBody{
		OrgID:            sk.OrgID,
		Name:             sk.Name,
		Kind:             sk.Kind,
		Description:      sk.Description,
		SkillMd:          sk.SkillMD,
		References:       refs,
		BinaryReferences: binary,
		ContentSha:       sk.ContentSHA,
		License:          sk.License,
		Compatibility:    sk.Compatibility,
		UpdatedAt:        sk.UpdatedAt,
		Editable:         editable,
		Deletable:        deletable,
		// Read back from the manifest by loadCatalog, so the console can render
		// the toggle's current state rather than guessing it.
		Enabled: sk.Enabled,
		// See the list projection: one source of truth for what may not be
		// disabled, and it is the server's.
		Required: spec.RequiredSkills[sk.Name],
	}
}

// splitBinaryReferences partitions a skill's aux-file map for the JSON
// response boundary: encoding/json never errors on invalid UTF-8 — it
// silently replaces bad bytes with U+FFFD — so a binary aux file (an image,
// etc.) would come back mangled if inlined into `references` without the
// encoder ever noticing. Non-UTF-8 entries are pulled out and listed by path
// (sorted) in `binaryReferences` instead; their content is never inlined.
func splitBinaryReferences(references map[string]string) (map[string]string, []string) {
	refs := make(map[string]string, len(references))
	binary := make([]string, 0)
	for path, content := range references {
		if utf8.ValidString(content) {
			refs[path] = content
			continue
		}
		binary = append(binary, path)
	}
	sort.Strings(binary)
	return refs, binary
}

// skillEditable is a thin call to the package spec's single editability seam
// — org + imported are editable, platform is reconcile-managed and read-only.
func skillEditable(kind string) bool {
	return spec.SkillEditable(kind)
}

// requireSlug validates a single DNS-label slug path param, returning a 400
// envelope error on failure. Delegates to validate.Slug.
func requireSlug(name, v string) error {
	if err := validate.Slug(v); err != nil {
		return apierr.BadRequest(name + ": " + err.Error())
	}
	return nil
}

// multipartFormFilePart advances the strict server's multipart.Reader to the
// named file field and returns that part as a stream. A body without the
// field keeps the retired Huma handler's 400.
func multipartFormFilePart(body *multipart.Reader, field string) (io.Reader, error) {
	if body == nil {
		return nil, apierr.BadRequest("missing '" + field + "' field (tarball)")
	}
	for {
		part, err := body.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, apierr.BadRequest("missing '" + field + "' field (tarball)")
		}
		if err != nil {
			return nil, apierr.BadRequest("can't decode multipart body: " + err.Error())
		}
		if part.FormName() == field {
			return part, nil
		}
	}
}

// mapSkillError translates skill service sentinels + structured validation
// errors onto the flat envelope, mirroring the retired Huma handler's status
// classification.
func mapSkillError(err error) error {
	var verr *spec.SkillValidationError
	switch {
	case errors.As(err, &verr):
		return apierr.BadRequest(verr.Error())
	case errors.Is(err, spec.ErrSkillNameCollision):
		return apierr.Conflict(err.Error())
	case errors.Is(err, spec.ErrSkillNotEditable):
		return apierr.Forbidden("built-in skills are read-only")
	case errors.Is(err, spec.ErrSkillRequired):
		// Conflict, not Forbidden: the caller has every right to manage their
		// library — this one skill just cannot be absent from it. The message says
		// what breaks, because "forbidden" alone invites a retry.
		return apierr.Conflict(
			"this skill carries the coding run's workflow and cannot be disabled — " +
				"every build in this organization would refuse to start without it",
		)
	case errors.Is(err, spec.ErrSkillNotFound):
		return apierr.NotFound("skill not found")
	}
	return apierr.WithCause(apierr.Internal("internal error"), err)
}
