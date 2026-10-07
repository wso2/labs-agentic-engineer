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

// Package dependencies is the umbrella for a design component's external
// dependency graph, mirroring OpenChoreo's
// Workload.spec.dependencies.{endpoints[],resources[]} split into two child
// features by kind:
//
//   - dependencies/endpoints — the components (in-org or published by
//     another org) this component calls;
//   - dependencies/resources — the external systems and platform-provisioned
//     resources this component consumes.
//
// This parent package hosts the authenticated MCP discovery server
// (mcp_server.go / mcp_tools.go / ports.go) — the read-only tools the design
// agent queries BEFORE inventing something: a dependency
// (list_external_resources / get_external_resource_schema / list_org_endpoints /
// list_platform_resource_types and the rest) or an agent guardrail
// (list_guardrail_policies) — and composes the two child families beneath it.
//
// Invariant (F3): dependency names — external AND platform-resource — share ONE
// OpenChoreo Resource namespace per project and are matched project-wide (the
// per-project Resource metadata.name is `<project>-<depName>`, not scoped by
// component or kind), so a name must be UNIQUE per project across kinds. Two
// deps of different kinds colliding on one name is rejected loudly at the
// authoring choke point (openchoreo.ApplyResource's 409 spec.type-kind guard).
package mcpdiscovery
