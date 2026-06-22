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

package requirements

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/humakit"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// --- Inputs / Outputs ------------------------------------------------------
// Inputs embed humakit.OrgScopedInput, which declares {orgHandle} and applies
// the tenant gate (the IDOR fence) by construction. The {projectName} and
// per-file path params are sibling fields.

type reqProjectInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
}

type reqFileInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	Name        string `path:"name" doc:"Requirement filename"`
	Body        struct {
		Content string `json:"content" doc:"New file content"`
	}
}

type reqFileDeleteInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	Name        string `path:"name" doc:"Requirement filename"`
}

type reqVersionInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	Tag         string `path:"tag" doc:"Version tag (e.g. v1)"`
}

type generateRequirementFileInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	Name        string `path:"name" doc:"Requirement filename"`
	Body        struct {
		SkillID string   `json:"skillId" doc:"Document-generation skill ID (required)"`
		Sources []string `json:"sources,omitempty" doc:"Optional source filenames to feed the skill"`
		Prompt  string   `json:"prompt,omitempty" doc:"Optional user prompt (for bootstrap skills)"`
	}
}

type requirementsBundleOutput struct{ Body *models.RequirementsBundle }
type requirementsVersionsOutput struct{ Body []models.ArtifactVersion }

// RegisterRequirements registers the requirements feature's non-streaming HTTP
// operations on the Huma API. It is the code-first replacement for
// registerRequirementsRoutes (api/requirements_routes.go): same paths, same
// auth posture, with the spec generated from the typed inputs/outputs.
//
// The streaming generate endpoint (POST .../files/{name}/generate, SSE) is NOT
// migrated here — it stays on the legacy router because Huma's typed-output
// model does not express an http.Flusher event stream.
func RegisterRequirements(api huma.API, svc RequirementsService) {
	prefix := "/api/v1/organizations/{orgHandle}/projects/{projectName}/requirements"

	huma.Register(api, huma.Operation{
		OperationID: "get-requirements",
		Method:      http.MethodGet,
		Path:        prefix,
		Summary:     "Get the requirements bundle",
		Tags:        []string{"Requirements"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *reqProjectInput) (*requirementsBundleOutput, error) {
		out, err := svc.GetRequirements(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to get requirements")
		}
		return &requirementsBundleOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-requirement-file",
		Method:      http.MethodPut,
		Path:        prefix + "/files/{name}",
		Summary:     "Create or update a requirement file",
		Tags:        []string{"Requirements"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *reqFileInput) (*requirementsBundleOutput, error) {
		if in.Name == "" {
			return nil, huma.Error400BadRequest("filename is required")
		}
		out, err := svc.UpdateRequirementFile(ctx, in.OrgHandle, in.ProjectName, in.Name, in.Body.Content)
		if err != nil {
			if errors.Is(err, RequirementsDirLockBusy) {
				return nil, huma.Error409Conflict("another writer is editing the requirements directory")
			}
			return nil, huma.Error500InternalServerError("failed to update requirement file")
		}
		return &requirementsBundleOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-requirement-file",
		Method:      http.MethodDelete,
		Path:        prefix + "/files/{name}",
		Summary:     "Delete a requirement file",
		Tags:        []string{"Requirements"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *reqFileDeleteInput) (*requirementsBundleOutput, error) {
		if in.Name == "" {
			return nil, huma.Error400BadRequest("filename is required")
		}
		out, err := svc.DeleteRequirementFile(ctx, in.OrgHandle, in.ProjectName, in.Name)
		if err != nil {
			if errors.Is(err, RequirementsDirLockBusy) {
				return nil, huma.Error409Conflict("another writer is editing the requirements directory")
			}
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return &requirementsBundleOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "save-requirements",
		Method:      http.MethodPost,
		Path:        prefix + "/save",
		Summary:     "Save the requirements bundle (cut a new version)",
		Tags:        []string{"Requirements"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *reqProjectInput) (*requirementsBundleOutput, error) {
		out, err := svc.SaveAndProceed(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			if errors.Is(err, RequirementsDirLockBusy) {
				return nil, huma.Error409Conflict("another writer is editing the requirements directory")
			}
			if errors.Is(err, artifacts.ErrSpecNotFound) {
				return nil, huma.Error404NotFound("requirements not found")
			}
			return nil, huma.Error500InternalServerError("failed to save requirements")
		}
		return &requirementsBundleOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "discard-requirements",
		Method:      http.MethodPost,
		Path:        prefix + "/discard",
		Summary:     "Discard unsaved requirements changes",
		Tags:        []string{"Requirements"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *reqProjectInput) (*requirementsBundleOutput, error) {
		out, err := svc.DiscardChanges(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			if errors.Is(err, RequirementsDirLockBusy) {
				return nil, huma.Error409Conflict("another writer is editing the requirements directory")
			}
			return nil, huma.Error500InternalServerError("failed to discard requirements")
		}
		return &requirementsBundleOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-requirements-versions",
		Method:      http.MethodGet,
		Path:        prefix + "/versions",
		Summary:     "List requirements versions",
		Tags:        []string{"Requirements"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *reqProjectInput) (*requirementsVersionsOutput, error) {
		versions, err := svc.ListVersions(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list versions")
		}
		return &requirementsVersionsOutput{Body: versions}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-requirements-at-version",
		Method:      http.MethodGet,
		Path:        prefix + "/versions/{tag}",
		Summary:     "Get the requirements bundle at a version tag",
		Tags:        []string{"Requirements"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *reqVersionInput) (*requirementsBundleOutput, error) {
		if in.Tag == "" {
			return nil, huma.Error400BadRequest("tag is required")
		}
		out, err := svc.GetRequirementsAtTag(ctx, in.OrgHandle, in.ProjectName, in.Tag)
		if err != nil {
			if errors.Is(err, artifacts.ErrSpecNotFound) {
				return nil, huma.Error404NotFound("requirements not found at tag")
			}
			return nil, huma.Error500InternalServerError("failed to get requirements at tag")
		}
		return &requirementsBundleOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "generate-requirement-file",
		Method:      http.MethodPost,
		Path:        prefix + "/files/{name}/generate",
		Summary:     "Generate a requirement file via a skill (SSE stream)",
		Tags:        []string{"Requirements"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *generateRequirementFileInput) (*huma.StreamResponse, error) {
		// Pre-stream validation mirrors GenerateRequirementFile: filename + skillId
		// are required before any headers are sent.
		if in.Name == "" {
			return nil, huma.Error400BadRequest("filename is required")
		}
		if in.Body.SkillID == "" {
			return nil, huma.Error400BadRequest("skillId is required")
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", "text/event-stream")
			hctx.SetHeader("Cache-Control", "no-cache")
			hctx.SetHeader("Connection", "keep-alive")
			hctx.SetHeader("X-Accel-Buffering", "no")
			hctx.SetHeader("x-vercel-ai-ui-message-stream", "v1")
			hctx.SetStatus(http.StatusOK)
			w := hctx.BodyWriter()
			flush := func() {
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			flush()
			// Stream errors are surfaced as UI message-stream error frames by the
			// service after headers are sent; the HTTP status cannot change here.
			_ = svc.StreamGenerate(hctx.Context(), in.OrgHandle, in.ProjectName, in.Name, in.Body.SkillID, in.Body.Sources, in.Body.Prompt, w, flush)
		}}, nil
	})
}
