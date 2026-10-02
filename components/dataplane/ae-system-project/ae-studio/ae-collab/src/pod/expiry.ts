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
 * Token expiry on a Room (07 §11). A connection lives only as long as the
 * token it authenticated with: at `exp` it is closed, unless `onTokenSync`
 * re-verified a fresher token first and re-armed the deadline. Time comes from
 * a `Clock`, so the tests drive the deadline instead of sleeping.
 */

import type { Connection } from "@hocuspocus/server";

export interface Clock {
  /** Milliseconds since the epoch. */
  now(): number;
  /**
   * Runs `fn` once at `atMs`, or on the next turn when that has passed (never
   * synchronously); returns the cancel.
   */
  schedule(atMs: number, fn: () => void): () => void;
}

/** setTimeout caps a delay at 2^31-1 ms (~24.8 days); a later deadline is reached in steps. */
const MAX_DELAY_MS = 2 ** 31 - 1;

export const systemClock: Clock = {
  now: () => Date.now(),
  schedule(atMs, fn) {
    let timer: NodeJS.Timeout;
    const wait = () => {
      const delay = Math.max(0, atMs - Date.now());
      timer = setTimeout(delay > MAX_DELAY_MS ? wait : fn, Math.min(delay, MAX_DELAY_MS));
      // A pending deadline never holds the process open.
      timer.unref();
    };
    wait();
    return () => clearTimeout(timer);
  },
};

/**
 * The close a connection gets when its token ran out. Hocuspocus carries only
 * the reason to the client; the code is for our side.
 */
export const TOKEN_EXPIRED = { code: 4401, reason: "token-expired" } as const;

export interface ExpiryGuard {
  /**
   * Closes `connection` at `expSec` (a JWT `exp`, in seconds), replacing any
   * deadline armed before. A non-finite `exp` (dev mode) arms nothing.
   */
  arm(connection: Connection, expSec: number): void;
}

/** One deadline per connection; `onExpired` runs after the connection was closed. */
export function createExpiryGuard(clock: Clock, onExpired: (connection: Connection) => void): ExpiryGuard {
  const cancels = new Map<Connection, () => void>();
  return {
    arm(connection, expSec) {
      const previous = cancels.get(connection);
      if (previous) {
        previous();
      } else {
        connection.onClose(() => {
          cancels.get(connection)?.();
          cancels.delete(connection);
        });
      }
      if (!Number.isFinite(expSec)) {
        cancels.set(connection, () => {});
        return;
      }
      cancels.set(
        connection,
        clock.schedule(expSec * 1000, () => {
          cancels.delete(connection);
          connection.close(TOKEN_EXPIRED);
          onExpired(connection);
        }),
      );
    },
  };
}
