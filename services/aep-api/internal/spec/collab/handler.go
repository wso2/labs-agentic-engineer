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

package collab

import (
	"context"
	"strings"

	"github.com/wso2/aep/aep-api/internal/authz"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Handler serves the collaboration feature. GetSpecCollabSession is the
// org-scoped session descriptor; ValidateCollabAccess is the server-to-server
// room authorization the collab-server calls with the forwarded user Bearer
// (+ X-Room-Id). Both read the Authorization header only for the best-effort
// display identity — the JWT signature is verified by the outer middleware.
// The project-ownership oracle (§6.6g) is repo (sourcecontrol.RepoService).
type Handler struct{ repo sourcecontrol.RepoService }

// New returns the slice's handler.
func New(repo sourcecontrol.RepoService) *Handler { return &Handler{repo: repo} }

func (h *Handler) GetSpecCollabSession(ctx context.Context, request gen.GetSpecCollabSessionRequestObject) (gen.GetSpecCollabSessionResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	// The tenant gate already bound the caller's org. Confirm the project
	// exists under it via the repo oracle — 404 otherwise.
	if repo, err := h.repo.GetRepo(ctx, org, request.ProjectName); err != nil || repo == nil {
		return nil, apierr.NotFound("project not found")
	}
	name, email := spec.ParseDisplayIdentity(request.Params.Authorization)
	return gen.GetSpecCollabSession200JSONResponse(gen.CollabSessionOutputBody{
		RoomID:   "spec-" + org + "-" + request.ProjectName,
		WsURL:    "/collab",
		UserName: name,
		Email:    email,
	}), nil
}

// ValidateCollabAccess keeps the Huma-era S2S semantics exactly: the acting
// org is recovered from the VERIFIED token claims (never the room ID), the
// room must carry the caller-org prefix, and the room's project must resolve
// through the ownership oracle — 403 on any mismatch, never a hint of whether
// the room exists. The claims check stays even though the tenant gate 401s
// claimless requests in ENFORCE: in LOG mode the gate passes them through, and
// this handler must still refuse (the collab server relies on it).
//
// Admission needs ae:design-view (permission_gate.go); the answer also reports
// whether this joiner holds the stronger ae:design. That split is what stops a
// viewer from editing through the socket: the permission gate can only answer
// "may this request happen", and every participant's edits arrive as Yjs
// updates on a connection the gate never sees again.
func (h *Handler) ValidateCollabAccess(ctx context.Context, request gen.ValidateCollabAccessRequestObject) (gen.ValidateCollabAccessResponseObject, error) {
	claims := auth.ClaimsFromContext(ctx)
	org := auth.ResolveOuHandle(claims)
	if claims == nil || org == "" {
		return nil, apierr.Unauthorized("invalid or missing token")
	}

	roomID := strings.TrimSpace(request.Params.XRoomID)
	if roomID == "" {
		return nil, apierr.BadRequest("missing X-Room-Id")
	}
	// The room must belong to the verified caller's org.
	prefix := "spec-" + org + "-"
	if !strings.HasPrefix(roomID, prefix) {
		return nil, apierr.Forbidden("forbidden")
	}
	project := strings.TrimPrefix(roomID, prefix)
	if project == "" {
		return nil, apierr.Forbidden("forbidden")
	}
	// Project-ownership oracle: the project's repo row must exist for this org.
	if repo, err := h.repo.GetRepo(ctx, org, project); err != nil || repo == nil {
		return nil, apierr.Forbidden("forbidden")
	}

	name, email := spec.ParseDisplayIdentity(request.Params.Authorization)
	return gen.ValidateCollabAccess200JSONResponse(gen.CollabValidateOutputBody{
		Name:        name,
		Email:       email,
		ProjectName: project,
		CanWrite:    holdsDesignWrite(claims),
	}), nil
}

// holdsDesignWrite reports whether the joiner holds ae:design — the same
// permission ApplyFiles requires — as opposed to the ae:design-view that got
// them into the room.
func holdsDesignWrite(claims *auth.Claims) bool {
	for _, p := range claims.Permissions() {
		if p == authz.PermissionDesign {
			return true
		}
	}
	return false
}
