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
import http from "node:http";
import { startMcpAuthProxy } from "./mcp_auth_proxy.js";

function listen(handler: http.RequestListener): Promise<{ url: string; close: () => Promise<void> }> {
  const server = http.createServer(handler);
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      if (!addr || typeof addr === "string") {
        reject(new Error("bind failed"));
        return;
      }
      resolve({
        url: `http://127.0.0.1:${addr.port}/internal/v1/mcp`,
        close: () =>
          new Promise<void>((res, rej) => {
            server.close((err) => (err ? rej(err) : res()));
          }),
      });
    });
  });
}

test("mcp auth proxy: retries once on 401 then succeeds", async () => {
  const seen: string[] = [];
  const upstream = await listen((req, res) => {
    const auth = req.headers.authorization ?? "";
    seen.push(auth);
    if (auth === "Bearer stale") {
      res.writeHead(401).end("no");
      return;
    }
    res.writeHead(200, { "content-type": "application/json" }).end(`{"ok":true}`);
  });
  let token = "stale";
  const proxy = await startMcpAuthProxy({
    upstreamUrl: upstream.url,
    source: {
      getToken: async () => token,
      invalidate: () => {
        token = "fresh";
      },
    },
    canRefresh: true,
    onFatal: () => {
      throw new Error("onFatal must not run");
    },
  });
  try {
    const res = await fetch(proxy.url, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: `{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
    });
    assert.equal(res.status, 200);
    assert.equal(await res.text(), `{"ok":true}`);
    assert.deepEqual(seen, ["Bearer stale", "Bearer fresh"]);
  } finally {
    await proxy.close();
    await upstream.close();
  }
});

test("mcp auth proxy: second 401 calls onFatal and returns 401", async () => {
  const upstream = await listen((_req, res) => {
    res.writeHead(401).end("no");
  });
  const fatals: string[] = [];
  let n = 0;
  const proxy = await startMcpAuthProxy({
    upstreamUrl: upstream.url,
    source: {
      getToken: async () => `t-${++n}`,
      invalidate: () => {
        /* next token is t-2 */
      },
    },
    canRefresh: true,
    onFatal: (err) => {
      fatals.push(err.message);
    },
  });
  try {
    const res = await fetch(proxy.url, { method: "POST", body: "{}" });
    assert.equal(res.status, 401);
    assert.equal(fatals.length, 1);
    assert.match(fatals[0]!, /after token refresh/);
  } finally {
    await proxy.close();
    await upstream.close();
  }
});

test("mcp auth proxy: gzipped upstream is forwarded as decoded body", async () => {
  const zlib = await import("node:zlib");
  const payload = Buffer.from(`{"ok":true}`, "utf8");
  const gz = zlib.gzipSync(payload);
  const upstream = await listen((_req, res) => {
    res.writeHead(200, {
      "content-type": "application/json",
      "content-encoding": "gzip",
      "content-length": String(gz.length),
    });
    res.end(gz);
  });
  const proxy = await startMcpAuthProxy({
    upstreamUrl: upstream.url,
    source: {
      getToken: async () => "tok",
      invalidate: () => {
        /* unused */
      },
    },
    canRefresh: true,
    onFatal: () => {
      throw new Error("onFatal must not run");
    },
  });
  try {
    const res = await fetch(proxy.url, { method: "POST", body: "{}" });
    assert.equal(res.status, 200);
    assert.equal(res.headers.get("content-encoding"), null);
    assert.equal(await res.text(), `{"ok":true}`);
  } finally {
    await proxy.close();
    await upstream.close();
  }
});

test("mcp auth proxy: first mint failure calls onFatal and returns 401", async () => {
  const upstream = await listen((_req, res) => {
    res.writeHead(200).end("ok");
  });
  const fatals: string[] = [];
  const proxy = await startMcpAuthProxy({
    upstreamUrl: upstream.url,
    source: {
      getToken: async () => {
        throw new Error("thunder down");
      },
      invalidate: () => {
        throw new Error("invalidate must not run");
      },
    },
    canRefresh: true,
    onFatal: (err) => {
      fatals.push(err.message);
    },
  });
  try {
    const res = await fetch(proxy.url, { method: "POST", body: "{}" });
    assert.equal(res.status, 401);
    assert.equal(fatals.length, 1);
    assert.match(fatals[0]!, /token mint failed: thunder down/);
  } finally {
    await proxy.close();
    await upstream.close();
  }
});

test("mcp auth proxy: oversized body is 413 and never reaches upstream", async () => {
  let hits = 0;
  const upstream = await listen((_req, res) => {
    hits++;
    res.writeHead(200).end("ok");
  });
  const proxy = await startMcpAuthProxy({
    upstreamUrl: upstream.url,
    source: {
      getToken: async () => "tok",
      invalidate: () => {
        throw new Error("invalidate must not run");
      },
    },
    canRefresh: true,
    onFatal: () => {
      throw new Error("onFatal must not run");
    },
  });
  try {
    const res = await fetch(proxy.url, { method: "POST", body: "x".repeat(1024 * 1024 + 1) });
    assert.equal(res.status, 413);
    assert.equal(hits, 0);
  } finally {
    await proxy.close();
    await upstream.close();
  }
});
