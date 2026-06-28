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

package access

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/humakit"
	"github.com/wso2/asdlc/asdlc-service/models"
)

type createAccessRequestInput struct {
	humakit.OrgScopedInput
	ProjectName   string `path:"projectName" doc:"Consumer project name (DNS-label slug)"`
	ComponentName string `path:"componentName" doc:"Consumer component requesting access"`
	Body          struct {
		// OrgServiceName is the catalog key the consumer's dependency references
		// — the provider OC component name to publish org-wide.
		OrgServiceName string `json:"orgServiceName" doc:"Org-service name (provider OC component name) to request access to"`
	}
}

type createAccessRequestOutput struct {
	Body *models.AccessRequest
}

// RegisterAccess registers the cross-project access-request endpoint (P3.5). A
// consumer asks the provider project to publish a project-only org-service
// org-wide; this creates the provider publish task + GitHub issue (or dedupes
// onto an existing one) and records the request.
func RegisterAccess(api huma.API, svc *AccessService) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-access-request",
		Method:        http.MethodPost,
		Path:          "/api/v1/organizations/{orgHandle}/projects/{projectName}/components/{componentName}/access-requests",
		Summary:       "Request cross-project access to a project-only org-service",
		Tags:          []string{"Access Requests"},
		Security:      humakit.SecurityUserJWT,
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createAccessRequestInput) (*createAccessRequestOutput, error) {
		if svc == nil {
			return nil, huma.Error503ServiceUnavailable("access requests are not configured")
		}
		if strings.TrimSpace(in.Body.OrgServiceName) == "" {
			return nil, huma.Error422UnprocessableEntity("orgServiceName is required")
		}
		ar, err := svc.RequestAccess(ctx, RequestAccessInput{
			OrgHandle:         in.OrgHandle,
			ConsumerProject:   in.ProjectName,
			ConsumerComponent: in.ComponentName,
			OrgServiceName:    strings.TrimSpace(in.Body.OrgServiceName),
		})
		if err != nil {
			if errors.Is(err, ErrOrgServiceNotFound) {
				return nil, huma.Error404NotFound("org service not found in catalog")
			}
			return nil, huma.Error500InternalServerError("failed to create access request", err)
		}
		return &createAccessRequestOutput{Body: ar}, nil
	})
}
