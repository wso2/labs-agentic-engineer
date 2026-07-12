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

package dependencies

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/aep/aep-api/internal/feature/dependencies/resources"
	"github.com/wso2/aep/aep-api/internal/platform/humakit"
)

// This endpoint is the HTTP transport of the same discovery data the
// list_platform_resource_types MCP tool serves, backed by the package's existing
// ResourceTypeLister port (see ports.go; *resources.ResourceTypeCatalog
// satisfies it). The catalog is cluster-global, so the endpoint only requires an
// authenticated caller.

// listResourceTypesInput carries no parameters beyond the org-scoped auth gate;
// the catalog is cluster-wide, so there is nothing to filter by. Embedding
// OrgScopedInput enforces a valid Thunder JWT (the IDOR fence).
type listResourceTypesInput struct {
	humakit.OrgScopedInput
}

// platformResourceTypeDTO is the HTTP view of one installed resource type: its
// resourceType name, human description, provisioning parameter schema, and the
// output names a consumer binds as env vars. Mirrors resources.PlatformResourceType
// minus the AEP-internal Markers.
type platformResourceTypeDTO struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Outputs     []string       `json:"outputs,omitempty"`
}

type platformResourceTypesOutput struct{ Body []platformResourceTypeDTO }

// RegisterResourceTypes registers the platform-resource-type discovery endpoint
// on the public API. A nil lister registers the route but answers 503 (so
// code-first spec generation still emits the surface with the feature unwired).
func RegisterResourceTypes(api huma.API, lister ResourceTypeLister) {
	huma.Register(api, huma.Operation{
		OperationID: "list-platform-resource-types",
		Method:      http.MethodGet,
		Path:        "/dependencies/platform-resource-types",
		Summary:     "List the platform-provisioned resource types available on the cluster",
		Tags:        []string{"Dependencies"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, _ *listResourceTypesInput) (*platformResourceTypesOutput, error) {
		if lister == nil {
			return nil, huma.Error503ServiceUnavailable("resource-type catalog is not configured")
		}
		types, err := lister.List(ctx)
		if err != nil {
			// The catalog reads cluster ClusterResourceTypes over OpenChoreo; a
			// failure is an upstream (data-plane) fault, not the caller's.
			return nil, huma.Error502BadGateway("failed to list platform resource types", err)
		}
		return &platformResourceTypesOutput{Body: toPlatformResourceTypeDTOs(types)}, nil
	})
}

func toPlatformResourceTypeDTOs(in []resources.PlatformResourceType) []platformResourceTypeDTO {
	out := make([]platformResourceTypeDTO, 0, len(in))
	for _, t := range in {
		out = append(out, platformResourceTypeDTO{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
			Outputs:     t.Outputs,
		})
	}
	return out
}
