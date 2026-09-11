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

// @vitest-environment jsdom

import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// One SSE body per attach. The hook reconnects on EOF-without-[DONE], so a
// test that wants a single attach must end its body with the sentinel.
let bodies: string[] = [];
const GET = vi.fn(() => {
  const text = bodies.shift() ?? "";
  return Promise.resolve({
    data: new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode(text));
        controller.close();
      },
    }),
    error: undefined,
  });
});
// The factory is hoisted above `GET`'s initialiser, so it reads it lazily.
vi.mock("../../../api/client", () => ({
  client: {
    GET: (...args: unknown[]) => GET(...(args as [])),
  },
}));

import { runEventKey, useRunProgress } from "./useRunProgress";

function frames(...events: unknown[]): string {
  return events.map((e) => `data: ${JSON.stringify(e)}\n\n`).join("");
}

const cycle = (id: string, kind: string) => ({
  type: "cycle",
  cycle: { id, kind, attempts: 1, createdAt: "2026-07-10T09:00:00Z" },
});

// A v2 `event` frame: the RunEvent, plus the cycle and attempt aep-api stamps
// on it as it relays. A runner knows what it is doing but not which cycle of
// which run it turned out to be, which is why those live on the frame.
const evt = (cycleId: string, seq: number, summary: string, attempt = 1) => ({
  type: "event",
  cycleId,
  attempt,
  event: {
    v: 2,
    seq,
    ts: "2026-07-10T09:01:00Z",
    agentId: "lead",
    kind: "tool_use",
    tool: "Bash",
    summary,
  },
});

const DONE = "data: [DONE]\n\n";

afterEach(() => {
  bodies = [];
  GET.mockClear();
});

describe("useRunProgress", () => {
  it("opens nothing without a run id", () => {
    const { result } = renderHook(() => useRunProgress("acme", undefined));
    expect(GET).not.toHaveBeenCalled();
    expect(result.current.phase).toBe("idle");
  });

  // A stream nobody asked for is IDLE, not connecting. Reporting `connecting`
  // for it had a connection-free surface (every build session collapsed) telling
  // the user it was "attaching to the run feed" — forever, since no attach was
  // ever going to happen.
  it("is idle, not connecting, while it is disabled", () => {
    const { result } = renderHook(() => useRunProgress("acme", "run-1", false));
    expect(GET).not.toHaveBeenCalled();
    expect(result.current.phase).toBe("idle");
  });

  it("groups events under the cycle that produced them", async () => {
    bodies = [
      frames(
        cycle("c1", "coding"),
        cycle("c2", "fix"),
        evt("c1", 1, "first"),
        evt("c2", 2, "second"),
        evt("c1", 3, "third"),
        { type: "done", state: "succeeded" },
      ) + DONE,
    ];
    const { result } = renderHook(() => useRunProgress("acme", "run-1"));

    await waitFor(() => expect(result.current.phase).toBe("ended"));
    expect(result.current.cycles.map((c) => c.cycle.id)).toEqual(["c1", "c2"]);
    expect(result.current.cycles[0]?.events.map((e) => e.summary)).toEqual([
      "first",
      "third",
    ]);
    expect(result.current.cycles[1]?.events.map((e) => e.summary)).toEqual([
      "second",
    ]);
    expect(result.current.settledState).toBe("succeeded");
  });

  // v1 `line` frames still exist on the contract for the compatibility window,
  // and aep-api lifts the cycles it holds into v2 before serving them. A console
  // that also rendered them would be rendering the same run through two
  // formatters — the drift this package exists to prevent.
  it("ignores a v1 line frame rather than rendering the feed twice", async () => {
    bodies = [
      frames(
        cycle("c1", "coding"),
        evt("c1", 1, "first"),
        {
          type: "line",
          line: { cycleId: "c1", cycleKind: "coding", cycleIndex: 1, kind: "log", emitter: "main", seq: 2, summary: "legacy" },
        },
        { type: "done", state: "succeeded" },
      ) + DONE,
    ];
    const { result } = renderHook(() => useRunProgress("acme", "run-1"));

    await waitFor(() => expect(result.current.phase).toBe("ended"));
    expect(result.current.cycles[0]?.events.map((e) => e.summary)).toEqual(["first"]);
  });

  it("keeps a cycle's events when its record is re-emitted with fresher facts", async () => {
    // Branch, PR and merge SHA are learned from webhooks after the fact, so a
    // second cycle frame is an UPSERT — it must not reset the accumulated log.
    bodies = [
      frames(
        cycle("c1", "coding"),
        evt("c1", 1, "first"),
        {
          type: "cycle",
          cycle: {
            id: "c1",
            kind: "coding",
            attempts: 1,
            branch: "aep/m1-c1",
            prNumber: 3,
            createdAt: "2026-07-10T09:00:00Z",
          },
        },
        { type: "done", state: "succeeded" },
      ) + DONE,
    ];
    const { result } = renderHook(() => useRunProgress("acme", "run-1"));

    await waitFor(() => expect(result.current.phase).toBe("ended"));
    expect(result.current.cycles[0]?.cycle.branch).toBe("aep/m1-c1");
    expect(result.current.cycles[0]?.events).toHaveLength(1);
  });

  // An `event` frame carries its cycle's ID and nothing else about it — not its
  // kind, which is the chip the reader sees. So an event that outran its cycle
  // frame is HELD rather than opening a section under an invented kind: nothing
  // is lost and nothing is made up.
  it("holds an event that outran its cycle frame until the frame arrives", async () => {
    bodies = [
      frames(
        evt("c9", 1, "orphan"),
        cycle("c9", "validation"),
        evt("c9", 2, "after"),
        { type: "done", state: "succeeded" },
      ) + DONE,
    ];
    const { result } = renderHook(() => useRunProgress("acme", "run-1"));

    await waitFor(() => expect(result.current.phase).toBe("ended"));
    expect(result.current.cycles[0]?.cycle.id).toBe("c9");
    expect(result.current.cycles[0]?.cycle.kind).toBe("validation");
    expect(result.current.cycles[0]?.events.map((e) => e.summary)).toEqual([
      "orphan",
      "after",
    ]);
  });

  it("dedups a replayed event across a reconnect", async () => {
    // First attach ends WITHOUT [DONE] — a dropped connection, not a settled
    // run — so the hook reattaches and the server replays from the start.
    bodies = [
      frames(cycle("c1", "coding"), evt("c1", 1, "first")),
      frames(
        cycle("c1", "coding"),
        evt("c1", 1, "first"),
        evt("c1", 2, "second"),
        { type: "done", state: "succeeded" },
      ) + DONE,
    ];
    const { result } = renderHook(() => useRunProgress("acme", "run-1"));

    await waitFor(() => expect(result.current.phase).toBe("ended"), {
      timeout: 6000,
    });
    expect(GET).toHaveBeenCalledTimes(2);
    expect(result.current.cycles).toHaveLength(1);
    expect(result.current.cycles[0]?.events.map((e) => e.summary)).toEqual([
      "first",
      "second",
    ]);
  }, 10000);

  // `seq` restarts at the beginning when a cycle is re-dispatched, so without
  // the attempt in the key a re-dispatch's first event would dedup away as a
  // replay of the original's — the run would appear to stop talking.
  it("keeps a re-dispatch's events, whose seq starts over", async () => {
    bodies = [
      frames(
        cycle("c1", "coding"),
        evt("c1", 1, "attempt one", 1),
        evt("c1", 1, "attempt two", 2),
        { type: "done", state: "failed" },
      ) + DONE,
    ];
    const { result } = renderHook(() => useRunProgress("acme", "run-1"));

    await waitFor(() => expect(result.current.phase).toBe("ended"));
    expect(result.current.cycles[0]?.events.map((e) => e.summary)).toEqual([
      "attempt one",
      "attempt two",
    ]);
  });
});

describe("runEventKey", () => {
  const base = {
    v: 2 as const,
    ts: "2026-07-10T09:01:00Z",
    agentId: "lead",
    kind: "tool_use" as const,
    cycleId: "c1",
    attempt: 1,
    seq: 7,
  };

  it("keys an event on its cycle, attempt and sequence", () => {
    expect(runEventKey(base)).toBe("c1:1:7");
  });

  it("keeps seq 0 distinct rather than treating it as unsequenced", () => {
    // v1 read a 0 as "the BFF wrapped raw stdout" and fell back to a timestamp.
    // v2 requires seq of every event, so 0 is simply an attempt's FIRST event —
    // and a fallback here would key two attempts' first events the same.
    expect(runEventKey({ ...base, seq: 0 })).toBe("c1:1:0");
    expect(runEventKey({ ...base, seq: 0, attempt: 2 })).toBe("c1:2:0");
  });
});
