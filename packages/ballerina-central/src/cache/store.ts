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
 * What a docs cache has to be able to do, and the implementation that does
 * nothing.
 *
 * The interface exists so `src/` never reads the environment or touches a
 * filesystem outside `cache/disk.ts`, and so every existing test stays hermetic
 * by getting `NULL_CACHE` and being unable to tell the difference.
 *
 * NOTHING HERE MAY THROW OR REPORT. Cache trouble is never the caller's
 * problem: an unwritable directory, a foreign uid, a full disk and a corrupt
 * entry all have to come out as "no cached copy", with no byte on stdout, no
 * byte on stderr and no non-zero exit. The alternative — an unusable
 * `BAL_LIBRARY_CACHE_DIR` failing at exit 2 — sends the agent into the skill's
 * argument-error advice in a loop it can never escape.
 *
 * WHAT IS CACHED IS THE RAW PAYLOAD, not the IR and not the rendered string. The
 * IR and the rendering are our code, and this design changes three of those
 * modules: an IR entry's key would need a build identity in it, and a runner
 * whose baked bundle differs from a mounted `dist/` would serve output from the
 * wrong renderer. The raw payload is not derived from our code, so the
 * coordinates are the whole key. Re-deriving costs about 110ms of parse and
 * transform against 5 to 7 seconds of download.
 *
 * THE KEY HAS NO IDENTITY DIMENSION. That is correct only while `fetchJson`
 * sends no headers and only public Central data is reachable. If a Central token
 * is ever threaded through `HttpOptions`, this cache must be disabled or keyed by
 * a token fingerprint: `$HOME` outlives the per-task workspace scrub, and mode
 * 0600 buys nothing against the same uid.
 */

/** A package's immutable coordinates — the whole key of a docs entry. */
export interface DocsKey {
  readonly org: string;
  readonly name: string;
  readonly version: string;
}

/** A package without a version, which is what the versions list is keyed by. */
export type PackageKey = Omit<DocsKey, "version">;

/** The one mutable answer Central gives, and when we last believed it. */
export interface LatestEntry {
  readonly version: string;
  readonly atMs: number;
}

export interface DocsCache {
  /** The raw payload, or `undefined` for any reason whatsoever. */
  readDocs(key: DocsKey): unknown;
  writeDocs(key: DocsKey, payload: unknown): void;
  /** Best-effort. Used to make a corrupt entry self-healing and by `--refresh`. */
  removeDocs(key: DocsKey): void;
  readLatest(key: PackageKey): LatestEntry | undefined;
  writeLatest(key: PackageKey, entry: LatestEntry): void;
  /**
   * Every version of one package already on disk, newest first by version order.
   * The offline fallback: a warm payload plus one registry blip should not be a
   * hard failure that burns the client's whole 300s budget.
   */
  listVersions(key: PackageKey): readonly string[];
  /**
   * One line for `--help`. The only place the cache is allowed to speak, and it
   * is on stderr beside usage text, outside both the document and the `Failure`
   * contract — which is how an operator proves the cache is alive in a runner
   * image without parsing anything.
   */
  describe(): string;
}

export const NULL_CACHE: DocsCache = {
  readDocs: () => undefined,
  writeDocs: () => undefined,
  removeDocs: () => undefined,
  readLatest: () => undefined,
  writeLatest: () => undefined,
  listVersions: () => [],
  describe: () => "disabled",
};

/**
 * Compare two published versions the way a human would.
 *
 * Lexicographic order is wrong in both directions that matter: `1.9.0` sorts
 * above `1.10.0`, and `2.0.0-alpha` above `2.0.0`. This only has to pick the
 * newest thing already on disk, so it is dotted-numeric with a prerelease
 * suffix ranking below its own release, and not a semver implementation.
 */
export function compareVersions(a: string, b: string): number {
  const split = (value: string): { numbers: number[]; prerelease: string } => {
    const dash = value.indexOf("-");
    const core = dash === -1 ? value : value.slice(0, dash);
    return {
      numbers: core.split(".").map((part) => Number.parseInt(part, 10) || 0),
      prerelease: dash === -1 ? "" : value.slice(dash + 1),
    };
  };
  const left = split(a);
  const right = split(b);
  const width = Math.max(left.numbers.length, right.numbers.length);
  for (let i = 0; i < width; i++) {
    const difference = (left.numbers[i] ?? 0) - (right.numbers[i] ?? 0);
    if (difference !== 0) return difference;
  }
  if (left.prerelease === right.prerelease) return 0;
  // A release outranks any prerelease of the same core version.
  if (left.prerelease === "") return 1;
  if (right.prerelease === "") return -1;
  return left.prerelease < right.prerelease ? -1 : 1;
}
