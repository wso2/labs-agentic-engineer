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
)

// --- Inputs / Outputs ------------------------------------------------------
// Inputs embed humakit.OrgScopedInput, which declares {orgHandle} and applies
// the tenant gate (the IDOR fence) by construction.

type chatUndoInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	TurnID      string `path:"turnId" doc:"Chat turn identifier to undo"`
}

type chatBaselineFileInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	BaselineID  string `path:"baselineId" doc:"Session baseline snapshot identifier"`
	Name        string `path:"name" doc:"Requirement filename"`
}

type chatBaselineInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	BaselineID  string `path:"baselineId" doc:"Session baseline snapshot identifier"`
}

type streamChatInput struct {
	humakit.OrgScopedInput
	ProjectName string          `path:"projectName" doc:"Project name (DNS-label slug)"`
	Body        ChatTurnRequest `doc:"Chat turn request (message is required)"`
}

type chatUndoOutput struct {
	Body struct {
		Files map[string]string `json:"files"`
	}
}

type chatBaselineFileOutput struct{ Body *RequirementsSnapshotFile }

// RegisterRequirementsChat registers the requirements-chat feature's
// non-streaming HTTP operations on the Huma API: the per-turn undo endpoint and
// the per-file session-baseline endpoints (View original / Revert / Drop). It
// is the code-first replacement for the non-streaming subset of
// registerRequirementsChatRoutes (api/requirements_chat_routes.go).
//
// The chat SSE stream (POST .../chat, text/event-stream) is NOT migrated here —
// it stays on the legacy router because Huma's typed-output model does not
// express an http.Flusher event stream.
func RegisterRequirementsChat(api huma.API, svc RequirementsChatService) {
	prefix := "/api/v1/organizations/{orgHandle}/projects/{projectName}/requirements"

	huma.Register(api, huma.Operation{
		OperationID: "undo-requirements-chat-turn",
		Method:      http.MethodPost,
		Path:        prefix + "/chat/turns/{turnId}/undo",
		Summary:     "Undo a requirements-chat turn",
		Tags:        []string{"Requirements"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *chatUndoInput) (*chatUndoOutput, error) {
		if in.TurnID == "" {
			return nil, huma.Error400BadRequest("turnId is required")
		}
		files, err := svc.UndoTurn(ctx, in.OrgHandle, in.ProjectName, in.TurnID)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		out := &chatUndoOutput{}
		out.Body.Files = files
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-requirements-chat-baseline-file",
		Method:      http.MethodGet,
		Path:        prefix + "/chat/baseline/{baselineId}/files/{name}",
		Summary:     "Get a requirement file as captured in the chat-session baseline",
		Tags:        []string{"Requirements"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *chatBaselineFileInput) (*chatBaselineFileOutput, error) {
		if in.BaselineID == "" || in.Name == "" {
			return nil, huma.Error400BadRequest("baselineId and filename are required")
		}
		res, err := svc.GetSessionBaselineFile(ctx, in.OrgHandle, in.ProjectName, in.BaselineID, in.Name)
		if err != nil {
			if errors.Is(err, artifacts.ErrArtifactNotFound) {
				return nil, huma.Error404NotFound("baseline snapshot not found")
			}
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return &chatBaselineFileOutput{Body: res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "revert-requirements-chat-baseline-file",
		Method:        http.MethodPost,
		Path:          prefix + "/chat/baseline/{baselineId}/files/{name}/revert",
		Summary:       "Revert a requirement file to the chat-session baseline",
		Tags:          []string{"Requirements"},
		Security:      humakit.SecurityUserJWT,
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *chatBaselineFileInput) (*struct{}, error) {
		if in.BaselineID == "" || in.Name == "" {
			return nil, huma.Error400BadRequest("baselineId and filename are required")
		}
		if err := svc.RevertFileToBaseline(ctx, in.OrgHandle, in.ProjectName, in.BaselineID, in.Name); err != nil {
			if errors.Is(err, RequirementsDirLockBusy) {
				return nil, huma.Error409Conflict("chat_in_progress")
			}
			if errors.Is(err, artifacts.ErrArtifactNotFound) {
				return nil, huma.Error404NotFound("baseline snapshot not found")
			}
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return &struct{}{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "drop-requirements-chat-baseline",
		Method:        http.MethodDelete,
		Path:          prefix + "/chat/baseline/{baselineId}",
		Summary:       "Drop the chat-session baseline snapshot",
		Tags:          []string{"Requirements"},
		Security:      humakit.SecurityUserJWT,
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *chatBaselineInput) (*struct{}, error) {
		if in.BaselineID == "" {
			return nil, huma.Error400BadRequest("baselineId is required")
		}
		if err := svc.DropSessionBaseline(ctx, in.OrgHandle, in.ProjectName, in.BaselineID); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return &struct{}{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "stream-requirements-chat",
		Method:      http.MethodPost,
		Path:        prefix + "/chat",
		Summary:     "Stream a requirements-chat turn (SSE)",
		Tags:        []string{"Requirements"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *streamChatInput) (*huma.StreamResponse, error) {
		// Pre-stream validation mirrors StreamChat: message is required before any
		// headers are sent.
		if in.Body.Message == "" {
			return nil, huma.Error400BadRequest("message is required")
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
			_ = svc.StreamChat(hctx.Context(), in.OrgHandle, in.ProjectName, in.Body, w, flush)
		}}, nil
	})
}
