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

// The fake is driven over raw HTTP here, not through the FilesClient, so its
// wire behavior is pinned against the contract on its own.

import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { existsSync } from "node:fs";
import http from "node:http";
import { startFakeFilesSocket } from "./fake-files-socket.js";

interface Reply {
  status: number;
  type: string;
  retryAfter: string | undefined;
  body: unknown;
}

function call(
  socketPath: string,
  method: string,
  path: string,
  body?: unknown,
): Promise<Reply> {
  return new Promise((resolve, reject) => {
    const payload = body === undefined ? undefined : JSON.stringify(body);
    const req = http.request(
      {
        socketPath,
        method,
        path,
        headers: payload ? { "Content-Type": "application/json" } : {},
      },
      (res) => {
        let raw = "";
        res.setEncoding("utf8");
        res.on("data", (c: string) => (raw += c));
        res.on("end", () =>
          resolve({
            status: res.statusCode ?? 0,
            type: res.headers["content-type"] ?? "",
            retryAfter: res.headers["retry-after"] as string | undefined,
            body: raw ? JSON.parse(raw) : undefined,
          }),
        );
      },
    );
    req.on("error", reject);
    req.end(payload);
  });
}

function gitBlobSha(content: string): string {
  const buf = Buffer.from(content, "utf8");
  return createHash("sha1")
    .update(`blob ${buf.length}\0`)
    .update(buf)
    .digest("hex");
}

test("shas are git blob shas, so a seed baseline matches what git would say", async () => {
  const fake = await startFakeFilesSocket({ files: { "specs/a.md": "hello\n" } });
  try {
    const r = await call(fake.path, "GET", "/projects/greeter/bundle?prefix=specs/");
    assert.equal(r.status, 200);
    const bundle = r.body as { commitSha: string; files: { sha: string }[] };
    assert.equal(bundle.files[0]!.sha, gitBlobSha("hello\n"));
    assert.equal(bundle.files[0]!.sha, "ce013625030ba8dba906f756967f9e9ca394464a");
  } finally {
    await fake.close();
  }
});

test("lookup answers known with a stable repository and the current head", async () => {
  const fake = await startFakeFilesSocket({ files: {} });
  try {
    const r = await call(fake.path, "GET", "/projects/greeter");
    assert.equal(r.status, 200);
    const body = r.body as Record<string, unknown>;
    assert.deepEqual(Object.keys(body).sort(), ["headSha", "known", "owner", "repo"]);
    assert.equal(body.known, true);
  } finally {
    await fake.close();
  }
});

test("bundle without a prefix reads the whole tree at one commit", async () => {
  const fake = await startFakeFilesSocket({
    files: { "specs/a.md": "a", "README.md": "r" },
  });
  try {
    const r = await call(fake.path, "GET", "/projects/greeter/bundle");
    const bundle = r.body as { commitSha: string; files: { path: string }[] };
    assert.deepEqual(bundle.files.map((f) => f.path).sort(), ["README.md", "specs/a.md"]);
    const head = (await call(fake.path, "GET", "/projects/greeter")).body as { headSha: string };
    assert.equal(bundle.commitSha, head.headSha);
  } finally {
    await fake.close();
  }
});

test("apply rejects unknown fields like the pod's validator (no owner/repo ever)", async () => {
  const fake = await startFakeFilesSocket({ files: {} });
  try {
    const bodies = [
      { writes: [], deletes: [], message: "m", owner: "o" },
      { writes: [{ path: "specs/a.md", content: "a", baseSha: "", repo: "r" }], deletes: [], message: "m" },
      { writes: [], message: "m" },
    ];
    for (const body of bodies) {
      const r = await call(fake.path, "POST", "/projects/greeter/apply", body);
      assert.equal(r.status, 400);
      assert.equal(r.type, "application/problem+json");
      assert.equal((r.body as { code: string }).code, "path_invalid");
    }
  } finally {
    await fake.close();
  }
});

test("apply checks every baseSha and applies nothing on a conflict", async () => {
  const fake = await startFakeFilesSocket({ files: { "specs/a.md": "a", "specs/b.md": "b" } });
  try {
    const r = await call(fake.path, "POST", "/projects/greeter/apply", {
      writes: [
        { path: "specs/a.md", content: "a2", baseSha: gitBlobSha("a") },
        { path: "specs/b.md", content: "b2", baseSha: "stale" },
        { path: "specs/c.md", content: "c", baseSha: gitBlobSha("x") },
      ],
      deletes: [{ path: "specs/gone.md", baseSha: "s" }],
      message: "m",
    });
    assert.equal(r.status, 409);
    assert.equal(r.type, "application/json");
    assert.deepEqual(r.body, {
      code: "conflict",
      conflicts: [
        { path: "specs/b.md", baseSha: "stale", currentSha: gitBlobSha("b") },
        { path: "specs/c.md", baseSha: gitBlobSha("x"), currentSha: "" },
        { path: "specs/gone.md", baseSha: "s", currentSha: "" },
      ],
    });
    const bundle = (await call(fake.path, "GET", "/projects/greeter/bundle")).body as {
      files: { path: string; content: string }[];
    };
    assert.equal(bundle.files.find((f) => f.path === "specs/a.md")!.content, "a");
  } finally {
    await fake.close();
  }
});

test("apply deletes, writes and moves the head; a no-op is changed:false with no warnings", async () => {
  const fake = await startFakeFilesSocket({
    files: { "specs/a.md": "a", "specs/b.md": "b" },
    warnings: [{ path: "specs/a.md", message: "w" }],
  });
  try {
    const before = (await call(fake.path, "GET", "/projects/greeter")).body as { headSha: string };
    const r = await call(fake.path, "POST", "/projects/greeter/apply", {
      writes: [{ path: "specs/a.md", content: "a2", baseSha: gitBlobSha("a") }],
      deletes: [{ path: "specs/b.md", baseSha: gitBlobSha("b") }],
      message: "m",
    });
    assert.equal(r.status, 200);
    const out = r.body as { commitSha: string; changed: boolean; files: unknown[]; warnings: unknown[] };
    assert.equal(out.changed, true);
    assert.notEqual(out.commitSha, before.headSha);
    assert.deepEqual(out.files, [{ path: "specs/a.md", sha: gitBlobSha("a2") }]);
    assert.deepEqual(out.warnings, [{ path: "specs/a.md", message: "w" }]);

    const noop = await call(fake.path, "POST", "/projects/greeter/apply", {
      writes: [{ path: "specs/a.md", content: "a2", baseSha: gitBlobSha("a2") }],
      deletes: [],
      message: "m",
    });
    const same = noop.body as { commitSha: string; changed: boolean; warnings: unknown[] };
    assert.equal(same.changed, false);
    assert.equal(same.commitSha, out.commitSha);
    assert.deepEqual(same.warnings, []);
  } finally {
    await fake.close();
  }
});

test("failNext answers one request with a problem, then serves normally", async () => {
  const fake = await startFakeFilesSocket({ files: {} });
  try {
    fake.failNext(503, "aep_api_unavailable");
    const r = await call(fake.path, "GET", "/projects/greeter");
    assert.equal(r.status, 503);
    assert.equal(r.type, "application/problem+json");
    assert.equal(r.retryAfter, "5");
    assert.equal((r.body as { code: string }).code, "aep_api_unavailable");
    assert.equal((await call(fake.path, "GET", "/projects/greeter")).status, 200);
  } finally {
    await fake.close();
  }
});

test("an invalid project name or unknown route is a problem", async () => {
  const fake = await startFakeFilesSocket({ files: {} });
  try {
    const bad = await call(fake.path, "GET", "/projects/Not_A_Slug");
    assert.equal(bad.status, 400);
    assert.equal((bad.body as { code: string }).code, "path_invalid");
    const missing = await call(fake.path, "GET", "/nope");
    assert.equal(missing.status, 404);
    assert.equal((missing.body as { code: string }).code, "not_found");
  } finally {
    await fake.close();
  }
});

test("a body over 25 MiB is payload_too_large", async () => {
  const fake = await startFakeFilesSocket({ files: {} });
  try {
    const big = "x".repeat(25 * 1024 * 1024 + 1);
    const r = await call(fake.path, "POST", "/projects/greeter/apply", {
      writes: [{ path: "specs/big.md", content: big, baseSha: "" }],
      deletes: [],
      message: "m",
    });
    assert.equal(r.status, 413);
    assert.equal((r.body as { code: string }).code, "payload_too_large");
  } finally {
    await fake.close();
  }
});

test("close removes the socket file", async () => {
  const fake = await startFakeFilesSocket({ files: {} });
  assert.equal(existsSync(fake.path), true);
  await fake.close();
  assert.equal(existsSync(fake.path), false);
});
