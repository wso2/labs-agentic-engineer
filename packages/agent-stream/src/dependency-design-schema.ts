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
 * the name matches the directory, suggestions and a provider never coexist,
 * config keys follow a chosen provider, the contract file name fits the style,
 * and `assumed` is echoed but never authored.
 */

import { z } from "zod";
import type { DependencyDesign, SdkManifest } from "./contracts/dependency-design.js";
import type { DiagramBundleReader } from "./design-diagrams.js";
import type { Equal } from "./type-equal.js";

export const dependencyStyleSchema = z.enum(["rest-api", "graphql", "sdk"]);

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

const provenanceSchema = z.strictObject({
  sourceUrl: z.string().optional(),
  sha256: z.string().regex(/^[0-9a-f]{64}$/, "a lower-case hex SHA-256").optional(),
  fetchedAt: z.string().optional(),
  sliced: z.boolean().optional(),
});

const assumptionSchema = z.strictObject({
  by: z.string().min(1),
  at: z.string().min(1),
  note: z.string().optional(),
});

export const dependencyDesignSchema = z.strictObject({
  name: z.string().min(1),
  description: z.string().optional(),
  source: z.enum(["project", "org"]).optional(),
  provider: z.string().min(1).optional(),
  style: dependencyStyleSchema.optional(),
  contract: z.string().min(1).optional(),
  sdk: z.string().min(1).optional(),
  provenance: provenanceSchema.optional(),
  suggestions: z.array(dependencySuggestionSchema).optional(),
  config: z.array(configKeySchema).optional(),
  assumed: assumptionSchema.optional(),
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

/** Contract file names a style accepts, in the directory beside dependency.json. */
export const CONTRACT_FILES_BY_STYLE: Readonly<Record<"rest-api" | "graphql", readonly string[]>> = {
  "rest-api": ["openapi.yaml", "openapi.yml", "openapi.json"],
  graphql: ["schema.graphql", "schema.graphqls"],
};
/** The one name an SDK manifest may have. */
export const SDK_MANIFEST_FILE = "sdk.json";

export interface DependencyDesignProblem {
  code: "INVALID_JSON" | "SCHEMA_VIOLATION";
  message: string;
}

/**
 * Validate a candidate dependency.json (or sdk.json) body for `path`. Returns
 * null when the path is neither, or the content is valid; otherwise the
 * problem, phrased for the model's self-correction. `bundle`, when given, is
 * the files as they stand before this write — it is how the gate tells an
 * echoed `assumed` record from an authored one.
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

  const res = dependencyDesignSchema.safeParse(parsed.value);
  if (!res.success) {
    const issues = res.error.issues.map((i) => `${i.path.join(".") || "(root)"}: ${i.message}`).join("; ");
    return { code: "SCHEMA_VIOLATION", message: `${path} violates the DependencyDesign schema — ${issues}.` };
  }
  const d = res.data;
  const dir = m[1]!;
  const violation = (message: string): DependencyDesignProblem => ({
    code: "SCHEMA_VIOLATION",
    message: `${path}: ${message}`,
  });

  if (d.name !== dir) {
    return violation(`"name" must equal the dependency directory ("${dir}"), got "${d.name}".`);
  }
  if (d.suggestions && d.provider) {
    return violation(
      `"suggestions" and "provider" never coexist — once the user chose a service, REMOVE suggestions and set provider + style; keep suggestions only while no service is chosen.`,
    );
  }
  if (d.suggestions && (d.style || d.contract || d.sdk)) {
    return violation(
      `while "suggestions" are open, "style", "contract" and "sdk" stay unset — they describe the chosen service, and none is chosen yet.`,
    );
  }
  if (d.config?.length && !d.provider && d.source !== "org") {
    return violation(
      `"config" is derived from the chosen service and is written only once "provider" is set — leave it out until the user has chosen; the resolve flow derives the keys.`,
    );
  }
  if ((d.contract || d.sdk) && !d.style) {
    return violation(`"style" is required once a "contract" or "sdk" is named — it says how the component talks to the system.`);
  }
  if (d.contract !== undefined) {
    if (d.contract.includes("/")) {
      return violation(`"contract" is a file name in this directory (e.g. "openapi.yaml"), not a path — got "${d.contract}".`);
    }
    const allowed = d.style === "sdk" ? CONTRACT_FILES_BY_STYLE["rest-api"].concat(CONTRACT_FILES_BY_STYLE.graphql) : CONTRACT_FILES_BY_STYLE[d.style!];
    if (!allowed.includes(d.contract)) {
      return violation(
        `"contract" for style "${d.style}" must be one of ${allowed.map((f) => `"${f}"`).join(", ")}, got "${d.contract}".`,
      );
    }
  }
  if (d.sdk !== undefined) {
    if (d.style !== "sdk") {
      return violation(`"sdk" is only meaningful on style "sdk", got style "${d.style}".`);
    }
    if (d.sdk !== SDK_MANIFEST_FILE) {
      return violation(`"sdk" must be "${SDK_MANIFEST_FILE}" (the manifest beside this file), got "${d.sdk}".`);
    }
  }
  if (d.style === "sdk" && !d.sdk) {
    return violation(`style "sdk" needs its manifest: write ${dependencyDir(dir)}/${SDK_MANIFEST_FILE} and set "sdk": "${SDK_MANIFEST_FILE}".`);
  }
  for (const k of d.config ?? []) {
    if (k.secret && k.defaultValue !== undefined) {
      return violation(`config key "${k.key}" is secret and cannot carry a defaultValue.`);
    }
  }
  if (bundle) {
    const authored = assumptionChanged(d.assumed, bundle.read(path));
    if (authored) {
      return violation(
        `"assumed" is the user's permission record — it is written when the user accepts your proposal from the dependency's definition in the spec view, never by you. Leave the field exactly as the file already has it (or omit it), and ask the user to accept the assumption instead.`,
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

/**
 * The user's `assumed` record is the platform's, not the agent's: a write of
 * `dependency.json` that leaves it out (a re-emission from a snapshot taken
 * before the user authorized the assumption, or a model that simply forgot
 * to carry it) must not lose it. The record is put back from the file already
 * in the bundle before the gate looks — so the agent never drops it, and the
 * one write that legitimately removes it (a real document replacing the
 * assumption) is the platform's own, which does not pass through here.
 * Returns `content` unchanged for any other path, an unparseable write (the
 * gate reports that), or when nothing was on file.
 */
export function preserveAssumption(path: string, content: string, prior: string | undefined): string {
  if (!DEPENDENCY_DESIGN_JSON_RE.test(path) || prior === undefined) return content;
  let before: unknown;
  let next: unknown;
  try {
    before = JSON.parse(prior);
    next = JSON.parse(content);
  } catch {
    return content;
  }
  const was = (before as { assumed?: unknown } | null)?.assumed;
  if (was === undefined || typeof next !== "object" || next === null || Array.isArray(next)) return content;
  if ((next as { assumed?: unknown }).assumed !== undefined) return content;
  return JSON.stringify({ ...(next as Record<string, unknown>), assumed: was }, null, 2) + (content.endsWith("\n") ? "\n" : "");
}

/**
 * True when `next` introduces or alters the assumption record relative to the
 * file already in the bundle. An unreadable or absent prior file means the
 * write is authoring the record. Omitting a record the file has is allowed —
 * that is the platform's own path when a real contract replaces an assumption.
 */
function assumptionChanged(next: unknown, prior: string | undefined): boolean {
  if (next === undefined) return false;
  if (prior === undefined) return true;
  let before: unknown;
  try {
    before = JSON.parse(prior);
  } catch {
    return true;
  }
  const was = (before as { assumed?: unknown } | null)?.assumed;
  return canonical(was) !== canonical(next);
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
    return {
      problem: {
        code: "INVALID_JSON",
        message: `${path} is not valid JSON: ${e instanceof Error ? e.message : String(e)}. Re-emit the whole file.`,
      },
    };
  }
}
