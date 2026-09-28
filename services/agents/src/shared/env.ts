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
 * The single .env loader. `config.ts` composes this BEFORE reading any
 * `process.env`, so a value set only in `.env` (not the ambient env) is honored
 * — not silently dropped. The walk is path-independent (it finds the env file
 * going up to the monorepo root), so it survives moving the package.
 */

import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

let loaded = false;

/**
 * Load the nearest env file (walking up from this file to the filesystem root)
 * into `process.env`, once. At each ancestor we prefer `deployments/.env` (the
 * file everyone running the local stack is expected to have — setup-aep.sh
 * generates it) and fall back to a plain `.env`. No-op if already loaded or
 * none is found — ambient env then wins, which is what we want.
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
    if (parent === dir) break; // reached filesystem root
    dir = parent;
  }
}

/**
 * Parse a positive-integer env var, falling back when it is unset, empty, or
 * non-numeric. `Number("")` is 0 and `Number("abc")`/`parseInt("twenty")` are
 * NaN — both must NOT silently become a degenerate value (a 0 port, a NaN step
 * budget that disables the cap).
 */
export function intEnv(value: string | undefined, fallback: number): number {
  const n = Number(value);
  return Number.isInteger(n) && n > 0 ? n : fallback;
}

/**
 * Parse a boolean env var. Unset/empty → `fallback`. `"1"`, `"true"`, `"yes"`,
 * `"on"` (case-insensitive) are true; `"0"`, `"false"`, `"no"`, `"off"` are
 * false. Any other value falls back — a typo never silently flips the flag.
 */
export function boolEnv(value: string | undefined, fallback: boolean): boolean {
  if (value === undefined || value === "") return fallback;
  const v = value.trim().toLowerCase();
  if (["1", "true", "yes", "on"].includes(v)) return true;
  if (["0", "false", "no", "off"].includes(v)) return false;
  return fallback;
}
