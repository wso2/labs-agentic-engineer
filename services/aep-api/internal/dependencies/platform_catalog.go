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
	"sort"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// PlatformResourceType is the architect-facing view of an installed
// ClusterResourceType: its name (the open-string resourceType), the
// provisioning parameter schema, and the output names a consumer can bind.
type PlatformResourceType struct {
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters,omitempty"` // openAPIV3Schema.properties
	Outputs    []string       `json:"outputs,omitempty"`

	// Description is the PE-authored `aep.wso2.com/description` annotation:
	// human prose describing what the type provides and when to depend on
	// it. Unlike Markers below it IS serialized — it is part of the
	// architect-facing discovery contract (the architect reads it to pick
	// the right resourceType), and the console drawer shows it to users.
	Description string `json:"description,omitempty"`

	// Markers carries the PE-authored `aep.wso2.com/*` labels/annotations
	// extracted off this ClusterResourceType (see markers.go) — the signal
	// design-save and runtimeconfig key auth-role/consumer-URL/skill
	// behavior on instead of a hardcoded resourceType name. Deliberately
	// `json:"-"`: it is an AEP-internal wiring signal, not part of the
	// architect-facing MCP discovery contract, and must not appear in the
	// list_platform_resource_types tool payload.
	Markers TypeMarkers `json:"-"`
}

// ResourceTypeCatalog lists installed cluster-scoped ClusterResourceTypes
// (read-only). AEP NEVER authors these — it only discovers them (the cluster
// PE installs them out-of-band). When enabled is false, List returns an empty
// slice without calling OpenChoreo (deployments with no platform-resource catalog).
type ResourceTypeCatalog struct {
	rc      openchoreo.ResourceClient
	enabled bool
}

// NewResourceTypeCatalog wires the read-only discovery over the OC client.
// Pass enabled=false to disable catalog discovery (empty list, no OC call).
func NewResourceTypeCatalog(rc openchoreo.ResourceClient, enabled bool) *ResourceTypeCatalog {
	return &ResourceTypeCatalog{rc: rc, enabled: enabled}
}

// List returns the installed ClusterResourceTypes sorted by name, projecting
// each to its architect-facing slice (name, description, parameter
// properties, output names). When the catalog is disabled, returns an empty
// slice without calling OpenChoreo.
func (c *ResourceTypeCatalog) List(ctx context.Context) ([]PlatformResourceType, error) {
	if !c.enabled {
		return []PlatformResourceType{}, nil
	}
	cts, err := c.rc.ListClusterResourceTypes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PlatformResourceType, 0, len(cts))
	for _, ct := range cts {
		markers := MarkersFrom(ct.Metadata.Labels, ct.Metadata.Annotations)
		prt := PlatformResourceType{
			Name:        ct.Metadata.Name,
			Description: markers.Description,
			Markers:     markers,
		}
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

// TypesByName is List keyed by resourceType name — the lookup form every
// per-dependency consumer wants, in one OC call. Design save reads it to key BOTH
// of its derivations (auth off the markers, wiring off the outputs) without a
// second round-trip; MarkersByName below is the narrower view over the same
// projection, so there is one place a ClusterResourceType is interpreted.
func (c *ResourceTypeCatalog) TypesByName(ctx context.Context) (map[string]PlatformResourceType, error) {
	types, err := c.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]PlatformResourceType, len(types))
	for _, t := range types {
		out[t.Name] = t
	}
	return out, nil
}

// MarkersByName returns the extracted TypeMarkers for every installed
// ClusterResourceType, keyed by name, in one OC call. Consumers that need ONLY
// the markers (runtimeconfig's consumer-URL patch and skill attachment) hold
// this narrower port rather than the whole type record.
func (c *ResourceTypeCatalog) MarkersByName(ctx context.Context) (map[string]TypeMarkers, error) {
	types, err := c.TypesByName(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]TypeMarkers, len(types))
	for name, t := range types {
		out[name] = t.Markers
	}
	return out, nil
}
