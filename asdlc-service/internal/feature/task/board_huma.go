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

package task

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/humakit"
)

type boardInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
}

type boardOutput struct{ Body *ProjectBoard }

// RegisterBoard registers the board feature's org-scoped GET on the Huma API.
// Code-first replacement for registerBoardRoutes (api/board_routes.go): same
// path, method, and auth posture. Pass the BoardService from app.go.
func RegisterBoard(api huma.API, svc BoardService) {
	huma.Register(api, huma.Operation{
		OperationID: "get-board",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/board",
		Summary:     "Get a project's kanban board",
		Tags:        []string{"Board"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *boardInput) (*boardOutput, error) {
		board, err := svc.GetBoard(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to get board")
		}
		return &boardOutput{Body: board}, nil
	})
}
