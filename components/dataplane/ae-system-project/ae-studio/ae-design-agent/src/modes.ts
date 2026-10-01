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
 * Which servers this process runs. The pod renders `AE_*` and no
 * `AGENT_JWT_*`; the chart Deployment renders `AGENT_JWT_*` and no `AE_ORG_ID`.
 * So the pod never starts the legacy HS256/JWKS server (or its Postgres
 * store), and the chart never starts the pod listeners. The legacy rule is
 * today's: a JWKS URL or a shared secret (`shared/config.ts` `auth`).
 */

import { loadPodConfig, type PodConfig } from "./pod/config.js";

export interface Modes {
  pod: PodConfig | null;
  legacy: boolean;
}

/** Throws when neither mode is configured, or when the pod env is partial. */
export function selectModes(env: Readonly<Record<string, string | undefined>>): Modes {
  const pod = loadPodConfig(env);
  const legacy = Boolean(env.AGENT_JWT_JWKS_URL || env.AGENT_JWT_SECRET);
  if (!pod && !legacy) {
    throw new Error("ae-design-agent: neither pod (AE_*) nor legacy (AGENT_JWT_*) config is set");
  }
  return { pod, legacy };
}
