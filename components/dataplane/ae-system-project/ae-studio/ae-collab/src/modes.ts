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

/**
 * Which server this process runs: the AE Studio pod's listeners or the legacy
 * collab server of the chart Deployment, never both. The pod renders `AE_*`
 * and none of the legacy keys; the chart renders `AEP_API_BASE` and no
 * `AE_ORG_ID`. Pod config wins outright, so a legacy flag in a pod env
 * (`COLLAB_DEV`, `COLLAB_MOCK_BFF`, `AEP_API_BASE`) starts nothing: dev mode
 * can never run in the pod. The legacy server starts only with a source for
 * its oracle and seeds, a BFF (real or mock) or explicit dev mode; with none
 * the process refuses to boot rather than serve fixtures.
 */

import { loadConfig, type CollabConfig } from "./env.js";
import { loadPodConfig, type PodConfig } from "./pod/config.js";

type Modes = { pod: PodConfig; legacy: null } | { pod: null; legacy: CollabConfig };

/** Throws when neither mode is configured, or when the pod env is partial. */
export function selectModes(env: Readonly<Record<string, string | undefined>>): Modes {
  const pod = loadPodConfig(env);
  if (pod) return { pod, legacy: null };
  const legacy = loadConfig(env);
  if (legacy.aepApiBase === null && !legacy.devMode) {
    throw new Error(
      "ae-collab: no config: set the pod env (AE_ORG_ID, ...) or, for the legacy server, " +
        "AEP_API_BASE, COLLAB_MOCK_BFF=1 or COLLAB_DEV=1",
    );
  }
  return { pod: null, legacy };
}
