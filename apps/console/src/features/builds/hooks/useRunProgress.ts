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

import { useEffect, useRef, useState } from "react";
import { parseSseStream } from "@aep/agent-stream";
import { client } from "../../../api/client";
import type { components } from "../../../generated/aep-api";

type RunProgressEvent = components["schemas"]["RunProgressEvent"];
type RunEvent = components["schemas"]["RunEvent"];
type RunCycleView = components["schemas"]["RunCycleView"];

/**
 * One v2 event as this hook holds it: the frame's stamps folded onto the event.
 *
 * `cycleId` and `attempt` live on the SSE FRAME, not on the RunEvent — a runner
 * knows what it is doing but not which cycle of which run it turned out to be,
 * so aep-api stamps them as it relays. Folding them on is what lets everything
 * downstream (the dedup key, the list key, the per-cycle grouping) work on one
 * value instead of carrying a pair around.
 */
export type StampedRunEvent = RunEvent & { cycleId: string; attempt: number };

/**
 * Where the stream is.
 *
 * `idle` is NOT a connection state: it means nobody asked for one (the hook was
 * called with `enabled: false`). It exists because reporting `connecting` for a
 * stream that was never opened made a collapsed, connection-free surface tell the
 * user it was "attaching to the run feed" forever.
 */
export type RunProgressPhase =
  | "idle"
  | "connecting"
  | "live"
  | "reconnecting"
  | "ended";

/** One cycle's section of the feed: the cycle record plus its own events. */
export interface RunProgressCycle {
  cycle: RunCycleView;
  events: StampedRunEvent[];
}

export interface RunProgressState {
  /** Cycles in dispatch order, each holding the lines attributed to it. */
  cycles: RunProgressCycle[];
  /** Terminal run state from the `done` frame — the stream is over. */
  settledState: string | undefined;
  phase: RunProgressPhase;
}

const RECONNECT_DELAY_MS = 3_000;

/**
 * Identity of one event — the replay-dedup key and the list key.
 *
 * All three parts are load-bearing. `seq` is monotonic only WITHIN one attempt
 * and starts again at 0 when a cycle is re-dispatched, so `attempt` is what
 * keeps a re-dispatch's first event from erasing the original's; `cycleId`
 * separates the cycles sharing this one stream. A producer that retries a flush,
 * or a reconnect that replays from the start, sends the same triple again and
 * the second write is a no-op — which is what makes replay idempotent.
 *
 * Unlike v1 there is no fallback arm: `seq` is required of every RunEvent, and 0
 * is a legitimate first event rather than the "unsequenced" marker it was.
 */
export function runEventKey(e: StampedRunEvent): string {
  return `${e.cycleId}:${String(e.attempt)}:${String(e.seq)}`;
}

async function openRunStream(
  projectName: string,
  runId: string,
  signal: AbortSignal,
): Promise<ReadableStream<Uint8Array>> {
  const { data, error } = await client.GET(
    "/projects/{projectName}/runs/{runId}/progress",
    {
      params: { path: { projectName, runId } },
      parseAs: "stream",
      signal,
    },
  );
  if (error || !data) throw new Error("Failed to attach to the run feed");
  return data as ReadableStream<Uint8Array>;
}

/**
 * Attach to ONE run's progress stream and accumulate it grouped by cycle.
 *
 * The wire format is the platform's standard agent SSE (`data:` JSON frames,
 * keep-alive comments, `[DONE]` sentinel), so this reuses agent-stream's parser
 * exactly as useTaskLog does; the frames here are RunProgressEvents carrying v2
 * RunEvents. ONLY a terminal run settles the stream, so `ended` is a fact about
 * the run, not about the connection — a live run's stream simply stays open, and
 * an EOF without `[DONE]` is a dropped connection to reattach (idempotent by
 * contract: cycles upsert by id, events dedup by key).
 *
 * v1 `line` frames are NOT consumed. The contract keeps them for the
 * compatibility window and a single stream can carry both, but a v1 line has no
 * agent ids at all — aep-api lifts the ones it holds into v2 server-side and
 * marks the agents it deduced `role: "inferred"`, so one envelope reaches here
 * and the console never has to render two.
 *
 * Passing no runId keeps the hook inert — no stream is opened. That is how the
 * feed stays closed until the user opens it.
 */
export function useRunProgress(
  projectName: string,
  runId: string | undefined,
  /** Open the stream at all. False keeps a page that nobody is watching
   *  connection-free — the property the old run-level feed toggle gave us. */
  enabled = true,
): RunProgressState {
  const [cycles, setCycles] = useState<RunProgressCycle[]>([]);
  const [settledState, setSettledState] = useState<string>();
  const [phase, setPhase] = useState<RunProgressPhase>("idle");
  const seen = useRef(new Set<string>());

  useEffect(() => {
    // Reset on run change (the hook instance survives a param update).
    seen.current = new Set();
    setCycles([]);
    setSettledState(undefined);
    // Not enabled = nobody is looking. A settled version whose cycles are all
    // collapsed must open no connection and replay no history — and must not
    // claim to be connecting either.
    if (!runId || !enabled) {
      setPhase("idle");
      return;
    }
    setPhase("connecting");

    const controller = new AbortController();
    let disposed = false;

    // Events whose cycle frame has not arrived yet, by cycle id.
    //
    // An `event` frame carries its cycle's ID and nothing else about it — not
    // its kind, not when it started — so an event that outruns its cycle frame
    // cannot open a section of its own without INVENTING a kind, which is the
    // chip the reader sees. Held instead, and flushed the moment the real record
    // lands: nothing is lost and nothing is made up. The server sends the cycle
    // frame first, so this is a reconnect-ordering guard, not the normal path.
    const orphans = new Map<string, StampedRunEvent[]>();

    const upsertCycle = (cycle: RunCycleView) =>
      setCycles((prev) => {
        const held = orphans.get(cycle.id) ?? [];
        orphans.delete(cycle.id);
        const i = prev.findIndex((c) => c.cycle.id === cycle.id);
        if (i === -1) return [...prev, { cycle, events: held }];
        const next = prev.slice();
        // Keep the accumulated events: a re-emitted cycle record is fresher
        // metadata (branch, PR, merge SHA learned from webhooks), not a reset.
        next[i] = { cycle, events: [...(prev[i]?.events ?? []), ...held] };
        return next;
      });

    const appendEvent = (event: StampedRunEvent) =>
      setCycles((prev) => {
        const i = prev.findIndex((c) => c.cycle.id === event.cycleId);
        if (i === -1) {
          orphans.set(event.cycleId, [...(orphans.get(event.cycleId) ?? []), event]);
          return prev;
        }
        const next = prev.slice();
        const at = prev[i];
        if (!at) return prev;
        next[i] = { cycle: at.cycle, events: [...at.events, event] };
        return next;
      });

    const consume = async (): Promise<"done" | "eof"> => {
      const body = await openRunStream(projectName, runId, controller.signal);
      setPhase("live");
      const frames = parseSseStream(body);
      while (true) {
        const next = await frames.next();
        if (next.done) return next.value;
        const event = next.value as unknown as RunProgressEvent;
        switch (event.type) {
          case "cycle":
            if (event.cycle) upsertCycle(event.cycle);
            break;
          case "event": {
            // The frame's stamps are what attribute the event; without a cycle
            // there is no section to put it in, so it is dropped rather than
            // filed under a guess.
            if (!event.event || !event.cycleId) break;
            const stamped: StampedRunEvent = {
              ...event.event,
              cycleId: event.cycleId,
              attempt: event.attempt ?? 0,
            };
            const key = runEventKey(stamped);
            if (seen.current.has(key)) break;
            seen.current.add(key);
            appendEvent(stamped);
            break;
          }
          case "done":
            if (event.state) setSettledState(event.state);
            break;
        }
      }
    };

    const run = async () => {
      while (!disposed) {
        try {
          const end = await consume();
          if (end === "done") {
            setPhase("ended");
            return;
          }
        } catch {
          if (disposed) return;
        }
        setPhase("reconnecting");
        await new Promise((r) => setTimeout(r, RECONNECT_DELAY_MS));
      }
    };
    void run();

    return () => {
      disposed = true;
      controller.abort();
    };
  }, [projectName, runId, enabled]);

  return { cycles, settledState, phase };
}
