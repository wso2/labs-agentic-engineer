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
 * Post-turn DSL compilation. The agent authors `*.dsl` sources (tiny,
 * skill-guided, cheaply editable); this client compiles each one it just
 * wrote into the sibling `.excalidraw` scene via `@aep/excalidraw-dsl` — the
 * same deterministic compiler the legacy console used, so validity is by
 * construction, not model discipline. The service stays file-agnostic; like
 * all disk concerns, compilation is the CALLER's job (in production, the
 * BFF's).
 */

import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { basename, join } from "node:path";
import { tryDslToExcalidraw, type DslKind } from "@aep/excalidraw-dsl";

export interface DslCompileResult {
  /** The `.dsl` source path (thread-relative). */
  path: string;
  /** The sibling `.excalidraw` output path (thread-relative). */
  outPath: string;
  ok: boolean;
  /** Parse/render failure, when !ok — surfaced to the user, nothing written. */
  error?: string;
}

/** Infer the DSL dialect from the filename (`erd`/`domain` → domain-model). */
function kindFor(path: string): DslKind {
  const base = basename(path).toLowerCase();
  return base.startsWith("erd") || base.startsWith("domain") ? "domain-model" : "wireframes";
}

/**
 * Compile every `.dsl` among `changedPaths` (thread-relative) into its
 * sibling `.excalidraw` under `threadDir`. Non-DSL paths and since-deleted
 * files are skipped; a failed parse reports instead of writing.
 */
export function compileDslArtifacts(threadDir: string, changedPaths: readonly string[]): DslCompileResult[] {
  const results: DslCompileResult[] = [];
  for (const rel of changedPaths) {
    if (!rel.endsWith(".dsl")) continue;
    const abs = join(threadDir, rel);
    if (!existsSync(abs)) continue; // removed this turn — nothing to compile
    const outPath = rel.replace(/\.dsl$/, ".excalidraw");
    const res = tryDslToExcalidraw(kindFor(rel), readFileSync(abs, "utf8"));
    if (res.ok) {
      writeFileSync(join(threadDir, outPath), res.json);
      results.push({ path: rel, outPath, ok: true });
    } else {
      results.push({ path: rel, outPath, ok: false, error: res.error });
    }
  }
  return results;
}
