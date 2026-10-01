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
 * SSE keep-alive (§12.3.3): while a turn streams, emit a comment frame every
 * `intervalMs` so an idle-timeout ingress doesn't kill a long generation. A
 * comment line (`:` prefix) carries no `data:`, so consumers — including this
 * package's own `streamTurn` — ignore it.
 *
 * Extracted from the SSE route so the cadence is unit-testable with mock timers
 * (no HTTP, no wall-clock wait). The returned `stop` is idempotent; the timer is
 * `unref`'d so it never keeps the process alive.
 */

/** SSE comment frame. Two trailing newlines terminate the frame like a data event. */
export const KEEP_ALIVE_FRAME = ": keep-alive\n\n";

/**
 * Start writing `KEEP_ALIVE_FRAME` via `write` every `intervalMs`. Returns a
 * `stop` that clears the timer (safe to call more than once).
 */
export function startKeepAlive(write: (frame: string) => void, intervalMs: number): () => void {
  const timer = setInterval(() => write(KEEP_ALIVE_FRAME), intervalMs);
  // Node timers have unref(); guard for exotic runtimes / fake timers.
  (timer as { unref?: () => void }).unref?.();
  let stopped = false;
  return () => {
    if (stopped) return;
    stopped = true;
    clearInterval(timer);
  };
}
