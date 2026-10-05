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

import { afterEach, beforeEach, mock, test } from "node:test";
import assert from "node:assert/strict";
import { createRemoteGitTools } from "./remote_git.js";

// Every call writes its event line to stderr; keep it out of the test output
// (the one test that reads the lines mocks the stream itself).
beforeEach(() => {
  mock.method(process.stderr, "write", () => true);
});
afterEach(() => {
  mock.restoreAll();
});

function fakeFetch(routes: Record<string, { status: number; body: unknown }>) {
  const calls: string[] = [];
  const impl = (async (input: string | URL) => {
    const u = String(input);
    calls.push(u);
    const hit = Object.entries(routes).find(([k]) => u.startsWith(k));
    if (!hit) return new Response("not found", { status: 404 });
    return new Response(JSON.stringify(hit[1].body), { status: hit[1].status });
  }) as typeof fetch;
  return { impl, calls };
}

test("refuses a foreign owner without calling GitHub", async () => {
  const f = fakeFetch({});
  const tools = createRemoteGitTools({ token: "t", owner: "acme", fetchImpl: f.impl });
  const res = await tools.call("get_remote_git_file_contents", { owner: "evil", repo: "x", path: "a" });
  assert.equal(res?.isError, true);
  assert.match(res!.content[0].text, /not owned by the caller's organization/);
  assert.equal(f.calls.length, 0);
});

test("owner compare is case-insensitive", async () => {
  const f = fakeFetch({
    "https://api.github.com/repos/Acme/svc/contents/openapi.yaml": {
      status: 200,
      body: { type: "file", sha: "s1", encoding: "base64", content: Buffer.from("openapi: 3.0.0").toString("base64") },
    },
  });
  const tools = createRemoteGitTools({ token: "t", owner: "acme", fetchImpl: f.impl });
  const res = await tools.call("get_remote_git_file_contents", { owner: "Acme", repo: "svc", path: "openapi.yaml" });
  assert.equal(res?.isError, undefined);
  assert.deepEqual(JSON.parse(res!.content[0].text), { content: "openapi: 3.0.0", sha: "s1", isDirectory: false });
});

test("refuses a scope qualifier", async () => {
  const f = fakeFetch({});
  const tools = createRemoteGitTools({ token: "t", owner: "acme", fetchImpl: f.impl });
  const res = await tools.call("search_remote_git_code", { owner: "acme", repo: "svc", query: "secret repo:acme/other" });
  assert.equal(res?.isError, true);
  assert.equal(f.calls.length, 0);
});

test("search scopes to the one repo", async () => {
  const f = fakeFetch({ "https://api.github.com/search/code": { status: 200, body: { items: [{ path: "a/openapi.yaml", sha: "s" }] } } });
  const tools = createRemoteGitTools({ token: "t", owner: "acme", fetchImpl: f.impl });
  const res = await tools.call("search_remote_git_code", { owner: "acme", repo: "svc", query: "openapi" });
  assert.deepEqual(JSON.parse(res!.content[0].text), { items: [{ path: "a/openapi.yaml", sha: "s" }] });
  assert.match(decodeURIComponent(f.calls[0]), /q=openapi repo:acme\/svc&per_page=30/);
});

test("binary content is withheld with a note", async () => {
  const f = fakeFetch({
    "https://api.github.com/repos/acme/svc/contents/logo.png": {
      status: 200,
      body: { type: "file", sha: "s2", encoding: "base64", content: Buffer.from([0, 159, 146, 150]).toString("base64") },
    },
  });
  const tools = createRemoteGitTools({ token: "t", owner: "acme", fetchImpl: f.impl });
  const view = JSON.parse((await tools.call("get_remote_git_file_contents", { owner: "acme", repo: "svc", path: "logo.png" }))!.content[0].text);
  assert.equal(view.content, undefined);
  assert.match(view.note, /^binary file \(4 bytes\)/);
});

test("encoding none is refused as too large to inline", async () => {
  const f = fakeFetch({ "https://api.github.com/repos/acme/svc/contents/big": { status: 200, body: { type: "file", sha: "s", encoding: "none", content: "" } } });
  const tools = createRemoteGitTools({ token: "t", owner: "acme", fetchImpl: f.impl });
  const res = await tools.call("get_remote_git_file_contents", { owner: "acme", repo: "svc", path: "big" });
  assert.equal(res?.isError, true);
  assert.match(res!.content[0].text, /too large to inline/);
});

test("a tool it does not own is not answered", () => {
  const tools = createRemoteGitTools({ token: "t", owner: "acme" });
  assert.equal(tools.call("list_org_component_endpoints", {}), undefined);
});

// ---- beyond the brief: the rest of the Go client's contract ----

test("an empty owner is refused without calling GitHub", async () => {
  const f = fakeFetch({});
  const tools = createRemoteGitTools({ token: "t", owner: "acme", fetchImpl: f.impl });
  const res = await tools.call("get_remote_git_file_contents", { repo: "x", path: "a" });
  assert.equal(res?.isError, true);
  assert.equal(f.calls.length, 0);
});

test("every scope qualifier is refused, case-insensitively", async () => {
  const f = fakeFetch({});
  const tools = createRemoteGitTools({ token: "t", owner: "acme", fetchImpl: f.impl });
  for (const q of ["x ORG:evil", "x user:evil", "x Fork:true", "x REPO:evil/y"]) {
    const res = await tools.call("search_remote_git_code", { owner: "acme", repo: "svc", query: q });
    assert.equal(res?.isError, true, q);
  }
  assert.equal(f.calls.length, 0);
});

test("sends the read-only GitHub headers with the mounted token", async () => {
  let seen: Headers | undefined;
  const impl = (async (_input: string | URL, init?: RequestInit) => {
    seen = new Headers(init?.headers);
    return new Response(JSON.stringify({ items: [] }), { status: 200 });
  }) as typeof fetch;
  const tools = createRemoteGitTools({ token: "pat-1", owner: "acme", fetchImpl: impl });
  await tools.call("search_remote_git_code", { owner: "acme", repo: "svc", query: "openapi" });
  assert.equal(seen?.get("authorization"), "Bearer pat-1");
  assert.equal(seen?.get("accept"), "application/vnd.github+json");
  assert.equal(seen?.get("x-github-api-version"), "2022-11-28");
});

test("path segments are escaped, slashes kept, ref passed as a query", async () => {
  const f = fakeFetch({ "https://api.github.com/repos/acme/svc/contents/": { status: 200, body: [] } });
  const tools = createRemoteGitTools({ token: "t", owner: "acme", fetchImpl: f.impl });
  await tools.call("get_remote_git_file_contents", { owner: "acme", repo: "svc", path: "/specs/a b?.yaml", ref: "feat/x" });
  assert.equal(f.calls[0], "https://api.github.com/repos/acme/svc/contents/specs/a%20b%3F.yaml?ref=feat%2Fx");
});

test("a directory lists its entries", async () => {
  const f = fakeFetch({
    "https://api.github.com/repos/acme/svc/contents/specs": {
      status: 200,
      body: [{ path: "specs/a.yaml", type: "file", sha: "s1", size: 3 }],
    },
  });
  const tools = createRemoteGitTools({ token: "t", owner: "acme", fetchImpl: f.impl });
  const res = await tools.call("get_remote_git_file_contents", { owner: "acme", repo: "svc", path: "specs" });
  assert.deepEqual(JSON.parse(res!.content[0].text), {
    isDirectory: true,
    entries: [{ path: "specs/a.yaml", type: "file", sha: "s1" }],
  });
});

test("oversized text is truncated on a character boundary", async () => {
  const text = "a".repeat((128 << 10) - 1) + "é" + "tail";
  const f = fakeFetch({
    "https://api.github.com/repos/acme/svc/contents/big.yaml": {
      status: 200,
      body: { type: "file", sha: "s", encoding: "base64", content: Buffer.from(text).toString("base64") },
    },
  });
  const tools = createRemoteGitTools({ token: "t", owner: "acme", fetchImpl: f.impl });
  const view = JSON.parse((await tools.call("get_remote_git_file_contents", { owner: "acme", repo: "svc", path: "big.yaml" }))!.content[0].text);
  assert.equal(view.content, "a".repeat((128 << 10) - 1));
  assert.equal(view.note, `truncated to the first ${(128 << 10) - 1} of ${Buffer.byteLength(text)} bytes`);
});

test("a GitHub failure is a tool error carrying the status", async () => {
  const f = fakeFetch({ "https://api.github.com/search/code": { status: 403, body: { message: "rate limited" } } });
  const tools = createRemoteGitTools({ token: "t", owner: "acme", fetchImpl: f.impl });
  const res = await tools.call("search_remote_git_code", { owner: "acme", repo: "svc", query: "openapi" });
  assert.equal(res?.isError, true);
  assert.match(res!.content[0].text, /status 403/);
});

test("search surfaces at most 30 hits", async () => {
  const items = Array.from({ length: 40 }, (_, i) => ({ path: `p${i}`, sha: `s${i}`, extra: "x" }));
  const f = fakeFetch({ "https://api.github.com/search/code": { status: 200, body: { items } } });
  const tools = createRemoteGitTools({ token: "t", owner: "acme", fetchImpl: f.impl });
  const out = JSON.parse((await tools.call("search_remote_git_code", { owner: "acme", repo: "svc", query: "x" }))!.content[0].text);
  assert.equal(out.items.length, 30);
  assert.deepEqual(out.items[0], { path: "p0", sha: "s0" });
});

test("each call logs one value-free event line", async (t) => {
  const lines: string[] = [];
  t.mock.method(process.stderr, "write", (chunk: string | Uint8Array) => {
    lines.push(String(chunk));
    return true;
  });
  const f = fakeFetch({ "https://api.github.com/search/code": { status: 200, body: { items: [] } } });
  const tools = createRemoteGitTools({ token: "secret-pat", owner: "acme", fetchImpl: f.impl });
  await tools.call("search_remote_git_code", { owner: "acme", repo: "svc", query: "openapi" });
  await tools.call("search_remote_git_code", { owner: "evil", repo: "svc", query: "openapi" });
  t.mock.restoreAll();
  assert.deepEqual(lines.map((l) => JSON.parse(l)), [
    { event: "remote_git.call", tool: "search_remote_git_code", repo: "acme/svc", status: "ok" },
    { event: "remote_git.call", tool: "search_remote_git_code", repo: "evil/svc", status: "refused" },
  ]);
  assert.ok(lines.every((l) => !l.includes("secret-pat")));
});

test("descriptors name exactly the two tools", () => {
  const tools = createRemoteGitTools({ token: "t", owner: "acme" });
  assert.deepEqual(tools.descriptors.map((d) => d.name), ["get_remote_git_file_contents", "search_remote_git_code"]);
});
