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
 * One turn's replay buffer: the port of aep-api's `spec/turn_stream.go`
 * broker, one instance per turn. The turn appends every stream part; watchers
 * attach from a frame id (`?from=N`), get the stored frames from there and
 * then tail live frames until the end. A frame's id is its index in the
 * buffer, so a reader resumes at `last id + 1` and sees every frame once.
 *
 * `end` appends the turn's terminal part (`turn-completed` / `turn-failed`)
 * and `[DONE]`; nothing is appended after it. The buffer holds no timer:
 * whoever owns it drops it `REPLAY_RETENTION_MS` after the end.
 *
 * Caps as in turn_stream.go: past `REPLAY_MAX_PARTS` parts or
 * `REPLAY_MAX_BYTES` bytes the buffer stops storing (a runaway stream).
 * Attached tailers keep tailing, a new attach while the turn runs is refused
 * (`ReplayTruncatedError`), and an attach after the end replays only the
 * terminal frames, because the stored frames have a gap before them.
 */

import { SSE_DONE, type StreamPart } from "@aep/agent-stream";

/** How long a finished turn's buffer stays attachable. */
export const REPLAY_RETENTION_MS = 120_000;
/** Most parts one turn's buffer stores. */
export const REPLAY_MAX_PARTS = 16_384;
/** Most bytes (JSON-encoded parts) one turn's buffer stores. */
export const REPLAY_MAX_BYTES = 16 * 1024 * 1024;
/** Live frames a tailer may have unread before it is dropped (it re-attaches). */
const TAILER_BACKLOG = 1024;

export type TurnFailReason = "agent-error" | "stream-died" | "shutdown" | "internal";

/** The part a turn's stream ends with, before `[DONE]`. */
export type TurnEndPart =
  | { type: "turn-completed" }
  | {
      type: "turn-failed";
      reason: TurnFailReason;
      message?: string;
      code?: string;
      host?: string;
      resetAt?: string;
    };

export type ReplayPart = StreamPart | TurnEndPart | typeof SSE_DONE;

export interface ReplayFrame {
  id: number;
  part: ReplayPart;
}

/** The running turn overflowed its caps: a replay would have a gap. Attach again after it ends. */
export class ReplayTruncatedError extends Error {
  constructor() {
    super("the turn's replay buffer overflowed; attach again after the turn ends");
    this.name = "ReplayTruncatedError";
  }
}

export class ReplayBuffer {
  private readonly maxParts: number;
  private readonly maxBytes: number;
  private readonly frames: ReplayFrame[] = [];
  private bytes = 0;
  private nextId = 0;
  private truncated = false;
  private done = false;
  private readonly tailers = new Set<Tailer>();

  constructor(caps: { maxParts?: number; maxBytes?: number } = {}) {
    this.maxParts = caps.maxParts ?? REPLAY_MAX_PARTS;
    this.maxBytes = caps.maxBytes ?? REPLAY_MAX_BYTES;
  }

  /** True once `end` has run. */
  get ended(): boolean {
    return this.done;
  }

  /** Buffer one part and hand it to live tailers. Dropped after `end`. */
  append(part: StreamPart): void {
    if (this.done) return;
    const frame = this.nextFrame(part);
    if (!this.truncated) {
      const size = Buffer.byteLength(JSON.stringify(part));
      if (this.frames.length >= this.maxParts || this.bytes + size > this.maxBytes) {
        this.truncated = true;
      } else {
        this.frames.push(frame);
        this.bytes += size;
      }
    }
    this.fanOut(frame);
  }

  /** Append the terminal part and `[DONE]`, then end every tailer. A second call is a no-op. */
  end(terminal: TurnEndPart): void {
    if (this.done) return;
    this.done = true;
    // The terminal frames are always stored: a late attacher needs the outcome.
    for (const part of [terminal, SSE_DONE] as const) {
      const frame = this.nextFrame(part);
      this.frames.push(frame);
      this.fanOut(frame);
    }
    this.endTailers();
  }

  /** End every live tailer (the owner dropped the buffer). */
  dispose(): void {
    this.endTailers();
  }

  /**
   * The stored frames from id `from` (a negative `from` reads as 0), then,
   * while the turn runs, its live frames until the end. Registered at call
   * time, so nothing appended after this call is missed.
   */
  attach(from: number): AsyncIterable<ReplayFrame> {
    if (this.truncated && !this.done) throw new ReplayTruncatedError();
    const start = Math.max(0, from);
    const stored = this.truncated ? this.frames.filter((f) => typeof f.part === "string" || isTurnEnd(f.part)) : this.frames;
    const tailer = new Tailer(
      start,
      stored.filter((f) => f.id >= start),
      () => this.tailers.delete(tailer),
    );
    if (this.done) tailer.finish();
    else this.tailers.add(tailer);
    return tailer;
  }

  private nextFrame(part: ReplayPart): ReplayFrame {
    return { id: this.nextId++, part };
  }

  private fanOut(frame: ReplayFrame): void {
    for (const t of this.tailers) t.push(frame);
  }

  private endTailers(): void {
    for (const t of this.tailers) t.finish();
    this.tailers.clear();
  }
}

function isTurnEnd(part: ReplayPart): boolean {
  return typeof part !== "string" && (part.type === "turn-completed" || part.type === "turn-failed");
}

/** One attached reader: the replayed frames, then live frames from `from` on. */
class Tailer implements AsyncIterableIterator<ReplayFrame> {
  private readonly queue: ReplayFrame[];
  /** Replayed frames still at the head of the queue (they do not count as backlog). */
  private replayLeft: number;
  private ended = false;
  private waiter: ((r: IteratorResult<ReplayFrame>) => void) | undefined;

  constructor(
    private readonly from: number,
    replay: ReplayFrame[],
    private readonly detach: () => void,
  ) {
    this.queue = [...replay];
    this.replayLeft = replay.length;
  }

  push(frame: ReplayFrame): void {
    if (this.ended || frame.id < this.from) return;
    if (this.waiter) {
      const w = this.waiter;
      this.waiter = undefined;
      w({ value: frame, done: false });
      return;
    }
    if (this.queue.length - this.replayLeft >= TAILER_BACKLOG) {
      // Fell behind: end without the terminal, the reader re-attaches.
      this.detach();
      this.finish();
      return;
    }
    this.queue.push(frame);
  }

  finish(): void {
    this.ended = true;
    if (this.waiter && this.queue.length === 0) {
      const w = this.waiter;
      this.waiter = undefined;
      w({ value: undefined, done: true });
    }
  }

  next(): Promise<IteratorResult<ReplayFrame>> {
    const frame = this.queue.shift();
    if (frame) {
      if (this.replayLeft > 0) this.replayLeft--;
      return Promise.resolve({ value: frame, done: false });
    }
    if (this.ended) return Promise.resolve({ value: undefined, done: true });
    return new Promise((resolve) => (this.waiter = resolve));
  }

  return(): Promise<IteratorResult<ReplayFrame>> {
    this.detach();
    this.queue.length = 0;
    this.finish();
    return Promise.resolve({ value: undefined, done: true });
  }

  [Symbol.asyncIterator](): AsyncIterableIterator<ReplayFrame> {
    return this;
  }
}
