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

// One agent's stretch of the run's time axis, split into the parts it was
// working and the parts it was blocked.
//
// The split is the whole reason a timeline is worth drawing. A lead that took 55
// minutes reads as the slowest thing in the run until you see that 49 of those
// minutes are one unbroken WAITING stretch while a spawned agent built a web
// app — at which point the question "where did the time go" has an answer, and
// it is not the lead.
//
// Pure interval arithmetic, in its own module because that is what it is: no
// events, no clock, no idea what an agent is.

export type LaneSpanKind = "working" | "waiting";

export interface LaneSpan {
  kind: LaneSpanKind;
  startMs: number;
  endMs: number;
}

/** A closed stretch of the axis. Zero-length and inverted ones are dropped. */
export interface Interval {
  startMs: number;
  endMs: number;
}

/**
 * Overlapping intervals folded into the fewest that cover the same ground.
 *
 * Two agents spawned at once produce two overlapping waits, and drawing both
 * would paint the parent's lane twice over — the second stretch covering the
 * first, so the working gap between them disappears rather than being shown.
 */
export function mergeIntervals(intervals: readonly Interval[]): Interval[] {
  const sorted = intervals
    .filter((i) => i.endMs > i.startMs)
    .sort((a, b) => a.startMs - b.startMs);
  const merged: Interval[] = [];
  for (const next of sorted) {
    const last = merged[merged.length - 1];
    if (last && next.startMs <= last.endMs) {
      last.endMs = Math.max(last.endMs, next.endMs);
      continue;
    }
    merged.push({ ...next });
  }
  return merged;
}

/**
 * One lane's spans: `waits` painted as `waiting`, everything else in
 * [`startMs`, `endMs`] as `working`.
 *
 * Waits are clamped to the lane rather than allowed to overhang it. A child that
 * outlives its parent's own settle is a real shape — a backgrounded agent does
 * exactly that — and a span drawn past the lane's end would put that child's
 * time on the parent's row, in the one view whose whole job is to say who spent
 * what.
 *
 * An empty result means the lane has no extent to draw (nothing was timestamped,
 * or it began and ended in the same instant). A surface renders that as the
 * agent's own state across the full width rather than as an empty row — a blank
 * row on a progress surface is indistinguishable from a run that stopped talking.
 */
export function laneSpans(
  startMs: number,
  endMs: number,
  waits: readonly Interval[] = [],
): LaneSpan[] {
  if (!(endMs > startMs)) return [];
  const blocked = mergeIntervals(
    waits.map((w) => ({
      startMs: Math.max(startMs, w.startMs),
      endMs: Math.min(endMs, w.endMs),
    })),
  );
  const spans: LaneSpan[] = [];
  let cursor = startMs;
  for (const wait of blocked) {
    if (wait.startMs > cursor) spans.push({ kind: "working", startMs: cursor, endMs: wait.startMs });
    spans.push({ kind: "waiting", startMs: wait.startMs, endMs: wait.endMs });
    cursor = wait.endMs;
  }
  if (cursor < endMs) spans.push({ kind: "working", startMs: cursor, endMs });
  return spans;
}
