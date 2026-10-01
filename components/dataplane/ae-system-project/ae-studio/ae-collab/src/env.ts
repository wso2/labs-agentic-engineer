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

export interface CollabConfig {
  port: number;
  /** BFF origin incl. API prefix, e.g. http://localhost:9090/api/v1. */
  aepApiBase: string | null;
  /** Skip the BFF oracle and seed rooms from fixtures. Only with COLLAB_DEV and no
   *  BFF (real or mock); never in cluster. */
  devMode: boolean;
  /** Run the embedded mock BFF and point the real code paths at it
   *  (stand-in for #81 / #86 phase 2). Never in cluster. */
  mockBff: boolean;
  mockBffPort: number;
  /** Committer (#133): quiet period before a flush commits. */
  commitDebounceMs: number;
  /** Committer (#133): max wait during continuous editing (the ~5min cap). */
  commitMaxDebounceMs: number;
}

function flag(value: string | undefined): boolean {
  return value === "1" || value === "true";
}

export function loadConfig(env: Readonly<Record<string, string | undefined>> = process.env): CollabConfig {
  const mockBff = flag(env.COLLAB_MOCK_BFF);
  const mockBffPort = Number(env.COLLAB_MOCK_BFF_PORT ?? 8092);
  const realBase = env.AEP_API_BASE?.trim().replace(/\/$/, "");
  const aepApiBase = mockBff ? `http://127.0.0.1:${mockBffPort}/api/v1` : realBase || null;
  // Dev mode is explicit only: missing config never implies it (a cluster
  // that lost AEP_API_BASE must not serve fixtures). A configured BFF, real
  // or mock, outranks the flag, so `pnpm dev` (which sets COLLAB_DEV) still
  // runs the real oracle and seed paths when one is given.
  const devMode = flag(env.COLLAB_DEV) && aepApiBase === null && !mockBff;
  return {
    port: Number(env.COLLAB_PORT ?? 8091),
    aepApiBase,
    devMode,
    mockBff,
    mockBffPort,
    commitDebounceMs: Number(env.COLLAB_COMMIT_DEBOUNCE_MS ?? 60_000),
    commitMaxDebounceMs: Number(env.COLLAB_COMMIT_MAX_DEBOUNCE_MS ?? 300_000),
  };
}
