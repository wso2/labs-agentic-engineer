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

// A Registered External resource's card holds one draft, and one Save writes
// the whole record (register, or update). This is that draft: what it starts
// from, whether it changed, what stops a Save, and the request a Save sends.
// Promote to organization has a smaller form of its own, the values and
// instructions the organization adds to a project's copy (`promote*`).

type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];
type EnvValueCellDTO = components["schemas"]["EnvValueCellDTO"];
type ConfigKeyDTO = components["schemas"]["ConfigKeyDTO"];
type ResourceContractType = components["schemas"]["ResourceContractType"];
type RegisterExternalResourceRequest = components["schemas"]["RegisterExternalResourceRequest"];
type ResourceDocWriteDTO = components["schemas"]["ResourceDocWriteDTO"];
type PromoteExternalResourceRequest = components["schemas"]["PromoteExternalResourceRequest"];
type EnvValueWriteDTO = components["schemas"]["EnvValueWriteDTO"];

export interface DraftKey {
  key: string;
  description: string;
  secret: boolean;
}

/**
 * A new contract document for the record: an address the platform fetches,
 * or a file chosen here. Neither leaves the record's document as it is.
 */
export interface ContractDraft {
  type: ResourceContractType;
  url: string;
  fileName: string;
  content: string;
}

export interface ResourceDraft {
  name: string;
  provider: string;
  description: string;
  consumptionInstructions: string;
  keys: DraftKey[];
  /** One value per key and environment, by `cellKey`. A secret starts blank: its value is never read back. */
  values: Record<string, string>;
  contract: ContractDraft;
}

export function cellKey(environment: string, key: string): string {
  return `${environment}:${key}`;
}

const NO_CONTRACT: ContractDraft = { type: "openapi", url: "", fileName: "", content: "" };

/** The draft a card opens with: the record as saved, or an empty one for New resource. */
export function draftOfResource(resource: ExternalResourceDTO | null): ResourceDraft {
  if (!resource) {
    return {
      name: "",
      provider: "",
      description: "",
      consumptionInstructions: "",
      keys: [{ key: "", description: "", secret: true }],
      values: {},
      contract: NO_CONTRACT,
    };
  }
  const keys = (resource.config ?? []).map((k) => ({ key: k.key, description: k.description ?? "", secret: Boolean(k.secret) }));
  const secret = new Set(keys.filter((k) => k.secret).map((k) => k.key));
  const values: Record<string, string> = {};
  for (const cell of resource.envCells ?? []) {
    values[cellKey(cell.environment, cell.key)] = secret.has(cell.key) ? "" : (cell.value ?? "");
  }
  return {
    name: resource.name,
    provider: resource.provider ?? "",
    description: resource.description ?? "",
    consumptionInstructions: resource.consumptionInstructions ?? "",
    keys,
    values,
    contract: { ...NO_CONTRACT, type: resource.contract?.type ?? "openapi" },
  };
}

function comparable(draft: ResourceDraft): string {
  const values = Object.fromEntries(Object.entries(draft.values).filter(([, v]) => v !== ""));
  return JSON.stringify({
    ...draft,
    name: draft.name.trim(),
    provider: draft.provider.trim(),
    description: draft.description.trim(),
    consumptionInstructions: draft.consumptionInstructions.trim(),
    keys: draft.keys.map((k) => ({ ...k, key: k.key.trim(), description: k.description.trim() })),
    values: Object.keys(values)
      .sort()
      .map((k) => [k, values[k]]),
    contract: draft.contract.url.trim() || draft.contract.fileName ? draft.contract : null,
  });
}

/** Whether Save has anything to write, and leaving anything to lose. */
export function isResourceDirty(draft: ResourceDraft, saved: ExternalResourceDTO | null): boolean {
  return comparable(draft) !== comparable(draftOfResource(saved));
}

function configured(cells: readonly EnvValueCellDTO[] | null | undefined, environment: string, key: string): boolean {
  return (cells ?? []).some((c) => c.environment === environment && c.key === key && c.status === "configured");
}

export interface ResourceProblems {
  name?: string;
  provider?: string;
  description?: string;
  consumptionInstructions?: string;
  keys?: string;
  keyRows: { key?: string; description?: string }[];
  /** By `cellKey`. */
  values: Record<string, string>;
  contract?: string;
}

const REQUIRED = "Required.";

/**
 * What stops a Save, by field; null when nothing does. Every key needs a value
 * in every environment, save a secret that already has one (left blank, it
 * keeps it). `new` is refused as a name: it is New resource's own address.
 */
export function resourceProblems(
  draft: ResourceDraft,
  { saved, environments }: { saved: ExternalResourceDTO | null; environments: readonly string[] },
): ResourceProblems | null {
  const p: ResourceProblems = { keyRows: [], values: {} };
  let bad = false;
  const flag = <K extends "name" | "provider" | "description" | "consumptionInstructions" | "keys" | "contract">(field: K, message: string) => {
    p[field] = message;
    bad = true;
  };
  const name = draft.name.trim();
  if (!name) flag("name", REQUIRED);
  else if (!saved && name === "new") flag("name", "Choose another name: “new” is taken by the console.");
  if (!draft.provider.trim()) flag("provider", REQUIRED);
  if (!draft.description.trim()) flag("description", REQUIRED);
  if (!draft.consumptionInstructions.trim()) flag("consumptionInstructions", REQUIRED);
  if (draft.keys.length === 0) flag("keys", "Add at least one key.");
  const seen = new Set<string>();
  p.keyRows = draft.keys.map((k) => {
    const row: { key?: string; description?: string } = {};
    const key = k.key.trim();
    if (!key) row.key = REQUIRED;
    else if (seen.has(key)) row.key = "Each key once.";
    seen.add(key);
    if (!k.description.trim()) row.description = REQUIRED;
    if (row.key || row.description) bad = true;
    return row;
  });
  for (const k of draft.keys) {
    const key = k.key.trim();
    if (!key) continue;
    for (const env of environments) {
      if ((draft.values[cellKey(env, key)] ?? "").trim()) continue;
      if (k.secret && configured(saved?.envCells, env, key)) continue;
      p.values[cellKey(env, key)] = REQUIRED;
      bad = true;
    }
  }
  if (draft.contract.fileName && !draft.contract.content) flag("contract", "That file is empty. Choose the document itself.");
  return bad ? p : null;
}

/**
 * The request a Save sends. The record's resource docs go back as they are
 * (the card does not edit them, and an update without them drops them); its
 * contract document is replaced only when the draft names a new one.
 */
export function resourceRequest(
  draft: ResourceDraft,
  { saved, environments }: { saved: ExternalResourceDTO | null; environments: readonly string[] },
): RegisterExternalResourceRequest {
  const config: ConfigKeyDTO[] = draft.keys.map((k) => ({ key: k.key.trim(), description: k.description.trim(), secret: k.secret }));
  const envValues: EnvValueWriteDTO[] = config.flatMap((k) =>
    environments.map((environment) => ({ environment, key: k.key, value: draft.values[cellKey(environment, k.key)] ?? "" })),
  );
  const { type, url, fileName, content } = draft.contract;
  const contract = fileName && content ? { type, fileName, content } : url.trim() ? { type, url: url.trim() } : undefined;
  const resourceDocs = (saved?.resourceDocs ?? []).flatMap((d): ResourceDocWriteDTO[] =>
    d.url ? [{ type: d.type, url: d.url }] : d.path ? [{ type: d.type, path: d.path }] : [],
  );
  return {
    name: draft.name.trim(),
    provider: draft.provider.trim(),
    description: draft.description.trim(),
    consumptionInstructions: draft.consumptionInstructions.trim(),
    config,
    envValues,
    ...(contract ? { contract } : {}),
    ...(resourceDocs.length > 0 ? { resourceDocs } : {}),
  };
}

export interface PromoteDraft {
  consumptionInstructions: string;
  /** By `cellKey`; blank is carried over from the project's own value. */
  values: Record<string, string>;
}

/**
 * What stops a Promote; null when nothing does. Instructions are the
 * organization's to write. A value left blank is carried over from the
 * project; the project's secrets come over together, so in one environment
 * either every secret is typed or none is.
 */
export function promoteProblems(
  draft: PromoteDraft,
  { keys, environments }: { keys: readonly ConfigKeyDTO[]; environments: readonly string[] },
): { consumptionInstructions?: string; environments: Record<string, string> } | null {
  const problems: { consumptionInstructions?: string; environments: Record<string, string> } = { environments: {} };
  let bad = false;
  if (!draft.consumptionInstructions.trim()) {
    problems.consumptionInstructions = "Say how a project should use it.";
    bad = true;
  }
  const secrets = keys.filter((k) => k.secret);
  for (const env of environments) {
    const typed = secrets.filter((k) => (draft.values[cellKey(env, k.key)] ?? "").trim()).length;
    if (typed > 0 && typed < secrets.length) {
      problems.environments[env] = "Type every secret here, or none to carry the project's over.";
      bad = true;
    }
  }
  return bad ? problems : null;
}

export function promoteRequest(
  draft: PromoteDraft,
  { keys, environments }: { keys: readonly ConfigKeyDTO[]; environments: readonly string[] },
): PromoteExternalResourceRequest {
  return {
    consumptionInstructions: draft.consumptionInstructions.trim(),
    envValues: keys.flatMap((k) =>
      environments.flatMap((environment) => {
        const value = draft.values[cellKey(environment, k.key)] ?? "";
        return value.trim() ? [{ environment, key: k.key, value }] : [];
      }),
    ),
  };
}
