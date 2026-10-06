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

/**
 * Full loop: real origin → record proxy → cassette on disk → replay server —
 * asserting the recorded chunks, scrubbing, and that the replayed response
 * matches what the origin produced.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { once } from "node:events";
import { mkdtempSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { REDACTED, loadCassette, saveCassette } from "../src/cassette.js";
import { createRecordProxy } from "../src/record-proxy.js";
import { serveCassettes } from "../src/replay.js";

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

/** An origin that streams three SSE frames with real inter-chunk delays. */
function startOrigin(): Promise<{ port: number; close: () => void }> {
  const server = http.createServer(async (req, res) => {
    if (req.method === "POST" && req.url?.endsWith("/turns")) {
      req.resume();
      res.writeHead(200, {
        "content-type": "text/event-stream",
        "cache-control": "no-cache, no-transform",
      });
      res.write('data: {"type":"text-delta","text":"hé"}\n\n');
      await sleep(25);
      res.write('data: {"type":"text-delta","text":"llo"}\n\n');
      await sleep(25);
      res.write("data: [DONE]\n\n");
      res.end();
      return;
    }
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ ok: true }));
  });
  server.listen(0);
  return once(server, "listening").then(() => {
    const address = server.address();
    const port = typeof address === "object" && address ? address.port : 0;
    return { port, close: () => server.close() };
  });
}

test("record proxy captures chunks+timing+scrubbed headers; replay serves them back", async () => {
  const origin = await startOrigin();
  const outDir = mkdtempSync(join(tmpdir(), "cassette-"));

  const proxy = createRecordProxy({
    target: `http://localhost:${origin.port}`,
    outDir,
    match: /\/turns$/,
  });
  proxy.listen(0);
  await once(proxy, "listening");
  const proxyAddress = proxy.address();
  const proxyPort = typeof proxyAddress === "object" && proxyAddress ? proxyAddress.port : 0;

  try {
    // A non-matching request is proxied but NOT recorded.
    const misc = await fetch(`http://localhost:${proxyPort}/api/v1/misc`);
    assert.deepEqual(await misc.json(), { ok: true });

    // The matching streaming request is proxied verbatim AND recorded.
    const recordedPath =
      "/api/v1/projects/p1/conversations/550e8400-e29b-41d4-a716-446655440000/turns";
    const res = await fetch(`http://localhost:${proxyPort}${recordedPath}`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        accept: "text/event-stream",
        authorization: "Bearer super-secret",
      },
      body: JSON.stringify({ useCase: "requirements-generate", instruction: "hi" }),
    });
    assert.equal(res.status, 200);
    assert.equal(
      await res.text(),
      'data: {"type":"text-delta","text":"hé"}\n\n' +
        'data: {"type":"text-delta","text":"llo"}\n\n' +
        "data: [DONE]\n\n",
    );

    const files = readdirSync(outDir).sort();
    assert.equal(files.length, 1, `expected exactly one cassette, got ${files.join(", ")}`);
    const cassette = loadCassette(join(outDir, files[0]!));

    assert.equal(cassette.request.method, "POST");
    assert.equal(cassette.request.path, recordedPath);
    assert.deepEqual(cassette.request.body, {
      useCase: "requirements-generate",
      instruction: "hi",
    });
    assert.equal(cassette.request.headers["authorization"], REDACTED);
    assert.equal(cassette.response.status, 200);
    assert.equal(cassette.response.headers["content-type"], "text/event-stream");

    assert.ok(cassette.chunks.length >= 2, "streamed frames arrive as separate chunks");
    const last = cassette.chunks[cassette.chunks.length - 1]!;
    assert.ok(last.tMs >= 40, `delays are recorded (last chunk at ${last.tMs}ms)`);
    const body = Buffer.concat(
      cassette.chunks.map((c) => Buffer.from(c.b64, "base64")),
    ).toString("utf8");
    assert.ok(body.endsWith("data: [DONE]\n\n"));

    // A gzipped cassette round-trips identically.
    const gzFile = join(outDir, "copy.json.gz");
    saveCassette(gzFile, cassette);
    assert.deepEqual(loadCassette(gzFile), cassette);

    // Replay the cassette over real HTTP and compare against the origin's body.
    const replay = await serveCassettes({ cassettes: [cassette], timeScale: 0 });
    try {
      // A LATER run: same flow, different conversation UUID — must still match.
      const replayed = await fetch(
        `${replay.url}/api/v1/projects/p1/conversations/f47ac10b-58cc-4372-a567-0e02b2c3d479/turns`,
        {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ useCase: "requirements-generate", instruction: "different run" }),
        },
      );
      assert.equal(replayed.status, 200);
      assert.equal(replayed.headers.get("content-type"), "text/event-stream");
      assert.equal(await replayed.text(), body);

      // A different useCase on the same path must NOT match the recording.
      const wrongUseCase = await fetch(
        `${replay.url}/api/v1/projects/p1/conversations/f47ac10b-58cc-4372-a567-0e02b2c3d479/turns`,
        {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ useCase: "design-generate", instruction: "x" }),
        },
      );
      assert.equal(wrongUseCase.status, 404);

      const miss = await fetch(`${replay.url}/nope`);
      assert.equal(miss.status, 404);
    } finally {
      await replay.close();
    }
  } finally {
    proxy.close();
    origin.close();
  }
});
