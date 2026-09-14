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

// The build request's approval inputs, derived from the build preflight.
//
// Build no longer collects external config values: those are gathered on the
// Builds page while the coding agent runs and enforced at the deploy gate, and
// the BFF ignores any `external-config` entry a request carries (it authors the
// unset instance from the design's union schema instead). What POST /build
// still needs from preflight is the APPROVALS — clicking Build is the user's
// consent to the platform resources it will provision and the cross-project
// endpoints it will publish — so every path that starts a build derives them
// from the same place, here.

import type { components } from "../../../generated/aep-api";

type PreflightItem = components["schemas"]["PreflightItem"];
type BuildInputItem = components["schemas"]["BuildInputItem"];

/**
 * One approval input per preflight item the build must consent to:
 *
 *   - `platform-resource` — carries the design-authored provisioning
 *     `parameters` through, since that is the payload the provisioner reads.
 *   - `org-service` — an unresolved cross-project endpoint; preflight only
 *     ever raises one while it is unresolved/blocked, so it reaches
 *     a build request solely through the resolution drawer.
 *
 * Every other kind is dropped: `external-config` values are not a build
 * concern any more, the pasted `external-spec` is assembled where it is typed,
 * and the resolution blockers have no input to submit at all.
 */
export function approvalInputsFor(items: PreflightItem[]): BuildInputItem[] {
  const inputs: BuildInputItem[] = [];
  for (const item of items) {
    if (item.kind === "platform-resource") {
      inputs.push({
        component: item.component,
        dependency: item.dependency,
        kind: "platform-resource",
        approved: true,
        ...(item.parameters ? { parameters: item.parameters } : {}),
      });
    } else if (item.kind === "org-service") {
      inputs.push({
        component: item.component,
        dependency: item.dependency,
        kind: "org-service",
        approved: true,
      });
    }
  }
  return inputs;
}
