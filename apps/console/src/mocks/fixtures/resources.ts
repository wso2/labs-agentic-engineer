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

type PlatformResourceTypeDTO = components["schemas"]["PlatformResourceTypeDTO"];
type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type OrgEndpointDTO = components["schemas"]["OrgEndpointDTO"];

// What a project can depend on, in mock mode: two platform types, one
// Registered External resource the organization holds values for, two
// External resources projects defined for themselves (one ready to promote,
// one whose project has not chosen a provider), and two endpoints projects
// offer. The environments are the deploy mock's.

export const platformTypes: PlatformResourceTypeDTO[] = [
  {
    name: "postgres-cnpg",
    description: "A PostgreSQL database, one per project and environment.",
    parameters: {
      storage: { type: "string", description: "Disk size, such as 1Gi." },
      instances: { type: "integer", description: "Replicas." },
    },
    outputs: ["host", "port", "dbname", "user", "password"],
    consumers: [{ projectId: "acme-expenses", componentName: "expense-api" }],
  },
  {
    name: "thunder-app",
    description: "An OAuth client on the platform's identity provider, for end-user sign-in.",
    parameters: {},
    outputs: ["clientId", "issuer"],
    consumers: [],
  },
];

const ENVS = ["development", "staging", "production"];

export const externalResources: ExternalResourceDTO[] = [
  {
    name: "currency-service",
    provider: "Open Exchange Rates",
    description: "Live and historical exchange rates.",
    consumptionInstructions: "Cache rates for an hour; the plan allows 1,000 calls a month.",
    scope: "org",
    config: [
      { key: "OXR_APP_ID", description: "The app ID", secret: true },
      { key: "OXR_BASE_URL", description: "Where the API is", secret: false },
    ],
    envCells: ENVS.flatMap((environment) => [
      { environment, key: "OXR_APP_ID", status: "configured" as const },
      { environment, key: "OXR_BASE_URL", status: "configured" as const, value: "https://openexchangerates.org/api" },
    ]),
    contract: { type: "openapi", path: "currency-service/openapi.yaml" },
    resourceDocs: [{ type: "documentation", url: "https://docs.openexchangerates.org" }],
    consumers: [{ projectId: "acme-expenses", componentName: "expense-api" }],
  },
  {
    name: "payroll-service",
    provider: "Xero",
    description: "Payroll and reimbursements.",
    scope: "project",
    project: "acme-expenses",
    config: [
      { key: "XERO_CLIENT_ID", description: "OAuth client ID", secret: true },
      { key: "XERO_CLIENT_SECRET", description: "OAuth client secret", secret: true },
    ],
    contract: { type: "openapi", path: "specs/design/dependencies/payroll-service/openapi.yaml", origin: "provider" },
    consumers: [{ projectId: "acme-expenses", componentName: "expense-api" }],
  },
  {
    name: "maps-service",
    description: "Office locations on a map.",
    scope: "project",
    project: "employee-onboarding",
    config: [],
    consumers: [],
  },
];

export const orgEndpoints: OrgEndpointDTO[] = [
  { name: "expense-api", project: "acme-expenses", endpoint: "rest", type: "HTTP", namespaceVisible: true },
  { name: "people-directory", project: "employee-onboarding", endpoint: "graphql", type: "GraphQL", namespaceVisible: true },
];
