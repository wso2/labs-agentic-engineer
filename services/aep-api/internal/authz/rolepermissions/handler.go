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

package rolepermissions

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/authz"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Handler is the role-permissions slice of the strict interface.
type Handler struct{ svc *authz.AuthZService }

// New wires the slice over the AuthZService. A nil service answers 503 on every
// operation.
func New(svc *authz.AuthZService) *Handler { return &Handler{svc: svc} }

func (h *Handler) LogAuthzRolePermissions(ctx context.Context, request gen.LogAuthzRolePermissionsRequestObject) (gen.LogAuthzRolePermissionsResponseObject, error) {
	if h.svc == nil {
		return nil, apierr.ServiceUnavailable("the authz service is not configured")
	}
	orgHandle := tenant.BoundOrgFromContext(ctx)
	if err := h.svc.ModifyRolePermissions(ctx, orgHandle, *request.Body); err != nil {
		return nil, apierr.Internal("failed to apply role permissions")
	}
	return gen.LogAuthzRolePermissions200JSONResponse{Status: "applied"}, nil
}
