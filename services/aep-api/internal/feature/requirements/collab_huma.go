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
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/humakit"
)

// --- Inputs / Outputs ------------------------------------------------------
// GetCollabSession is org-scoped, so its input embeds humakit.OrgScopedInput
// (the tenant gate). ValidateCollabAccess is the server-to-server carve-out:
// the org is recovered from the verified JWT, not the path, so its input does
// NOT embed OrgScopedInput. Both read the Authorization header for the display
// identity; validate also reads X-Room-Id.

type collabSessionInput struct {
	humakit.OrgScopedInput
	ProjectName   string `path:"projectName" doc:"Project name (DNS-label slug)"`
	Authorization string `header:"Authorization" doc:"Bearer token; the display identity is decoded from it"`
}

type collabValidateInput struct {
	RoomID        string `header:"X-Room-Id" doc:"Collaboration room ID (spec-<org>-<project>)"`
	Authorization string `header:"Authorization" doc:"Bearer token; the display identity is decoded from it"`
}

type collabSessionOutput struct {
	Body struct {
		RoomID   string `json:"roomId"`
		WSURL    string `json:"wsUrl"`
		UserName string `json:"userName"`
		Email    string `json:"email"`
	}
}

type collabValidateOutput struct {
	Body struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		// ProjectName is the room's project, resolved by this oracle from
		// `spec-<org>-<project>` — the collab service can't split the room ID
		// itself (it doesn't know the org) and needs this for the seed read (#114).
		ProjectName string `json:"projectName"`
	}
}

// RegisterCollab registers the collaboration feature's HTTP operations on the
// Huma API. It is the code-first replacement for registerCollabRoutes
// (api/collab_routes.go). The `repos` dependency is the §6.6g project-ownership
// oracle (gitrepo.RepoService satisfies it) — mirror the legacy
// NewCollabController(repos) wiring.
//
// GetCollabSession is org-scoped (gated by OrgScopedInput). ValidateCollabAccess
// is the server-to-server carve-out the collab-server calls with the forwarded
// user Bearer (+ X-Room-Id); the JWT signature is verified by the upstream jwt
// middleware on the mux, and this handler then enforces the room's
// project-ownership oracle. It does NOT embed OrgScopedInput because the org is
// recovered from the verified token, not the {orgHandle} path.
func RegisterCollab(api huma.API, repos repoOracle) {
	huma.Register(api, huma.Operation{
		OperationID: "get-collab-session",
		Method:      http.MethodGet,
		Path:        "/projects/{projectName}/requirements/collab-session",
		Summary:     "Get the collaboration session descriptor",
		Tags:        []string{"Collaboration"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *collabSessionInput) (*collabSessionOutput, error) {
		// The tenant gate (OrgScopedInput) already verified caller org ==
		// {orgHandle}. Confirm the project exists under the org via the repo
		// oracle — 404 otherwise.
		if repo, err := repos.GetRepo(ctx, in.OrgHandle, in.ProjectName); err != nil || repo == nil {
			return nil, huma.Error404NotFound("project not found")
		}
		name, email := parseDisplayIdentity(in.Authorization)
		out := &collabSessionOutput{}
		out.Body.RoomID = "spec-" + in.OrgHandle + "-" + in.ProjectName
		out.Body.WSURL = "/collab"
		out.Body.UserName = name
		out.Body.Email = email
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "validate-collab-access",
		Method:      http.MethodGet,
		Path:        "/collab/validate",
		Summary:     "Authorize a collaboration room (server-to-server)",
		Tags:        []string{"Collaboration"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *collabValidateInput) (*collabValidateOutput, error) {
		claims := auth.ClaimsFromContext(ctx)
		org := auth.ResolveOuHandle(claims)
		if claims == nil || org == "" {
			return nil, huma.Error401Unauthorized("invalid or missing token")
		}

		roomID := strings.TrimSpace(in.RoomID)
		if roomID == "" {
			return nil, huma.Error400BadRequest("missing X-Room-Id")
		}
		// The room must belong to the verified caller's org.
		prefix := "spec-" + org + "-"
		if !strings.HasPrefix(roomID, prefix) {
			return nil, huma.Error403Forbidden("forbidden")
		}
		project := strings.TrimPrefix(roomID, prefix)
		if project == "" {
			return nil, huma.Error403Forbidden("forbidden")
		}
		// Project-ownership oracle: the project's repo row must exist for this org.
		if repo, err := repos.GetRepo(ctx, org, project); err != nil || repo == nil {
			return nil, huma.Error403Forbidden("forbidden")
		}

		name, email := parseDisplayIdentity(in.Authorization)
		out := &collabValidateOutput{}
		out.Body.Name = name
		out.Body.Email = email
		out.Body.ProjectName = project
		return out, nil
	})
}
