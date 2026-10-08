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
 * The one transpiler every reader runs `prototype.tsx` through — the check,
 * the render check and the frame alike — so what the check accepts is what
 * the frame runs. Sucrase (TypeScript + automatic JSX) keeps line numbers, so
 * a finding's `line N` is the author's line.
 */

import { transform } from "sucrase";
import { SOURCE_FILE, type Finding } from "../findings.js";

export type TranspileResult = { ok: true; code: string } | { ok: false; findings: Finding[] };

const OPTIONS = {
  jsxRuntime: "automatic",
  production: true,
  // An import runs whether or not a binding is read, so the check and the run see the same imports.
  keepUnusedImports: true,
} as const;

/** `prototype.tsx` as the CommonJS module body the frame and the render check run. */
export function transpileSource(source: string): TranspileResult {
  try {
    return { ok: true, code: transform(source, { ...OPTIONS, transforms: ["typescript", "jsx", "imports"] }).code };
  } catch (e) {
    return { ok: false, findings: [syntaxFinding(e)] };
  }
}

/** `prototype.tsx` as an ES module with TypeScript and JSX removed: what the static checks read. */
export function transpileForAnalysis(source: string): string {
  return transform(source, { ...OPTIONS, transforms: ["typescript", "jsx"] }).code;
}

export function syntaxFinding(e: unknown): Finding {
  const line = (e as { loc?: { line?: unknown } } | null)?.loc?.line;
  return {
    code: "SYNTAX_ERROR",
    file: SOURCE_FILE,
    location: typeof line === "number" ? `line ${line}` : "(file)",
    message: e instanceof Error ? e.message : String(e),
  };
}
