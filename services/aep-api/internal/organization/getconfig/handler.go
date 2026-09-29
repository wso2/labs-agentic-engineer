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

package getconfig

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Handler serves get-config. The deny-by-default tenant gate binds the active
// org before this runs, so org is read from context, never from the request.
type Handler struct{ config *organization.Service }

// New returns the slice's handler.
func New(config *organization.Service) *Handler { return &Handler{config: config} }

// GetConfig requires holding EITHER ae:github-config or ae:model-config to
// reach here at all (permission_gate.go's operationPermissions — OR
// semantics, same as every other either-suffices entry). A caller holding
// just one of the two still needs the OTHER section redacted
// (organization.RedactConfigForPermissions, shared with patchconfig's own
// response): the gate only decides whether the call is answered at all, not
// which half of the answer is theirs to see.
func (h *Handler) GetConfig(ctx context.Context, _ gen.GetConfigRequestObject) (gen.GetConfigResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	proj, err := h.config.Get(ctx, org)
	if err != nil {
		return nil, apierr.Internal("failed to load config")
	}

	organization.RedactConfigForPermissions(proj, auth.ClaimsFromContext(ctx).Permissions())

	return gen.GetConfig200JSONResponse(*proj), nil
}
