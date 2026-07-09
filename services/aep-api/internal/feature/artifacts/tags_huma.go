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

package artifacts

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/internal/platform/humakit"
)

type listTagsInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
}

type listTagsOutput struct{ Body *TagList }

// RegisterTags registers the spec-version tag read (#117). The console's
// overview and spec view poll it for the "vN published" / "draft changes"
// chips.
func RegisterTags(api huma.API, svc ArtifactService) {
	huma.Register(api, huma.Operation{
		OperationID: "list-project-tags",
		Method:      http.MethodGet,
		Path:        "/projects/{projectName}/tags",
		Summary:     "List spec version tags",
		Tags:        []string{"Projects"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *listTagsInput) (*listTagsOutput, error) {
		tags, err := svc.ListSpecVersionTags(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			switch {
			case errors.Is(err, gitrepo.ErrRepoNotFound), errors.Is(err, gitrepo.ErrRepoNotReady):
				return nil, huma.Error404NotFound("project repository not found")
			default:
				return nil, huma.Error500InternalServerError("internal error")
			}
		}
		return &listTagsOutput{Body: tags}, nil
	})
}
