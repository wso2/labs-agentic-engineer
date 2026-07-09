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
	"errors"
	"io"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/aep/aep-api/internal/platform/humakit"
	"github.com/wso2/aep/aep-api/internal/platform/validate"
)

// --- Inputs / Outputs ------------------------------------------------------
// Inputs embed humakit.OrgScopedInput, whose Resolve binds the active org from the verified token (no {orgHandle} path param) and applies
// the tenant gate (the IDOR fence) by construction. The {name} ops add the
// AgentSkills slug path param, validated in-handler with the same validate.Slug
// check the legacy controller used.

type listSkillsInput struct {
	humakit.OrgScopedInput
}

type skillNameInput struct {
	humakit.OrgScopedInput
	Name string `path:"name" doc:"Skill name (slug)"`
}

type createSkillInput struct {
	humakit.OrgScopedInput
	Body CreateSkillInput
}

type updateSkillInput struct {
	humakit.OrgScopedInput
	Name string `path:"name" doc:"Skill name (slug)"`
	Body UpdateSkillInput
}

// importSkillForm is the multipart body for the import upload: a single
// `file` field carrying the gzip-compressed AgentSkills tarball.
type importSkillForm struct {
	File huma.FormFile `form:"file" contentType:"application/octet-stream" required:"true" doc:"Gzip-compressed AgentSkills tarball"`
}

type importSkillInput struct {
	humakit.OrgScopedInput
	RawBody huma.MultipartFormFiles[importSkillForm]
}

// skillDetailBody is the full single-skill response — the resolved Skill plus
// the derived `editable` flag. Mirrors the legacy controller's skillDetail.
type skillDetailBody struct {
	Skill
	Editable bool `json:"editable"`
}

type skillDetailOutput struct{ Body skillDetailBody }

type skillListOutput struct {
	Body map[string]any
}

type skillUpdatesOutput struct {
	Body map[string]any
}

type skillSyncOutput struct {
	Body map[string]any
}

type skillDeleteOutput struct {
	Body map[string]any
}

type importResultOutput struct{ Body *ImportResult }

// RegisterSkill registers the org-scoped skills catalogue operations on the
// Huma API. Same auth posture as every org-scoped op (the active org is bound
// from the verified token — no {orgHandle} path param). Built-ins are read-only
// — PUT/DELETE against them surface 403.
//
// The base is /skills (not /org/skills): skills are their own resource
// collection, and the org-config consolidation dropped the redundant /org
// prefix across the whole surface so no /org/* routes remain (§2a of
// docs/design/org-config-consolidation.md). Skills are NOT part of /config;
// only the path prefix changed.
func RegisterSkill(
	api huma.API,
	skillSvc *SkillService,
	mutationSvc *SkillMutationService,
	importSvc *SkillImportService,
) {
	const base = "/skills"

	huma.Register(api, huma.Operation{
		OperationID: "list-skills",
		Method:      http.MethodGet,
		Path:        base,
		Summary:     "List skills",
		Tags:        []string{"Skills"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *listSkillsInput) (*skillListOutput, error) {
		summaries, err := skillSvc.ListSummaries(ctx, in.OrgHandle)
		if err != nil {
			return nil, mapSkillError(err)
		}
		return &skillListOutput{Body: map[string]any{"skills": summaries}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-skill",
		Method:        http.MethodPost,
		Path:          base,
		Summary:       "Create a custom skill",
		Tags:          []string{"Skills"},
		Security:      humakit.SecurityUserJWT,
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createSkillInput) (*skillDetailOutput, error) {
		if mutationSvc == nil {
			return nil, huma.Error503ServiceUnavailable("skill mutation not configured")
		}
		sk, err := mutationSvc.Create(ctx, in.OrgHandle, in.OrgHandle, in.Body)
		if err != nil {
			return nil, mapSkillError(err)
		}
		return &skillDetailOutput{Body: skillDetailBody{Skill: *sk, Editable: true}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "import-skill",
		Method:        http.MethodPost,
		Path:          base + "/import",
		Summary:       "Import an AgentSkills tarball",
		Tags:          []string{"Skills"},
		Security:      humakit.SecurityUserJWT,
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *importSkillInput) (*importResultOutput, error) {
		if importSvc == nil {
			return nil, huma.Error503ServiceUnavailable("skill import not configured")
		}
		file := in.RawBody.Data().File
		if !file.IsSet || file.File == nil {
			return nil, huma.Error400BadRequest("missing 'file' field (tarball)")
		}
		// Cap the bytes handed to the import service to the legacy upload
		// ceiling; the service applies its own decompressed-payload budget.
		var reader io.Reader = io.LimitReader(file, importMaxUploadBytes)
		result, err := importSvc.Import(ctx, in.OrgHandle, in.OrgHandle, reader)
		if err != nil {
			return nil, mapSkillError(err)
		}
		return &importResultOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-skill-updates",
		Method:      http.MethodGet,
		Path:        base + "/updates",
		Summary:     "List available built-in skill updates",
		Tags:        []string{"Skills"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *listSkillsInput) (*skillUpdatesOutput, error) {
		updates, err := skillSvc.UpdatesAvailable(ctx, in.OrgHandle)
		if err != nil {
			return nil, mapSkillError(err)
		}
		if updates == nil {
			updates = []SkillUpdate{}
		}
		return &skillUpdatesOutput{Body: map[string]any{
			"updates": updates,
			"count":   len(updates),
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "sync-skills",
		Method:      http.MethodPost,
		Path:        base + "/sync",
		Summary:     "Sync built-in skills",
		Tags:        []string{"Skills"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *listSkillsInput) (*skillSyncOutput, error) {
		updated, err := skillSvc.Reconcile(ctx, in.OrgHandle)
		if err != nil {
			return nil, mapSkillError(err)
		}
		return &skillSyncOutput{Body: map[string]any{
			"status":  "synced",
			"updated": updated,
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-skill",
		Method:      http.MethodGet,
		Path:        base + "/{name}",
		Summary:     "Get a skill",
		Tags:        []string{"Skills"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *skillNameInput) (*skillDetailOutput, error) {
		if err := validate.Slug(in.Name); err != nil {
			return nil, huma.Error400BadRequest("name: " + err.Error())
		}
		sk, err := skillSvc.Resolve(ctx, in.OrgHandle, in.Name)
		if err != nil {
			return nil, mapSkillError(err)
		}
		if sk == nil {
			return nil, huma.Error404NotFound("skill not found")
		}
		return &skillDetailOutput{Body: skillDetailBody{Skill: *sk, Editable: isUserKind(sk.Kind)}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-skill",
		Method:      http.MethodPut,
		Path:        base + "/{name}",
		Summary:     "Update a custom skill",
		Tags:        []string{"Skills"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *updateSkillInput) (*skillDetailOutput, error) {
		if mutationSvc == nil {
			return nil, huma.Error503ServiceUnavailable("skill mutation not configured")
		}
		if err := validate.Slug(in.Name); err != nil {
			return nil, huma.Error400BadRequest("name: " + err.Error())
		}
		sk, err := mutationSvc.Update(ctx, in.OrgHandle, in.OrgHandle, in.Name, in.Body)
		if err != nil {
			return nil, mapSkillError(err)
		}
		return &skillDetailOutput{Body: skillDetailBody{Skill: *sk, Editable: true}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-skill",
		Method:      http.MethodDelete,
		Path:        base + "/{name}",
		Summary:     "Delete a custom or imported skill",
		Tags:        []string{"Skills"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *skillNameInput) (*skillDeleteOutput, error) {
		if mutationSvc == nil {
			return nil, huma.Error503ServiceUnavailable("skill mutation not configured")
		}
		if err := validate.Slug(in.Name); err != nil {
			return nil, huma.Error400BadRequest("name: " + err.Error())
		}
		if err := mutationSvc.Delete(ctx, in.OrgHandle, in.OrgHandle, in.Name); err != nil {
			return nil, mapSkillError(err)
		}
		return &skillDeleteOutput{Body: map[string]any{"status": "deleted", "name": in.Name}}, nil
	})
}

// mapSkillError translates skill service sentinels + structured validation
// errors into RFC 9457 problem responses, mirroring the legacy controller's
// writeMutationError status classification (this is the migration's new error
// contract — the bespoke { code } body is dropped in favour of standard
// problem details).
func mapSkillError(err error) error {
	var verr *SkillValidationError
	switch {
	case errors.As(err, &verr):
		return huma.Error400BadRequest(verr.Error())
	case errors.Is(err, ErrSkillNameCollision):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, ErrSkillNotEditable):
		return huma.Error403Forbidden("built-in skills are read-only")
	case errors.Is(err, ErrSkillNotFound):
		return huma.Error404NotFound("skill not found")
	}
	return huma.Error500InternalServerError("internal error")
}
