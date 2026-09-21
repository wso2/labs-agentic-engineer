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

// A dependency's definition file, as the pane renders it. The pane reads the
// FILE (live doc ahead of the commit, or the committed copy), the way the
// component view reads design.json — so the definition view is a renderer for
// a path, not a page over the read model. What the file cannot know (status,
// reason, flags, who uses it) the read model supplies on top.

import { dependencyDesignSchema } from "@aep/agent-stream";

/** The file's shape as the write-gate's schema reads it (optional fields absent, never undefined). */
export type DependencyDefinition = ReturnType<typeof dependencyDesignSchema.parse>;

export type ParsedDependencyDefinition =
  | { ok: true; definition: DependencyDefinition }
  | { ok: false; message: string };

/**
 * How the consuming component talks to the system, in the user's words. It is
 * COMPUTED from the contract's type and never stored — the file dropped
 * `style` when the resource block landed, so this is the one place that turns
 * a document kind into the sentence a person reads.
 */
const CONSUMED_AS: Record<string, string> = {
  openapi: "REST API",
  graphql: "GraphQL",
  sdk: "SDK",
};

/** "Consumed as" for a contract type; "" when no contract settles it yet. */
export function consumedAsLabel(type: string | undefined): string {
  if (!type) return "";
  return CONSUMED_AS[type] ?? type;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function str(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

/**
 * A file written before `suggestions` existed carries `candidates` — the
 * retired "two or more researched fits" field. It reads as suggestions, the
 * way the platform's read path lifts it (dependency_json.go), so the view
 * shows the Service card with the options rather than a parse error; the
 * next write by the agent or the resolve flow lands `suggestions`.
 */
function liftRetiredCandidates(raw: unknown): unknown {
  if (!isRecord(raw) || !("candidates" in raw)) return raw;
  const { candidates, ...rest } = raw as { candidates?: unknown; suggestions?: unknown[] };
  if (!Array.isArray(candidates)) return raw;
  const lifted = candidates
    .filter((c): c is { name: string; style?: string; description?: string } => typeof c === "object" && c !== null && typeof (c as { name?: unknown }).name === "string")
    .map((c) => ({ name: c.name, ...(c.style ? { style: c.style } : {}), ...(c.description ? { description: c.description } : {}) }));
  return { ...rest, suggestions: [...(Array.isArray(rest.suggestions) ? rest.suggestions : []), ...lifted] };
}

/** The contract type a retired `style` meant, by the file name it pointed at. */
const TYPE_BY_STYLE: Record<string, "openapi" | "graphql" | "sdk"> = {
  "rest-api": "openapi",
  graphql: "graphql",
  sdk: "sdk",
};

/** The `resource.contract` a flat file's `style` + `contract`/`sdk` describe. */
function liftFlatContract(flat: Record<string, unknown>): Record<string, unknown> | undefined {
  const style = str(flat.style);
  const type = (style && TYPE_BY_STYLE[style]) ?? "openapi";
  const path = type === "sdk" ? (str(flat.sdk) ?? str(flat.contract)) : str(flat.contract);
  if (!path) return undefined;
  // A flat file said "the agent wrote this" by carrying the user's acceptance
  // record; the origin vocabulary says it outright.
  const accepted = isRecord(flat.assumed) ? flat.assumed : undefined;
  return { type, path, ...(accepted ? { origin: "assumed", accepted } : {}) };
}

/** `provenance` as the resource shape holds it: no `sliced`, `fetchedAt` is `readOn`. */
function liftFlatProvenance(value: unknown): Record<string, unknown> | undefined {
  if (!isRecord(value)) return undefined;
  const out: Record<string, unknown> = {};
  const sourceUrl = str(value.sourceUrl);
  const registry = str(value.registry);
  const sha256 = str(value.sha256);
  // `sliced` is gone with sliced contracts — a contract is a whole document
  // now — and the instant the source was read is `readOn` at both levels.
  const readOn = str(value.readOn) ?? str(value.fetchedAt);
  if (sourceUrl) out.sourceUrl = sourceUrl;
  if (registry) out.registry = registry;
  if (sha256) out.sha256 = sha256;
  if (readOn) out.readOn = readOn;
  return Object.keys(out).length > 0 ? out : undefined;
}

/**
 * The FLAT file — provider, style, contract and config at the top level, with
 * `source: "org"` for a registered one — as the nested `resource` block. Old
 * repos still hold these; the platform lifts them on read, and the console
 * renders the file itself, so it must lift the same way or every definition
 * written before the resource block reads as a parse error.
 *
 * A file that already has `resource` is passed through untouched, so a lift is
 * never applied twice.
 */
function liftFlatFile(raw: unknown): unknown {
  if (!isRecord(raw) || raw.resource !== undefined) return raw;
  const name = str(raw.name) ?? "";
  const resource: Record<string, unknown> = { name };
  // `source: "org"` said "this is the org registry's record"; the copy says it
  // with `resource.ref`, which is always the dependency's own name.
  if (raw.source === "org" && name) resource.ref = name;
  const description = str(raw.description);
  if (description) resource.description = description;
  const provider = str(raw.provider);
  if (provider) resource.provider = provider;
  if (Array.isArray(raw.config)) resource.config = raw.config;
  const contract = liftFlatContract(raw);
  if (contract) resource.contract = contract;

  const lifted: Record<string, unknown> = { name, resource };
  const provenance = liftFlatProvenance(raw.provenance);
  if (provenance) lifted.provenance = provenance;
  if (Array.isArray(raw.suggestions)) lifted.suggestions = raw.suggestions;
  return lifted;
}

/** Parse a dependency.json; the message names what is wrong with it. */
export function parseDependencyDefinition(text: string): ParsedDependencyDefinition {
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch (e) {
    return { ok: false, message: e instanceof Error ? e.message : "not valid JSON" };
  }
  const result = dependencyDesignSchema.safeParse(liftFlatFile(liftRetiredCandidates(raw)));
  if (!result.success) {
    const issue = result.error.issues[0];
    const where = issue && issue.path.length > 0 ? `${issue.path.join(".")}: ` : "";
    return { ok: false, message: `${where}${issue?.message ?? "does not match the definition schema"}` };
  }
  return { ok: true, definition: result.data };
}
