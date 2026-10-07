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

// ListProjectVersions serves what each version built (B5), for the console
// to say what changed in a feature since it was last built.
func (h *Handler) ListProjectVersions(ctx context.Context, request gen.ListProjectVersionsRequestObject) (gen.ListProjectVersionsResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	versions, err := h.artifacts.ListVersions(ctx, org, request.ProjectName)
	if err != nil {
		switch {
		case errors.Is(err, sourcecontrol.ErrRepoNotFound), errors.Is(err, sourcecontrol.ErrRepoNotReady):
			return nil, apierr.NotFound("project repository not found")
		default:
			return nil, apierr.WithCause(apierr.Internal("internal error"), err)
		}
	}
	out := gen.SpecVersionList{Versions: make([]gen.SpecVersion, 0, len(versions))}
	for _, v := range versions {
		sv := gen.SpecVersion{
			Name:        v.Name,
			Features:    make([]gen.VersionFeature, 0, len(v.Features)),
			ProductWide: nonNil(v.ProductWide),
			HeldBack:    nonNil(v.HeldBack),
			Fixes:       v.Fixes,
		}
		for _, f := range v.Features {
			vf := gen.VersionFeature{ID: f.ID, Name: f.Name, Lines: make([]gen.VersionLine, 0, len(f.Lines))}
			for _, l := range f.Lines {
				vf.Lines = append(vf.Lines, gen.VersionLine{ID: l.ID, Words: l.Words})
			}
			sv.Features = append(sv.Features, vf)
		}
		out.Versions = append(out.Versions, sv)
	}
	return gen.ListProjectVersions200JSONResponse(out), nil
}

// nonNil keeps a required list a list on the wire when it is empty.
func nonNil(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

// GetSpecState serves what the spec workspace needs beside its documents (N5).
func (h *Handler) GetSpecState(ctx context.Context, request gen.GetSpecStateRequestObject) (gen.GetSpecStateResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	st, err := h.artifacts.SpecState(ctx, org, request.ProjectName)
	if err != nil {
		switch {
		case errors.Is(err, sourcecontrol.ErrRepoNotFound), errors.Is(err, sourcecontrol.ErrRepoNotReady):
			return nil, apierr.NotFound("project repository not found")
		default:
			return nil, apierr.WithCause(apierr.Internal("internal error"), err)
		}
	}
	out := gen.SpecState{DesignedFrom: st.DesignedFrom, Documents: make([]gen.SourceDocument, 0, len(st.Documents))}
	for _, name := range st.Documents {
		// Coverage (what each page says and where it landed) is S5's.
		out.Documents = append(out.Documents, gen.SourceDocument{ID: name, Title: name, Rows: []gen.SourceDocumentRow{}})
	}
	return gen.GetSpecState200JSONResponse(out), nil
}
