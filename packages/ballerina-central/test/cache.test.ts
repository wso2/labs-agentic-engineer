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
 * The cache, driven through the real command.
 *
 * Two properties matter more than the rest and both are tested against stdout
 * rather than against the store: a hit produces the SAME DOCUMENT as a fetch, and
 * a cache that is broken in any way produces NO OBSERVABLE EFFECT — no byte on
 * stderr, no non-zero exit, no wrong document. The second is what makes "cache
 * trouble is never the caller's problem" a fact rather than an intention, and it
 * is the one an implementation is most likely to break by helpfully reporting
 * something.
 */

import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { createDiskCache } from "../src/cache/disk.js";
import { resolveCacheLocation } from "../src/cache/location.js";
import { compareVersions, type DocsCache } from "../src/cache/store.js";
import { LATEST_TTL_MS, type FetchLike } from "../src/central/client.js";
import { run, type CliStreams } from "../src/cli.js";
import { loadRawFixture } from "./corpus.js";

const SLUG = "ballerinax__kafka";
const PKG = "ballerinax/kafka";
const VERSION = "4.6.5";

function capture(): CliStreams & { stdout: () => string; stderr: () => string } {
  let out = "";
  let error = "";
  return {
    out: (text) => {
      out += text;
    },
    errorOut: (text) => {
      error += text;
    },
    stdout: () => out,
    stderr: () => error,
  };
}

/** Central, replayed, counting how many times each endpoint was hit. */
function countingCentral(): { fetch: FetchLike; docs: () => number; versions: () => number } {
  const payload = loadRawFixture(SLUG);
  let docs = 0;
  let versions = 0;
  return {
    fetch: (url) => {
      if (url.includes("/docs/")) {
        docs++;
        return Promise.resolve(new Response(JSON.stringify(payload), { status: 200 }));
      }
      versions++;
      return Promise.resolve(new Response(JSON.stringify([VERSION]), { status: 200 }));
    },
    docs: () => docs,
    versions: () => versions,
  };
}

function freshRoot(): string {
  return mkdtempSync(join(tmpdir(), "bal-cache-"));
}

function cacheAt(root: string, options: Partial<Parameters<typeof createDiskCache>[0]> = {}): DocsCache {
  return createDiskCache({ root, ...options });
}

function docsEntry(root: string): string {
  return join(root, "v1", "docs", "ballerinax", "kafka", `${VERSION}.json`);
}

/** Strips the one line a cache hit is meant to change. */
function withoutProvenance(document: string): string {
  return document
    .split("\n")
    .filter((line) => !line.startsWith("| Source |"))
    .join("\n");
}

// ---------------------------------------------------------------------------
// Hit and miss
// ---------------------------------------------------------------------------

test("a miss fetches, a hit does not, and both produce the same document", async () => {
  const root = freshRoot();
  const cache = cacheAt(root);
  const central = countingCentral();

  const cold = capture();
  assert.equal(await run([PKG], cold, { fetch: central.fetch, cache }), 0);
  assert.equal(central.docs(), 1);

  const warm = capture();
  assert.equal(await run([PKG], warm, { fetch: central.fetch, cache }), 0);
  assert.equal(central.docs(), 1, "the second run must not fetch the docs again");
  assert.equal(central.versions(), 1, "nor re-resolve the version inside the TTL");

  assert.equal(withoutProvenance(warm.stdout()), withoutProvenance(cold.stdout()));
  assert.match(cold.stdout(), /^\| Source \| central \|$/m);
  assert.match(warm.stdout(), /^\| Source \| cache \|$/m);
});

test("the entry is the payload as Central served it, uncompressed", () => {
  const root = freshRoot();
  const cache = cacheAt(root);
  const payload = loadRawFixture(SLUG);
  cache.writeDocs({ org: "ballerinax", name: "kafka", version: VERSION }, payload);
  // No gzip level to choose, no bad-gzip corruption mode to handle. Disk is not the
  // constrained resource: both runner mounts are emptyDirs and the cache dies with
  // the run.
  assert.deepEqual(JSON.parse(readFileSync(docsEntry(root), "utf-8")), payload);
});

test("every verb reads the one cached payload, so four questions cost one fetch", async () => {
  const root = freshRoot();
  const cache = cacheAt(root);
  const central = countingCentral();

  for (const argv of [[PKG], ["ops", PKG], ["type", PKG, "TopicPartition"], ["api", PKG]]) {
    const streams = capture();
    assert.equal(await run(argv, streams, { fetch: central.fetch, cache }), 0, argv.join(" "));
  }
  // This is the whole point of the cache. At 4.9 to 6.6 seconds per invocation the
  // CLI can only be asked once, which is what forced a 21,818-line document to be
  // navigated by hand.
  assert.equal(central.docs(), 1);
});

// ---------------------------------------------------------------------------
// Every way an entry can be wrong
// ---------------------------------------------------------------------------

test("each corruption mode falls through to the network, silently", async () => {
  const payload = JSON.stringify(loadRawFixture(SLUG));
  const wrongCoordinates = structuredClone(loadRawFixture(SLUG)) as {
    docsData: { modules: { version: string }[] };
  };
  for (const module of wrongCoordinates.docsData.modules) module.version = "9.9.9";

  const cases: [string, string][] = [
    ["truncated", payload.slice(0, Math.floor(payload.length / 2))],
    ["not JSON at all", "<html>maintenance</html>"],
    ["empty", ""],
    ["JSON but not a payload", '{"hello":"world"}'],
    ["schema drift", '{"apiDocsVersion":"1.0.0","docsData":{"modules":[{"id":"kafka","orgName":"ballerinax"}]}}'],
    ["coordinates that do not match the path", JSON.stringify(wrongCoordinates)],
  ];

  for (const [label, contents] of cases) {
    const root = freshRoot();
    const cache = cacheAt(root);
    mkdirSync(join(root, "v1", "docs", "ballerinax", "kafka"), { recursive: true });
    writeFileSync(docsEntry(root), contents);

    const central = countingCentral();
    const streams = capture();
    const exitCode = await run([PKG], streams, { fetch: central.fetch, cache });

    assert.equal(exitCode, 0, `${label}: must still succeed`);
    assert.equal(streams.stderr(), "", `${label}: must say nothing`);
    assert.equal(central.docs(), 1, `${label}: must fetch`);
    assert.match(streams.stdout(), /^\| Source \| central \|$/m, `${label}: must report the fetch`);
    // Self-healing: the bad entry is replaced, so the next run is a hit.
    assert.deepEqual(JSON.parse(readFileSync(docsEntry(root), "utf-8")), loadRawFixture(SLUG), `${label}: rewritten`);
  }
});

test("a payload the schema rejects is never written, so the drift is not made permanent", async () => {
  const root = freshRoot();
  const cache = cacheAt(root);
  const streams = capture();
  const exitCode = await run([PKG], streams, {
    cache,
    fetch: (url) =>
      Promise.resolve(
        new Response(url.includes("/docs/") ? '{"docsData":{"modules":[]}}' : JSON.stringify([VERSION]), {
          status: 200,
        }),
      ),
  });
  assert.equal(exitCode, 1);
  assert.throws(() => readdirSync(join(root, "v1", "docs", "ballerinax", "kafka")), /ENOENT/);
});

test("a root whose parent is a regular file disables caching for every uid", async () => {
  // Pointed at a path under a FILE rather than at a mode-000 directory: the latter
  // goes vacuous under a root CI user, and a test that passes because it is running
  // as root is worse than no test.
  const parent = join(freshRoot(), "regular-file");
  writeFileSync(parent, "not a directory");
  const cache = cacheAt(join(parent, "cache"));

  const central = countingCentral();
  const streams = capture();
  assert.equal(await run([PKG], streams, { fetch: central.fetch, cache }), 0);
  assert.equal(streams.stderr(), "");
  assert.match(cache.describe(), /unusable/);

  const again = capture();
  assert.equal(await run([PKG], again, { fetch: central.fetch, cache }), 0);
  assert.equal(central.docs(), 2, "nothing was cached, so the second run fetches too");
});

test("a coordinate that is not obviously safe never reaches the filesystem", () => {
  const root = freshRoot();
  const cache = cacheAt(root);
  // `parseQualifiedName` and `parseVersion` reject all of these first; this is the
  // inner guard, kept because the outer one is a regex someone could loosen.
  for (const version of ["..", ".", "../../etc/passwd", "a/b", "", "with space"]) {
    cache.writeDocs({ org: "ballerinax", name: "kafka", version }, { anything: true });
    assert.equal(cache.readDocs({ org: "ballerinax", name: "kafka", version }), undefined, `version ${version}`);
  }
  for (const org of ["..", ".", "../..", "a/b"]) {
    cache.writeDocs({ org, name: "kafka", version: VERSION }, { anything: true });
    assert.equal(cache.readDocs({ org, name: "kafka", version: VERSION }), undefined, `org ${org}`);
    assert.deepEqual(cache.listVersions({ org, name: "kafka" }), [], `listVersions ${org}`);
  }
  assert.equal(readdirSync(root).length, 0, "not even the format directory should exist");
});

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

test("two concurrent runs leave one entry and no temp files", async () => {
  const root = freshRoot();
  const central = countingCentral();
  // Forced onto the same temp name, which is the worst case a per-pid suffix is
  // meant to avoid. Both still rename onto the same target, and rename is atomic:
  // no third process can observe a partial file.
  const collide = cacheAt(root, { random: () => 0.5, pid: 1234 });

  const [a, b] = [capture(), capture()];
  await Promise.all([
    run([PKG], a, { fetch: central.fetch, cache: collide }),
    run([PKG], b, { fetch: central.fetch, cache: collide }),
  ]);

  const entries = readdirSync(join(root, "v1", "docs", "ballerinax", "kafka"));
  assert.deepEqual(entries, [`${VERSION}.json`], "one entry, and nothing ending in .tmp");
  assert.deepEqual(JSON.parse(readFileSync(docsEntry(root), "utf-8")), loadRawFixture(SLUG));
});

// ---------------------------------------------------------------------------
// The versions list: TTL, refresh, offline
// ---------------------------------------------------------------------------

test("the versions list is believed for ten minutes and re-asked after", async () => {
  const root = freshRoot();
  const cache = cacheAt(root);
  const central = countingCentral();
  let now = 1_000_000;

  const first = capture();
  await run([PKG], first, { fetch: central.fetch, cache, now: () => now });
  assert.equal(central.versions(), 1);

  now += LATEST_TTL_MS - 1;
  await run([PKG], capture(), { fetch: central.fetch, cache, now: () => now });
  assert.equal(central.versions(), 1, "one millisecond inside the TTL is still inside it");

  now += 1;
  await run([PKG], capture(), { fetch: central.fetch, cache, now: () => now });
  assert.equal(central.versions(), 2, "at the boundary it is re-asked");
});

test("a clock that jumped backwards does not make a future-stamped entry immortal", async () => {
  const root = freshRoot();
  const cache = cacheAt(root);
  const central = countingCentral();
  await run([PKG], capture(), { fetch: central.fetch, cache, now: () => 5_000_000 });
  assert.equal(central.versions(), 1);
  await run([PKG], capture(), { fetch: central.fetch, cache, now: () => 1_000 });
  assert.equal(central.versions(), 2, "an entry stamped in the future is not fresh, it is wrong");
});

test("--refresh re-resolves and re-downloads unconditionally", async () => {
  const root = freshRoot();
  const cache = cacheAt(root);
  const central = countingCentral();

  await run([PKG], capture(), { fetch: central.fetch, cache });
  assert.equal(central.docs(), 1);

  const refreshed = capture();
  // Unconditional on purpose. An earlier draft made the re-download conditional on
  // the version having changed, which made the flag a no-op in exactly the case its
  // own error message recommends it for.
  assert.equal(await run([PKG, "--refresh"], refreshed, { fetch: central.fetch, cache }), 0);
  assert.equal(central.docs(), 2);
  assert.equal(central.versions(), 2);
  assert.match(refreshed.stdout(), /^\| Source \| central \|$/m);
});

test("with the registry unreachable and a payload on disk, the lookup still answers and says it is unverified", async () => {
  const root = freshRoot();
  const cache = cacheAt(root);
  const central = countingCentral();
  let now = 1_000_000;
  await run([PKG], capture(), { fetch: central.fetch, cache, now: () => now });

  // The registry blip, after the versions TTL has expired.
  now += LATEST_TTL_MS * 2;
  const offline = capture();
  const exitCode = await run([PKG], offline, {
    cache,
    now: () => now,
    maxAttempts: 1,
    baseDelayMs: 1,
    fetch: (url) => {
      assert.doesNotMatch(url, /\/docs\//, "the docs must come off disk, not the network");
      return Promise.resolve(new Response("", { status: 503 }));
    },
  });

  // Without this, a warm cached payload plus one blip is a hard failure that can
  // burn the client's whole 300s budget — four times over in a four-verb episode.
  assert.equal(exitCode, 0);
  assert.match(offline.stdout(), /^\| Source \| cache \(stale: registry unreachable, version unverified\) \|$/m);
});

test("with the registry unreachable and nothing on disk, the failure is honest", async () => {
  const cache = cacheAt(freshRoot());
  const streams = capture();
  const exitCode = await run([PKG], streams, {
    cache,
    maxAttempts: 1,
    baseDelayMs: 1,
    fetch: () => Promise.resolve(new Response("", { status: 503 })),
  });
  assert.equal(exitCode, 1);
  assert.equal(streams.stdout(), "");
  assert.equal((JSON.parse(streams.stderr()) as { kind: string }).kind, "upstream");
});

test("the newest version on disk is chosen by version order, not by filename order", () => {
  const root = freshRoot();
  const cache = cacheAt(root);
  for (const version of ["1.9.0", "1.10.0", "2.0.0", "2.0.0-alpha"]) {
    cache.writeDocs({ org: "ballerinax", name: "kafka", version }, { v: version });
  }
  assert.deepEqual(cache.listVersions({ org: "ballerinax", name: "kafka" }), [
    "2.0.0",
    "2.0.0-alpha",
    "1.10.0",
    "1.9.0",
  ]);
});

test("version comparison is dotted-numeric with prereleases below their release", () => {
  // Lexicographic order is wrong in both directions that matter.
  assert.ok(compareVersions("1.10.0", "1.9.0") > 0);
  assert.ok(compareVersions("2.0.0", "2.0.0-alpha") > 0);
  assert.ok(compareVersions("2.0.0-beta", "2.0.0-alpha") > 0);
  assert.equal(compareVersions("1.2.3", "1.2.3"), 0);
  assert.ok(compareVersions("1.2", "1.2.1") < 0);
});

// ---------------------------------------------------------------------------
// Where it lives
// ---------------------------------------------------------------------------

test("the cache location is a pure function of the environment", () => {
  const base = { homedir: "/home/aep", tmpdir: "/tmp", uid: 1000 };

  assert.deepEqual(resolveCacheLocation({ ...base, env: { BAL_LIBRARY_CACHE: "off" } }), {
    kind: "disabled",
    reason: "BAL_LIBRARY_CACHE=off",
  });

  assert.equal(
    resolveCacheLocation({ ...base, env: { BAL_LIBRARY_CACHE_DIR: "/somewhere/else" } }).kind === "directory"
      ? "/somewhere/else"
      : "",
    "/somewhere/else",
  );

  const xdg = resolveCacheLocation({ ...base, env: { XDG_CACHE_HOME: "/xdg" } });
  assert.equal(xdg.kind === "directory" ? xdg.root : "", "/xdg/bal-library");

  // Relative XDG_CACHE_HOME is invalid per the spec, so it is ignored rather than
  // resolved against the working directory — which for a coding agent is a git
  // clone the platform commits.
  const relative = resolveCacheLocation({ ...base, env: { XDG_CACHE_HOME: "relative/path" } });
  assert.equal(relative.kind === "directory" ? relative.root : "", "/home/aep/.cache/bal-library");

  const fallback = resolveCacheLocation({ ...base, homedir: "", env: {} });
  assert.equal(fallback.kind === "directory" ? fallback.root : "", "/tmp/bal-library-1000");
  // Mode 0700 with the uid in the name, because /tmp is world-writable and shared
  // with the agent's own scratch files.
  assert.equal(fallback.kind === "directory" ? fallback.mode : 0, 0o700);

  assert.equal(resolveCacheLocation({ env: {}, homedir: "", tmpdir: "", uid: 0 }).kind, "disabled");
});
