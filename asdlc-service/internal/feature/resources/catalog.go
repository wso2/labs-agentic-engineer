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

package resources

import (
	"context"
	"sort"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
)

// PlatformResourceType is the architect-facing view of an installed
// ClusterResourceType: its name (the open-string resourceType), the
// provisioning parameter schema, and the output names a consumer can bind.
type PlatformResourceType struct {
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters,omitempty"` // openAPIV3Schema.properties
	Outputs    []string       `json:"outputs,omitempty"`
}

// ResourceTypeCatalog lists installed cluster-scoped ClusterResourceTypes
// (read-only). app-factory NEVER authors these — it only discovers them.
type ResourceTypeCatalog struct{ rc openchoreo.ResourceClient }

func NewResourceTypeCatalog(rc openchoreo.ResourceClient) *ResourceTypeCatalog {
	return &ResourceTypeCatalog{rc: rc}
}

func (c *ResourceTypeCatalog) List(ctx context.Context) ([]PlatformResourceType, error) {
	cts, err := c.rc.ListClusterResourceTypes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PlatformResourceType, 0, len(cts))
	for _, ct := range cts {
		prt := PlatformResourceType{Name: ct.Metadata.Name}
		if ct.Spec.Parameters != nil {
			if props, ok := ct.Spec.Parameters.OpenAPIV3Schema["properties"].(map[string]any); ok {
				prt.Parameters = props
			}
		}
		for _, o := range ct.Spec.Outputs {
			prt.Outputs = append(prt.Outputs, o.Name)
		}
		out = append(out, prt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
