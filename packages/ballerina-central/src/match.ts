/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * Turning the name an agent typed into the name a package declares.
 *
 * Exact match first, then normalised equality — and MORE THAN ONE normalised
 * match is a failure with every match listed, never a silent pick. That is not
 * theoretical: `ballerinax/github`'s `ManifestConversions` declares both
 * `clientId` and `client_id`, and `ballerina/http` has 61 constant-versus-class
 * collisions of the `STATUS_ACCEPTED` / `StatusAccepted` shape. Picking one of
 * those and printing it would answer a different question than the one asked,
 * with nothing in the output to say so.
 */

/** Letters and digits, lower-cased: what `STATUS_ACCEPTED` and `StatusAccepted` share. */
export function normalise(name: string): string {
  return name.replace(/[^A-Za-z0-9]/g, "").toLowerCase();
}

export type NameMatch =
  | { readonly kind: "found"; readonly name: string }
  /** Several declarations normalise to the same thing. The caller has to choose. */
  | { readonly kind: "ambiguous"; readonly candidates: readonly string[] }
  | { readonly kind: "missing"; readonly candidates: readonly string[] };

/** The longest run of characters two strings share, case-insensitively. */
function longestCommonSubstring(a: string, b: string): number {
  const left = a.toLowerCase();
  const right = b.toLowerCase();
  let best = 0;
  // O(n·m) over identifier-length strings, run once per candidate on a miss only.
  let previous = new Array<number>(right.length + 1).fill(0);
  for (let i = 1; i <= left.length; i++) {
    const current = new Array<number>(right.length + 1).fill(0);
    for (let j = 1; j <= right.length; j++) {
      if (left[i - 1] === right[j - 1]) {
        current[j] = (previous[j - 1] ?? 0) + 1;
        best = Math.max(best, current[j] ?? 0);
      }
    }
    previous = current;
  }
  return best;
}

/** How many near-misses a failure carries. */
export const MAX_CANDIDATES = 8;
/** Shorter overlaps than this are noise — every identifier shares `e` with every other. */
const MIN_OVERLAP = 4;

/**
 * Names worth suggesting for a request that matched nothing.
 *
 * Ranked by the longest run of characters shared with the request, which puts
 * `FullRepository` first for `FullRepo` and still surfaces `Repository` and
 * `MinimalRepository` behind it. Capped, because the alternative is the whole
 * roster — 33,431 bytes for github's 1,227 names — inside a JSON object on
 * stderr.
 */
export function nearMisses(requested: string, names: readonly string[]): readonly string[] {
  return names
    .map((name) => ({ name, overlap: longestCommonSubstring(requested, name) }))
    .filter((scored) => scored.overlap >= MIN_OVERLAP)
    .sort((a, b) => b.overlap - a.overlap || a.name.localeCompare(b.name))
    .slice(0, MAX_CANDIDATES)
    .map((scored) => scored.name);
}

/**
 * Resolve one requested name against a roster.
 *
 * `names` is the declaration roster in the package's own order; the returned name
 * is always one of them verbatim, so the caller never has to reconcile the
 * spelling it asked for with the spelling it got.
 */
export function matchName(requested: string, names: readonly string[]): NameMatch {
  const trimmed = requested.trim();
  if (names.includes(trimmed)) return { kind: "found", name: trimmed };

  const wanted = normalise(trimmed);
  const normalised = wanted === "" ? [] : names.filter((name) => normalise(name) === wanted);
  if (normalised.length === 1 && normalised[0] !== undefined) return { kind: "found", name: normalised[0] };
  if (normalised.length > 1) return { kind: "ambiguous", candidates: normalised };

  return { kind: "missing", candidates: nearMisses(trimmed, names) };
}
