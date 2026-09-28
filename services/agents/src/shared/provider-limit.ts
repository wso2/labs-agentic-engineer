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
 * Provider limits (429). A 429 means one of two things: wait a little (a rate
 * or concurrency limit) or the plan is spent until it resets, and the status
 * alone cannot tell them apart. One pure rule decides (`providerLimit`), the
 * same rule the runner applies to a coding run's retries:
 *
 * - a WAIT: `retry-after` under 5 minutes while this streak of 429 retries has
 *   run for under 5 minutes. A streak ends when the provider answers anything
 *   but a 429, so two short rate limits far apart are not added up. The AI SDK retries it (honouring `retry-after` up to 60 s,
 *   else backing off from 2 s, `MODEL_MAX_RETRIES` times).
 * - a PROVIDER LIMIT: a `retry-after` past 5 minutes, or a 5-minute streak of
 *   429s, which covers a provider that states no reset. The turn stops at once with
 *   a `provider_limit` frame instead of sleeping on a spent plan.
 *
 * While a wait is being ridden out the turn says so: `onWait` fires on each
 * such 429 and the SSE route turns it into a `provider-wait` frame, so a reader
 * sees "waiting on the model provider" instead of a silent stall.
 *
 * The rule runs inside the fetch each turn's model is built with, because only
 * there is every attempt visible (the SDK's own retries included) with its
 * headers and body. What a spent plan actually returns is learned from
 * production, not tested up front: every 429 writes one `model_provider_429`
 * line to the service log, never to the feed or the chat.
 */

/** How long a stated wait, or a run of 429s, may last before it is a provider limit. */
export const PROVIDER_LIMIT_AFTER_MS = 5 * 60_000;

/** The AI SDK's retries per model call: short waits ride out about two minutes. */
export const MODEL_MAX_RETRIES = 6;

export type ProviderLimitVerdict = "wait" | "provider_limit";

/**
 * The rule. `retryAfterMs` is the provider's stated wait (absent when it stated
 * none); `waitedMs` is how long this streak of 429s has run.
 */
export function providerLimit(retryAfterMs: number | undefined, waitedMs: number): ProviderLimitVerdict {
  if (retryAfterMs !== undefined && retryAfterMs > PROVIDER_LIMIT_AFTER_MS) return "provider_limit";
  return waitedMs >= PROVIDER_LIMIT_AFTER_MS ? "provider_limit" : "wait";
}

/**
 * The turn stopped on the provider's usage limit. Thrown from the model's fetch
 * so the AI SDK does not retry it (it retries only its own retryable errors),
 * and mapped by the SSE route to the `provider_limit` frame.
 */
export class ProviderLimitError extends Error {
  readonly code = "provider_limit";
  constructor(
    readonly host: string,
    /** When the provider said the limit resets, when it said. */
    readonly resetAt?: Date,
  ) {
    super(
      resetAt
        ? `${host}'s usage limit is reached. Try again after ${resetAt.toISOString()}.`
        : `${host}'s usage limit is reached. Try again later.`,
    );
    this.name = "ProviderLimitError";
  }
}

/** The provider's stated wait: `retry-after-ms`, else `retry-after` (seconds or an HTTP date). */
export function retryAfterMs(headers: Headers, now: number): number | undefined {
  const ms = Number.parseFloat(headers.get("retry-after-ms") ?? "");
  if (Number.isFinite(ms) && ms >= 0) return ms;
  const raw = headers.get("retry-after");
  if (!raw) return undefined;
  const seconds = Number.parseFloat(raw);
  if (Number.isFinite(seconds) && /^\s*[0-9.]+\s*$/.test(raw)) return Math.max(0, seconds * 1000);
  const date = Date.parse(raw);
  return Number.isNaN(date) ? undefined : Math.max(0, date - now);
}

/**
 * When the limit resets, when the provider said: its stated wait, else the
 * latest `x-ratelimit-reset*` / `ratelimit-reset` it sent (seconds from now, or
 * an epoch in seconds, or an ISO date — providers disagree).
 */
export function resetAtOf(headers: Headers, now: number): Date | undefined {
  const wait = retryAfterMs(headers, now);
  if (wait !== undefined) return new Date(now + wait);
  let latest: number | undefined;
  headers.forEach((value, name) => {
    if (!/^(x-)?ratelimit-reset/.test(name)) return;
    const at = parseReset(value, now);
    if (at !== undefined && (latest === undefined || at > latest)) latest = at;
  });
  return latest === undefined ? undefined : new Date(latest);
}

function parseReset(value: string, now: number): number | undefined {
  if (/^\s*[0-9.]+\s*$/.test(value)) {
    const n = Number.parseFloat(value);
    // An epoch in seconds is far larger than any reset delay stated in seconds.
    return n > 1e9 ? n * 1000 : now + n * 1000;
  }
  const date = Date.parse(value);
  return Number.isNaN(date) ? undefined : date;
}

/** The response headers a `model_provider_429` line records, names and values. */
function limitHeaders(headers: Headers): Record<string, string> {
  const out: Record<string, string> = {};
  headers.forEach((value, name) => {
    if (/^(retry-after(-ms)?|x-ratelimit-.*|ratelimit.*)$/.test(name)) out[name] = value;
  });
  return out;
}

/** Characters of a 429's body the log line keeps. */
const LOGGED_BODY_CHARS = 300;

/** The one structured line every 429 writes. */
export interface ProviderLimitLogLine {
  msg: "model_provider_429";
  source: "agents";
  org?: string;
  host: string;
  format: string;
  model: string;
  status: number;
  limitHeaders: Record<string, string>;
  /** The first 300 characters of the body, the connection key's literal removed. */
  body: string;
  verdict: ProviderLimitVerdict;
  waitedMs: number;
}

/** Where 429 lines go; the service log (stderr) unless a test captures them. */
export type ProviderLimitLog = (line: ProviderLimitLogLine) => void;

const stderrLog: ProviderLimitLog = (line) => {
  process.stderr.write(`${JSON.stringify(line)}\n`);
};

export interface ProviderLimitWatch {
  /** The connection's key: removed from the logged body. */
  apiKey: string;
  host: string;
  format: string;
  model: string;
  org?: string;
  log?: ProviderLimitLog;
  /** The clock, injected so the 5-minute budget is testable. */
  now?: () => number;
  /**
   * Called on each 429 the rule calls a wait, before the SDK sleeps and
   * retries — the turn is waiting on `host`. Never called for a provider limit.
   */
  onWait?: (host: string) => void;
}

/**
 * `base` with the 429 rule applied to every response. Build one per turn: the
 * 5-minute budget counts from the first 429 of the current streak, and any
 * other response ends the streak. A wait is returned to the SDK untouched (it
 * retries); a provider limit throws `ProviderLimitError`.
 */
export function watchProviderLimits(base: typeof globalThis.fetch, watch: ProviderLimitWatch): typeof globalThis.fetch {
  const now = watch.now ?? Date.now;
  const log = watch.log ?? stderrLog;
  let streakStart: number | undefined;
  return (async (input, init) => {
    const res = await base(input, init);
    if (res.status !== 429) {
      streakStart = undefined;
      return res;
    }
    const at = now();
    streakStart ??= at;
    const waitedMs = at - streakStart;
    const verdict = providerLimit(retryAfterMs(res.headers, at), waitedMs);
    // A clone for the log, so the SDK still reads the body it retries on.
    const text = await res.clone().text().catch(() => "");
    log({
      msg: "model_provider_429",
      source: "agents",
      ...(watch.org ? { org: watch.org } : {}),
      host: watch.host,
      format: watch.format,
      model: watch.model,
      status: res.status,
      limitHeaders: limitHeaders(res.headers),
      // Scrubbed BEFORE the cut, so a key straddling character 300 cannot leak its head.
      body: scrub(text, watch.apiKey).slice(0, LOGGED_BODY_CHARS),
      verdict,
      waitedMs,
    });
    if (verdict === "wait") {
      watch.onWait?.(watch.host);
      return res;
    }
    await res.body?.cancel();
    throw new ProviderLimitError(watch.host, resetAtOf(res.headers, at));
  }) as typeof globalThis.fetch;
}

function scrub(text: string, secret: string): string {
  return secret === "" ? text : text.split(secret).join("<redacted>");
}

/**
 * The provider limit a failed turn ended on, if it ended on one: the
 * `ProviderLimitError` anywhere in the error's chain (the SDK wraps it in a
 * `RetryError` after earlier waits), or 429 retries that ran out before the
 * 5-minute budget did — the turn gave up on the provider's limit either way.
 */
export function providerLimitIn(err: unknown, host: string): ProviderLimitError | undefined {
  const seen = new Set<unknown>();
  const queue: unknown[] = [err];
  let exhausted429 = false;
  while (queue.length > 0) {
    const e = queue.shift();
    if (e === null || typeof e !== "object" || seen.has(e)) continue;
    seen.add(e);
    if (e instanceof ProviderLimitError) return e;
    const o = e as { cause?: unknown; errors?: unknown; lastError?: unknown; reason?: unknown; statusCode?: unknown };
    if (o.reason === "maxRetriesExceeded" && (o.lastError as { statusCode?: unknown } | undefined)?.statusCode === 429) {
      exhausted429 = true;
    }
    queue.push(o.cause, o.lastError, ...(Array.isArray(o.errors) ? o.errors : []));
  }
  return exhausted429 ? new ProviderLimitError(host) : undefined;
}
