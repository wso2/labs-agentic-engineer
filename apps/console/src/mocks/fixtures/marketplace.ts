/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import type { components } from "../../generated/aep-api";

type OrgEndpointDTO = components["schemas"]["OrgEndpointDTO"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type PlatformResourceTypeDTO = components["schemas"]["PlatformResourceTypeDTO"];
type EnvValueCellDTO = components["schemas"]["EnvValueCellDTO"];
type EnvironmentDTO = components["schemas"]["EnvironmentDTO"];
type ApiError = components["schemas"]["Error"];

// Scenario switch (api-guidelines: mocks must produce empty AND error
// states). Toggle in the browser devtools:
//   localStorage.setItem('aep:mock:marketplace', 'empty' | 'some' | 'error')
// Shared by Resources catalog GETs, Marketplace Endpoints GETs, and register.
// "empty": no platform types, external resources, org environments, or endpoints (default).
// "some": postgres-cnpg, Registered External stripe, Project External github,
//         org environments development + staging-local, and seed org endpoints.
// "error": GET list endpoints fail (load-error state). POST register still uses the
//         in-memory catalog.
export type MarketplaceScenario = "empty" | "some" | "error";

export const emptyOrgEndpoints: OrgEndpointDTO[] = [];

// Two billing rows share a project so the catalog click-through lands on one
// overview. Every fixture is namespace-visible — the page does not filter.
export const seedOrgEndpoints: OrgEndpointDTO[] = [
  {
    name: "invoice-api",
    project: "billing",
    endpoint: "rest",
    type: "HTTP",
    namespaceVisible: true,
  },
  {
    name: "payments-api",
    project: "billing",
    endpoint: "grpc",
    type: "gRPC",
    namespaceVisible: true,
  },
  {
    name: "leads-api",
    project: "crm",
    endpoint: "http",
    type: "HTTP",
    namespaceVisible: true,
  },
];

export const marketplaceError: ApiError = {
  code: "internal_error",
  message: "Mock error scenario for Marketplace Endpoints",
};

export const marketplaceLoadError: ApiError = {
  code: "internal_error",
  message: "Failed to load resource catalog",
};

// Registered External: org value-plane cells (one per config key × environment).
// Secret cells omit `value`; plain cells may include it when configured.
const stripeEnvCells: EnvValueCellDTO[] = [
  { environment: "development", key: "api_key", status: "configured" },
  { environment: "development", key: "region", status: "configured", value: "us" },
  { environment: "production", key: "api_key", status: "unset" },
  { environment: "production", key: "region", status: "unset" },
];

export const seedExternalResources: ExternalResourceDTO[] = [
  {
    name: "stripe",
    description: "Stripe payments API",
    config: [
      { key: "api_key", secret: true, description: "Secret API key" },
      { key: "region", secret: false, description: "Stripe account region" },
    ],
    consumers: [{ projectId: "demo-shop", componentName: "checkout-api" }],
    consumptionInstructions: "Use the secret key as Bearer.",
    envCells: stripeEnvCells,
    resourceDocs: [
      { type: "openapi", url: "https://example.com/stripe/openapi.yaml" },
      { type: "documentation", path: "stripe/README.md" },
    ],
    instances: [
      { project: "demo-shop", environment: "development", status: "Ready" },
    ],
  },
  {
    name: "github",
    description: "GitHub API token for repository access",
    config: [{ key: "token", secret: true, description: "Personal access token" }],
    consumers: [],
    envCells: [],
  },
];

export const seedPlatformResourceTypes: PlatformResourceTypeDTO[] = [
  {
    name: "postgres-cnpg",
    description: "Managed Postgres via CloudNativePG",
    parameters: { size: { type: "string", description: "Storage size" } },
    outputs: ["host", "port", "database", "connectionUrl"],
    consumers: [{ projectId: "demo-shop", componentName: "catalog-api" }],
  },
];

// OpenChoreo Environment names for Registered External env-value columns.
// Pair is development + staging-local, not a hardcoded Dev/Staging/Production trio.
export const seedOrgEnvironments: EnvironmentDTO[] = [
  { name: "development" },
  { name: "staging-local" },
];

// In-memory catalog for GET list + POST register. Starts as a slice copy of
// seed when the scenario is `some`, `[]` when `empty` (or `error`). Mutations
// survive for the worker process until reset.
let catalog: ExternalResourceDTO[] | undefined;

export function resetExternalResourceCatalog(): void {
  catalog = undefined;
}

export function externalResourceCatalog(
  scenario: MarketplaceScenario,
): ExternalResourceDTO[] {
  if (catalog === undefined) {
    catalog = scenario === "some" ? seedExternalResources.slice() : [];
  }
  return catalog;
}
