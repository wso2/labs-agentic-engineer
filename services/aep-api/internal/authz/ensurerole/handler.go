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

package ensurerole

import (
	"context"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/authz"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	authn "github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Handler is the ensure-role slice of the strict interface.
type Handler struct{ svc *authz.AuthZService }

// New wires the slice over the AuthZService. A nil service answers 503.
func New(svc *authz.AuthZService) *Handler { return &Handler{svc: svc} }

func (h *Handler) EnsureAuthzRole(ctx context.Context, _ gen.EnsureAuthzRoleRequestObject) (gen.EnsureAuthzRoleResponseObject, error) {
	if h.svc == nil {
		return nil, apierr.ServiceUnavailable("the authz service is not configured")
	}
	orgHandle := tenant.BoundOrgFromContext(ctx)
	// A first-time user has no OC-side grants yet, so bootstrapping their own
	// AE roles cannot run under their forwarded JWT; use the BFF's service
	// identity, impersonating this org, for this call only.
	svcCtx := authn.WithServiceIdentity(ctx)
	if err := h.svc.EnsureAuthzRole(svcCtx, orgHandle); err != nil {
		slog.ErrorContext(ctx, "authz: ensure role failed", "orgHandle", orgHandle, "err", err)
		return nil, apierr.Internal("failed to ensure authz role")
	}
	return gen.EnsureAuthzRole200JSONResponse{Status: "ok"}, nil
}
