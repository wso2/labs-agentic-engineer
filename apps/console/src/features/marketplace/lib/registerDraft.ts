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

import type { components } from "../../../generated/aep-api";
import type { ResourceDocRow } from "../components/ResourceDocsFields";

type ConfigKeyDTO = components["schemas"]["ConfigKeyDTO"];
type ResourceDocPointerDTO = components["schemas"]["ResourceDocPointerDTO"];
type DocType = ResourceDocPointerDTO["type"];

export const DRAFT_EXTERNAL_RESOURCE_TOOL = "draftExternalResource";

const DOC_TYPES = new Set<string>([
  "documentation",
  "openapi",
  "graphql",
  "asyncapi",
  "protobuf",
]);

export type RegisterDraft = {
  name?: string;
  description?: string;
  consumptionInstructions?: string;
  config?: Array<{ key: string; description: string; secret: boolean }>;
  resourceDocs?: Array<{ type: ResourceDocPointerDTO["type"]; url: string }>;
};

export type RegisterFormSnapshot = {
  name: string;
  description: string;
  consumptionInstructions: string;
  keys: ConfigKeyDTO[];
  values: Record<string, string>;
  docs: ResourceDocRow[];
};

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function parseConfigEntry(
  value: unknown,
): { key: string; description: string; secret: boolean } | null {
  if (!isPlainObject(value)) return null;
  if (typeof value.key !== "string" || value.key === "") return null;
  if (typeof value.description !== "string") return null;
  if (typeof value.secret !== "boolean") return null;
  return { key: value.key, description: value.description, secret: value.secret };
}

function hasFileRowFields(value: Record<string, unknown>): boolean {
  return (
    (typeof value.path === "string" && value.path !== "") ||
    (typeof value.fileName === "string" && value.fileName !== "") ||
    (typeof value.content === "string" && value.content !== "")
  );
}

function parseDocEntry(
  value: unknown,
): { type: DocType; url: string } | null {
  if (!isPlainObject(value)) return null;
  if (typeof value.type !== "string" || !DOC_TYPES.has(value.type)) return null;
  if (typeof value.url !== "string" || value.url === "") return null;
  if (hasFileRowFields(value)) return null;
  return { type: value.type as DocType, url: value.url };
}

export function parseRegisterDraft(input: unknown): RegisterDraft | null {
  if (!isPlainObject(input)) return null;
  const draft: RegisterDraft = {};
  if (typeof input.name === "string") draft.name = input.name;
  if (typeof input.description === "string") draft.description = input.description;
  if (typeof input.consumptionInstructions === "string") {
    draft.consumptionInstructions = input.consumptionInstructions;
  }
  if (Array.isArray(input.config)) {
    draft.config = input.config.flatMap((entry) => {
      const parsed = parseConfigEntry(entry);
      return parsed ? [parsed] : [];
    });
  }
  if (Array.isArray(input.resourceDocs)) {
    draft.resourceDocs = input.resourceDocs.flatMap((entry) => {
      const parsed = parseDocEntry(entry);
      return parsed ? [parsed] : [];
    });
  }
  return draft;
}

function urlDocRow(entry: { type: DocType; url: string }): ResourceDocRow {
  return {
    type: entry.type,
    source: "url",
    url: entry.url,
    fileName: "",
    content: "",
    path: "",
  };
}

/** Upsert URL docs by type; never drop file rows or URL types the draft omitted. */
function patchUrlDocs(
  current: ResourceDocRow[],
  draftDocs: Array<{ type: DocType; url: string }>,
): ResourceDocRow[] {
  const files = current.filter((row) => row.source !== "url");
  const urls = new Map(
    current
      .filter((row) => row.source === "url")
      .map((row) => [row.type, row] as const),
  );
  for (const entry of draftDocs) {
    const parsed = parseDocEntry(entry);
    if (!parsed) continue;
    urls.set(parsed.type, urlDocRow(parsed));
  }
  return [...files, ...urls.values()];
}

function applyKeys(
  current: ConfigKeyDTO[],
  config: RegisterDraft["config"],
  freezeKeys: boolean,
): ConfigKeyDTO[] {
  if (config === undefined) return current;
  if (!freezeKeys) {
    return config.map((entry) => ({
      key: entry.key,
      description: entry.description,
      secret: entry.secret,
    }));
  }
  const byKey = new Map(config.map((entry) => [entry.key, entry]));
  return current.map((row) => {
    const patch = byKey.get(row.key);
    if (!patch) return row;
    return { ...row, description: patch.description };
  });
}

export function applyRegisterDraft(
  current: RegisterFormSnapshot,
  draft: RegisterDraft,
  mode: { freezeName: boolean; freezeKeys: boolean },
): RegisterFormSnapshot {
  return {
    name: mode.freezeName || draft.name === undefined ? current.name : draft.name,
    description:
      draft.description === undefined ? current.description : draft.description,
    consumptionInstructions:
      draft.consumptionInstructions === undefined
        ? current.consumptionInstructions
        : draft.consumptionInstructions,
    keys: applyKeys(current.keys, draft.config, mode.freezeKeys),
    values: current.values,
    docs:
      draft.resourceDocs === undefined
        ? current.docs
        : patchUrlDocs(current.docs, draft.resourceDocs),
  };
}
