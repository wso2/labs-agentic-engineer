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
 * The derived-artifact pipeline, as PER-EVENT seams the turn loop drives the
 * instant a source file lands — so a derived view appears on disk as its source
 * does, not batched to turn end. This is the playground's stand-in for what the
 * BFF owns in production, kept as thin functions so the turn loop stays chat
 * mechanics only.
 *
 *   *.dsl                        → sibling .excalidraw           (per-file compile)
 *   design.json (all components) → specs/design/cell-diagram.gen.json (aggregate)
 *
 * Everything written here ends in .excalidraw / .gen.json — the extensions
 * readSnapshot excludes — so derived output can never leak into the agent's
 * bundle or the prompt.
 */

import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { compileDslArtifacts } from "./dsl.js";
import { buildProjectDesign, toCellDiagramProject } from "@aep/design-projection";

/** Thread-relative path of the project-level cell-diagram rollup. */
export const CELL_DIAGRAM_PATH = "specs/design/cell-diagram.gen.json";

export interface DerivedNote {
  ok: boolean;
  message: string;
}

/**
 * Compile ONE changed path's derived view, if it has one: a `*.dsl` source
 * compiles to its sibling `.excalidraw`. Returns a display note, or `null` when
 * `path` is not a compilable source (so the caller can drive it per change).
 * The source must already be on disk — `compileDslArtifacts` reads it back.
 */
export function compileDslDerived(threadDir: string, path: string): DerivedNote | null {
  const [r] = compileDslArtifacts(threadDir, [path]);
  if (!r) return null;
  return r.ok
    ? { ok: true, message: `${r.outPath} (compiled)` }
    : { ok: false, message: `${r.path}: DSL error — ${r.error}` };
}

/**
 * Rebuild the project-level cell-diagram rollup from the CURRENT snapshot and
 * write it to `CELL_DIAGRAM_PATH`. An aggregate over every component's
 * design.json, so it is regenerated wholesale whenever any `specs/design/` file
 * changes; an intermediate (mid-turn) snapshot that can't project yet yields an
 * error note that a later, consistent rebuild overwrites. The in-memory
 * ProjectDesign aggregate is the BFF's on-demand serving shape — only this
 * cell-diagram view is materialized (for the diagram team to consume).
 */
export function projectCellDiagram(
  threadDir: string,
  threadName: string,
  snapshot: Record<string, string>,
): DerivedNote {
  try {
    const cell = toCellDiagramProject(buildProjectDesign(threadName, snapshot));
    writeFileSync(join(threadDir, CELL_DIAGRAM_PATH), JSON.stringify(cell, null, 2) + "\n");
    return { ok: true, message: `${CELL_DIAGRAM_PATH} (projected)` };
  } catch (e) {
    // e.g. a hand-edited design.json with an unsupported type — report, don't
    // kill the chat session over a derived view.
    return { ok: false, message: `${CELL_DIAGRAM_PATH}: ${e instanceof Error ? e.message : String(e)}` };
  }
}
