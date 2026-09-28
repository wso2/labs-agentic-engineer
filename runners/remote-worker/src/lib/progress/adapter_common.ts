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

// What every runtime's TRANSLATOR shares: the contract's field caps, the
// plan-status vocabulary, and the heartbeat budget.
//
// Both are rules about the FEED, not about any runtime's messages — the
// contract caps a `summary` at 200 whoever composed it, and "at most one
// heartbeat per agent per ten seconds" is the platform's cadence whichever
// runtime is waiting. They live here so every adapter enforces the same
// numbers by construction rather than by copying them, which is the drift a
// shared contract exists to prevent.

import type { RunEvent, RunEventInput } from "./emitter.js";

/** `summary`, `phrase`, `label` and `detail` are all capped at 200 in the contract. */
const MAX_SUMMARY = 200;

/** `report` is capped at 500: a last line, not a transcript. */
export const MAX_REPORT = 500;

/**
 * How often one agent may say "still alive" while it is waiting.
 *
 * Bounded because the point of a heartbeat is that a row's age is honest, not
 * that every tick is on the record: a 55-minute run produces a few hundred of
 * these at this cadence, and one per `tool_progress` message would produce
 * thousands. It is the design's default (§13) and is a per-AGENT budget, so a
 * run with four agents waiting still says so about all four.
 */
const HEARTBEAT_MIN_INTERVAL_MS = 10_000;

/** Whitespace collapsed to single spaces, then bounded with an ellipsis. */
export function cap(s: string, max: number): string {
  const collapsed = s.replace(/\s+/g, " ").trim();
  if (collapsed.length <= max) return collapsed;
  return collapsed.slice(0, max - 1) + "…";
}

export function trimSummary(s: string): string {
  return cap(s, MAX_SUMMARY);
}

/**
 * An agent's closing words: the runtime's own LAST line, bounded.
 *
 * The last line rather than the whole summary because that is where a report
 * lives — an agent told to "reply with exactly one line" produces exactly that
 * (`REPORT: alpha done, 1 file`), while one that narrates first puts its
 * conclusion at the end. Bounded because this is a row, not a transcript, and
 * because everything on this feed reaches a user-visible build log.
 */
export function reportFrom(summary: string): string {
  const lines = summary.split("\n").map((l) => l.trim()).filter((l) => l !== "");
  return cap(lines[lines.length - 1] ?? "", MAX_REPORT);
}

/**
 * A task-list entry's status in the contract's vocabulary, or undefined for one
 * it has no word for. OpenCode's `cancelled` is a deletion.
 */
export function planStatus(v: unknown): NonNullable<RunEvent["itemStatus"]> | undefined {
  if (v === "pending" || v === "in_progress" || v === "completed" || v === "deleted") return v;
  if (v === "cancelled") return "deleted";
  return undefined;
}

/**
 * Pass a heartbeat through, or drop it: at most one per agent per interval.
 *
 * Returns the event list the translator should emit — `[event]` or `[]` — so a
 * dropped beat is an empty translation, never a different event. That is why
 * the run loop routes liveness by message class rather than by what came back:
 * a dropped beat produces nothing, and nothing must not read as activity.
 */
export type HeartbeatLimiter = (agentId: string, event: RunEventInput) => RunEventInput[];

export interface HeartbeatLimiterOptions {
  /** The translator's clock, so the budget is testable without sleeping. */
  now: () => number;
  /** Per-agent budget; the default is the design's 10s. */
  intervalMs?: number;
}

/** One limiter per RUN — its clocks are that run's agents. */
export function createHeartbeatLimiter(opts: HeartbeatLimiterOptions): HeartbeatLimiter {
  const intervalMs = opts.intervalMs ?? HEARTBEAT_MIN_INTERVAL_MS;
  const lastHeartbeatAt = new Map<string, number>();
  return (agentId, event) => {
    const at = opts.now();
    const last = lastHeartbeatAt.get(agentId);
    if (last !== undefined && at - last < intervalMs) return [];
    lastHeartbeatAt.set(agentId, at);
    return [event];
  };
}
