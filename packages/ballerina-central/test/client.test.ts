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
 * The boundary, driven without a network: which failures are worth retrying,
 * which are answers, and what each one costs the caller.
 */

import assert from "node:assert/strict";
import { test } from "node:test";
import {
  fetchDocs,
  fetchJson,
  parseCentralDocs,
  parseDependenciesToml,
  resolveLatestVersion,
  type FetchLike,
} from "../src/central/client.js";
import { parseQualifiedName } from "../src/qualified.js";
import { loadRawFixture } from "./corpus.js";

const GITHUB = (() => {
  const parsed = parseQualifiedName("ballerinax/github");
  if (!parsed.ok) throw new Error("unreachable");
  return parsed.value;
})();

/** Answers each call from the queue, and records how many calls happened. */
function scriptedFetch(responses: readonly (() => Response | Promise<Response>)[]): {
  fetch: FetchLike;
  calls: () => number;
} {
  let index = 0;
  return {
    fetch: () => {
      const next = responses[index++];
      if (!next) throw new Error("scripted fetch ran out of responses");
      return Promise.resolve(next());
    },
    calls: () => index,
  };
}

const json = (body: unknown, init?: ResponseInit): Response =>
  new Response(JSON.stringify(body), { status: 200, ...init });

// Fast enough that the retry path costs a test run nothing.
const FAST = { maxAttempts: 3, baseDelayMs: 1, budgetMs: 5_000, timeoutMs: 1_000 } as const;

test("a 503 is retried and the retry's answer is used", async () => {
  const scripted = scriptedFetch([
    () => new Response("", { status: 503 }),
    () => json({ hello: "world" }),
  ]);
  const result = await fetchJson("https://example.test/x", { ...FAST, fetch: scripted.fetch });
  assert.equal(result.ok, true);
  assert.deepEqual(result.ok ? result.value : undefined, { hello: "world" });
  assert.equal(scripted.calls(), 2);
});

test("retries stop at maxAttempts and report how many were spent", async () => {
  const scripted = scriptedFetch([
    () => new Response("", { status: 502 }),
    () => new Response("", { status: 502 }),
    () => new Response("", { status: 502 }),
  ]);
  const result = await fetchJson("https://example.test/x", { ...FAST, fetch: scripted.fetch });
  assert.equal(result.ok, false);
  if (result.ok) return;
  assert.equal(result.error.kind, "upstream");
  assert.equal(result.error.kind === "upstream" ? result.error.attempts : 0, 3);
  assert.equal(scripted.calls(), 3);
});

test("a 404 is an answer, not a hiccup — it is never retried", async () => {
  const scripted = scriptedFetch([() => new Response("", { status: 404 })]);
  const result = await fetchJson("https://example.test/x", { ...FAST, fetch: scripted.fetch });
  assert.equal(result.ok, false);
  assert.equal(result.ok ? "" : result.error.kind, "upstream");
  assert.equal(scripted.calls(), 1);
});

test("a body that is not JSON is not retried either", async () => {
  const scripted = scriptedFetch([() => new Response("<html>maintenance</html>", { status: 200 })]);
  const result = await fetchJson("https://example.test/x", { ...FAST, fetch: scripted.fetch });
  assert.equal(result.ok, false);
  assert.equal(scripted.calls(), 1);
});

test("a request that never answers becomes a timeout, not a hang", async () => {
  const fetchImpl: FetchLike = (_url, init) =>
    new Promise((_resolve, reject) => {
      init?.signal?.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
    });
  const result = await fetchJson("https://example.test/slow", {
    fetch: fetchImpl,
    timeoutMs: 10,
    maxAttempts: 1,
    budgetMs: 200,
    baseDelayMs: 1,
  });
  assert.equal(result.ok, false);
  assert.equal(result.ok ? "" : result.error.kind, "timeout");
});

test("a 404 from the docs endpoint names the version that is missing", async () => {
  const scripted = scriptedFetch([() => new Response("", { status: 404 })]);
  const result = await fetchDocs(GITHUB, "9.9.9" as never, { ...FAST, fetch: scripted.fetch });
  assert.equal(result.ok, false);
  if (result.ok) return;
  assert.equal(result.error.kind, "package-not-found");
  assert.equal(result.error.kind === "package-not-found" ? result.error.qualified : "", "ballerinax/github:9.9.9");
});

test("the newest version is the first entry Central returns", async () => {
  const scripted = scriptedFetch([() => json(["6.0.0", "5.4.1", "5.4.0"])]);
  const result = await resolveLatestVersion(GITHUB, { ...FAST, fetch: scripted.fetch });
  assert.equal(result.ok, true);
  assert.deepEqual(result.ok ? result.value : undefined, { version: "6.0.0", stale: false });
});

test("Central's 400 for an unpublished name reads as 'no such package'", async () => {
  // The most common caller mistake is a typo, and Central reports it as a 400.
  const scripted = scriptedFetch([() => new Response("", { status: 400 })]);
  const result = await resolveLatestVersion(GITHUB, { ...FAST, fetch: scripted.fetch });
  assert.equal(result.ok, false);
  assert.equal(result.ok ? "" : result.error.kind, "package-not-found");
  assert.equal(scripted.calls(), 1);
});

test("an empty version list means the package does not exist", async () => {
  const scripted = scriptedFetch([() => json([])]);
  const result = await resolveLatestVersion(GITHUB, { ...FAST, fetch: scripted.fetch });
  assert.equal(result.ok, false);
  assert.equal(result.ok ? "" : result.error.kind, "package-not-found");
});

test("every failure the outside world can cause carries a suggestion", async () => {
  // `result.ts` states that each failure says what to do about it, and three of
  // them used to omit it — the three that fire during a Central outage, when the
  // reader has nothing else to offer.
  const timeout = await fetchJson("https://example.invalid/x", {
    fetch: (_url, init) =>
      new Promise((_resolve, reject) => init?.signal?.addEventListener("abort", () => reject(new Error("aborted")))),
    timeoutMs: 5,
    maxAttempts: 1,
    baseDelayMs: 1,
  });
  assert.equal(timeout.ok, false);
  if (!timeout.ok) {
    assert.equal(timeout.error.kind, "timeout");
    assert.ok("suggestion" in timeout.error && timeout.error.suggestion.length > 0);
  }

  const upstream = await fetchJson("https://example.invalid/x", {
    fetch: () => Promise.resolve(new Response("nope", { status: 500 })),
    maxAttempts: 1,
    baseDelayMs: 1,
  });
  assert.equal(upstream.ok, false);
  if (!upstream.ok) {
    assert.equal(upstream.error.kind, "upstream");
    assert.ok("suggestion" in upstream.error && upstream.error.suggestion.length > 0);
  }

  const drift = parseCentralDocs({ docsData: { modules: [] } }, "x/y:1.0.0");
  assert.equal(drift.ok, false);
  if (!drift.ok) {
    assert.equal(drift.error.kind, "schema-drift");
    // Addressed to a human on purpose: no argument the agent can change will make
    // a payload this reader cannot parse parse.
    assert.ok("suggestion" in drift.error && /Report the/.test(drift.error.suggestion));
  }
});

test("the version Central reports goes through the parser rather than being asserted", async () => {
  // It becomes a cache path segment, and `..` satisfies Central's own format.
  const resolved = await resolveLatestVersion(GITHUB, {
    fetch: () => Promise.resolve(new Response(JSON.stringify([".."]), { status: 200 })),
    maxAttempts: 1,
    baseDelayMs: 1,
  });
  assert.equal(resolved.ok, false);
  if (!resolved.ok) assert.equal(resolved.error.kind, "validation");
});

test("a payload missing a field the reader reads is schema drift, with its path", () => {
  const docs = loadRawFixture("ballerinax__sap") as { docsData: { modules: Record<string, unknown>[] } };
  const module = docs.docsData.modules[0];
  assert.ok(module);
  // A rename is the dangerous case: the reader would silently render no records.
  const renamed: Record<string, unknown> = { ...module, recordTypes: module["records"] };
  delete renamed["records"];
  const result = parseCentralDocs({ docsData: { modules: [renamed] } }, "ballerinax/sap:1.3.1");
  assert.equal(result.ok, false);
  if (result.ok) return;
  assert.equal(result.error.kind, "schema-drift");
  const paths = result.error.kind === "schema-drift" ? result.error.issues.map((issue) => issue.path) : [];
  assert.deepEqual(paths, ["docsData.modules.0.records"]);
});

test("a payload with no modules at all is drift, not an empty library", () => {
  const result = parseCentralDocs({ docsData: { modules: [] } }, "x/y:1.0.0");
  assert.equal(result.ok, false);
});

test("Dependencies.toml yields the locked version of each package", () => {
  const toml = `[ballerina]
dependencies-toml-version = "2"

[[package]]
org = "ballerina"
name = "http"
version = "2.16.6"

[[package]]
org = "ballerinax"
name = "github"
version = "6.0.0"
modules = [{org = "ballerinax", packageName = "github", moduleName = "github"}]
`;
  const versions = parseDependenciesToml(toml);
  assert.equal(versions.get("ballerina/http"), "2.16.6");
  assert.equal(versions.get("ballerinax/github"), "6.0.0");
  // The `[ballerina]` table is metadata, not a package.
  assert.equal(versions.size, 2);
});

test("a truncated Dependencies.toml entry is skipped rather than half-read", () => {
  const versions = parseDependenciesToml('[[package]]\norg = "ballerinax"\nname = "github"\n');
  assert.equal(versions.size, 0);
});
