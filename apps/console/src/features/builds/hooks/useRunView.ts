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

import { useCallback, useSyncExternalStore } from "react";

/**
 * Which question a reader is asking of a cycle.
 *
 * `crew` answers "who is doing what right now", `timeline` answers "where did
 * the time go". They are never shown at once: both draw the same agents, and
 * stacking them spends the cycle's height twice on one fact. A reader switches
 * when the question changes, which is why the choice is remembered.
 */
export type RunView = "crew" | "timeline";

/**
 * `crew` is the default because liveness is the primary job — a reader must
 * never have to wonder whether the run is stuck, and the timeline is a
 * retrospective.
 */
export const DEFAULT_RUN_VIEW: RunView = "crew";

const STORAGE_KEY = "aep:builds:run-view";

function isRunView(value: string | null): value is RunView {
  return value === "crew" || value === "timeline";
}

/**
 * Read the remembered choice.
 *
 * Every access is guarded: a private window, a browser set to block site data
 * and a thumbnail capture all THROW on `localStorage` rather than returning
 * null, and a preference is never worth a blank page. An unrecognised value is
 * ignored the same way a typo in a mock key is — it must not look like the
 * toggle is broken.
 */
function readRunView(): RunView {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    return isRunView(stored) ? stored : DEFAULT_RUN_VIEW;
  } catch {
    return DEFAULT_RUN_VIEW;
  }
}

/** Remember the choice per browser. Failure to remember is not failure to switch. */
function writeRunView(view: RunView): void {
  try {
    localStorage.setItem(STORAGE_KEY, view);
  } catch {
    // Nothing to do and nothing to tell the user: the view still switched.
  }
}

// ONE choice for the page, not one per accordion.
//
// A run holds several cycles and each draws its own toggle. Held as component
// state, switching one left the others still in the view they mounted with — so
// one page showed a crew and a timeline at once, which is the exact thing this
// toggle exists to prevent. The choice is a property of the READER, so it lives
// beside the storage that persists it and every toggle reads the same value.
let current: RunView | undefined;
const listeners = new Set<() => void>();

function snapshot(): RunView {
  // Read from storage once per page load; after that this module is the truth,
  // so a live feed re-rendering every second does not touch storage at all.
  current ??= readRunView();
  return current;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/**
 * For tests, which need a page that has not made up its mind yet.
 *
 * A deliberate seam: the store is module state, so without this one test's
 * choice becomes the next test's default — and the whole point of the store is
 * that a choice outlives a component.
 */
export function resetRunViewForTest(): void {
  current = undefined;
  listeners.clear();
}

/**
 * The remembered view, and a setter that remembers.
 *
 * Per BROWSER rather than per cycle or per run. A reader who came to ask where
 * the time went is usually about to ask it of the next cycle too, and re-picking
 * the same view on every accordion is the friction that makes a second view go
 * unused.
 */
export function useRunView(): [RunView, (view: RunView) => void] {
  const view = useSyncExternalStore(subscribe, snapshot, () => DEFAULT_RUN_VIEW);
  const choose = useCallback((next: RunView) => {
    if (next === current) return;
    current = next;
    writeRunView(next);
    for (const listener of listeners) listener();
  }, []);
  return [view, choose];
}
