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

package rolespanel

import (
	"context"
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/identity"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Handler is the Security-panel slice of the strict interface. A nil panel
// answers 503 on every operation, matching every other nil-tolerant slice: a
// stack with no identity provider and no store leaves the surface present but
// unwired rather than 404-ing a route the contract promises.
type Handler struct{ panel *identity.PanelService }

// New wires the slice over the panel service. A nil service is a supported
// configuration (503 per op).
func New(panel *identity.PanelService) *Handler { return &Handler{panel: panel} }

// unavailable is the one 503 body, so the wording cannot drift between the four
// operations.
func (h *Handler) unavailable() error {
	return apierr.ServiceUnavailable("the identity panel is not configured")
}

func (h *Handler) enabled() bool { return h != nil && h.panel.Enabled() }

func (h *Handler) GetProjectRoles(ctx context.Context, request gen.GetProjectRolesRequestObject) (gen.GetProjectRolesResponseObject, error) {
	if !h.enabled() {
		return nil, h.unavailable()
	}
	view, err := h.panel.View(ctx, tenant.BoundOrgFromContext(ctx), request.ProjectName)
	if err != nil {
		return nil, apierr.Internal("failed to read the project's roles")
	}
	return gen.GetProjectRoles200JSONResponse(toView(view)), nil
}

func (h *Handler) RevealTestUserPassword(ctx context.Context, request gen.RevealTestUserPasswordRequestObject) (gen.RevealTestUserPasswordResponseObject, error) {
	if !h.enabled() {
		return nil, h.unavailable()
	}
	out, err := h.panel.Reveal(ctx, tenant.BoundOrgFromContext(ctx), request.ProjectName, request.Username)
	if err != nil {
		return nil, mapErr(err, "failed to reveal the test user's password")
	}
	return gen.RevealTestUserPassword200JSONResponse(toPassword(out)), nil
}

func (h *Handler) RotateTestUserPassword(ctx context.Context, request gen.RotateTestUserPasswordRequestObject) (gen.RotateTestUserPasswordResponseObject, error) {
	if !h.enabled() {
		return nil, h.unavailable()
	}
	out, err := h.panel.Rotate(ctx, tenant.BoundOrgFromContext(ctx), request.ProjectName, request.Username)
	if err != nil {
		// The half-applied rotate is the one failure whose MESSAGE is the
		// remedy: the directory took the new password and the platform did not
		// record it, so an opaque "internal error" would leave the operator with
		// an account nobody can sign in as and no idea to rotate again.
		if errors.Is(err, identity.ErrPasswordChangedNotRecorded) {
			return nil, apierr.Internal(identity.ErrPasswordChangedNotRecorded.Error())
		}
		return nil, mapErr(err, "failed to rotate the test user's password")
	}
	return gen.RotateTestUserPassword200JSONResponse(toPassword(out)), nil
}

func (h *Handler) DeleteTestUser(ctx context.Context, request gen.DeleteTestUserRequestObject) (gen.DeleteTestUserResponseObject, error) {
	if !h.enabled() {
		return nil, h.unavailable()
	}
	out, err := h.panel.Delete(ctx, tenant.BoundOrgFromContext(ctx), request.ProjectName, request.Username)
	if err != nil {
		return nil, mapErr(err, "failed to delete the test user")
	}
	// The status carries the shared-account warning: the role is untouched, and
	// other projects may still be pointing at the account that just went away.
	status := fmt.Sprintf("deleted test user %s; the role was left in place", out.Username)
	if out.RemainingReferences > 0 {
		status = fmt.Sprintf("%s. WARNING: %d other project(s) still reference this account and their "+
			"validation runs will no longer be able to sign in as it",
			status, out.RemainingReferences)
	}
	return gen.DeleteTestUser200JSONResponse(gen.StatusMsg{Status: status}), nil
}

// mapErr turns the domain's refusal into the wire's. ErrPanelNotFound covers
// BOTH fences — the project does not reference the username, and the platform
// does not own the account — and both must read the same on the wire: a 404 that
// distinguished them would tell project A that a username it may not touch
// exists under project B.
func mapErr(err error, generic string) error {
	if errors.Is(err, identity.ErrPanelNotFound) {
		return apierr.NotFound("no such test user for this project")
	}
	return apierr.Internal(generic)
}

func toView(v identity.PanelView) gen.ProjectRolesView {
	out := gen.ProjectRolesView{DirectoryAvailable: v.DirectoryAvailable}
	for _, r := range v.Roles {
		out.Roles = append(out.Roles, gen.ProjectRoleState{
			Name:            r.Name,
			Description:     r.Description,
			PlatformCreated: r.PlatformCreated,
			MemberCount:     r.MemberCount,
		})
	}
	for _, u := range v.TestUsers {
		out.TestUsers = append(out.TestUsers, gen.ProjectTestUserState{
			Username:            u.Username,
			RoleName:            u.RoleName,
			Supplied:            u.Supplied,
			ColdStart:           u.ColdStart,
			Exists:              u.Exists,
			Owned:               u.Owned,
			RotatedAt:           u.RotatedAt,
			ReferencingProjects: u.ReferencingProjects,
			ReferencingCount:    u.ReferencingCount,
		})
	}
	return out
}

func toPassword(d identity.PasswordDisclosure) gen.TestUserPassword {
	return gen.TestUserPassword{Username: d.Username, Password: d.Password, RotatedAt: d.RotatedAt}
}
