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

package tags

import (
	"context"
	"errors"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Handler serves the artifacts feature: the spec-version tag read (#117). The
// console's overview and spec view poll it for the "vN published" /
// "draft changes" chips.
type Handler struct{ artifacts spec.ArtifactService }

// New returns the slice's handler.
func New(artifacts spec.ArtifactService) *Handler { return &Handler{artifacts: artifacts} }

func (h *Handler) ListProjectTags(ctx context.Context, request gen.ListProjectTagsRequestObject) (gen.ListProjectTagsResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	tags, err := h.artifacts.ListSpecVersionTags(ctx, org, request.ProjectName)
	if err != nil {
		switch {
		case errors.Is(err, sourcecontrol.ErrRepoNotFound), errors.Is(err, sourcecontrol.ErrRepoNotReady):
			return nil, apierr.NotFound("project repository not found")
		default:
			return nil, apierr.WithCause(apierr.Internal("internal error"), err)
		}
	}
	return gen.ListProjectTags200JSONResponse(gen.TagList{
		Tags:      tags.Tags,
		Latest:    tags.Latest,
		SpecDirty: tags.SpecDirty,
	}), nil
}
