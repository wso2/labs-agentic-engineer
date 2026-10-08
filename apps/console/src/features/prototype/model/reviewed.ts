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

// Which revision of each prototype this browser has reviewed, kept per browser
// in localStorage (not shared: one person's looking is not another's). Storage
// can be missing or throw (a private window, blocked site data), and the dot
// then simply stays on: nothing here may break the page.

const KEY = (projectName: string) => `aep:prototype-reviewed:${projectName}`;

type Reviewed = Record<string, string>;

const listeners = new Set<() => void>();

function read(projectName: string): Reviewed {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(KEY(projectName)) ?? "{}");
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return {};
    return Object.fromEntries(Object.entries(parsed).filter((e): e is [string, string] => typeof e[1] === "string"));
  } catch {
    return {};
  }
}

/** Remember that the revision `hash` of a component's prototype was shown in a review. */
export function markReviewed(projectName: string, component: string, hash: string): void {
  const reviewed = read(projectName);
  if (reviewed[component] === hash) return;
  try {
    localStorage.setItem(KEY(projectName), JSON.stringify({ ...reviewed, [component]: hash }));
  } catch {
    // Not remembered: the dot shows again on the next load.
  }
  for (const fn of listeners) fn();
}

/** Be told when a review is recorded here or in another tab; returns the unsubscribe. */
export function subscribeReviewed(fn: () => void): () => void {
  listeners.add(fn);
  window.addEventListener("storage", fn);
  return () => {
    listeners.delete(fn);
    window.removeEventListener("storage", fn);
  };
}

/** The components whose current revision (by hash) this browser has not reviewed. */
export function unreviewed(projectName: string, hashes: Readonly<Record<string, string>>): string[] {
  const reviewed = read(projectName);
  return Object.keys(hashes).filter((component) => reviewed[component] !== hashes[component]);
}
