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

package patchconfig

import (
	"context"
	"errors"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Handler serves update-config. The PATCH body is a three-state patch (absent =
// keep, null = clear, value = replace); all logic lives in organization.Service,
// and this slice only maps HTTP <-> domain and translates a SectionError.
type Handler struct{ config *organization.Service }

// New returns the slice's handler.
func New(config *organization.Service) *Handler { return &Handler{config: config} }

// UpdateConfig's own permission gate (updateConfigPermissions) only requires
// a permission for the section(s) the PATCH body actually touches — a caller
// holding just ae:github-config can legally patch gitProvider alone. But the
// response echoes the FULL projection (every section, not just the one
// patched), so the same redaction GetConfig applies is needed here too:
// without it, patching gitProvider would hand that caller the org's llm
// section detail for free.
func (h *Handler) UpdateConfig(ctx context.Context, request gen.UpdateConfigRequestObject) (gen.UpdateConfigResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	actor := auth.ActorFromContext(ctx)
	proj, err := h.config.Patch(ctx, org, actor, *request.Body)
	if err != nil {
		return nil, mapPatchError(err)
	}

	organization.RedactConfigForPermissions(proj, auth.ClaimsFromContext(ctx).Permissions())

	return gen.UpdateConfig200JSONResponse(*proj), nil
}

// mapPatchError turns a PATCH failure into the envelope. A SectionError
// carries the offending section, so the response includes a body.<section>
// field the console uses to highlight that form section; anything else
// collapses to an opaque 500 that never echoes the internal cause. Probe
// rejections that were 422 under the problem-details dialect are 400 now
// (the error-model break).
func mapPatchError(err error) error {
	var se *organization.SectionError
	if errors.As(err, &se) {
		details := []gen.ErrorDetail{{Field: "body." + se.Section, Message: se.Message}}
		switch se.Status {
		case http.StatusConflict:
			return apierr.New(http.StatusConflict, apierr.CodeConflict, se.Message, details)
		case http.StatusBadGateway:
			return apierr.New(http.StatusBadGateway, apierr.CodeBadGateway, se.Message, details)
		default:
			return apierr.New(http.StatusBadRequest, apierr.CodeValidationFailed, se.Message, details)
		}
	}
	return apierr.Internal("internal error")
}
