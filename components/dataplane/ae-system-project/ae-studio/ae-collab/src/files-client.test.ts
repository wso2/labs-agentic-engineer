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
  ApplyConflictError,
  FilesDeniedError,
  FilesUnavailableError,
  createFilesClient,
} from "./files-client.js";
import { startFakeFilesSocket } from "./fake-files-socket.js";

const EMPTY = { writes: [], deletes: [], message: "m" };

test("apply maps 409 conflict, 404 denial, 503 disk_full and a dead socket", async () => {
  const fake = await startFakeFilesSocket({ files: { "specs/a.md": "a" } });
  const c = createFilesClient(fake.path);
  const [seed] = await c.bundle("greeter");
  fake.pushExternal("specs/a.md", "b");
  await assert.rejects(
    c.apply("greeter", {
      writes: [{ path: "specs/a.md", content: "c", baseSha: seed!.sha }],
      deletes: [],
      message: "m",
    }),
    (e) => e instanceof ApplyConflictError && e.paths[0] === "specs/a.md",
  );
  fake.failNext(404, "project_unknown");
  await assert.rejects(c.apply("greeter", EMPTY), FilesDeniedError);
  fake.failNext(503, "disk_full");
  await assert.rejects(c.apply("greeter", EMPTY), FilesUnavailableError);
  await fake.close();
  await assert.rejects(c.lookup("greeter"), FilesUnavailableError);
});

test("apply returns the pod's warnings", async () => {
  const fake = await startFakeFilesSocket({
    files: {},
    warnings: [{ path: "specs/design/design.cell", message: "scaffolded" }],
  });
  try {
    const out = await createFilesClient(fake.path).apply("greeter", {
      writes: [{ path: "specs/design/design.cell", content: "x", baseSha: "" }],
      deletes: [],
      message: "m",
    });
    assert.deepEqual(out.warnings, [
      { path: "specs/design/design.cell", message: "scaffolded" },
    ]);
  } finally {
    await fake.close();
  }
});

test("lookup returns the repository and head; bundle reads specs/ only", async () => {
  const fake = await startFakeFilesSocket({
    files: { "specs/a.md": "a", "src/main.go": "package main" },
  });
  try {
    const c = createFilesClient(fake.path);
    const before = await c.lookup("greeter");
    assert.ok(before.owner && before.repo && before.headSha);
    assert.equal("known" in before, false);
    const files = await c.bundle("greeter");
    assert.deepEqual(
      files.map((f) => f.path),
      ["specs/a.md"],
    );
    assert.equal(files[0]!.content, "a");
    fake.pushExternal("specs/a.md", "b");
    assert.notEqual((await c.lookup("greeter")).headSha, before.headSha);
  } finally {
    await fake.close();
  }
});

test("a successful apply returns the commit and new shas, and the next bundle sees them", async () => {
  const fake = await startFakeFilesSocket({ files: { "specs/a.md": "a" } });
  try {
    const c = createFilesClient(fake.path);
    const [a] = await c.bundle("greeter");
    const out = await c.apply("greeter", {
      writes: [{ path: "specs/a.md", content: "a2", baseSha: a!.sha }],
      deletes: [],
      message: "edit",
    });
    assert.equal(out.files.length, 1);
    assert.equal(out.files[0]!.path, "specs/a.md");
    assert.equal(out.commitSha, (await c.lookup("greeter")).headSha);
    const [after] = await c.bundle("greeter");
    assert.equal(after!.sha, out.files[0]!.sha);
    assert.equal(after!.content, "a2");
  } finally {
    await fake.close();
  }
});

// Every status the Files socket answers, by retry class: a verdict the
// committer must give up on (denied) vs an outage it retries (unavailable).
const CLASSES: { status: number; code: string; cls: "denied" | "unavailable" }[] = [
  { status: 400, code: "path_invalid", cls: "denied" },
  { status: 404, code: "project_unknown", cls: "denied" },
  { status: 404, code: "not_found", cls: "denied" },
  { status: 413, code: "payload_too_large", cls: "denied" },
  { status: 408, code: "request_timeout", cls: "unavailable" },
  { status: 425, code: "too_early", cls: "unavailable" },
  { status: 429, code: "too_many_requests", cls: "unavailable" },
  { status: 409, code: "not_fast_forward", cls: "unavailable" },
  { status: 500, code: "internal_error", cls: "unavailable" },
  { status: 502, code: "github_error", cls: "unavailable" },
  { status: 503, code: "disk_full", cls: "unavailable" },
  { status: 503, code: "aep_api_unavailable", cls: "unavailable" },
];

for (const { status, code, cls } of CLASSES) {
  test(`${status} ${code} is ${cls} on every operation, carrying the code`, async () => {
    const fake = await startFakeFilesSocket({ files: { "specs/a.md": "a" } });
    try {
      const c = createFilesClient(fake.path);
      const want = cls === "denied" ? FilesDeniedError : FilesUnavailableError;
      const calls = [
        () => c.lookup("greeter"),
        () => c.bundle("greeter"),
        () => c.apply("greeter", EMPTY),
      ];
      for (const call of calls) {
        fake.failNext(status, code);
        await assert.rejects(
          call(),
          (e) => e instanceof want && e.code === code,
        );
      }
    } finally {
      await fake.close();
    }
  });
}

test("an error body that is not a problem still classifies by status", async () => {
  const fake = await startFakeFilesSocket({ files: {} });
  try {
    const c = createFilesClient(fake.path);
    fake.failNextRaw(502, "text/plain", "bad gateway");
    await assert.rejects(
      c.bundle("greeter"),
      (e) => e instanceof FilesUnavailableError && e.code === "http_502",
    );
    fake.failNextRaw(403, "text/html", "<html>no</html>");
    await assert.rejects(
      c.lookup("greeter"),
      (e) => e instanceof FilesDeniedError && e.code === "http_403",
    );
  } finally {
    await fake.close();
  }
});

test("a socket that never existed is unavailable", async () => {
  const c = createFilesClient("/nonexistent/ae-collab-files.sock");
  await assert.rejects(
    c.bundle("greeter"),
    (e) => e instanceof FilesUnavailableError && e.code === "socket_unreachable",
  );
});

test("the client never sends owner or repo, and encodes the project segment", async () => {
  const fake = await startFakeFilesSocket({ files: {} });
  try {
    const c = createFilesClient(fake.path);
    // The fake rejects unknown fields like the pod's validator does, so a
    // passing apply proves the body carries only writes/deletes/message.
    await c.apply("greeter", {
      writes: [{ path: "specs/new.md", content: "n", baseSha: "" }],
      deletes: [],
      message: "m",
    });
    await assert.rejects(c.lookup("a/b"), FilesDeniedError);
    assert.deepEqual(fake.requests.at(-1), {
      method: "GET",
      url: "/projects/a%2Fb",
    });
  } finally {
    await fake.close();
  }
});

test("a stalled socket fails the request as an outage at the deadline", async () => {
  const fake = await startFakeFilesSocket({ files: { "specs/a.md": "a" } });
  try {
    const c = createFilesClient(fake.path, { requestTimeoutMs: 100 });
    fake.stallNext("apply");
    const started = Date.now();
    await assert.rejects(
      c.apply("greeter", EMPTY),
      (e) => e instanceof FilesUnavailableError && e.code === "timeout" && e.status === 0,
    );
    assert.ok(Date.now() - started < 2_000);
    // The same client serves the next request.
    assert.equal((await c.bundle("greeter")).length, 1);
  } finally {
    await fake.close();
  }
});

test("not_fast_forward is an outage (retry later), not a conflict", async () => {
  const fake = await startFakeFilesSocket({ files: {} });
  try {
    fake.failNext(409, "not_fast_forward", "apply");
    await assert.rejects(
      createFilesClient(fake.path).apply("greeter", EMPTY),
      (e) => e instanceof FilesUnavailableError && e.code === "not_fast_forward",
    );
  } finally {
    await fake.close();
  }
});

test("a write-rule refusal carries the one path the pod named", async () => {
  const fake = await startFakeFilesSocket({ files: {} });
  try {
    const err = await createFilesClient(fake.path)
      .apply("greeter", { writes: [{ path: "notes/a.md", content: "x", baseSha: "" }], deletes: [], message: "m" })
      .catch((e: unknown) => e);
    assert.ok(err instanceof FilesDeniedError);
    assert.equal(err.code, "path_invalid");
    assert.equal(err.path, "notes/a.md");
    fake.failNext(400, "path_invalid", "apply");
    const pathless = await createFilesClient(fake.path).apply("greeter", EMPTY).catch((e: unknown) => e);
    assert.ok(pathless instanceof FilesDeniedError);
    assert.equal(pathless.path, undefined);
  } finally {
    await fake.close();
  }
});
