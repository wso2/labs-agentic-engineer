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

// "Waiting on the model provider": while a running turn's
// model call waits out a short 429, the agents service streams a
// `provider-wait` frame and the turn's working indicator says so instead of a
// bare "Working…". Transient by design — memory only, never the persisted chat
// log: a wait is over by the time anyone reloads, and a stale label replayed
// from storage would claim a wait nobody is in.

import { useCallback, useSyncExternalStore } from "react";

const waits = new Map<string, string>();
const listeners = new Map<string, Set<() => void>>();

function notify(key: string): void {
  for (const fn of listeners.get(key) ?? []) fn();
}

/** The turn streaming into `key`'s log is waiting on `host`. */
export function setProviderWait(key: string, host: string): void {
  if (waits.get(key) === host) return;
  waits.set(key, host);
  notify(key);
}

/** The wait is over: the model answered, or the stream ended. */
export function clearProviderWait(key: string): void {
  if (!waits.delete(key)) return;
  notify(key);
}

/** The host `key`'s running turn is waiting on, or undefined. */
export function useProviderWait(key: string): string | undefined {
  return useSyncExternalStore(
    useCallback(
      (fn: () => void) => {
        const set = listeners.get(key) ?? new Set();
        set.add(fn);
        listeners.set(key, set);
        return () => {
          set.delete(fn);
          if (set.size === 0) listeners.delete(key);
        };
      },
      [key],
    ),
    () => waits.get(key),
  );
}

/** The working indicator's label while a turn waits on `host`. */
export function providerWaitLabel(host: string): string {
  return `Waiting on the model provider (${host})…`;
}
