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
 * Which server this process runs, one of three:
 *
 *   pod     `AE_ORG_ID` set: the AE Studio pod's Room (`pod/`). A pod env
 *           that also carries a legacy key (`COLLAB_DEV`, `COLLAB_MOCK_BFF`,
 *           `AEP_API_BASE`) fails the boot: dev mode, the mock BFF or the old
 *           oracle can never run in the pod, and a mixed env is a wiring fault
 *           worth a restart loop, not a guess.
 *   dev     `COLLAB_DEV=1` and no BFF: the pod's Room with auth bypassed and a
 *           fake Files socket (`pnpm dev`). Explicit only.
 *   legacy  the chart Deployment's collab server over the BFF, real
 *           (`AEP_API_BASE`) or mock (`COLLAB_MOCK_BFF=1`); a configured BFF
 *           outranks `COLLAB_DEV`. Deleted in Task 2.12.
 *
 * With none the process refuses to boot rather than serve fixtures.
 */

import { loadConfig, type CollabConfig } from "./env.js";
import { loadDevConfig, loadPodConfig, type DevConfig, type PodConfig } from "./pod/config.js";

/** Keys that configure only the legacy server or dev mode; any of them in a pod env is an error. */
const LEGACY_KEYS = ["COLLAB_DEV", "COLLAB_MOCK_BFF", "AEP_API_BASE"] as const;

type Modes =
  | { mode: "pod"; config: PodConfig }
  | { mode: "dev"; config: DevConfig }
  | { mode: "legacy"; config: CollabConfig };

/** Throws when no mode is configured, when the pod env is partial, or when it carries a legacy key. */
export function selectModes(env: Readonly<Record<string, string | undefined>>): Modes {
  const pod = loadPodConfig(env);
  if (pod) {
    const legacyKeys = LEGACY_KEYS.filter((key) => env[key] !== undefined);
    if (legacyKeys.length > 0) {
      throw new Error(`ae-collab pod env: legacy keys set: ${legacyKeys.join(", ")}`);
    }
    return { mode: "pod", config: pod };
  }
  const legacy = loadConfig(env);
  if (legacy.aepApiBase !== null) return { mode: "legacy", config: legacy };
  // loadConfig sets devMode only for an explicit COLLAB_DEV with no BFF.
  if (legacy.devMode) return { mode: "dev", config: loadDevConfig(env) };
  throw new Error(
    "ae-collab: no config: set the pod env (AE_ORG_ID, ...), COLLAB_DEV=1 for dev mode or, " +
      "for the legacy server, AEP_API_BASE or COLLAB_MOCK_BFF=1",
  );
}
