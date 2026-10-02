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

type AeStudio = components["schemas"]["AeStudio"];

// Scenario knob (devtools):
//   localStorage.setItem('aep:mock:aeStudio', 'ready' | 'provisioning' | 'failed' | 'absent')
// `ready` is the default. `provisioning` answers `provisioning`
// PROVISIONING_POLLS times, then `ready`:
// - set it and reload: the first-visit hold ("Upgrading AE Studio") plays
//   through and lets the console in;
// - set it on a page that already saw `ready`, without reloading, and leave
//   and refocus the window once the 30 s staleTime has passed: the restart
//   banner shows until the answers turn `ready` again. Switching the knob
//   restarts the count.
export type AeStudioScenario = AeStudio["state"];

export const PROVISIONING_POLLS = 3;

// Fixed fake origins, one per container. Pod handlers match these origins
// exactly (never `*/v1/...`), so they cannot collide with `*/api/v1/...`.
const urls: NonNullable<AeStudio["urls"]> = {
  designAgent: "http://ae-design-agent.mock",
  collab: "ws://ae-collab.mock",
  tools: "http://ae-studio-tools.mock",
};

export function aeStudioFor(state: AeStudioScenario): AeStudio {
  return state === "ready" ? { state, urls } : { state };
}
