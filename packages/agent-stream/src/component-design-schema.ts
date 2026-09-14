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

// The fields an external dependency's definition used to carry on the
// component. They live in `specs/design/dependencies/<name>/dependency.json`
// now (one dependency, one definition); a component that still writes them
// gets a message naming the file, not a bare "unknown key".
const MOVED_DEPENDENCY_FIELDS = ["style", "package", "specPath", "candidates", "suggestions", "config"] as const;

const resourceWiringSchema = z.strictObject({
  ref: z.string().min(1),
  envBindings: z.record(z.string(), z.string()),
});

// One `dependencies.endpoints[]` entry for a sibling component. `component` is
// the SCOPED OC name (`<project>-<component>`) — the key OpenChoreo resolves the
// binding by, and the field an agent left to guess gets wrong (it writes the
// friendly name, the connection never resolves, and the consumer's ReleaseBinding
// never reaches Ready).
const endpointWiringSchema = z.strictObject({
  component: z.string().min(1),
  name: z.string().min(1),
  visibility: z.string().min(1),
  envBindings: z.record(z.string(), z.string()),
});

const dependencyWiringSchema = z.union([
  resourceWiringSchema,
  z.strictObject({ endpoint: endpointWiringSchema }),
]);

// One unified, kind-discriminated dependency edge. A single flat shape carries
// every kind's fields; `kind` selects which are meaningful (LENIENT within the
// known set, mirroring the Go codec) but unknown keys — status/reason
// especially — are rejected. An `external` edge is a REFERENCE by name: its
// definition (provider, style, contract, config keys, candidates) is the
// dependency's own file, gated by `dependency-design-schema.ts`.
const dependencySchema = z.strictObject({
  kind: z.enum(["component", "org-service", "external", "platform-resource"]),
  name: z.string().min(1),
  description: z.string().optional(),
  resourceType: z.string().optional(),
  // Values are typed per the target (Cluster)ResourceType's OpenAPI v3 schema —
  // e.g. postgres-cnpg declares `instances` as integer and `storage`/`version`
  // as string, so parameters are mixed scalar types, not string-only. The map
  // is marshalled verbatim into the OpenChoreo Resource spec.parameters, so a
  // number must survive as a JSON number for CRD validation to pass.
  parameters: z.record(z.string(), z.union([z.string(), z.number(), z.boolean()])).optional(),
  // component / platform-resource / external: the platform-stamped consumer-side
  // wiring (see contracts/component-design.ts `DependencyWiring`). ACCEPTED here rather
  // than rejected as agent-authored — unlike status/reason, this one is
  // PERSISTED in design.json, and the design agent reads-edits-writes the file,
  // so a rejection rule would reject its own echo. Design save re-derives and
  // overwrites it, which is what makes authoring moot.
  wiring: dependencyWiringSchema.optional(),
});

// The component's single network endpoint (mirrors Go models.ComponentEndpoint).
// Only `name` is declared — the shared key the coding agent's workload.yaml and
// the platform's api-configuration trait both reference. Defaults to "http"
// downstream when the whole block is omitted.
const endpointSchema = z.strictObject({
  name: z.string().min(1),
});

// `type` is an OPEN vocabulary (future kinds: worker, scheduled-task, …) but the
// browser-app kind has ONE canonical spelling: "web-application" (OpenChoreo's
// term). The agent habitually writes "webapp"/"web-app", which silently breaks
// deployment + runtime-config (both key on the exact string). Reject the known
// wrong aliases with a self-correct message — this normalizes NOTHING, it forces
// the agent to emit the canonical value. Mirrored in the Go fold gate
// (agentfold/designgate.go) and the architecture skill. NB: a zod
// `.refine` does not serialize to JSON Schema, so the generated
// component-design.schema.json is intentionally more permissive on `type` (a
// bare non-empty string); the alias rule is enforced by this gate + the Go fold.
const WEB_APPLICATION_ALIASES = new Set(["webapp", "web-app", "webapplication", "web application"]);
const componentTypeSchema = z.string().min(1).refine(
  (t) => !WEB_APPLICATION_ALIASES.has(t.trim().toLowerCase()),
  { message: 'use "web-application" (the canonical kind), not "webapp"/"web-app", for a browser app component type' },
);

// Managed-API exposure policy (platform-owned; mirrors Go models.ExposesAPI).
const exposesAPISchema = z.strictObject({
  managed: z.boolean().optional(),
  auth: z.string().optional(),
  userContext: z.string().optional(),
  orgPublished: z.boolean().optional(),
});

export const componentDesignSchema = z.strictObject({
  name: z.string().min(1),
  type: componentTypeSchema,
  version: z.string().min(1),
  language: z.string().min(1),
  // buildpack stays a bare string in the schema; the "docker"-only rule is a
  // post-parse check in checkComponentDesign (like name==dir) so it does NOT
  // serialize to the shared JSON Schema — the BFF save-gate + Go fold stay
  // permissive/untouched while the agent write-gate still self-corrects in-turn.
  buildpack: z.string().min(1),
  appPath: z.string().min(1),
  entrypoint: z.string().min(1),
  exposure: z.enum(["internet", "intranet"]),
  dependencies: z.array(dependencySchema),
  description: z.string().min(1),
  endpoint: endpointSchema.optional(),
  exposesAPI: exposesAPISchema.optional(),
  componentAgentInstructions: z.string().optional(),
  // Agent-authored during enrichment (#369): the PRD stories this component
  // serves. The build gate's coverage check reads it — every PRD story must
  // be claimed by some component's list before a version can be cut.
  stories: z.array(z.number().int().positive()).optional(),
  skillsPinned: z.array(z.string()).optional(),
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

  const moved = movedDependencyFields(parsed);
  if (moved) {
    return {
      code: "SCHEMA_VIOLATION",
      message:
        `${path}: dependencies[${moved.index}] ("${moved.name}") carries ${moved.fields.map((f) => `"${f}"`).join(", ")} — ` +
        `an external dependency's definition lives in specs/design/dependencies/${moved.name}/dependency.json ` +
        `(provider, style, contract file, config keys, suggestions), written once and shared by every component that uses it. ` +
        `Keep only { "kind": "external", "name": "${moved.name}" } here and put those fields in that file.`,
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
  // buildpack is effectively closed: the platform builds every component with the
  // "docker" buildpack. Enforced here (post-parse, like name==dir) rather than in
  // the zod schema so it does NOT serialize to the shared JSON Schema — the BFF
  // save-gate + Go fold stay permissive/untouched; the agent (the sole writer)
  // self-corrects in-turn. Mirrored in the architecture skill.
  if (res.data.buildpack !== "docker") {
    return {
      code: "SCHEMA_VIOLATION",
      message: `${path}: "buildpack" must be "docker" (the platform's single build path), got ${JSON.stringify(res.data.buildpack)}.`,
    };
  }
  return null;
}

/** The first dependency still carrying a field that moved to its own file, if any. */
function movedDependencyFields(parsed: unknown): { index: number; name: string; fields: string[] } | null {
  const deps = (parsed as { dependencies?: unknown } | null)?.dependencies;
  if (!Array.isArray(deps)) return null;
  for (const [index, dep] of deps.entries()) {
    if (typeof dep !== "object" || dep === null || (dep as { kind?: unknown }).kind !== "external") continue;
    const fields = MOVED_DEPENDENCY_FIELDS.filter((f) => (dep as Record<string, unknown>)[f] !== undefined);
    if (fields.length > 0) {
      const name = (dep as { name?: unknown }).name;
      return { index, name: typeof name === "string" ? name : "?", fields };
    }
  }
  return null;
}
