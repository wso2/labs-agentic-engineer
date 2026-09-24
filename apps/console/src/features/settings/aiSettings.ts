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
 * terms (model, API key, coding agent, Claude subscription) map onto
 * `/config`'s sections. Reading goes `ConfigProjection → AiSettings`; writing
 * goes `AiSettings + AiDraft → ConfigPatch`; a refusal's `details[].field`
 * maps back to a card field. Components never touch the sections.
 *
 * The mapping:
 * - model and coding agent → `agents.{model,runtime}`
 * - Anthropic API key      → `llm`
 * - Claude subscription    → `agents.subscription`
 *
 * Disconnecting the API key is not a draft change: it is destructive, confirmed,
 * and sent on its own (`llm: null`).
 */

import type { components } from "../../generated/aep-api";

type ConfigProjection = components["schemas"]["ConfigProjection"];
type ConfigPatch = components["schemas"]["ConfigPatch"];
type AgentRuntime = components["schemas"]["AgentRuntime"];
type AgentModel = components["schemas"]["AgentModel"];

type LLMProjection = components["schemas"]["LLMProjection"];
type SubscriptionProjection = components["schemas"]["SubscriptionProjection"];

/**
 * Only models the platform holds a cost rate for: a cycle's cost stamp is
 * all-or-nothing, so one unpriced model would blank the whole cycle's cost.
 */
export const MODELS: { value: AgentModel; label: string }[] = [
  { value: "claude-sonnet-5", label: "Claude Sonnet 5" },
  { value: "claude-haiku-4-5", label: "Claude Haiku 4.5" },
];

/**
 * `claude setup-token` values carry this prefix. The server refuses anything
 * else as a subscription; checking here says why before a round trip.
 */
export const SUBSCRIPTION_TOKEN_PREFIX = "sk-ant-oat";

/** What the server holds, in the card's terms. */
export interface AiSettings {
  model: AgentModel;
  apiKey: LLMProjection | null;
  runtime: AgentRuntime;
  subscription: SubscriptionProjection | null;
  updatedAt: string | null;
  updatedBy: string | null;
}

/** What the reader has chosen and not yet saved. */
export interface AiDraft {
  model: AgentModel;
  runtime: AgentRuntime;
  /** A newly pasted Anthropic API key; "" when none. */
  apiKey: string;
  /** The "Bill coding to a Claude subscription" switch. */
  billToSubscription: boolean;
  /** A newly pasted `claude setup-token` value; "" when none. */
  token: string;
}

export function aiSettingsFrom(config: ConfigProjection): AiSettings {
  const { agents, llm } = config;
  return {
    model: agents.model,
    apiKey: llm,
    runtime: agents.runtime,
    subscription: agents.subscription,
    updatedAt: agents.updatedAt ?? null,
    updatedBy: agents.updatedBy ?? null,
  };
}

export function draftFrom(saved: AiSettings): AiDraft {
  return {
    model: saved.model,
    runtime: saved.runtime,
    apiKey: "",
    billToSubscription: saved.subscription !== null,
    token: "",
  };
}

/** A subscription is used only by Claude Code. */
function usesSubscription(draft: AiDraft): boolean {
  return draft.runtime === "claude-code" && draft.billToSubscription;
}

/**
 * Saving this draft deletes the stored token: the reader chose OpenCode, or
 * turned the switch off.
 */
export function removesSubscription(saved: AiSettings, draft: AiDraft): boolean {
  return saved.subscription !== null && !usesSubscription(draft);
}

/**
 * A subscription token is stored beside the org's API key and cannot exist
 * without it, so it can be added once a key is connected or is being saved in
 * the same patch (the server judges the state the patch leaves).
 */
export function canAddSubscription(saved: AiSettings, draft: AiDraft): boolean {
  return saved.apiKey !== null || draft.apiKey.trim() !== "";
}

/** Why the draft cannot be saved, or undefined when it can. */
export function draftProblem(saved: AiSettings, draft: AiDraft): string | undefined {
  if (!usesSubscription(draft)) return undefined;
  if (!canAddSubscription(saved, draft)) {
    return "Add the Anthropic API key before a Claude subscription.";
  }
  const token = draft.token.trim();
  if (token === "") {
    return saved.subscription === null
      ? "Paste a token from claude setup-token, or turn off the Claude subscription."
      : undefined;
  }
  if (!token.startsWith(SUBSCRIPTION_TOKEN_PREFIX)) {
    return `This is not a Claude subscription token. Tokens from claude setup-token start with ${SUBSCRIPTION_TOKEN_PREFIX}.`;
  }
  return undefined;
}

/**
 * The patch that moves the server from `saved` to `draft`, carrying only what
 * changed; null when nothing did. Every section rides one PATCH, which the
 * server validates as a whole before writing any of it.
 */
export function aiSettingsPatch(saved: AiSettings, draft: AiDraft): ConfigPatch | null {
  const patch: ConfigPatch = {};

  const apiKey = draft.apiKey.trim();
  if (apiKey !== "") patch.llm = { kind: "anthropic", apiKey };

  const agents: NonNullable<ConfigPatch["agents"]> = {};
  if (draft.model !== saved.model) agents.model = draft.model;
  if (draft.runtime !== saved.runtime) agents.runtime = draft.runtime;

  // Sent only when it changes, so the stored token never has to come back.
  // Choosing OpenCode deletes it server-side too; saying so keeps the patch
  // honest about what the Save does.
  const token = draft.token.trim();
  if (usesSubscription(draft) && token !== "") {
    agents.subscription = { kind: "claude", token };
  } else if (removesSubscription(saved, draft)) {
    agents.subscription = null;
  }
  if (Object.keys(agents).length > 0) patch.agents = agents;

  return Object.keys(patch).length > 0 ? patch : null;
}

/**
 * Disconnecting the org's key. The server removes a stored subscription with
 * it, since the subscription cannot outlive the key.
 */
export function disconnectKeyPatch(): ConfigPatch {
  return { llm: null };
}

/** The card field a refusal belongs on. */
export type AiField = "apiKey" | "subscription" | "card";

/**
 * Where to show a refused save: the server names the section it refused in
 * `details[].field` (`body.llm`, `body.agents`, …). The card only sends models
 * and runtimes from the contract's enums, so every `agents` refusal it can meet
 * is about the subscription: its token, or the rule that it needs Claude Code
 * and an API key.
 */
export function refusedField(fields: string[]): AiField {
  if (fields.includes("body.llm")) return "apiKey";
  if (fields.includes("body.agents")) return "subscription";
  return "card";
}
