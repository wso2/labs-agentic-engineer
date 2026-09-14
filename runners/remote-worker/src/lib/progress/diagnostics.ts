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
// The claude adapter answers "what did the agent do". This answers "why is nothing
// happening", which the watchdog could previously only guess at: it could say
// the model turn was the slow half, never why.
//
// The why was already on the wire. The SDK emits a `system`/`api_retry` message
// for every retryable API failure, and the translator discards it with every
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

import type { RunEventInput } from "./emitter.js";

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

/**
 * A message that explains a stall or a death, rendered for the feed.
 *
 * `code` is the closed condition a consumer branches on and `detail` is what a
 * reader reads — the two halves of a v2 `notice`. Keeping them together here
 * means the wording and the code cannot drift apart, which they would if the
 * run loop picked a code per call site.
 */
export interface StallSignal {
  level: NonNullable<RunEventInput["level"]>;
  code: NonNullable<RunEventInput["code"]>;
  detail: string;
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

  // The one entry that is not a system subtype: the SDK reports a subscription
  // window's utilisation as its own top-level message type. It belongs here
  // because it answers the same question — a run that is about to be throttled,
  // or is being throttled, looks from outside exactly like a run thinking hard.
  if (m.type === "rate_limit_event") return readRateLimit(m);
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
        code: "compaction",
        detail: `[compact] ${trigger} compaction${size}${took ? ` in ${Math.round(took / 1000)}s` : ""}`,
      };
    }
    case "model_refusal_fallback": {
      const category = prose(m.api_refusal_category);
      return {
        level: "warn",
        code: "refusal",
        detail:
          `[model] ${str(m.original_model)} refused${category ? ` (${category})` : ""}` +
          ` — retried on ${str(m.fallback_model)}`,
      };
    }
    case "model_refusal_no_fallback": {
      const category = prose(m.api_refusal_category);
      const why = prose(m.api_refusal_explanation);
      return {
        level: "error",
        code: "refusal",
        detail:
          `[model] ${str(m.original_model)} refused${category ? ` (${category})` : ""} and no fallback ran` +
          `${why ? `: ${why}` : ""}`,
      };
    }
    case "permission_denied": {
      const why = prose(m.decision_reason) || prose(m.message);
      const by = prose(m.decision_reason_type);
      return {
        level: "warn",
        code: "permission_denied",
        detail: `[permission] ${str(m.tool_name)} denied${by ? ` by ${by}` : ""}${why ? `: ${why}` : ""}`,
      };
    }
    case "worker_shutting_down":
      // A snake_case reason set by the host CLI, never user input.
      return { level: "error", code: "terminated", detail: `[worker] shutting down: ${str(m.reason)}` };
    default:
      return undefined;
  }
}

/**
 * A subscription window's state, as the SDK reports it whenever it changes.
 *
 * Reported at `info` while the window is merely being consumed and louder as it
 * bites, because the three states are three different facts: "this run is
 * spending quota" is background, "the next call may be refused" is a warning
 * someone can act on, and "the provider is refusing" is the whole explanation
 * for a run that is about to go nowhere. Every field printed is a closed enum
 * or a number, same rule as the rest of this module — nothing here can carry a
 * prompt or a credential into a build log the console forwards.
 */
function readRateLimit(m: Record<string, unknown>): StallSignal | undefined {
  const info = m.rate_limit_info;
  if (!info || typeof info !== "object") return undefined;
  const i = info as Record<string, unknown>;
  const status = i.status === "rejected" ? "rejected" : i.status === "allowed_warning" ? "near the limit" : "allowed";
  const level = i.status === "rejected" ? "error" : i.status === "allowed_warning" ? "warn" : "info";
  const window = typeof i.rateLimitType === "string" ? i.rateLimitType : "";
  const used = typeof i.utilization === "number" ? ` at ${Math.round(i.utilization * 100)}%` : "";
  return {
    level,
    code: "rate_limit",
    detail: `[rate-limit] ${status}${window ? ` on the ${window} window` : ""}${used}`,
  };
}

/**
 * A per-run reader that drops a stall signal it has just said.
 *
 * `readStallSignal` answers about ONE message and cannot know it is the
 * seventeenth of its kind. That is what a live run got (2026-09-08): 17
 * `rate_limit` warnings, gaps as short as 8 seconds, and exactly THREE distinct
 * sentences between them — 82%, 83%, 84%. The runtime re-states a window's
 * utilisation on every change it notices, most of which round to the same whole
 * percent, and a warning that repeats every minute is one a reader learns to
 * skip past — including the minute it finally says `rejected`.
 *
 * **The unit of materiality is the rendered sentence, not the raw number.**
 * A band (10% steps, say) was the alternative and is worse in one specific way:
 * the sentence prints a whole percent, so banding would suppress a line whose
 * text visibly differs from the last one, leaving a feed that reports 80% and
 * then 90% while claiming to report every change. Deduping on the sentence
 * means the rule is exactly "never say the same thing twice", which is both
 * what the reader complained about and something a reader can verify from the
 * feed alone. A window that genuinely oscillates across a whole percent does
 * produce a line each way, and it should: only the LAST line said is remembered,
 * so this suppresses repetition, never a change.
 *
 * Only the rate limit is filtered. Every other signal here is a discrete
 * occurrence — a compaction happened, a tool call was denied, the worker is
 * going away — and a second one is a second fact, not a restatement.
 *
 * Per-run, like the adapter's registry: two runs sharing one of these would
 * swallow the second run's opening line.
 */
export function createStallSignalReader(): (message: unknown) => StallSignal | undefined {
  let lastRateLimit = "";
  return (message) => {
    const signal = readStallSignal(message);
    if (!signal || signal.code !== "rate_limit") return signal;
    const said = `${signal.level} ${signal.detail}`;
    if (said === lastRateLimit) return undefined;
    lastRateLimit = said;
    return signal;
  };
}

/**
 * Whether a message means "the model is working" rather than "the agent did
 * something".
 *
 * Both shapes exist only to say a turn is still alive: `thinking_tokens` counts
 * reasoning tokens as they accumulate, and a `stream_event` is one token frame
 * (present only under the developer options). Neither is progress, and the run
 * loop keeps both out of `watchdog.observe` for the same reason it keeps
 * retries out — a diagnostic that resets the idle clock hides the stall it
 * exists to report. What they DO produce is a rate-limited `heartbeat`, which
 * is the v2 answer to "is this silence a stall": bounded, attributed, and never
 * counted as work.
 */
export function isModelWaitFrame(message: unknown): boolean {
  if (!message || typeof message !== "object") return false;
  const m = message as Record<string, unknown>;
  return m.type === "stream_event" || (m.type === "system" && m.subtype === "thinking_tokens");
}

/**
 * Whether a message is the runtime saying "this tool call is still running".
 *
 * The other half of the same rule, and the half that is easy to get wrong: a
 * `tool_progress` produces a heartbeat, and once the rate limiter drops one it
 * produces NOTHING — at which point a loop that fell through to
 * `watchdog.observe` would reset the idle clock with an empty event list and
 * the watchdog would go silent for as long as the tool kept ticking. That is
 * precisely the stall it exists to report, so this is routed round `observe`
 * exactly like a retry.
 *
 * Kept apart from `isModelWaitFrame` because only that one is a token frame:
 * `observeStream` records "the model is producing", and a tool that is merely
 * slow says nothing about the model at all.
 */
export function isToolProgressFrame(message: unknown): boolean {
  return !!message && typeof message === "object" && (message as Record<string, unknown>).type === "tool_progress";
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
