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
type ApiError = components["schemas"]["Error"];

// Scenario switch (api-guidelines: mocks must produce empty AND error
// states). Toggle in the browser devtools:
//   localStorage.setItem('aep:mock:marketplace', 'empty' | 'some' | 'error')
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
