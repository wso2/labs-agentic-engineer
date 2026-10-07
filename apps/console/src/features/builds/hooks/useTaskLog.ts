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

// One task's live log (stream-task-log), copied from the old console's
// features/tasks/hooks/useTaskLog.ts. The wire is the platform's agent SSE,
// so agent-stream's parser reads it; reconnects are safe by contract (the
// server replays and lines are deduplicated by key).

type TaskStreamEvent = components["schemas"]["TaskStreamEvent"];
type TimelineEvent = components["schemas"]["TimelineEvent"];

export type TaskLogPhase = "connecting" | "live" | "reconnecting" | "ended";

export interface TaskLogState {
  /** The log's lines, oldest first. */
  lines: TimelineEvent[];
  phase: TaskLogPhase;
}

const RECONNECT_DELAY_MS = 3_000;

/**
 * A line's identity. Structured events carry a per-execution `seq`; plain
 * runner output arrives with seq 0 on every line, so those fall back to the
 * line's timestamp and text, which a replay repeats exactly.
 */
export function timelineEventKey(e: TimelineEvent): string {
  if (e.seq) return `${e.executionId}:${e.seq}`;
  return `${e.executionId}:0:${e.ts}:${e.summary ?? e.message ?? ""}`;
}

/** Attach to one task's log while it is open; the stream closes when it is. */
export function useTaskLog(projectName: string, issueNumber: number): TaskLogState {
  const [lines, setLines] = useState<TimelineEvent[]>([]);
  const [phase, setPhase] = useState<TaskLogPhase>("connecting");
  const seen = useRef(new Set<string>());

  useEffect(() => {
    seen.current = new Set();
    setLines([]);
    setPhase("connecting");
    const controller = new AbortController();
    let disposed = false;

    const consume = async (): Promise<"done" | "eof"> => {
      const { data, error } = await client.GET("/projects/{projectName}/tasks/{issueNumber}/log", {
        params: { path: { projectName, issueNumber } },
        parseAs: "stream",
        signal: controller.signal,
      });
      if (error || !data) throw new Error("Failed to attach to the task log");
      setPhase("live");
      const frames = parseSseStream(data as ReadableStream<Uint8Array>);
      while (true) {
        const next = await frames.next();
        if (next.done) return next.value;
        const event = next.value as unknown as TaskStreamEvent;
        if (event.type !== "line" || !event.line) continue;
        const line = event.line;
        const key = timelineEventKey(line);
        if (seen.current.has(key)) continue;
        seen.current.add(key);
        setLines((prev) => [...prev, line]);
      }
    };

    const run = async () => {
      while (!disposed) {
        try {
          if ((await consume()) === "done") {
            setPhase("ended");
            return;
          }
        } catch {
          if (disposed) return;
        }
        // An end without `[DONE]`, or a dropped connection: the task is still
        // live, so wait and attach again.
        setPhase("reconnecting");
        await new Promise((r) => setTimeout(r, RECONNECT_DELAY_MS));
      }
    };
    void run();
    return () => {
      disposed = true;
      controller.abort();
    };
  }, [projectName, issueNumber]);

  return { lines, phase };
}
