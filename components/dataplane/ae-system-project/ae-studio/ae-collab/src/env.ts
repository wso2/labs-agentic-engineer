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
  /** Skip the BFF oracle and seed rooms from fixtures. Never in cluster. */
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

export function loadConfig(env: NodeJS.ProcessEnv = process.env): CollabConfig {
  const mockBff = flag(env.COLLAB_MOCK_BFF);
  const mockBffPort = Number(env.COLLAB_MOCK_BFF_PORT ?? 8092);
  const aepApiBase = mockBff
    ? `http://127.0.0.1:${mockBffPort}/api/v1`
    : (env.AEP_API_BASE?.replace(/\/$/, "") ?? null);
  // Without any BFF (real or mock) there is no oracle or seed source — dev
  // mode is the only way to run, so imply it rather than refusing to start
  // (`make dev` ergonomics; the console's mock mode has the same spirit).
  // With the mock BFF the REAL code paths run, so dev mode stays off unless
  // explicitly forced.
  const devMode = flag(env.COLLAB_DEV) || aepApiBase === null;
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
