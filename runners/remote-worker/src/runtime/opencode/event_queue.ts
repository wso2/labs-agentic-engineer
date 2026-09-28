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

// The bus, read EAGERLY into a queue the run loop drains at its own pace.
//
// Two reasons it cannot simply be handed to the loop as the SDK returns it:
//
//   - The SDK's SSE stream is LAZY — the request is made on the first `next()`.
//     The design's rule is "subscribe BEFORE session.create, so the root's
//     `session.created` is the first event the adapter sees", and a lazy stream
//     that the loop only starts reading after `start()` returns would miss every
//     event between the prompt and the loop's first read. So the pump reads from
//     the moment it exists, and `connected` resolves on the server's own
//     `server.connected` — the proof the subscription is live — before the
//     runtime creates the session.
//   - The adapter's clock (`aep.tick`, messages.ts) has to interleave with the
//     bus, and one queue with two producers is the simplest correct merge.

import { TOOL_TICK } from "./messages.js";

export interface EventQueue {
  /** Resolves on the first bus event; rejects if the bus ends or fails first. */
  readonly connected: Promise<void>;
  /** Everything the bus delivered, then the ticks, in arrival order; ends when the bus does. */
  readonly messages: AsyncIterable<unknown>;
  /** Stop the clock. The bus itself is stopped by aborting its request. */
  stop(): void;
}

export interface EventQueueOptions {
  /** How often the adapter's clock ticks; 0 disables it. */
  tickMs: number;
}

/** Start reading `source` now, and merge the adapter's clock into it. */
export function pumpEvents(source: AsyncIterable<unknown>, opts: EventQueueOptions): EventQueue {
  const buffer: unknown[] = [];
  let waiting: { resolve: (r: IteratorResult<unknown>) => void; reject: (err: unknown) => void } | undefined;
  let failure: unknown;
  let ended = false;
  let seenFirst = false;
  let resolveConnected!: () => void;
  let rejectConnected!: (err: unknown) => void;
  const connected = new Promise<void>((resolve, reject) => {
    resolveConnected = resolve;
    rejectConnected = reject;
  });
  // A rejection nobody awaited (the runtime already gave up) must not crash the process.
  connected.catch(() => {});

  const push = (value: unknown): void => {
    if (ended) return;
    if (waiting) {
      const w = waiting;
      waiting = undefined;
      w.resolve({ value, done: false });
    } else {
      buffer.push(value);
    }
  };
  const finish = (err?: unknown): void => {
    if (ended) return;
    ended = true;
    failure = err;
    clearInterval(timer);
    if (!seenFirst) rejectConnected(err ?? new Error("the event stream ended before it connected"));
    if (waiting) {
      const w = waiting;
      waiting = undefined;
      if (err) w.reject(err);
      else w.resolve({ value: undefined, done: true });
    }
  };

  const timer = opts.tickMs > 0 ? setInterval(() => push({ type: TOOL_TICK }), opts.tickMs) : undefined;
  timer?.unref?.();

  void (async () => {
    try {
      for await (const event of source) {
        if (!seenFirst) {
          seenFirst = true;
          resolveConnected();
        }
        push(event);
      }
      finish();
    } catch (err) {
      finish(err);
    }
  })();

  const messages: AsyncIterable<unknown> = {
    [Symbol.asyncIterator]() {
      return {
        next(): Promise<IteratorResult<unknown>> {
          if (buffer.length > 0) return Promise.resolve({ value: buffer.shift(), done: false });
          if (ended) return failure ? Promise.reject(failure) : Promise.resolve({ value: undefined, done: true });
          return new Promise((resolve, reject) => {
            waiting = { resolve, reject };
          });
        },
      };
    },
  };

  return {
    connected,
    messages,
    stop: () => clearInterval(timer),
  };
}
