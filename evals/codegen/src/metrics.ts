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
 * What a finished coding run says about itself, read off its own files.
 *
 * Two sources, each the authority for one thing:
 *
 *   `progress.ndjson` → `run_settled`: the run's OUTCOME and token usage, as
 *     the runner reports it to the console. The outcome decides hard fail.
 *   `.logs/runtime.log` → the last SDK `result`: `total_cost_usd`, Claude
 *     Code's own estimate, recorded as reported (ADR-0005); null when absent.
 */

export interface Tokens {
  input: number;
  output: number;
  cacheRead: number;
  cacheCreation: number;
}

export interface RunSettled {
  outcome: string;
  /** The event's closed `code`, when it carries one. */
  code?: string;
  /** Why it failed, in the producer's words — for a reason line, never for a decision. */
  error?: string;
  tokens: Tokens;
}

/** The LAST `run_settled` event in a progress feed, or null when the run never settled. */
export function readRunSettled(ndjson: string): RunSettled | null {
  let settled: RunSettled | null = null;
  for (const event of jsonLines(ndjson)) {
    if (event.kind !== "run_settled") continue;
    const usage = (event.usage ?? {}) as Record<string, unknown>;
    settled = {
      outcome: typeof event.outcome === "string" ? event.outcome : "unknown",
      ...(typeof event.code === "string" ? { code: event.code } : {}),
      ...(typeof event.error === "string" ? { error: event.error } : {}),
      tokens: {
        input: num(usage.inputTokens),
        output: num(usage.outputTokens),
        cacheRead: num(usage.cacheReadTokens),
        cacheCreation: num(usage.cacheCreationTokens),
      },
    };
  }
  return settled;
}

/**
 * Whether the coding AGENT ever started: `run_started` is emitted when the
 * runtime's session reports in (its `init`).
 */
export function sawRunStarted(ndjson: string): boolean {
  return jsonLines(ndjson).some((event) => event.kind === "run_started");
}

/** How many events the feed holds. */
export function countEvents(ndjson: string): number {
  return jsonLines(ndjson).length;
}

/**
 * `total_cost_usd` of the LAST `result` message in an SDK transcript. The last,
 * because in a streaming-input session each result carries the running total.
 */
export function lastResultCost(transcript: string): number | null {
  let cost: number | null = null;
  for (const message of jsonLines(transcript)) {
    if (message.type === "result" && typeof message.total_cost_usd === "number") cost = message.total_cost_usd;
  }
  return cost;
}

function jsonLines(text: string): Record<string, unknown>[] {
  const out: Record<string, unknown>[] = [];
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed.startsWith("{")) continue;
    try {
      const value: unknown = JSON.parse(trimmed);
      if (value && typeof value === "object") out.push(value as Record<string, unknown>);
    } catch {
      // A torn last line from a killed run is expected; it carries nothing settled.
    }
  }
  return out;
}

function num(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}
