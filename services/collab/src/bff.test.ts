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

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  ApplyAuthError,
  ApplyConflictError,
  createBffClient,
} from "./bff.js";

test("applyFiles throws ApplyAuthError on 401", async () => {
  const bff = createBffClient("http://bff", async () =>
    new Response("nope", { status: 401 }),
  );
  await assert.rejects(
    () => bff.applyFiles("t", "proj", { writes: [], deletes: [], message: "m" }),
    (e: unknown) => e instanceof ApplyAuthError && e.status === 401,
  );
});

test("applyFiles throws ApplyAuthError on 403", async () => {
  const bff = createBffClient("http://bff", async () =>
    new Response("forbidden", { status: 403 }),
  );
  await assert.rejects(
    () => bff.applyFiles("t", "proj", { writes: [], deletes: [], message: "m" }),
    (e: unknown) =>
      e instanceof ApplyAuthError &&
      e.status === 403 &&
      e.message.includes("forbidden"),
  );
});

test("applyFiles still throws ApplyConflictError on 409", async () => {
  const bff = createBffClient("http://bff", async () =>
    new Response(JSON.stringify({ conflicts: [{ path: "a.md" }] }), {
      status: 409,
      headers: { "Content-Type": "application/json" },
    }),
  );
  await assert.rejects(
    () => bff.applyFiles("t", "proj", { writes: [], deletes: [], message: "m" }),
    (e: unknown) =>
      e instanceof ApplyConflictError && e.paths.includes("a.md"),
  );
});

// ---- the seed read ----

/** Records every request the client makes, answering each with `body`. */
function recordingFetch(body: unknown, status = 200) {
  const urls: string[] = [];
  const impl: typeof fetch = async (input) => {
    urls.push(String(input));
    return new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    });
  };
  return { urls, impl };
}

test("fetchSpecFiles reads the whole seed in ONE request", async () => {
  // The count is the point, not an implementation detail. Each request the seed
  // makes costs the BFF an origin round trip under the mirror's exclusive lock,
  // so a per-file fan-out is what pushed a 10-file room past the agents' sync
  // timeout. It also resolved the branch tip separately per file, which could
  // straddle two commits.
  const { urls, impl } = recordingFetch({
    commitSha: "abc123",
    files: [
      { path: "specs/requirements/prd.md", content: "req", sha: "s1" },
      { path: "specs/design/design.cell", content: "des", sha: "s2" },
    ],
  });
  const files = await createBffClient("http://bff", impl).fetchSpecFiles(
    "t",
    "proj",
  );

  assert.equal(urls.length, 1, `expected one request, got ${urls.join(", ")}`);
  assert.equal(urls[0], "http://bff/projects/proj/files/bundle?prefix=specs%2F");
  // Paths stay VERBATIM full repo paths — the one doc-key scheme shared with
  // the console, the committer and the agents' live-peer writes.
  assert.deepEqual(files, [
    { path: "specs/requirements/prd.md", content: "req", sha: "s1" },
    { path: "specs/design/design.cell", content: "des", sha: "s2" },
  ]);
});

test("fetchSpecFiles treats a null files array as an empty seed", async () => {
  // The contract marks `files` nullable, and a room with no committed specs is
  // ordinary (a brand-new project). Seeding nothing must open an empty room, not
  // throw on `.map` of null and fail the join.
  const { impl } = recordingFetch({ commitSha: "abc123", files: null });
  assert.deepEqual(
    await createBffClient("http://bff", impl).fetchSpecFiles("t", "proj"),
    [],
  );
});

test("fetchSpecFiles surfaces a failed read rather than seeding a partial room", async () => {
  const { impl } = recordingFetch({ title: "boom" }, 500);
  await assert.rejects(
    () => createBffClient("http://bff", impl).fetchSpecFiles("t", "proj"),
    /Failed to read spec files for proj \(500\)/,
  );
});
