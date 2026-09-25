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

import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

let loaded = false;

/**
 * Load the nearest env file (walking up from this file to the filesystem
 * root), once. Mirrors services/agents/src/shared/env.ts so both TS
 * services pick up deployments/.env identically in local dev.
 */
export function loadDotenv(): void {
  if (loaded) return;
  loaded = true;
  let dir = fileURLToPath(new URL(".", import.meta.url));
  for (let i = 0; i < 12; i++) {
    for (const rel of ["deployments/.env", ".env"]) {
      const candidate = join(dir, rel);
      if (existsSync(candidate)) {
        process.loadEnvFile(candidate);
        return;
      }
    }
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
}

export function intEnv(value: string | undefined, fallback: number): number {
  const n = Number(value);
  return Number.isInteger(n) && n > 0 ? n : fallback;
}

/** Returns AEP_API_BASE_URL (e.g. http://aep-api:9090). Throws when unset. */
export function loadAepApiBaseUrl(): string {
  if (!process.env.AEP_API_BASE_URL) loadDotenv();
  const url = process.env.AEP_API_BASE_URL;
  if (!url) {
    throw new Error(
      "AEP_API_BASE_URL is not set. Export it, or add it to deployments/.env (see .env.example).",
    );
  }
  return url.replace(/\/+$/, "");
}

/**
 * `Bearer <token>` with the RFC 6750 §2.1 syntax: the scheme, one or more
 * spaces, then a b64token. The credential alphabet excludes control
 * characters and whitespace, so the value is always a legal header value.
 */
const BEARER_HEADER = /^Bearer +[A-Za-z0-9\-._~+/]+=*$/i;

/**
 * Parses AEP_MCP_DEFAULT_BEARER, the server-side fallback bearer for callers
 * that cannot send an Authorization header (the OC SRE extension loader will
 * not send credentials to a plaintext URL). Unset, blank, or a bare scheme
 * with no credential (what `Bearer ${EMPTY}` renders to) disables the
 * fallback, so no request is forwarded with an empty credential. Anything
 * else must be `Bearer <token>` (see BEARER_HEADER); a malformed value throws
 * at startup rather than failing every handoff call at aep-api.
 */
export function parseDefaultBearer(value: string | undefined): string | undefined {
  const trimmed = value?.trim();
  if (!trimmed || /^bearer$/i.test(trimmed)) return undefined;
  if (!BEARER_HEADER.test(trimmed)) {
    throw new Error(
      "AEP_MCP_DEFAULT_BEARER must be `Bearer <token>` (or unset to disable the fallback).",
    );
  }
  return trimmed;
}
