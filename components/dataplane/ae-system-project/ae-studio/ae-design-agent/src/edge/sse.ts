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
 * Attach to a turn's stream (07 §1): server-sent events from the TurnDesk's
 * replay buffer, replay first, then the live tail. Each frame is
 * `id: <index>` + `data: <part>` (`[DONE]` is a frame too, so ids run without
 * a gap); `: keep-alive` every `keepAliveMs` while the turn is quiet. The
 * stream ends after `[DONE]`, or without it when the desk drops a reader
 * that fell too far behind (the client resumes with `from`). A client that
 * leaves only detaches its reader: the turn runs on.
 */

import type { Request, Response } from "express";
import { ReplayTruncatedError, type ReplayFrame } from "../turns/replay-buffer.js";
import type { TurnDesk } from "../turns/turn-desk.js";
import { startKeepAlive } from "../shared/keepalive.js";
import { notFound, sendProblem } from "./http.js";

/**
 * Where to resume: `?from=N` wins; else `Last-Event-ID` (the last frame the
 * client saw, so the next one); else 0. Malformed values are ignored.
 */
export function resumeFrom(req: Request): number {
  const from = typeof req.query.from === "string" ? req.query.from : undefined;
  if (from !== undefined && /^\d+$/.test(from)) return Number(from);
  const last = req.header("last-event-id");
  if (last !== undefined && /^\d+$/.test(last)) return Number(last) + 1;
  return 0;
}

/** Stream turn `turnId` from `from`; pre-stream 404 / 409 when it cannot be attached. */
export async function streamTurn(
  req: Request,
  res: Response,
  desk: TurnDesk,
  turnId: string,
  from: number,
  keepAliveMs: number,
): Promise<void> {
  let frames: AsyncIterable<ReplayFrame> | null;
  try {
    frames = desk.attach(turnId, from);
  } catch (err) {
    if (!(err instanceof ReplayTruncatedError)) throw err;
    sendProblem(res, 409, "replay_truncated", err.message);
    return;
  }
  if (!frames) {
    notFound(res, "turn_unknown");
    return;
  }
  res.writeHead(200, {
    "content-type": "text/event-stream",
    "cache-control": "no-cache, no-transform",
    connection: "keep-alive",
    "x-accel-buffering": "no",
  });
  res.flushHeaders();
  const reader = frames[Symbol.asyncIterator]();
  const stopKeepAlive = startKeepAlive((frame) => res.write(frame), keepAliveMs);
  const detach = () => void reader.return?.();
  res.on("close", detach);
  try {
    for (;;) {
      const next = await reader.next();
      if (next.done) break;
      const { id, part } = next.value;
      res.write(`id: ${id}\ndata: ${typeof part === "string" ? part : JSON.stringify(part)}\n\n`);
    }
  } finally {
    stopKeepAlive();
    res.off("close", detach);
    res.end();
  }
}
