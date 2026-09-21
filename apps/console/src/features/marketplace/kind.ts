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

type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];

// A Registered External resource says so itself: `scope` is "org" on a registry
// record and "project" on a type a project's build authored. That is the
// discriminator — the drawer, the Edit affordance and the Deployments board all
// ask this one question.
//
// The org value-plane cells (one per config key × environment) are the older
// way of telling the two apart, kept as a FALLBACK for a response that predates
// `scope`: a record with cells is a registry record. It is not a second rule —
// once `scope` is present it is the whole answer, so a registry record with no
// environments configured yet still reads as registered.
export function isRegisteredExternal(resource: ExternalResourceDTO): boolean {
  if (resource.scope) return resource.scope === "org";
  return Array.isArray(resource.envCells) && resource.envCells.length > 0;
}
