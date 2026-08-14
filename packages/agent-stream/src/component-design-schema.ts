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
// `secret: true` routes the value through the private secret path. `description`
// is an optional human-readable note (what the value is for) the Build
// dependency drawer renders under the field. `defaultValue` is an optional
// suggested initial value the agent MAY set for a NON-secret key (a region, a
// base URL); the drawer pre-fills the field with it. Never set for a secret.
const configKeySchema = z.strictObject({
  key: z.string().min(1),
  secret: z.boolean().optional(),
  description: z.string().optional(),
  defaultValue: z.string().optional(),
});

// One option in an ambiguous external dependency's resolution set (2+ required
// — see dependencySchema's `candidates`; a single candidate never occurs: one
// option fully known collapses to a resolved dep, one option partially known
// is a partial dep, not a candidate). Mirrors Go models.DependencyCandidate.
const dependencyCandidateSchema = z.strictObject({
  name: z.string().min(1),
  style: z.enum(["rest-api", "sdk"]),
  description: z.string().optional(),
  package: z.string().optional(),
});

// The dependency fields meaningful ONLY on kind="external" (candidates, style,
// package, specPath): a platform-resource is catalog-picked, an org-service
// is catalog-resolved — neither has web provenance. Enforced mechanically
// below (superRefine), mirrored in the Go fold gate (agentfold/designgate.go).
const EXTERNAL_ONLY_DEPENDENCY_FIELDS = [
  "candidates",
  "style",
  "package",
  "specPath",
] as const;

// The resolved consumer-side wiring: ONE VARIANT PER workload.yaml
// `dependencies:` sub-block, each byte-identical to one entry of its block so the
// coding agent copies it instead of transforming it.
//
// A UNION rather than one object of optional fields, because that is what keeps
// each block's all-or-nothing rule enforceable: a resource variant needs BOTH ref
// and envBindings (a ref with no bindings renders an unusable resources[] entry),
// and an endpoint variant needs the full target. Optional fields on one flat
// object would accept every half-stamped combination.
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

// One unified, kind-discriminated dependency edge — the successor to the legacy
// per-kind `connections[]`. A single flat shape carries every kind's fields;
// `kind` selects which are meaningful (LENIENT within the known set, mirroring
// the Go codec) but unknown keys — status/reason especially — are rejected.
// `needsSpec` is GONE (dependency-management schema revision — derived-state
// model): every resolution state is derived from which of style/package/
// candidates/specPath are present, never a stored flag.
const dependencyObjectSchema = z.strictObject({
  kind: z.enum(["component", "org-service", "external", "platform-resource"]),
  name: z.string().min(1),
  description: z.string().optional(),
  // external: REST API ("rest-api") or SDK ("sdk") shape.
  style: z.enum(["rest-api", "sdk"]).optional(),
  // external (sdk style): one ecosystem-prefixed package identifier, e.g.
  // "npm:stripe@^14" — version inline but optional.
  package: z.string().optional(),
  // external: the contract location — either a URL to a published spec
  // (recorded as-is, NOT fetched-and-stored) or a repo-relative path to a
  // user-provided committed spec (dependencies/<name>.openapi.yaml).
  specPath: z.string().optional(),
  // external: 2+ identified-but-not-pinned options — omitted, never empty or
  // single-item (a lone option is a partial dep, not a candidate).
  candidates: z.array(dependencyCandidateSchema).min(2).optional(),
  config: z.array(configKeySchema).optional(),
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

const dependencySchema = dependencyObjectSchema.superRefine((dep, ctx) => {
  if (dep.kind === "external") return;
  for (const field of EXTERNAL_ONLY_DEPENDENCY_FIELDS) {
    if (dep[field] !== undefined) {
      ctx.addIssue({
        code: "custom",
        path: [field],
        message: `${field} is only meaningful on an external dependency (kind="external"), got kind="${dep.kind}"`,
      });
    }
  }
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
