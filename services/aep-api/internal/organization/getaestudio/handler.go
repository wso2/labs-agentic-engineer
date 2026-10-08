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

package getaestudio

import (
	"context"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Handler serves get-ae-studio over the status reader, which owns the state
// logic and starts any converge without waiting for it.
type Handler struct {
	studio organization.AEStudioStatusReader
}

// New returns the slice's handler.
func New(s organization.AEStudioStatusReader) *Handler { return &Handler{studio: s} }

func (h *Handler) GetAeStudio(ctx context.Context, _ gen.GetAeStudioRequestObject) (gen.GetAeStudioResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if org == "" {
		// The gate in LOG mode lets a claimless request through unbound;
		// this read is org-scoped, so it fails closed.
		return nil, apierr.Unauthorized("authentication required")
	}
	st, err := h.studio.Status(ctx, org)
	if err != nil {
		slog.ErrorContext(ctx, "ae_studio.status_read_failed", "org", org, "error", err)
		return nil, apierr.Internal("failed to read AE Studio state")
	}
	out := gen.GetAeStudio200JSONResponse{State: gen.AeStudioState(st.State), Reason: gen.AeStudioReason(st.Reason)}
	if st.URLs != nil {
		out.Urls = &gen.AeStudioUrls{DesignAgent: st.URLs.DesignAgent, Collab: st.URLs.Collab, Tools: st.URLs.Tools}
	}
	return out, nil
}
