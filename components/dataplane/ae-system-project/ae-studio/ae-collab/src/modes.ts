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
 * Which Room this process runs, one of two:
 *
 *   pod  `AE_ORG_ID` set: the AE Studio pod's Room (`pod/`). A pod env that
 *        also carries a dev or legacy key (`COLLAB_DEV`, `COLLAB_MOCK_BFF`,
 *        `AEP_API_BASE`) fails the boot: dev mode can never run in the pod,
 *        and a mixed env is a wiring fault worth a restart loop, not a guess.
 *   dev  `COLLAB_DEV=1` and no `AE_ORG_ID`: the pod's Room with auth bypassed
 *        and a fake Files socket (`pnpm dev`). Explicit only.
 *
 * With neither the process refuses to boot rather than serve fixtures.
 */

import { loadDevConfig, loadPodConfig, type DevConfig, type PodConfig } from "./pod/config.js";

/** Keys no pod env may carry: dev mode's switch and the removed legacy server's. */
const NOT_IN_POD = ["COLLAB_DEV", "COLLAB_MOCK_BFF", "AEP_API_BASE"] as const;

type Modes = { mode: "pod"; config: PodConfig } | { mode: "dev"; config: DevConfig };

function flag(value: string | undefined): boolean {
  return value === "1" || value === "true";
}

/** Throws when no mode is configured, when the pod env is partial, or when it carries a key above. */
export function selectModes(env: Readonly<Record<string, string | undefined>>): Modes {
  const pod = loadPodConfig(env);
  if (pod) {
    const keys = NOT_IN_POD.filter((key) => env[key] !== undefined);
    if (keys.length > 0) {
      throw new Error(`ae-collab pod env: legacy keys set: ${keys.join(", ")}`);
    }
    return { mode: "pod", config: pod };
  }
  if (flag(env.COLLAB_DEV)) return { mode: "dev", config: loadDevConfig(env) };
  throw new Error("ae-collab: no config: set the pod env (AE_ORG_ID, ...) or COLLAB_DEV=1 for dev mode");
}
