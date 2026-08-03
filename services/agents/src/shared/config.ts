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

import { resolveDatabaseUrl } from "./database-url.js";
import { loadDotenv, intEnv, boolEnv } from "./env.js";

// Load the nearest .env BEFORE reading process.env, so AGENT_MODEL /
// AGENT_MAX_STEPS set only in .env are honored (not silently dropped).
loadDotenv();

const DAY_MS = 24 * 60 * 60 * 1000;

/** Anthropic `effort` levels (adaptive thinking depth + output spend). */
const REASONING_EFFORTS = ["low", "medium", "high", "xhigh", "max"] as const;
export type ReasoningEffort = (typeof REASONING_EFFORTS)[number];

function effortEnv(value: string | undefined, fallback: ReasoningEffort): ReasoningEffort {
  return (REASONING_EFFORTS as readonly string[]).includes(value ?? "")
    ? (value as ReasoningEffort)
    : fallback;
}

export const config = {
  model: process.env.AGENT_MODEL || "claude-sonnet-5",
  // Reasoning effort for every turn. The provider default is `high`; the spec
  // flows favor `medium` — faster time-to-first-artifact for the same quality
  // on well-specified generation prompts. effortEnv guards a bad override.
  reasoningEffort: effortEnv(process.env.AGENT_REASONING_EFFORT, "medium"),
  // A fresh "write an app" generation needs 10–15 file calls; steps batch
  // parallel tool calls, so the loop budget is higher than the call count.
  // intEnv guards a non-numeric value (which would NaN out the step cap).
  maxSteps: intEnv(process.env.AGENT_MAX_STEPS, 20),
  // Per-step output-token ceiling. The Anthropic provider defaults to a low cap
  // (~4096) when unset, which truncates a real spec/design mid-tool-call: the
  // model hits the limit before the `addFile` input JSON closes, so NO `tool-call`
  // frame is emitted, the client fold applies nothing, and the draft ends empty.
  // Sonnet-class models support 64k output tokens; default high so a single-doc
  // generation completes in one step. intEnv guards a non-numeric override.
  maxOutputTokens: intEnv(process.env.AGENT_MAX_OUTPUT_TOKENS, 64_000),
  logLevel: process.env.LOG_LEVEL || "info",
  // AI SDK DevTools (AGENT_DEVTOOLS, default off): wrap the model in
  // `devToolsMiddleware` so every LLM call is captured to
  // `.devtools/generations.json` and streamed to the DevTools trace UI
  // (`npx @ai-sdk/devtools`, port AI_SDK_DEVTOOLS_PORT / 4983). Heavier than the
  // timing lines (per-call file write + viewer notify), so it is opt-in.
  devtools: boolEnv(process.env.AGENT_DEVTOOLS, false),
  // SSE keep-alive cadence: a `: keep-alive` comment this often while a turn
  // streams, so long generations don't die behind an idle-timeout ingress.
  keepAliveMs: intEnv(process.env.AGENT_KEEPALIVE_MS, 15_000),

  // Shared workspaces mount root — read-only per-SHA snapshots written by
  // aep-api (services/aep-api/design/shared-workspace-volume.md). Compose/k8s
  // mount the volume :ro; nothing in this service ever writes it.
  workspaceMountRoot: process.env.WORKSPACE_MOUNT_ROOT || "/workspaces",

  // Collab server ws URL for room-scoped turns (#86 phase 4). Unset → any
  // turn carrying a `collab` block is rejected pre-stream (the deployment
  // doesn't do live rooms). Compose: ws://collab-server:3400.
  collabWsUrl: process.env.AGENT_COLLAB_WS_URL || undefined,

  // M2M gate (§12.3.2): a Bearer JWT with this audience, verified against a JWKS
  // (RS256) OR a shared secret (HS256) — whichever is configured. The gate is
  // ALWAYS ON; the composition root refuses to start if neither is set. No org
  // claim in the TOKEN is required (nothing calls back to the BFF); `X-Org-Id`
  // is load-bearing (the §12 fence asserts the conversation's org segment
  // equals it).
  auth: {
    audience: process.env.AGENT_JWT_AUDIENCE || "agents-service",
    issuer: process.env.AGENT_JWT_ISSUER || undefined,
    jwksUrl: process.env.AGENT_JWT_JWKS_URL || undefined,
    secret: process.env.AGENT_JWT_SECRET || undefined,
  },

  // ConversationStore selection: Postgres when a URL resolves (DATABASE_URL or
  // discrete DB_*), else the in-memory store (tests/evals). Threads embed
  // inlined file snapshots, so stored rows have real size — a TTL sweep on
  // updated_at reclaims them.
  database: {
    url: resolveDatabaseUrl(),
    conversationsTtlMs: intEnv(process.env.CONVERSATIONS_TTL_MS, 7 * DAY_MS),
    conversationsSweepMs: intEnv(process.env.CONVERSATIONS_SWEEP_MS, 60 * 60 * 1000),
  },
} as const;
