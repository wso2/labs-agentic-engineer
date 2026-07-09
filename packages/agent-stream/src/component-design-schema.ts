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
 * Runtime validation for the AUTHORED `components/<name>/design.json`
 * (`ComponentDesign` in `./contracts/component-design.ts` — the wire source of
 * truth; the Zod schema below is drift-guarded against it). The FileBundle calls
 * `checkComponentDesign` on every write to a matching path so the model gets
 * a one-round-trip self-correction (INVALID_JSON / SCHEMA_VIOLATION) instead
 * of downstream consumers meeting a broken file. `componentDesignSchema` is also
 * the single definition the BFF save-gate validates against, published as JSON
 * Schema via `./json-schema.ts` (§8 of the migration decision record).
 */

import { z } from "zod";
import type { ComponentDesign } from "./contracts/component-design.js";
import type { Equal } from "./type-equal.js";

// strictObject, matching the published component-design.schema.json
// (additionalProperties: false) and the BFF save-gate — a dependency carrying
// unknown properties (notably the read-time-computed status/reason, which the
// agent must NEVER author) must be rejected HERE so the agent self-corrects
// in-turn instead of committing a design.json the tag-time save gate 422s.

// One env-var key the component reads at runtime (mirrors Go models.ConfigKey).
const configKeySchema = z.strictObject({
  key: z.string().min(1),
  secret: z.boolean().optional(),
  credentialClass: z.string().optional(),
});

// One option attached to an ambiguous dependency (mirrors Go models.DependencyCandidate).
const dependencyCandidateSchema = z.strictObject({
  label: z.string().min(1),
  description: z.string().optional(),
  url: z.string().optional(),
});

// One unified, kind-discriminated dependency edge — the successor to the legacy
// per-kind `connections[]`. A single flat shape carries every kind's fields;
// `kind` selects which are meaningful (LENIENT within the known set, mirroring
// the Go codec) but unknown keys — status/reason especially — are rejected.
const dependencySchema = z.strictObject({
  kind: z.enum(["component", "org-service", "external", "platform-resource"]),
  name: z.string().min(1),
  description: z.string().optional(),
  needsSpec: z.boolean().optional(),
  specPath: z.string().optional(),
  specUrl: z.string().optional(),
  config: z.array(configKeySchema).optional(),
  resourceType: z.string().optional(),
  parameters: z.record(z.string(), z.string()).optional(),
  candidates: z.array(dependencyCandidateSchema).optional(),
});

// Managed-API exposure policy (platform-owned; mirrors Go models.ExposesAPI).
const exposesAPISchema = z.strictObject({
  managed: z.boolean().optional(),
  auth: z.string().optional(),
  userContext: z.string().optional(),
  orgPublished: z.boolean().optional(),
});

export const componentDesignSchema = z.strictObject({
  name: z.string().min(1),
  type: z.string().min(1),
  version: z.string().min(1),
  language: z.string().min(1),
  buildpack: z.string().min(1),
  appPath: z.string().min(1),
  entrypoint: z.string().min(1),
  exposure: z.enum(["internet", "intranet"]),
  dependencies: z.array(dependencySchema),
  description: z.string().min(1),
  exposesAPI: exposesAPISchema.optional(),
  componentAgentInstructions: z.string().optional(),
});

// Compile-time drift guard: schema ⇄ contracts wire type (cf. tool.ts).
const _drift: Equal<z.infer<typeof componentDesignSchema>, ComponentDesign> = true;
void _drift;

/** Matches `specs/design/components/<name>/design.json`, capturing the name. */
export const COMPONENT_DESIGN_JSON_RE = /^specs\/design\/components\/([^/]+)\/design\.json$/;

export interface ComponentDesignProblem {
  code: "INVALID_JSON" | "SCHEMA_VIOLATION";
  message: string;
}

/**
 * Validate a candidate design.json body for `path`. Returns null when the
 * path is not a component design.json or the content is valid; otherwise the
 * problem, phrased for the model's self-correction.
 */
export function checkComponentDesign(path: string, content: string): ComponentDesignProblem | null {
  const m = COMPONENT_DESIGN_JSON_RE.exec(path);
  if (!m) return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(content);
  } catch (e) {
    return {
      code: "INVALID_JSON",
      message: `${path} is not valid JSON: ${e instanceof Error ? e.message : String(e)}. Re-emit the whole file.`,
    };
  }

  const res = componentDesignSchema.safeParse(parsed);
  if (!res.success) {
    const issues = res.error.issues
      .map((i) => `${i.path.join(".") || "(root)"}: ${i.message}`)
      .join("; ");
    return { code: "SCHEMA_VIOLATION", message: `${path} violates the ComponentDesign schema — ${issues}.` };
  }

  const dir = m[1]!;
  if (res.data.name !== dir) {
    return {
      code: "SCHEMA_VIOLATION",
      message: `${path}: "name" must equal the component directory ("${dir}"), got "${res.data.name}".`,
    };
  }
  return null;
}
