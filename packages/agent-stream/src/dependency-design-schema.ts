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
 * Runtime validation for the AUTHORED `dependencies/<name>/dependency.json`
 * (`DependencyDesign` in `./contracts/dependency-design.ts` — the wire source
 * of truth; the Zod schema below is drift-guarded against it). The FileBundle
 * calls `checkDependencyDesign` on every write to a matching path so the model
 * gets a one-round-trip self-correction instead of downstream consumers meeting
 * a broken file. `dependencyDesignSchema` is also the single definition the BFF
 * save-gate validates against, published as JSON Schema via `./json-schema.ts`.
 *
 * The shape rules (what the schema cannot say) live in `checkDependencyDesign`:
 * the name matches the directory and the resource block's name matches it too,
 * suggestions and a provider never coexist, config keys follow a chosen
 * provider, the contract path fits its type and has no URL form, a copy's
 * `ref` may only be the stub that asks for the copy (the platform fills the
 * block), and the organization's instructions and the user's `accepted` record
 * are echoed but never authored. The Go fold (agentfold/dependencygate.go) is
 * an exact port; the two must agree.
 */

import { z } from "zod";
import type { DependencyDesign, SdkManifest } from "./contracts/dependency-design.js";
import type { DiagramBundleReader } from "./design-diagrams.js";
import type { Equal } from "./type-equal.js";

export const dependencyStyleSchema = z.enum(["rest-api", "graphql", "sdk"]);
export const resourceContractTypeSchema = z.enum(["openapi", "graphql", "sdk", "asyncapi", "protobuf", "documentation"]);
export const contractOriginSchema = z.enum(["registry", "provider", "derived", "assumed"]);

export const configKeySchema = z.strictObject({
  key: z.string().min(1),
  secret: z.boolean().optional(),
  description: z.string().optional(),
  defaultValue: z.string().optional(),
});

export const dependencySuggestionSchema = z.strictObject({
  name: z.string().min(1),
  style: dependencyStyleSchema.optional(),
  description: z.string().optional(),
});

const assumptionSchema = z.strictObject({
  by: z.string().min(1),
  at: z.string().min(1),
  note: z.string().optional(),
});

export const resourceProvenanceSchema = z.strictObject({
  sourceUrl: z.string().optional(),
  registry: z.string().optional(),
  sha256: z.string().regex(/^[0-9a-f]{64}$/, "a lower-case hex SHA-256").optional(),
  readOn: z.string().optional(),
});

export const resourceContractSchema = z.strictObject({
  type: resourceContractTypeSchema,
  path: z.string().min(1),
  origin: contractOriginSchema.optional(),
  accepted: assumptionSchema.optional(),
});

export const resourceDefinitionSchema = z.strictObject({
  ref: z.string().min(1).optional(),
  name: z.string().min(1),
  description: z.string().optional(),
  provider: z.string().min(1).optional(),
  config: z.array(configKeySchema).optional(),
  contract: resourceContractSchema.optional(),
  consumptionInstructions: z.string().optional(),
  provenance: resourceProvenanceSchema.optional(),
});

export const dependencyDesignSchema = z.strictObject({
  name: z.string().min(1),
  resource: resourceDefinitionSchema,
  provenance: resourceProvenanceSchema.optional(),
  suggestions: z.array(dependencySuggestionSchema).optional(),
});

const _drift: Equal<z.infer<typeof dependencyDesignSchema>, DependencyDesign> = true;
void _drift;

export const sdkManifestSchema = z.strictObject({
  packages: z.record(z.string().min(1), z.string().min(1)),
  docsUrl: z.string().optional(),
  calls: z.array(z.string().min(1)).optional(),
  derived: z.boolean().optional(),
  assumed: z.boolean().optional(),
});

const _sdkDrift: Equal<z.infer<typeof sdkManifestSchema>, SdkManifest> = true;
void _sdkDrift;

/** Matches `specs/design/dependencies/<name>/dependency.json`, capturing the name. */
export const DEPENDENCY_DESIGN_JSON_RE = /^specs\/design\/dependencies\/([^/]+)\/dependency\.json$/;
/** Matches `specs/design/dependencies/<name>/sdk.json`, capturing the name. */
export const SDK_MANIFEST_JSON_RE = /^specs\/design\/dependencies\/([^/]+)\/sdk\.json$/;

/** The dependency directory for `name`, relative to the repo root. */
export function dependencyDir(name: string): string {
  return `specs/design/dependencies/${name}`;
}

/** The dependency.json path for `name`. */
export function dependencyDesignPath(name: string): string {
  return `${dependencyDir(name)}/dependency.json`;
}

/** Contract file names a PROJECT contract type accepts, in the directory beside dependency.json. */
export const CONTRACT_FILES_BY_TYPE: Readonly<Record<"openapi" | "graphql" | "sdk", readonly string[]>> = {
  openapi: ["openapi.yaml", "openapi.yml", "openapi.json"],
  graphql: ["schema.graphql", "schema.graphqls"],
  sdk: ["sdk.json"],
};
/** The one name an SDK manifest may have. */
export const SDK_MANIFEST_FILE = "sdk.json";

/** The previous flat shape's top-level fields, named in their own message. */
const RETIRED_TOP_LEVEL = new Set(["source", "provider", "style", "contract", "sdk", "config", "assumed", "candidates", "description"]);

export interface DependencyDesignProblem {
  code: "INVALID_JSON" | "SCHEMA_VIOLATION";
  message: string;
}

/**
 * Validate a candidate dependency.json (or sdk.json) body for `path`. Returns
 * null when the path is neither, or the content is valid; otherwise the
 * problem, phrased for the model's self-correction. `bundle`, when given, is
 * the files as they stand before this write — it is how the gate tells an
 * echoed platform field from an authored one.
 */
export function checkDependencyDesign(
  path: string,
  content: string,
  bundle?: DiagramBundleReader,
): DependencyDesignProblem | null {
  const sdk = SDK_MANIFEST_JSON_RE.exec(path);
  if (sdk) return checkSdkManifest(path, content);
  const m = DEPENDENCY_DESIGN_JSON_RE.exec(path);
  if (!m) return null;

  const parsed = parseJson(path, content);
  if ("problem" in parsed) return parsed.problem;
  const dir = m[1]!;
  const violation = (message: string): DependencyDesignProblem => ({
    code: "SCHEMA_VIOLATION",
    message: `${path}: ${message}`,
  });

  // The retired flat shape gets its own message before the schema's generic one.
  if (typeof parsed.value === "object" && parsed.value !== null && !Array.isArray(parsed.value)) {
    for (const k of Object.keys(parsed.value)) {
      if (RETIRED_TOP_LEVEL.has(k)) {
        return violation(
          `"${k}" is not a top-level field any more — the resource's own fields (name, description, provider, config, contract) live in the "resource" block; style is not stored (the contract's type says it); "source" is the presence of "resource.ref"; the acceptance record is "resource.contract.accepted".`,
        );
      }
    }
    const res = (parsed.value as { resource?: unknown }).resource;
    if (typeof res === "object" && res !== null && !Array.isArray(res)) {
      const c = (res as { contract?: unknown }).contract;
      if (typeof c === "object" && c !== null && "url" in (c as Record<string, unknown>)) {
        return violation(
          `resource.contract has no "url" form — a contract is a FILE in this directory ({ "type", "path" }); an internet address belongs in "provenance.sourceUrl" and the document itself is copied beside this file.`,
        );
      }
    }
    if ((parsed.value as { resource?: unknown }).resource === undefined) {
      return violation(`"resource" is required — the block that says what the thing is (name, description; provider, config and contract once a service is chosen).`);
    }
  }

  const res = dependencyDesignSchema.safeParse(parsed.value);
  if (!res.success) {
    const issues = res.error.issues.map((i) => `${i.path.join(".") || "(root)"}: ${i.message}`).join("; ");
    return { code: "SCHEMA_VIOLATION", message: `${path} violates the DependencyDesign schema — ${issues}.` };
  }
  const d = res.data;
  const r = d.resource;

  if (d.name !== dir) {
    return violation(`"name" must equal the dependency directory ("${dir}"), got "${d.name}".`);
  }
  if (r.name !== d.name) {
    return violation(`"resource.name" must equal the dependency name ("${d.name}"), got "${r.name}".`);
  }
  if (r.ref !== undefined && r.ref !== d.name) {
    return violation(`"resource.ref" must equal the dependency name ("${d.name}") — a registered resource is always used under its own name; got "${r.ref}".`);
  }
  if (d.suggestions && r.provider) {
    return violation(
      `"suggestions" and "resource.provider" never coexist — once the user chose a service, REMOVE suggestions and set the provider; keep suggestions only while no service is chosen.`,
    );
  }
  if (d.suggestions && (r.contract || r.ref)) {
    return violation(`while "suggestions" are open, "resource.contract" and "resource.ref" stay unset — they describe the chosen service, and none is chosen yet.`);
  }
  if (r.config?.length && !r.provider && !r.ref) {
    return violation(
      `"resource.config" is derived from the chosen service and is written only once "resource.provider" is set — leave it out until the user has chosen; the resolve flow derives the keys.`,
    );
  }
  if (r.contract) {
    const c = r.contract;
    if (!(c.type in CONTRACT_FILES_BY_TYPE)) {
      return violation(`resource.contract.type: "${c.type}" is not an allowed value (openapi, graphql, sdk).`);
    }
    if (c.path.includes("/")) {
      return violation(`resource.contract.path is a file name in this directory (e.g. "openapi.yaml"), not a path — got "${c.path}".`);
    }
    const allowed = CONTRACT_FILES_BY_TYPE[c.type as keyof typeof CONTRACT_FILES_BY_TYPE];
    if (!allowed.includes(c.path)) {
      return violation(`resource.contract.path for type "${c.type}" must be one of ${allowed.map((f) => `"${f}"`).join(", ")}, got "${c.path}".`);
    }
  }
  for (const k of r.config ?? []) {
    if (k.secret && k.defaultValue !== undefined) {
      return violation(`config key "${k.key}" is secret and cannot carry a defaultValue.`);
    }
  }
  for (const [where, p] of [["provenance", d.provenance], ["resource.provenance", r.provenance]] as const) {
    if (p?.registry && (p.registry.includes("://") || p.registry.startsWith("/"))) {
      return violation(`${where}.registry: the org-resource-docs path ("<name>/<file>"), never a URL.`);
    }
  }
  if (bundle) {
    const prior = bundle.read(path);
    if (platformFieldChanged(r.consumptionInstructions, prior, (b) => b?.resource?.consumptionInstructions)) {
      return violation(
        `"resource.consumptionInstructions" is the organization's, copied by the platform when a registered resource is used here — write the stub { "name", "resource": { "ref", "name" } } and the platform fills the block at save. Leave the field exactly as the file already has it (or omit it).`,
      );
    }
    if (platformFieldChanged(r.contract?.accepted, prior, (b) => b?.resource?.contract?.accepted)) {
      return violation(
        `"resource.contract.accepted" is the user's permission record — it is written when the user accepts your proposal from the dependency's definition in the spec view, never by you. Leave the field exactly as the file already has it (or omit it), and ask the user to accept the assumption instead.`,
      );
    }
  }
  return null;
}

function checkSdkManifest(path: string, content: string): DependencyDesignProblem | null {
  const parsed = parseJson(path, content);
  if ("problem" in parsed) return parsed.problem;
  const res = sdkManifestSchema.safeParse(parsed.value);
  if (!res.success) {
    const issues = res.error.issues.map((i) => `${i.path.join(".") || "(root)"}: ${i.message}`).join("; ");
    return { code: "SCHEMA_VIOLATION", message: `${path} violates the SdkManifest schema — ${issues}.` };
  }
  if (Object.keys(res.data.packages).length === 0) {
    return {
      code: "SCHEMA_VIOLATION",
      message: `${path}: "packages" needs at least one language → package entry (e.g. "typescript": "npm:stripe@^14").`,
    };
  }
  for (const [lang, pkg] of Object.entries(res.data.packages)) {
    if (lang !== lang.toLowerCase()) {
      return { code: "SCHEMA_VIOLATION", message: `${path}: language keys are lower-case ("${lang.toLowerCase()}"), got "${lang}".` };
    }
    if (!/^[a-z][a-z0-9-]*:.+/.test(pkg)) {
      return {
        code: "SCHEMA_VIOLATION",
        message: `${path}: package for "${lang}" must be ecosystem-prefixed ("npm:…", "go:…", "pypi:…", "ballerina:…"), got "${pkg}".`,
      };
    }
  }
  return null;
}

type PriorResource = { ref?: unknown; consumptionInstructions?: unknown; contract?: { accepted?: unknown } };
type PriorShape = { resource?: PriorResource; assumed?: unknown } | null;

/**
 * The prior file's resource block. A file written before the nested shape has
 * none, and its acceptance record sits at the top level as `assumed` — the
 * codec lifts that when it DECODES, but this gate reads the raw prior, so it
 * lifts it here too. Without this, the first nested write over a flat file
 * silently drops the user's acceptance.
 */
function priorResource(before: unknown): PriorResource | null {
  const res = (before as PriorShape)?.resource;
  if (typeof res === "object" && res !== null && !Array.isArray(res)) return res;
  const assumed = (before as PriorShape)?.assumed;
  if (typeof assumed !== "object" || assumed === null || Array.isArray(assumed)) return null;
  return { contract: { accepted: assumed } };
}

/**
 * The platform's fields on a dependency's definition are not the agent's: the
 * organization's `consumptionInstructions` (landed by the platform when a stub
 * names a registered resource) and the user's `contract.accepted` record. A
 * write of `dependency.json` that leaves them out (a re-emission from a
 * snapshot taken before the platform's copy, or a model that simply forgot to
 * carry them) must not lose them. They are put back from the file already in
 * the bundle before the gate looks — the instructions only while the write
 * still keeps the copy's `ref`, since dropping the ref is how the agent
 * replaces a copy with a resource the project defines. Returns `content`
 * unchanged for any other path, an unparseable write (the gate reports that),
 * or when nothing was on file.
 */
export function preservePlatformFields(path: string, content: string, prior: string | undefined): string {
  if (!DEPENDENCY_DESIGN_JSON_RE.test(path) || prior === undefined) return content;
  let before: unknown;
  let next: unknown;
  try {
    before = JSON.parse(prior);
    next = JSON.parse(content);
  } catch {
    return content;
  }
  const beforeRes = priorResource(before);
  if (beforeRes === null) return content;
  if (typeof next !== "object" || next === null || Array.isArray(next)) return content;
  const nextRes = (next as { resource?: unknown }).resource;
  if (typeof nextRes !== "object" || nextRes === null || Array.isArray(nextRes)) return content;
  const out = { ...(nextRes as Record<string, unknown>) };
  let changed = false;
  // The organization's instructions come back only while the write still
  // names the copy (`ref`). A write that drops the ref is the agent replacing
  // the copy with a resource the project defines (Select a provider on a name
  // the organization never registered, or Reconsider), and an inline resource
  // never carries the organization's instructions — so the ref itself is never
  // put back, and neither are they.
  if (out.ref !== undefined) {
    const was = beforeRes.consumptionInstructions;
    if (was !== undefined && was !== null && out.consumptionInstructions === undefined) {
      out.consumptionInstructions = was;
      changed = true;
    }
  }
  const wasAccepted = beforeRes.contract?.accepted;
  if (wasAccepted !== undefined && wasAccepted !== null) {
    const nextC = out.contract;
    if (typeof nextC === "object" && nextC !== null && !Array.isArray(nextC) && (nextC as { accepted?: unknown }).accepted === undefined) {
      out.contract = { ...(nextC as Record<string, unknown>), accepted: wasAccepted };
      changed = true;
    }
  }
  if (!changed) return content;
  return JSON.stringify({ ...(next as Record<string, unknown>), resource: out }, null, 2) + (content.endsWith("\n") ? "\n" : "");
}

/**
 * True when `next` introduces or alters a platform field relative to the file
 * already in the bundle. An unreadable or absent prior file means the write is
 * authoring it. Omitting a field the file has is allowed — the preserve step
 * puts it back.
 */
function platformFieldChanged(next: unknown, prior: string | undefined, pick: (before: PriorShape) => unknown): boolean {
  if (next === undefined) return false;
  if (prior === undefined) return true;
  let before: unknown;
  try {
    before = JSON.parse(prior);
  } catch {
    return true;
  }
  return canonical(pick(before as PriorShape)) !== canonical(next);
}

/** JSON with sorted object keys — the Go fold compares the same way, so a re-ordered echo reads equal on both sides. */
function canonical(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  if (typeof value === "object" && value !== null) {
    return `{${Object.keys(value)
      .sort()
      .map((k) => `${JSON.stringify(k)}:${canonical((value as Record<string, unknown>)[k])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value) ?? "undefined";
}

function parseJson(path: string, content: string): { value: unknown } | { problem: DependencyDesignProblem } {
  try {
    return { value: JSON.parse(content) };
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    return {
      problem: {
        code: "INVALID_JSON",
        message: `${path} is not valid JSON (${msg}). Re-emit the file with the FULL corrected content.`,
      },
    };
  }
}
