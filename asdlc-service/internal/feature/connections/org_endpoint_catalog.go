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

package connections

import (
	"context"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
)

// OrgEndpointCatalog enumerates the service endpoints published by the
// Workloads in an org namespace — the dynamic source (P3) that supersedes the
// static `external_api_catalog.go` map. The architect discovers `org-service`
// targets here (via the MCP `list_org_endpoints` tool), and both design
// resolution and the consumer-wiring gate on each endpoint's namespace
// visibility.
//
// `orgHandle` is the OC namespace the org's Workloads live in. Locally that
// equals the org handle / registry key / SM-API org id (see ValueService) —
// the whole connections feature treats `orgHandle` as the OC namespace, and
// this catalog follows the same convention. (Cloud namespace resolution, when
// added, is uniform across connections, not special-cased here.)
type OrgEndpointCatalog struct {
	rc openchoreo.ResourceClient
}

func NewOrgEndpointCatalog(rc openchoreo.ResourceClient) *OrgEndpointCatalog {
	return &OrgEndpointCatalog{rc: rc}
}

// List returns every provider-side endpoint across the org's Workloads (one row
// per endpoint, carrying owner project/component + visibility). Returns nil when
// the catalog is not wired.
func (c *OrgEndpointCatalog) List(ctx context.Context, orgHandle string) ([]openchoreo.WorkloadEndpointInfo, error) {
	if c == nil || c.rc == nil {
		return nil, nil
	}
	return c.rc.ListWorkloadEndpoints(ctx, orgHandle)
}

// ResolveNamespaceVisible finds the namespace-visible endpoint published under
// the given `org-service` name (== the provider component name). Returns
// ok=false when no component with that name publishes a namespace-visible
// endpoint (i.e. it doesn't exist or is project-only) — that's the `unresolved`
// case. When a component publishes several namespace-visible endpoints, an HTTP
// one wins (a service exposes a single API endpoint in practice).
func (c *OrgEndpointCatalog) ResolveNamespaceVisible(ctx context.Context, orgHandle, name string) (openchoreo.WorkloadEndpointInfo, bool, error) {
	infos, err := c.List(ctx, orgHandle)
	if err != nil {
		return openchoreo.WorkloadEndpointInfo{}, false, err
	}
	var fallback *openchoreo.WorkloadEndpointInfo
	for i := range infos {
		e := &infos[i]
		if e.Component != name || !e.NamespaceVisible() {
			continue
		}
		if e.Type == "HTTP" {
			return *e, true, nil
		}
		if fallback == nil {
			fallback = e
		}
	}
	if fallback != nil {
		return *fallback, true, nil
	}
	return openchoreo.WorkloadEndpointInfo{}, false, nil
}
