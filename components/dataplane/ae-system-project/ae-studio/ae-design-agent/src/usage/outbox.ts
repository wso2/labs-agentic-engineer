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

// The usage outbox (07 §7): each finished turn's record goes to ae-studio-tools
// over the tools socket (`POST /turn-usage`). If the socket is down the record
// waits here and is retried every OUTBOX_RETRY_MS, one record at a time and in
// order. The outbox is bounded: past OUTBOX_CAP the oldest record is dropped. A
// record in flight is never the one dropped: it may already have landed. A
// record the socket refuses for good (a 4xx verdict) is dropped too, or it
// would block every later one. Both drops log `usage.dropped`, value-free.
// At shutdown `drain(timeoutMs)` pushes what is left within a bound
// (`pod/shutdown.ts`: ≤ 8 s, inside ae-studio-tools' 10 s socketDrainWindow).

import { ToolsSocketError, type ToolsSocket, type TurnRecord } from "../tools-socket/client.js";

/** The most records the outbox holds; the oldest goes first. */
export const OUTBOX_CAP = 200;
/** The wait between a failed hand-over and the next attempt. */
export const OUTBOX_RETRY_MS = 2_000;

/** One structured, value-free log line. */
export interface OutboxLogLine {
  msg: "usage.dropped";
  source: "ae-design-agent";
  reason: "overflow" | "rejected";
  turnId: string;
  status?: number;
}

export interface Outbox {
  /** Queue one finished turn's record. */
  push(r: TurnRecord): void;
  /** Start delivering: now, and again whenever a record is pushed. */
  run(): void;
  /** Deliver what is queued within `timeoutMs`; true when the outbox emptied. */
  drain(timeoutMs: number): Promise<boolean>;
}

export interface UsageOutboxOptions {
  /** Where log lines go; stdout unless a test captures them. */
  log?: (line: OutboxLogLine) => void;
}

const stdoutLog = (line: OutboxLogLine): void => {
  process.stdout.write(`${JSON.stringify(line)}\n`);
};

export class UsageOutbox implements Outbox {
  private readonly queue: TurnRecord[] = [];
  private readonly log: (line: OutboxLogLine) => void;
  private running = false;
  /** The record being handed over; it stays queued (at the head) until accepted. */
  private inFlight: TurnRecord | undefined;
  private retryTimer: NodeJS.Timeout | undefined;
  private emptied: Array<() => void> = [];

  constructor(
    private readonly tools: Pick<ToolsSocket, "postUsage">,
    opts: UsageOutboxOptions = {},
  ) {
    this.log = opts.log ?? stdoutLog;
  }

  /** Records waiting, the one in flight included. */
  get pending(): number {
    return this.queue.length;
  }

  push(r: TurnRecord): void {
    this.queue.push(r);
    while (this.queue.length > OUTBOX_CAP) {
      // The oldest record not in flight: the head may already have landed.
      const [dropped] = this.queue.splice(this.inFlight ? 1 : 0, 1);
      this.log({ msg: "usage.dropped", source: "ae-design-agent", reason: "overflow", turnId: dropped!.turnId });
    }
    this.kick();
  }

  run(): void {
    this.running = true;
    this.kick();
  }

  drain(timeoutMs: number): Promise<boolean> {
    this.running = true;
    // A record waiting out its retry goes now: the drain is the last chance.
    if (this.retryTimer) {
      clearTimeout(this.retryTimer);
      this.retryTimer = undefined;
    }
    this.kick();
    if (this.queue.length === 0) return Promise.resolve(true);
    return new Promise<boolean>((resolve) => {
      const onEmpty = () => {
        clearTimeout(deadline);
        resolve(true);
      };
      const deadline = setTimeout(() => {
        this.emptied = this.emptied.filter((f) => f !== onEmpty);
        resolve(false);
      }, timeoutMs);
      deadline.unref();
      this.emptied.push(onEmpty);
    });
  }

  /** Start a delivery pass unless one is running or a retry is waiting. */
  private kick(): void {
    if (!this.running || this.inFlight || this.retryTimer) return;
    void this.deliver();
  }

  private async deliver(): Promise<void> {
    try {
      while (this.queue.length > 0) {
        const head = this.queue[0]!;
        this.inFlight = head;
        try {
          await this.tools.postUsage(head);
        } catch (err) {
          if (err instanceof ToolsSocketError && err.permanent) {
            this.queue.shift();
            this.log({
              msg: "usage.dropped",
              source: "ae-design-agent",
              reason: "rejected",
              turnId: head.turnId,
              status: err.status,
            });
            continue;
          }
          this.retryTimer = setTimeout(() => {
            this.retryTimer = undefined;
            this.kick();
          }, OUTBOX_RETRY_MS);
          this.retryTimer.unref();
          return;
        }
        this.queue.shift();
      }
      const waiters = this.emptied;
      this.emptied = [];
      for (const resolve of waiters) resolve();
    } finally {
      this.inFlight = undefined;
    }
  }
}
