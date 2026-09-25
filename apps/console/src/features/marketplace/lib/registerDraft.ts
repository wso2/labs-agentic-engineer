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
import type { ContractRow } from "../components/ContractFields";
import type { ResourceDocRow } from "../components/ResourceDocsFields";

type ConfigKeyDTO = components["schemas"]["ConfigKeyDTO"];
type ResourceContractType = components["schemas"]["ResourceContractType"];
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

/** A contract may be any of these; an SDK manifest is one too. */
const CONTRACT_TYPES = new Set<string>([
  "openapi",
  "graphql",
  "sdk",
  "asyncapi",
  "protobuf",
  "documentation",
]);

export type RegisterDraft = {
  name?: string;
  /** The concrete system the resource is ("Open Exchange Rates"). */
  provider?: string;
  description?: string;
  consumptionInstructions?: string;
  config?: Array<{ key: string; description: string; secret: boolean }>;
  /**
   * The one document consumers code against. The agent only ever proposes an
   * ADDRESS for it — it never uploads bytes — so the draft's contract is a URL
   * the form offers to the platform to fetch.
   */
  contract?: { type: ResourceContractType; url: string };
  resourceDocs?: Array<{ type: ResourceDocPointerDTO["type"]; url: string }>;
};

export type RegisterFormSnapshot = {
  name: string;
  provider: string;
  description: string;
  consumptionInstructions: string;
  keys: ConfigKeyDTO[];
  values: Record<string, string>;
  contract: ContractRow;
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

function parseContract(
  value: unknown,
): { type: ResourceContractType; url: string } | null {
  if (!isPlainObject(value)) return null;
  if (typeof value.type !== "string" || !CONTRACT_TYPES.has(value.type)) return null;
  // A blank address is no address: the form trims before it submits, so a
  // whitespace-only URL would replace a real one and then vanish on the wire.
  if (typeof value.url !== "string" || value.url.trim() === "") return null;
  // A draft never carries document bytes — an upload is the user's own act.
  if (hasFileRowFields(value)) return null;
  return { type: value.type as ResourceContractType, url: value.url.trim() };
}

export function parseRegisterDraft(input: unknown): RegisterDraft | null {
  if (!isPlainObject(input)) return null;
  const draft: RegisterDraft = {};
  if (typeof input.name === "string") draft.name = input.name;
  if (typeof input.provider === "string") draft.provider = input.provider;
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
  const contract = parseContract(input.contract);
  if (contract) draft.contract = contract;
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

/**
 * The draft's contract as the form's block. An uploaded file is the user's own
 * and outranks a proposal, so a draft never displaces one; the type still
 * follows the draft, which is what the agent actually knows.
 */
function applyContract(current: ContractRow, contract: RegisterDraft["contract"]): ContractRow {
  if (contract === undefined) return current;
  // An uploaded file is the user's own act and outranks a draft. Its TYPE goes
  // with it: the bytes on file are that format, and taking the draft's type
  // would label the document as something it is not.
  if (current.fileName) return current;
  return { ...current, type: contract.type, url: contract.url };
}

export function applyRegisterDraft(
  current: RegisterFormSnapshot,
  draft: RegisterDraft,
  mode: { freezeName: boolean; freezeKeys: boolean },
): RegisterFormSnapshot {
  return {
    name: mode.freezeName || draft.name === undefined ? current.name : draft.name,
    provider: draft.provider === undefined ? current.provider : draft.provider,
    description:
      draft.description === undefined ? current.description : draft.description,
    consumptionInstructions:
      draft.consumptionInstructions === undefined
        ? current.consumptionInstructions
        : draft.consumptionInstructions,
    keys: applyKeys(current.keys, draft.config, mode.freezeKeys),
    values: current.values,
    contract: applyContract(current.contract, draft.contract),
    docs:
      draft.resourceDocs === undefined
        ? current.docs
        : patchUrlDocs(current.docs, draft.resourceDocs),
  };
}
