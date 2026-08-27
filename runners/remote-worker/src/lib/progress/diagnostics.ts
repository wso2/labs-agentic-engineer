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

// What a run reads about its OWN health, off the SDK messages the feed
// translator drops.
//
// `from-sdk.ts` answers "what did the agent do". This answers "why is nothing
// happening", which the watchdog could previously only guess at: it could say
// the model turn was the slow half, never why.
//
// The why was already on the wire. The SDK emits a `system`/`api_retry` message
// for every retryable API failure, and `from-sdk.ts` discards it with every
// other unrecognised system subtype — so a run stuck behind an overload storm
// looked exactly like a run thinking hard. Measured against a dead endpoint: 8
// retries in 69s on exponential backoff (0.2s, 0.6s, 1.2s, 2.3s, 4.2s, 9.7s,
// 16.5s, 33.6s), and not one line about any of them.
//
// This is on for EVERY run, cluster included, because it costs nothing a
// healthy run pays: no retries, no messages, no lines. The `error` field is a
// closed enum (`rate_limit`, `overloaded`, `server_error`,
// `authentication_failed`, …), so unlike the SDK's stderr or its debug log
// there is no free text here to leak a prompt or a credential into a build log
// the console forwards.
//
// Note what is deliberately NOT here: the CLI's stderr. It was the obvious
// place to look for retry detail and it does not carry any — probed against the
// same dead endpoint, stderr produced one unrelated startup warning while all 8
// retries went past on the message channel. Capturing it is a developer-only
// sink (see `openDebugSinks`), not the diagnosis.

/** One retryable API failure, as the SDK reports it. */
export interface ApiRetryInfo {
  attempt: number;
  maxRetries: number;
  retryDelayMs: number;
  /** null for connection errors (timeouts, refused) that never got an HTTP response. */
  errorStatus: number | null;
  /** SDKAssistantMessageError — a closed enum, never free text. */
  error: string;
}

function num(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

function str(v: unknown): string {
  return typeof v === "string" && v !== "" ? v : "unknown";
}

/**
 * The retry behind a message, or undefined for every other message.
 *
 * Shape-checked rather than trusted: the runner sees whatever SDK version the
 * image happens to ship, and a renamed field must degrade to "no retry
 * detail" instead of printing `retry NaN/undefined`.
 */
export function readApiRetry(message: unknown): ApiRetryInfo | undefined {
  if (!message || typeof message !== "object") return undefined;
  const m = message as Record<string, unknown>;
  if (m.type !== "system" || m.subtype !== "api_retry") return undefined;
  const status = m.error_status;
  return {
    attempt: num(m.attempt),
    maxRetries: num(m.max_retries),
    retryDelayMs: num(m.retry_delay_ms),
    errorStatus: typeof status === "number" ? status : null,
    error: typeof m.error === "string" && m.error !== "" ? m.error : "unknown",
  };
}

/**
 * The feed line for one retry.
 *
 * Every retry gets a line, not just the late ones: the count is bounded by
 * `max_retries`, and a single retry only ever happens when something IS wrong,
 * so there is no healthy run for this to add noise to. A threshold would have
 * to be tuned against an error class we do not control.
 */
export function apiRetryLine(info: ApiRetryInfo): string {
  // "no response" is the honest rendering of a null status: a refused
  // connection or a timeout never got one, and printing "HTTP 0" would invent
  // a status the API never returned.
  const where = info.errorStatus === null ? "no response" : `HTTP ${info.errorStatus}`;
  // Backoff delays are sub-minute by construction, so plain seconds reads
  // better here than the run-length format the watchdog uses.
  const next = `${Math.round(info.retryDelayMs / 1000)}s`;
  return `[api] retry ${info.attempt}/${info.maxRetries} after ${info.error} (${where}) — next attempt in ${next}`;
}

/** A system message that explains a stall or a death, rendered for the feed. */
export interface StallSignal {
  level: "info" | "warn" | "error";
  summary: string;
}

// Human prose the SDK passes through from elsewhere (a refusal explanation, a
// deciding component's reason). Bounded because none of it is ours and none of
// it is a contract — display only, never parsed.
const MAX_PROSE = 200;

function prose(v: unknown): string {
  if (typeof v !== "string" || v === "") return "";
  const collapsed = v.replace(/\s+/g, " ").trim();
  return collapsed.length <= MAX_PROSE ? collapsed : collapsed.slice(0, MAX_PROSE - 1) + "…";
}

function tokens(n: number): string {
  return n >= 1000 ? `${Math.round(n / 1000)}k` : String(n);
}

/**
 * The line for a system message that explains a silence or an ending, or
 * undefined for every other message.
 *
 * `api_retry` above answers "why is the model not answering". These answer the
 * questions left over when there are no retries: the turn is not stuck at all
 * but compacting; the model refused and the turn is over; a tool call was denied
 * and the agent is working around a wall it cannot see; the worker is going
 * away. Every one of them was dropped with the other unrecognised system
 * subtypes, and each is a stall or a death that the feed reported as silence.
 *
 * `permission_denied` only exists on the stream from SDK 0.3.223 — before it, a
 * DISALLOWED_TOOLS denial reached the feed as a tool call with a puzzling
 * result and nothing else.
 *
 * Everything printed here is a closed enum, a number, an id, or one bounded
 * prose field. Same reasoning as `api_retry`: on for every run because a healthy
 * run emits none of it, and nothing free-form enough to carry a prompt or a
 * credential into a console build log.
 */
export function readStallSignal(message: unknown): StallSignal | undefined {
  if (!message || typeof message !== "object") return undefined;
  const m = message as Record<string, unknown>;
  if (m.type !== "system") return undefined;

  switch (m.subtype) {
    case "compact_boundary": {
      // The one benign entry, and the reason it is here: an auto-compaction is
      // minutes of total silence with no tool in flight and no retry — the exact
      // shape of the stall this file exists to explain.
      const meta = m.compact_metadata && typeof m.compact_metadata === "object"
        ? (m.compact_metadata as Record<string, unknown>)
        : {};
      const trigger = meta.trigger === "manual" ? "manual" : "auto";
      const pre = num(meta.pre_tokens);
      const post = num(meta.post_tokens);
      const took = num(meta.duration_ms);
      const size = pre ? ` ${tokens(pre)}${post ? ` → ${tokens(post)}` : ""} tokens` : "";
      return {
        level: "info",
        summary: `[compact] ${trigger} compaction${size}${took ? ` in ${Math.round(took / 1000)}s` : ""}`,
      };
    }
    case "model_refusal_fallback": {
      const category = prose(m.api_refusal_category);
      return {
        level: "warn",
        summary:
          `[model] ${str(m.original_model)} refused${category ? ` (${category})` : ""}` +
          ` — retried on ${str(m.fallback_model)}`,
      };
    }
    case "model_refusal_no_fallback": {
      const category = prose(m.api_refusal_category);
      const why = prose(m.api_refusal_explanation);
      return {
        level: "error",
        summary:
          `[model] ${str(m.original_model)} refused${category ? ` (${category})` : ""} and no fallback ran` +
          `${why ? `: ${why}` : ""}`,
      };
    }
    case "permission_denied": {
      const why = prose(m.decision_reason) || prose(m.message);
      const by = prose(m.decision_reason_type);
      return {
        level: "warn",
        summary: `[permission] ${str(m.tool_name)} denied${by ? ` by ${by}` : ""}${why ? `: ${why}` : ""}`,
      };
    }
    case "worker_shutting_down":
      // A snake_case reason set by the host CLI, never user input.
      return { level: "error", summary: `[worker] shutting down: ${str(m.reason)}` };
    default:
      return undefined;
  }
}

/**
 * Whether this message is a streaming token frame.
 *
 * Only present when `includePartialMessages` is on, which is a developer-only
 * option: these arrive per token and belong to neither the feed nor
 * `claude.log`. Their one job is to let the watchdog tell a long generation
 * apart from a wedged one, which is the residual fault `api_retry` does not
 * explain — no retries and no tokens is a different problem from no retries
 * and 4,000 tokens.
 */
export function isStreamFrame(message: unknown): boolean {
  return !!message && typeof message === "object" && (message as Record<string, unknown>).type === "stream_event";
}
