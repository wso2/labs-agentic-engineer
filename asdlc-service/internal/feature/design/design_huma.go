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
// the tenant gate (the IDOR fence) by construction. Sibling path params
// (projectName, tag, componentName, path) are plain fields with path tags.

type designProjectInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
}

type designTagInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	Tag         string `path:"tag" doc:"Design version tag (e.g. v1-2)"`
}

type designFileInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	Path        string `path:"path" doc:"File path under specs/design/"`
	Body        struct {
		Content string `json:"content" doc:"New file contents"`
	}
}

type deleteDesignFileInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	Path        string `path:"path" doc:"File path under specs/design/"`
}

type deleteComponentInput struct {
	humakit.OrgScopedInput
	ProjectName   string `path:"projectName" doc:"Project name (DNS-label slug)"`
	ComponentName string `path:"componentName" doc:"Component directory name under components/"`
}

type generateDesignInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
}

type designOutput struct{ Body *models.Design }
type designBundleOutput struct{ Body *DesignBundle }
type designVersionsOutput struct{ Body []models.ArtifactVersion }

// RegisterDesign registers the design feature's NON-STREAMING HTTP operations
// on the Huma API. It is the code-first replacement for registerDesignRoutes
// (api/design_routes.go): same paths, same auth posture, with the spec
// generated from the typed inputs/outputs.
//
// The streaming POST .../design/generate operation (GenerateDesign) is NOT
// registered here — it uses text/event-stream + http.Flusher + a Vercel AI UI
// message stream that does not fit Huma's typed-output model; it is handled
// separately on the raw mux.
func RegisterDesign(api huma.API, svc DesignService) {
	huma.Register(api, huma.Operation{
		OperationID: "get-design",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/design",
		Summary:     "Get the assembled design",
		Tags:        []string{"Design"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *designProjectInput) (*designOutput, error) {
		design, err := svc.GetDesign(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			return nil, mapDesignError(err)
		}
		// A missing design is a 200 with a nil body (mirrors the controller).
		return &designOutput{Body: design}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-design-bundle",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/design/bundle",
		Summary:     "Get the design bundle (file map + assembled design)",
		Tags:        []string{"Design"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *designProjectInput) (*designBundleOutput, error) {
		bundle, err := svc.GetDesignBundle(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			return nil, mapDesignError(err)
		}
		return &designBundleOutput{Body: bundle}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-design-file",
		Method:      http.MethodPut,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/design/files/{path...}",
		Summary:     "Write a single file under specs/design/",
		Tags:        []string{"Design"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *designFileInput) (*designBundleOutput, error) {
		if in.Path == "" {
			return nil, huma.Error400BadRequest("path is required")
		}
		bundle, err := svc.UpdateDesignFile(ctx, in.OrgHandle, in.ProjectName, in.Path, in.Body.Content)
		if err != nil {
			return nil, mapDesignError(err)
		}
		return &designBundleOutput{Body: bundle}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-design-file",
		Method:      http.MethodDelete,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/design/files/{path...}",
		Summary:     "Delete a single file under specs/design/",
		Tags:        []string{"Design"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *deleteDesignFileInput) (*designBundleOutput, error) {
		if in.Path == "" {
			return nil, huma.Error400BadRequest("path is required")
		}
		bundle, err := svc.DeleteDesignFile(ctx, in.OrgHandle, in.ProjectName, in.Path)
		if err != nil {
			return nil, mapDesignError(err)
		}
		return &designBundleOutput{Body: bundle}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-design-component",
		Method:      http.MethodDelete,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/design/components/{componentName}",
		Summary:     "Delete a component directory under components/",
		Tags:        []string{"Design"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *deleteComponentInput) (*designBundleOutput, error) {
		if in.ComponentName == "" {
			return nil, huma.Error400BadRequest("componentName is required")
		}
		bundle, err := svc.DeleteComponent(ctx, in.OrgHandle, in.ProjectName, in.ComponentName)
		if err != nil {
			return nil, mapDesignError(err)
		}
		return &designBundleOutput{Body: bundle}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "save-design",
		Method:      http.MethodPost,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/design/save",
		Summary:     "Save the design and proceed",
		Tags:        []string{"Design"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *designProjectInput) (*designOutput, error) {
		design, err := svc.SaveAndProceed(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			if errors.Is(err, artifacts.ErrDesignNotFound) {
				return nil, huma.Error404NotFound("design not found")
			}
			if errors.Is(err, ErrSpecNotApproved) {
				return nil, huma.Error409Conflict("save requirements first — no v<N> baseline tag")
			}
			return nil, huma.Error500InternalServerError("failed to save and proceed design")
		}
		return &designOutput{Body: design}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "discard-design-changes",
		Method:      http.MethodPost,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/design/discard",
		Summary:     "Discard unsaved design changes",
		Tags:        []string{"Design"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *designProjectInput) (*designOutput, error) {
		design, err := svc.DiscardChanges(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to discard design changes")
		}
		return &designOutput{Body: design}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-design-versions",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/design/versions",
		Summary:     "List design versions",
		Tags:        []string{"Design"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *designProjectInput) (*designVersionsOutput, error) {
		versions, err := svc.ListDesignVersions(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list design versions")
		}
		return &designVersionsOutput{Body: versions}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-design-at-tag",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/design/versions/{tag}",
		Summary:     "Get the assembled design at a tag",
		Tags:        []string{"Design"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *designTagInput) (*designOutput, error) {
		if in.Tag == "" {
			return nil, huma.Error400BadRequest("tag is required")
		}
		design, err := svc.GetDesignAtTag(ctx, in.OrgHandle, in.ProjectName, in.Tag)
		if err != nil {
			if errors.Is(err, artifacts.ErrDesignNotFound) {
				return nil, huma.Error404NotFound("design not found")
			}
			return nil, huma.Error500InternalServerError("failed to get design at tag")
		}
		return &designOutput{Body: design}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-design-bundle-at-tag",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/design/versions/{tag}/bundle",
		Summary:     "Get the design bundle at a tag",
		Tags:        []string{"Design"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *designTagInput) (*designBundleOutput, error) {
		if in.Tag == "" {
			return nil, huma.Error400BadRequest("tag is required")
		}
		bundle, err := svc.GetDesignBundleAtTag(ctx, in.OrgHandle, in.ProjectName, in.Tag)
		if err != nil {
			if errors.Is(err, artifacts.ErrDesignNotFound) {
				return nil, huma.Error404NotFound("design not found")
			}
			return nil, huma.Error500InternalServerError("failed to get design bundle at tag")
		}
		return &designBundleOutput{Body: bundle}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "generate-design",
		Method:      http.MethodPost,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/design/generate",
		Summary:     "Generate the design (architect agent, SSE stream)",
		Tags:        []string{"Design"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *generateDesignInput) (*huma.StreamResponse, error) {
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
			_ = svc.StreamGenerateDesign(hctx.Context(), in.OrgHandle, in.ProjectName, w, flush)
		}}, nil
	})
}

// mapDesignError translates the design feature's sentinel errors into RFC 9457
// problem responses. The non-tag GET/bundle/file/component operations map every
// service failure to a 500 in the legacy controller; the tag + save operations
// carry their own 404/409 branches inline at the call site.
func mapDesignError(err error) error {
	switch {
	case errors.Is(err, artifacts.ErrDesignNotFound):
		return huma.Error404NotFound("design not found")
	}
	return huma.Error500InternalServerError("internal error")
}
