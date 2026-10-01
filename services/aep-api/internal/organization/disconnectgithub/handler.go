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

package disconnectgithub

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Handler serves disconnect-git-provider.
type Handler struct{ config *organization.Service }

// New returns the slice's handler.
func New(config *organization.Service) *Handler { return &Handler{config: config} }

func (h *Handler) DisconnectGitProvider(ctx context.Context, request gen.DisconnectGitProviderRequestObject) (gen.DisconnectGitProviderResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	connected, err := h.config.DisconnectGitProvider(ctx, org)
	if err != nil {
		return nil, apierr.Internal("disconnect failed")
	}
	status := "not_connected"
	if connected {
		status = "disconnected"
	}
	return gen.DisconnectGitProvider200JSONResponse(gen.DisconnectOutputBody{Status: status}), nil
}
