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
 * A component's `openapi.yaml` write-gate — validate on write, so validating
 * never costs a round trip.
 *
 * Before this, an agent that wanted its spec checked had to call the platform's
 * `validate_openapi_spec` MCP tool, which takes the document as a STRING. The
 * agent had just written that document through this bundle, and the tool has no
 * way to name a file, so the only way to ask was to retype all of it as tool
 * input. Measured on one design generation: the model re-emitted a 13KB spec it
 * had written two steps earlier — 4.1k output tokens and 28.9s — plus a wasted
 * step before it that called the tool with the literal placeholder string
 * `$(cat placeholder)`, which validated nothing. Roughly 32s of a 206s turn,
 * for a check the write path can do for free.
 *
 * Coverage is deliberately IDENTICAL to that tool's, so moving the check here
 * loses nothing: the platform's validator (`aep-api` `spec.ValidateOpenAPI`) is
 * itself purely structural — parse, `openapi: 3.x`, non-empty `paths`, at least
 * one operation. It resolves no `$ref`s and validates no schemas. Which is why
 * this needs no OpenAPI library: a heavyweight validator here would be both a
 * new dependency in a package that has no server-side ones and a stricter gate
 * than the tool it replaces, so specs that used to pass would start being
 * rejected.
 *
 * The YAML parse itself is already gated upstream (`checkYaml` runs first in
 * `commit`), so this starts from a document that parses.
 *
 * ONE thing was added since: the SECURITY block, in `./openapi-security.ts`.
 * It is not structural and it is not the MCP tool's coverage — it is the half
 * of the document whose mistakes are invisible in the document itself (a scope
 * no catalog declares, an OIDC scope that admits everyone, a required
 * `X-User-Id` the parameter binder answers 400 for), and judging it needs the
 * component's `design.json` and the project's `security.json`. That is why the
 * gate now takes a bundle reader; without one it is exactly the check it was.
 */

import { parse as parseYaml } from "yaml";
import type { DiagramBundleReader } from "./design-diagrams.js";
import { checkOpenapiSecurity } from "./openapi-security.js";

export interface OpenapiSpecProblem {
  code: "INVALID_OPENAPI";
  message: string;
}

/** Operation keys counted under a path item (OpenAPI 3.x). */
const HTTP_METHODS = new Set([
  "get",
  "put",
  "post",
  "delete",
  "options",
  "head",
  "patch",
  "trace",
]);

/**
 * A component's own spec, by basename — `specs/design/components/<name>/openapi.yaml`.
 *
 * A dependency's contract (`specs/design/dependencies/<name>/openapi.yaml`) is
 * deliberately NOT this gate's: it is a slice of a third-party document,
 * validated structurally by the slicer that cut it (or by the platform when
 * the user uploads one), and holding someone else's API to the platform's own
 * conventions would reject a write the agent is only relaying.
 */
function isComponentOpenapiPath(path: string): boolean {
  if (path.startsWith("specs/design/dependencies/")) return false;
  const base = (path.split("/").at(-1) ?? "").toLowerCase();
  return base === "openapi.yaml" || base === "openapi.yml";
}

/** `paths`-shaped record, or null when the value cannot hold path items. */
function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

/**
 * Validate a candidate `openapi.yaml` body for `path`. Returns null when the
 * path is not a component spec or the document is sound; otherwise the problem,
 * phrased for the model's self-correction.
 *
 * `bundle` is what lets the security rules run: they are cross-file, so a
 * caller that holds no bundle (a standalone structural check) gets the
 * structural gate alone rather than a half-applied one.
 */
export function checkOpenapiSpec(
  path: string,
  content: string,
  bundle?: DiagramBundleReader,
): OpenapiSpecProblem | null {
  if (!isComponentOpenapiPath(path)) return null;

  let doc: unknown;
  try {
    doc = parseYaml(content);
  } catch {
    // Unreachable through `commit` (the YAML gate rejects first), but this is a
    // pure function and a caller elsewhere must not get an exception.
    return null;
  }

  const root = asRecord(doc);
  if (!root) {
    return reject(path, "the document is not a YAML mapping");
  }

  const version = root["openapi"];
  if (typeof version !== "string" || !version.startsWith("3.")) {
    return reject(
      path,
      `it is not an OpenAPI 3.x document (openapi: ${version === undefined ? "absent" : JSON.stringify(version)})`,
    );
  }

  const paths = asRecord(root["paths"]);
  if (!paths || Object.keys(paths).length === 0) {
    return reject(path, "it has no paths");
  }

  let operations = 0;
  for (const item of Object.values(paths)) {
    const pathItem = asRecord(item);
    if (!pathItem) continue;
    for (const key of Object.keys(pathItem)) {
      if (HTTP_METHODS.has(key.toLowerCase())) operations++;
    }
  }
  if (operations === 0) {
    return reject(path, "it has no operations (no get/put/post/delete/... under any path)");
  }

  if (bundle) {
    const securityProblem = checkOpenapiSecurity(path, root, bundle);
    if (securityProblem) return rejectSecurity(path, securityProblem);
  }

  return null;
}

/** The tail every refusal carries: what happened to the file, and how to fix it in one step. */
const REMEDY =
  `The file is unchanged. Re-emit the WHOLE corrected document with removeFile + addFile. ` +
  `Every write to this path is validated here, so there is no need to check it with a separate tool.`;

/** `because` is a clause — "it has no paths" — that this punctuates. */
function reject(path: string, because: string): OpenapiSpecProblem {
  return { code: "INVALID_OPENAPI", message: `${path} was rejected — ${because}. ${REMEDY}` };
}

/** `sentence` is already finished prose, straight from the message catalog. */
function rejectSecurity(path: string, sentence: string): OpenapiSpecProblem {
  return { code: "INVALID_OPENAPI", message: `${path} was rejected — ${sentence} ${REMEDY}` };
}
