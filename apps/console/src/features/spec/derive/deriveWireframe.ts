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

import {
  compileWireframes,
  tryDslToExcalidraw,
  type DslKind,
  type WireframeCompile,
} from "@aep/excalidraw-dsl";

/** `erd`/`domain` filenames are domain-model DSL; everything else is wireframes. */
export function kindFor(path: string): DslKind {
  const base = (path.split("/").at(-1) ?? "").toLowerCase();
  return base.startsWith("erd") || base.startsWith("domain") ? "domain-model" : "wireframes";
}

/**
 * Compile a `.dsl` source into an Excalidraw scene JSON string. Returns null
 * when the DSL does not compile (empty / no screens) — a bad source never
 * throws into the render tree.
 */
export function deriveWireframeScene(path: string, dsl: string): string | null {
  const res = tryDslToExcalidraw(kindFor(path), dsl);
  return res.ok ? res.json : null;
}

/**
 * The live-edit variant: compile AND learn which screens this compile changed
 * versus `previous`, so the canvas can follow the agent's edit instead of
 * guessing. `previous` is this function's own prior result for the SAME file
 * (null on first compile or after switching files); the caller holds it — the
 * compiler remembers nothing. Domain-model DSLs have no screens and take the
 * plain path. Returns null when the source does not compile, exactly like
 * `deriveWireframeScene`.
 */
export type LiveCompile = Extract<WireframeCompile, { ok: true }>;

/**
 * Which of a compile's changed screens the canvas should actually move to.
 *
 * A surgical edit changes one or two screens: follow it, live, from the first
 * flush. But an agent that REWRITES the file makes the in-between flushes
 * genuinely incomplete — screens are absent until written back — so the
 * compiler honestly reports most of them as changed. Panning to that union is
 * the whole board, zoomed out to nothing. A change touching more than half the
 * screens is therefore a rewrite in flight, not an edit to follow: hold still
 * and let the canvas keep drawing until the report narrows. Deciding this from
 * the compiler's report rather than from a timer means a clean edit is never
 * delayed and a rewrite never yanks the viewport.
 */
export function focusTargets(compile: LiveCompile, previous: LiveCompile | null): string[] {
  const changed = compile.changedScreens;
  if (changed.length === 0) return [];
  // Measured against the LARGER of the two compiles: mid-rewrite the new one
  // has lost screens, and counting only what survived would make a three-of-
  // three change look like three-of-one.
  const total = Math.max(
    Object.keys(compile.fingerprints).length,
    previous ? Object.keys(previous.fingerprints).length : 0,
  );
  return changed.length * 2 > total ? [] : changed;
}

export function deriveLiveWireframe(
  path: string,
  dsl: string,
  previous: LiveCompile | null,
): LiveCompile | null {
  if (kindFor(path) !== "wireframes") {
    const res = tryDslToExcalidraw("domain-model", dsl);
    return res.ok
      ? { ok: true, json: res.json, changedScreens: [], fingerprints: {}, screenOrder: [] }
      : null;
  }
  const res = compileWireframes(dsl, previous);
  return res.ok ? res : null;
}
