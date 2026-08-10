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
 * The cache, on disk.
 *
 *   <root>/v1/docs/<org>/<name>/<version>.json      mode 0600, no TTL
 *   <root>/v1/latest/<org>/<name>.json              {"version":"6.0.0","atMs":…}
 *
 * `v1` is the on-disk format generation, bumped only when the stored bytes change
 * meaning. Deliberately not a build identity — see `store.ts` for why the raw
 * payload is what gets stored.
 *
 * Entries are stored UNCOMPRESSED, exactly as Central served them. Disk is not
 * the constrained resource: the runner pod declares no ephemeral-storage limit,
 * both its mounts are emptyDirs, and the cache does not outlive the run.
 * Compression would add a level to choose, a corruption mode to handle and a
 * compress step on the write path, to save bytes nobody is paying for.
 *
 * Concurrency is structural rather than hypothetical — fan-out is the runner's
 * default and recorded runs show two subagents' bash calls interleaving seconds
 * apart in one container sharing one `$HOME`. So every write goes to a
 * per-process temp file and is then `rename()`d, which is atomic on POSIX. There
 * is deliberately no lock and no single-flight: two processes that miss the same
 * package both fetch and both rename, the content is equivalent, and no third
 * process can observe a partial file. A lock could outlive the client's own 300s
 * budget and hang a run; a duplicate 5.7s download is the cheaper failure.
 */

import { lstatSync, mkdirSync, readdirSync, readFileSync, renameSync, rmSync, statSync, writeFileSync } from "node:fs";
import { join, resolve, sep } from "node:path";
import { compareVersions, type DocsCache, type DocsKey, type LatestEntry, type PackageKey } from "./store.js";

const FORMAT = "v1";
const ENTRY_MODE = 0o600;

/** Every path segment has to be one of these before it can reach `join`. */
const SAFE_SEGMENT = /^[A-Za-z0-9_.-]+$/;

function isSafeSegment(segment: string): boolean {
  return SAFE_SEGMENT.test(segment) && segment !== "." && segment !== "..";
}

/**
 * The path of an entry, or `undefined` if any part of it is not obviously safe.
 *
 * Three independent checks, because the first is a regex someone could later
 * loosen: every COORDINATE is validated raw, every path segment is validated
 * again after a suffix is attached, and then the RESOLVED path has to still start
 * with the root plus a separator. `parseQualifiedName` and `parseVersion` already
 * reject `.` and `..`, which makes all of this the inner guard rather than the
 * only one.
 *
 * The raw check is not redundant with the segment check: `..` with `.json`
 * attached becomes `...json`, which is a perfectly ordinary filename and passes.
 * That is harmless in itself — it traverses nothing — but a coordinate this store
 * would not accept as a directory name should not be accepted as a file name
 * either, or the two guards disagree about what a valid key is.
 */
function entryPath(root: string, coordinates: readonly string[], segments: readonly string[]): string | undefined {
  if (!coordinates.every(isSafeSegment)) return undefined;
  if (!segments.every(isSafeSegment)) return undefined;
  const candidate = resolve(join(root, ...segments));
  const base = resolve(root);
  return candidate.startsWith(base + sep) ? candidate : undefined;
}

function docsPath(root: string, key: DocsKey): string | undefined {
  const coordinates = [key.org, key.name, key.version];
  return entryPath(root, coordinates, [FORMAT, "docs", key.org, key.name, `${key.version}.json`]);
}

function docsDir(root: string, key: PackageKey): string | undefined {
  return entryPath(root, [key.org, key.name], [FORMAT, "docs", key.org, key.name]);
}

function latestPath(root: string, key: PackageKey): string | undefined {
  return entryPath(root, [key.org, key.name], [FORMAT, "latest", key.org, `${key.name}.json`]);
}

/**
 * Is this root usable, and is it ours?
 *
 * `lstat` rather than `stat`: a root that is a symlink is refused outright,
 * because following one is how a writable-looking path becomes somebody else's
 * directory. A root owned by another uid is refused for the same reason.
 */
function rootIsUsable(root: string, mode: number, uid: number): boolean {
  try {
    mkdirSync(root, { recursive: true, mode });
  } catch {
    return false;
  }
  try {
    const stats = lstatSync(root);
    if (stats.isSymbolicLink() || !stats.isDirectory()) return false;
    if (typeof stats.uid === "number" && stats.uid !== uid) return false;
    return true;
  } catch {
    return false;
  }
}

/** Temp name in the SAME directory as its target, so `rename` stays on one filesystem. */
function tempPathFor(target: string, pid: number, random: () => number): string {
  const suffix = Math.floor(random() * 0xffffffff).toString(16);
  return `${target}.${pid}-${suffix}.tmp`;
}

export interface DiskCacheOptions {
  readonly root: string;
  readonly mode?: number;
  readonly uid?: number;
  readonly pid?: number;
  /** Injectable so a concurrency test can force two writers onto one temp name. */
  readonly random?: () => number;
}

/**
 * A disk cache at `root`, or a store that silently does nothing if the root is
 * not usable. The caller cannot tell the two apart on purpose, and never has to
 * handle a cache error, because there is no cache error to handle.
 */
export function createDiskCache(options: DiskCacheOptions): DocsCache {
  const { root } = options;
  const mode = options.mode ?? 0o700;
  const uid = options.uid ?? (typeof process.getuid === "function" ? process.getuid() : 0);
  const pid = options.pid ?? process.pid;
  const random = options.random ?? Math.random;

  const usable = rootIsUsable(root, mode, uid);

  const writeAtomically = (target: string, contents: string): void => {
    if (!usable) return;
    const temp = tempPathFor(target, pid, random);
    try {
      mkdirSync(join(target, ".."), { recursive: true, mode });
      writeFileSync(temp, contents, { mode: ENTRY_MODE });
      renameSync(temp, target);
    } catch {
      // ENOSPC, EACCES, a vanished parent: leave nothing behind and say nothing.
      try {
        rmSync(temp, { force: true });
      } catch {
        /* the temp file is already gone, or was never created */
      }
    }
  };

  const readJson = (path: string): unknown => {
    try {
      return JSON.parse(readFileSync(path, "utf-8")) as unknown;
    } catch {
      // ENOENT, EACCES, a truncated file, a file that is not JSON: all one thing.
      return undefined;
    }
  };

  return {
    readDocs(key) {
      if (!usable) return undefined;
      const path = docsPath(root, key);
      return path === undefined ? undefined : readJson(path);
    },

    writeDocs(key, payload) {
      const path = docsPath(root, key);
      if (path === undefined) return;
      let contents: string;
      try {
        contents = JSON.stringify(payload);
      } catch {
        return;
      }
      writeAtomically(path, contents);
    },

    removeDocs(key) {
      if (!usable) return;
      const path = docsPath(root, key);
      if (path === undefined) return;
      try {
        rmSync(path, { force: true });
      } catch {
        /* best effort; the next successful fetch overwrites it anyway */
      }
    },

    readLatest(key) {
      if (!usable) return undefined;
      const path = latestPath(root, key);
      if (path === undefined) return undefined;
      const raw = readJson(path);
      if (raw === null || typeof raw !== "object") return undefined;
      const { version, atMs } = raw as { version?: unknown; atMs?: unknown };
      if (typeof version !== "string" || version === "" || typeof atMs !== "number") return undefined;
      return { version, atMs };
    },

    writeLatest(key, entry: LatestEntry) {
      const path = latestPath(root, key);
      if (path === undefined) return;
      writeAtomically(path, JSON.stringify(entry));
    },

    listVersions(key) {
      if (!usable) return [];
      const directory = docsDir(root, key);
      if (directory === undefined) return [];
      try {
        return readdirSync(directory)
          .filter((file) => file.endsWith(".json"))
          .map((file) => file.slice(0, -".json".length))
          .sort((a, b) => compareVersions(b, a));
      } catch {
        return [];
      }
    },

    describe() {
      if (!usable) return `${root} (unusable; caching disabled)`;
      try {
        statSync(root);
        return `${root} (writable)`;
      } catch {
        return `${root} (unusable; caching disabled)`;
      }
    },
  };
}
