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

// Package dependencies is the Dependencies & Provisioning domain KERNEL:
// the provisioner cores plus
// the org-service endpoint catalog that every service in this domain is built
// on. It is a kernel-root domain (like internal/delivery): the shared cores live
// in this root; the services (provisioning, runtimeconfig, mcpdiscovery) are
// sub-package slices that import only the root.
//
// It carries the two halves of OpenChoreo's Workload.spec.dependencies[]:
//
// RESOURCE dependencies (external_*.go, platform_*.go, naming.go, markers.go) —
// the DependencyKind values "external" (a third-party system the platform does
// not provision) and "platform-resource" (a resource provisioned through an
// OpenChoreo cluster resource type, e.g. a database or cache). The provisioner
// CORES only:
//
//   - ExternalResourceProvisioner (external_provisioner.go) — authors the OC
//     Resource model (ResourceType → Resource → pinned per-env bindings) with
//     secret values routed through SM-API; Deprovision and ResolveRunnerSecrets
//     (the per-run ExternalSecret inputs for the coding runner).
//   - naming.go — ExternalResourceName / ExternalResourceBindingName, the single
//     source of truth for the OC CR names (shared with the platform half).
//   - ResourceTypeCatalog (platform_catalog.go) — read-only discovery of the
//     installed cluster-scoped ClusterResourceTypes (AEP never authors them).
//   - ResourceProvisioner / OCNativeProvisioner (platform_provisioner.go) —
//     authors the OC Resource model for a platform-resource dep against a
//     DISCOVERED ClusterResourceType (never EnsureResourceType), async.
//
// ENDPOINT dependencies (catalog.go, resolve.go, endpoint_naming.go) — the
// DependencyKind values "component" (another component in the same org/project)
// and "org-service" (an endpoint published across an org boundary). Catalog is
// the dynamic source of "org-service" targets: it enumerates provider-side
// endpoints published by an org namespace's Workloads (ListWorkloadEndpoints)
// and resolves them by namespace visibility, project sibling, or owning
// component; endpoint_naming.go derives the `<UPPER_SNAKE>_URL` env var OC binds
// a resolved org-service address to.
//
// The value/param collection surface, the `provision` gate issues, the
// provision-Execution lifecycle and the readiness/convergence watchers that
// DRIVE these cores live in the domain's slices (provisioning/, runtimeconfig/);
// the MCP discovery surface + the resource-type/endpoint HTTP reads live in
// mcpdiscovery/. The root names NO feature and NO slice: the org catalog, SM-API
// writer, OC client and design reader are consumer-side ports (ports.go,
// resolve.go) wired concretely in the composition root.
package dependencies
