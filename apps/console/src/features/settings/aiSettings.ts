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
 * The AI agents card's adapter: the ONE place that knows how the card's
 * terms (model connection, coding agent, Claude subscription) map onto
 * `/config`'s sections. Reading goes `ConfigProjection → AiSettings`; writing
 * goes `AiSettings + AiDraft → ConfigPatch`; a refusal's `details[].field` and
 * `code` map back to a card field. Components never touch the sections.
 *
 * The mapping:
 * - model connection (format, URL, key, model) → `llm`, patched field by field
 * - coding agent                               → `agents.runtime`
 * - Claude subscription                        → `agents.subscription`
 *
 * The card holds no host rules: what a connection supports (`capabilities`,
 * `priced`) comes from the server, from a test result before a save and from
 * the projection after. Formats, their defaults and the runtimes that take
 * each come from `llmFormats`.
 *
 * Disconnecting is not a draft change: it is destructive, confirmed, and sent
 * on its own (`llm: null`).
 */

import type { components } from "../../generated/aep-api";

type ConfigProjection = components["schemas"]["ConfigProjection"];
type ConfigPatch = components["schemas"]["ConfigPatch"];
type AgentRuntime = components["schemas"]["AgentRuntime"];
type LLMPatch = components["schemas"]["LLMPatch"];
type LLMProjection = components["schemas"]["LLMProjection"];
type SubscriptionProjection = components["schemas"]["SubscriptionProjection"];

export type LLMFormatOption = components["schemas"]["LLMFormatOption"];
export type LLMCheck = components["schemas"]["LLMCheck"];
export type LLMCapabilities = components["schemas"]["LLMCapabilities"];
export type LLMFormat = LLMFormatOption["kind"];

/**
 * The card's words for each format. Labels are copy, so they live here; the
 * defaults and runtimes are the server's (`llmFormats`). `modelNote` says
 * whose ID the format's default model is, since model IDs are per provider.
 */
export const FORMAT_LABELS: Record<LLMFormat, { label: string; modelNote?: string }> = {
  anthropic: { label: "Anthropic Messages" },
  "openai-compatible": { label: "OpenAI-compatible", modelNote: "Ollama's model ID" },
};

/**
 * `claude setup-token` values carry this prefix. The server refuses anything
 * else as a subscription; checking here says why before a round trip.
 */
export const SUBSCRIPTION_TOKEN_PREFIX = "sk-ant-oat";

/**
 * The only subscription status coding dispatch accepts; the server refuses a
 * run on any other, so the card does not say who it bills.
 */
export const SUBSCRIPTION_ACTIVE = "active";

/** What the server holds, in the card's terms. */
export interface AiSettings {
  /** The org's model connection, key masked; null when it has none. */
  connection: LLMProjection | null;
  /** The formats a connection may speak, with defaults and runtimes. */
  formats: LLMFormatOption[];
  runtime: AgentRuntime;
  /**
   * The runtimes this installation can run, the only ones a save may choose.
   * `runtime` can be missing from it: an org that chose a runtime before the
   * installation lost its runner image.
   */
  availableRuntimes: AgentRuntime[];
  subscription: SubscriptionProjection | null;
}

/** What the reader has chosen and not yet saved. */
export interface AiDraft {
  kind: LLMFormat;
  baseURL: string;
  model: string;
  /** A newly pasted API key; "" when none. */
  apiKey: string;
  runtime: AgentRuntime;
  /** A newly pasted `claude setup-token` value; "" when none. */
  token: string;
  /** The reader chose Remove on the stored subscription token. */
  removeToken: boolean;
}

/** What a connection supports, as the card draws it. */
export interface ConnectionView {
  capabilities: LLMCapabilities;
  priced: boolean;
  model: string;
  host: string;
}

export function aiSettingsFrom(config: ConfigProjection): AiSettings {
  const { agents, llm } = config;
  return {
    connection: llm,
    formats: config.llmFormats,
    runtime: agents.runtime,
    availableRuntimes: agents.availableRuntimes,
    subscription: agents.subscription,
  };
}

/**
 * Who last changed the card, and when: the later of the connection's and the
 * agents section's stamps. Either may be missing (no connection, or nobody
 * ever chose a runtime), and a migrated connection has no author.
 */
export function lastChange(config: ConfigProjection): { at: string | null; by: string | null } {
  const stamps = [
    { at: config.llm?.updatedAt ?? null, by: config.llm?.updatedBy ?? null },
    { at: config.agents.updatedAt, by: config.agents.updatedBy },
  ].filter((s) => s.at !== null || s.by !== null);
  if (stamps.length === 0) return { at: null, by: null };
  return stamps.reduce((a, b) => (time(b.at) > time(a.at) ? b : a));
}

function time(at: string | null): number {
  const t = at ? Date.parse(at) : NaN;
  return Number.isNaN(t) ? -Infinity : t;
}

/** The format a first connect starts on: Anthropic's, when offered. */
function firstFormat(formats: LLMFormatOption[]): LLMFormatOption | undefined {
  return formats.find((f) => f.kind === "anthropic") ?? formats[0];
}

export function formatOption(
  formats: LLMFormatOption[],
  kind: LLMFormat,
): LLMFormatOption | undefined {
  return formats.find((f) => f.kind === kind);
}

export function draftFrom(saved: AiSettings): AiDraft {
  const c = saved.connection;
  const first = firstFormat(saved.formats);
  return {
    kind: c?.kind ?? first?.kind ?? "anthropic",
    baseURL: c?.baseURL ?? first?.defaultBaseURL ?? "",
    model: c?.model ?? first?.defaultModel ?? "",
    apiKey: "",
    runtime: saved.runtime,
    token: "",
    removeToken: false,
  };
}

/** The URL's host, for display and for "did the host move"; "" when it is not a URL. */
export function hostOf(url: string): string {
  try {
    return new URL(url.trim()).host.toLowerCase();
  } catch {
    return "";
  }
}

/**
 * Whether the draft needs a typed key: a first connect, or any change to the
 * format or the base URL. The stored key is never reused for a different
 * endpoint, and the server refuses such an edit without one. A model change
 * alone keeps the key.
 */
export function keyRequired(saved: AiSettings, draft: AiDraft): boolean {
  const c = saved.connection;
  if (c === null) return true;
  return draft.kind !== c.kind || draft.baseURL.trim() !== c.baseURL;
}

/**
 * The draft after choosing format `next`. The URL and model are swapped only
 * while they still hold the old format's defaults (or are empty), so nothing
 * the reader typed is lost; the runtime moves to one the new format takes.
 */
export function switchFormat(
  draft: AiDraft,
  formats: LLMFormatOption[],
  next: LLMFormat,
): AiDraft {
  if (draft.kind === next) return draft;
  const from = formatOption(formats, draft.kind);
  const to = formatOption(formats, next);
  if (!to) return draft;
  const url = draft.baseURL.trim();
  const model = draft.model.trim();
  const urlIsDefault = url === "" || url === (from?.defaultBaseURL ?? "");
  const modelIsDefault = model === "" || model === from?.defaultModel;
  return {
    ...draft,
    kind: next,
    baseURL: urlIsDefault ? (to.defaultBaseURL ?? "") : draft.baseURL,
    model: modelIsDefault ? to.defaultModel : draft.model,
    runtime: to.runtimes.includes(draft.runtime) ? draft.runtime : (to.runtimes[0] ?? draft.runtime),
  };
}

/** Whether the draft's format can run `runtime` on this installation. */
export function formatRuns(saved: AiSettings, draft: AiDraft, runtime: AgentRuntime): boolean {
  return formatOption(saved.formats, draft.kind)?.runtimes.includes(runtime) ?? false;
}

/** The labels of the formats that can run `runtime`, for a disabled tile's reason. */
export function formatsRunning(formats: LLMFormatOption[], runtime: AgentRuntime): string[] {
  return formats.filter((f) => f.runtimes.includes(runtime)).map((f) => FORMAT_LABELS[f.kind].label);
}

/**
 * The saved runtime was moved by a format change: the draft's format cannot
 * run it, so `switchFormat` chose another.
 */
export function runtimeMovedByFormat(saved: AiSettings, draft: AiDraft): boolean {
  return draft.runtime !== saved.runtime && !formatRuns(saved, draft, saved.runtime);
}

/** Whether this installation can run `runtime`, and so whether it can be chosen. */
export function runtimeAvailable(saved: AiSettings, runtime: AgentRuntime): boolean {
  return saved.availableRuntimes.includes(runtime);
}

/** The draft's format and host are the saved connection's. */
function sameEndpoint(saved: AiSettings, draft: AiDraft): boolean {
  const c = saved.connection;
  return c !== null && c.kind === draft.kind && hostOf(c.baseURL) === hostOf(draft.baseURL);
}

/**
 * What the draft's connection supports: the latest test (or save) result, else
 * the saved connection while the draft still names it; null when the draft
 * names a connection nobody has probed yet.
 */
export function connectionView(
  saved: AiSettings,
  draft: AiDraft,
  check: LLMCheck | null,
): ConnectionView | null {
  if (check) {
    return { capabilities: check.capabilities, priced: check.priced, model: check.model, host: hostOf(check.baseURL) };
  }
  const c = saved.connection;
  if (c && sameEndpoint(saved, draft) && c.model === draft.model.trim()) {
    return { capabilities: c.capabilities, priced: c.priced, model: c.model, host: hostOf(c.baseURL) };
  }
  return null;
}

/**
 * Whether the draft's connection can carry a Claude subscription. It depends
 * on the format and host only, so a model change on the saved endpoint keeps
 * the saved answer.
 */
export function subscriptionOffered(
  saved: AiSettings,
  draft: AiDraft,
  check: LLMCheck | null,
): boolean {
  if (check) return check.capabilities.claudeSubscription;
  return sameEndpoint(saved, draft) && (saved.connection?.capabilities.claudeSubscription ?? false);
}

/** One line of the info box; `strong` is a part of `text` drawn in bold. */
export interface InfoLine {
  text: string;
  strong?: string;
}

/**
 * The info box under the connection: where prompts go, whether usage has a
 * dollar figure, how agents search, and what chat attachments do. Read from
 * the server's capabilities, never from the host name.
 */
export function infoLines(
  capabilities: LLMCapabilities,
  priced: boolean,
  model: string,
  host: string,
): InfoLine[] {
  const where = host || "the endpoint you enter";
  return [
    { text: `Prompts and code from every agent go to ${where}.`, strong: where },
    {
      text: priced
        ? `Usage is priced in USD: ${model} has a rate.`
        : `Usage shows tokens, not dollars: the platform has no rate for ${model}. Your provider bills you directly.`,
    },
    { text: searchLine(capabilities.webSearch, where) },
    { text: attachLine(capabilities, model) },
  ];
}

/**
 * The info box before the draft's connection has been probed: only where
 * prompts go is known; the rest is the server's to say.
 */
export function untestedInfoLines(host: string): InfoLine[] {
  const where = host || "the endpoint you enter";
  return [
    { text: `Prompts and code from every agent go to ${where}.`, strong: where },
    { text: "Test connection shows what it supports: pricing, web search and chat attachments." },
  ];
}

function searchLine(webSearch: LLMCapabilities["webSearch"], host: string): string {
  switch (webSearch) {
    case "anthropic-server-tool":
      return "Agents can search the web through Anthropic's search tool.";
    case "ollama-api":
      return "Agents search the web through Ollama's search API, with this key.";
    case "none":
      return `Agents work without web search on ${host}.`;
  }
}

function attachLine(c: LLMCapabilities, model: string): string {
  const pdf = c.nativePdf ? "PDFs are read as documents." : "PDFs are sent as extracted text.";
  switch (c.imageInput) {
    case "yes":
      return c.nativePdf
        ? "Chat reads attached images and PDFs."
        : "Chat reads attached images; PDFs are sent as extracted text.";
    case "no":
      return `Chat does not accept images: ${model} does not read them. ${pdf}`;
    case "unknown":
      return `Chat sends attached images; if ${model} does not read them, the provider's error says so. ${pdf}`;
  }
}

/** What a test (or a probing save) found, as the status beside Test connection. */
export function checkStatus(check: LLMCheck): { severity: "success" | "warning"; text: string } {
  const host = hostOf(check.baseURL);
  if (check.warning === "provider_limit") {
    return {
      severity: "warning",
      text: `The key works, but ${host}'s usage limit is reached. You can save; each run or chat turn is blocked until it resets.`,
    };
  }
  switch (check.modelListed) {
    case "yes":
      return { severity: "success", text: `Connected to ${host} · ${check.model} is available` };
    case "no":
      return {
        severity: "warning",
        text: `Connected to ${host}, but ${check.model} is not in its model list. You can still save; runs fail if it is not served.`,
      };
    case "unknown":
      return {
        severity: "warning",
        text: `Connected to ${host}. It does not list models, so ${check.model} is unchecked.`,
      };
  }
}

/** A subscription is used only by Claude Code, on a connection that takes one. */
function usesSubscription(draft: AiDraft, offered: boolean): boolean {
  return draft.runtime === "claude-code" && offered && !draft.removeToken;
}

/**
 * Saving this draft deletes the stored token: the reader removed it, chose
 * OpenCode, or moved the connection to one that cannot carry it.
 */
export function removesSubscription(saved: AiSettings, draft: AiDraft, offered: boolean): boolean {
  return saved.subscription !== null && !usesSubscription(draft, offered);
}

/**
 * Whether the draft's connection section differs from what is saved, so the
 * save carries `llm`. With no saved connection that is any typed key or any
 * field moved off the first format's defaults: a runtime-only change on an
 * unconnected org carries no `llm`.
 */
function connectionChanged(saved: AiSettings, draft: AiDraft): boolean {
  const c = saved.connection ?? defaultsOf(saved);
  return (
    draft.apiKey.trim() !== "" ||
    draft.kind !== c.kind ||
    draft.baseURL.trim() !== c.baseURL ||
    draft.model.trim() !== c.model
  );
}

function defaultsOf(saved: AiSettings): { kind: LLMFormat; baseURL: string; model: string } {
  const d = draftFrom({ ...saved, connection: null });
  return { kind: d.kind, baseURL: d.baseURL, model: d.model };
}

/** Why the draft cannot be saved, and the field that has to change. */
export interface DraftProblem {
  field: Extract<AiField, "apiKey" | "baseURL" | "model" | "connection" | "subscription">;
  message: string;
}

/** Why the draft cannot be saved, or undefined when it can. */
export function draftProblem(saved: AiSettings, draft: AiDraft, offered: boolean): DraftProblem | undefined {
  if (connectionChanged(saved, draft)) {
    if (draft.baseURL.trim() === "") return { field: "baseURL", message: "Enter the base URL." };
    if (draft.model.trim() === "") return { field: "model", message: "Enter the model." };
    if (keyRequired(saved, draft) && draft.apiKey.trim() === "") {
      const host = hostOf(draft.baseURL);
      return { field: "apiKey", message: host ? `Paste the API key for ${host}.` : "Paste the API key." };
    }
    const runtimes = formatOption(saved.formats, draft.kind)?.runtimes ?? [];
    if (runtimes.length === 0) {
      return {
        field: "connection",
        message: `No coding agent on this installation runs the ${FORMAT_LABELS[draft.kind].label} format.`,
      };
    }
  }
  if (!usesSubscription(draft, offered)) return undefined;
  const token = draft.token.trim();
  if (token === "" || token.startsWith(SUBSCRIPTION_TOKEN_PREFIX)) return undefined;
  return {
    field: "subscription",
    message: `This is not a Claude subscription token. Tokens from claude setup-token start with ${SUBSCRIPTION_TOKEN_PREFIX}.`,
  };
}

/**
 * The connection as the draft names it, for `POST /config/llm/test`. The key
 * rides only when typed; the server refuses a test without one, so callers ask
 * for it first.
 */
export function testBody(draft: AiDraft): LLMPatch {
  const apiKey = draft.apiKey.trim();
  return {
    kind: draft.kind,
    baseURL: draft.baseURL.trim(),
    model: draft.model.trim(),
    ...(apiKey !== "" ? { apiKey } : {}),
  };
}

/**
 * The patch that moves the server from `saved` to `draft`, carrying only what
 * changed; null when nothing did. Every section rides one PATCH, which the
 * server validates as a whole before writing any of it.
 */
export function aiSettingsPatch(saved: AiSettings, draft: AiDraft, offered: boolean): ConfigPatch | null {
  const patch: ConfigPatch = {};

  if (connectionChanged(saved, draft)) {
    const c = saved.connection;
    const llm: LLMPatch = {};
    const baseURL = draft.baseURL.trim();
    const model = draft.model.trim();
    const apiKey = draft.apiKey.trim();
    // A first connect states the whole connection; afterwards only what moved.
    if (!c || draft.kind !== c.kind) llm.kind = draft.kind;
    if (!c || baseURL !== c.baseURL) llm.baseURL = baseURL;
    if (!c || model !== c.model) llm.model = model;
    if (apiKey !== "") llm.apiKey = apiKey;
    patch.llm = llm;
  }

  const agents: NonNullable<ConfigPatch["agents"]> = {};
  if (draft.runtime !== saved.runtime) agents.runtime = draft.runtime;
  // Sent only when it changes, so the stored token never has to come back.
  // Leaving Claude Code or the Anthropic API deletes it server-side too;
  // saying so keeps the patch honest about what the Save does.
  const token = draft.token.trim();
  if (usesSubscription(draft, offered) && token !== "") {
    agents.subscription = { kind: "claude", token };
  } else if (removesSubscription(saved, draft, offered)) {
    agents.subscription = null;
  }
  if (Object.keys(agents).length > 0) patch.agents = agents;

  return Object.keys(patch).length > 0 ? patch : null;
}

/**
 * Disconnecting the org's connection. The server removes a stored
 * subscription with it, since the subscription cannot outlive the connection.
 */
export function disconnectPatch(): ConfigPatch {
  return { llm: null };
}

/** The card field a refusal belongs on. */
export type AiField =
  | "apiKey"
  | "baseURL"
  | "model"
  | "connection"
  | "subscription"
  | "runtime"
  | "card";

const LLM_FIELD_BY_CODE: Record<string, AiField> = {
  llm_base_url_invalid: "baseURL",
  llm_host_refused: "baseURL",
  llm_key_required: "apiKey",
  llm_key_too_short: "apiKey",
  llm_key_rejected: "apiKey",
  anthropic_key_invalid: "apiKey",
  anthropic_oauth_token_coding_only: "apiKey",
  secret_store_write_failed: "apiKey",
};

const AGENTS_FIELD_BY_CODE: Record<string, AiField> = {
  agents_runtime_unavailable: "runtime",
  agents_runtime_requires_anthropic_format: "runtime",
};

/**
 * Where to show a refused save or test: the server names the section it
 * refused in `details[].field` (`body.llm`, `body.agents`) and the reason in
 * `code`. A connection refusal that is not about one field (unreachable, an
 * unexpected answer, the test rate limit) shows beside Test connection; an
 * `agents` refusal that is not about the runtime is about the subscription.
 */
export function refusedField(fields: string[], code?: string): AiField {
  if (fields.includes("body.llm") || code?.startsWith("llm_")) {
    return (code !== undefined ? LLM_FIELD_BY_CODE[code] : undefined) ?? "connection";
  }
  if (fields.includes("body.agents")) {
    return (code !== undefined ? AGENTS_FIELD_BY_CODE[code] : undefined) ?? "subscription";
  }
  return "card";
}
