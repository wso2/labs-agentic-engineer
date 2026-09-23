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
 * Wireframes `.dsl` write-gate. The flow dialect computes all geometry from
 * structure, so overlap/out-of-frame are inexpressible and no longer gated —
 * what remains the agent's job is SYNTAX: an unknown keyword, a misplaced
 * `left`/`right`/table-`row`, or the retired coordinate dialect would be
 * silently dropped by the tolerant compile path, which is how content quietly
 * goes missing from a wireframe. So, like the design.json schema gate, a
 * syntactically invalid write aborts with a line-numbered, self-correctable
 * error and the bundle stays byte-for-byte unchanged. Streamed previews stay
 * tolerant; only the committed write is strict.
 */

import { validateWireframeSyntax } from "@aep/excalidraw-dsl";

export interface WireframeSyntaxProblem {
  code: "INVALID_DSL";
  message: string;
}

/** Cap the echoed issues so one bad file doesn't flood the tool result. */
const MAX_ISSUES = 8;

/** `erd`/`domain` basenames are the domain-model DSL — a different grammar. */
function isWireframesDslPath(path: string): boolean {
  if (!path.endsWith(".dsl")) return false;
  const base = (path.split("/").at(-1) ?? "").toLowerCase();
  return !(base.startsWith("erd") || base.startsWith("domain"));
}

/**
 * Validate a candidate wireframes `.dsl` body for `path`. Returns null when
 * the path is not a wireframes DSL or the syntax is clean; otherwise the
 * problem, phrased for the model's self-correction.
 */
export function checkWireframeLayout(
  path: string,
  content: string,
): WireframeSyntaxProblem | null {
  if (!isWireframesDslPath(path)) return null;
  const issues = validateWireframeSyntax(content);
  if (issues.length === 0) return null;
  const shown = issues.slice(0, MAX_ISSUES);
  const more = issues.length - shown.length;
  return {
    code: "INVALID_DSL",
    message:
      `${path} has ${issues.length} DSL syntax problem${issues.length === 1 ? "" : "s"} — invalid lines would be silently dropped from the wireframe. The file is unchanged:\n` +
      shown.map((s) => `- ${s}`).join("\n") +
      (more > 0 ? `\n(+${more} more)` : "") +
      `\nRe-emit the WHOLE corrected file in ONE retry, fixing every line listed above. Layout is computed from structure (stack / row / split) — never write x,y coordinates.`,
  };
}
