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
  createServer,
  type Server,
  type IncomingMessage,
  type ServerResponse,
} from "node:http";
import type { AddressInfo } from "node:net";
import {
  resolveAnthropicKey,
  resetAnthropicCache,
  invalidateAnthropicCache,
  AnthropicKeyError,
} from "./anthropic-key-resolver.js";

interface TestServer {
  server: Server;
  baseUrl: string;
  requests: number;
}

/**
 * Stand up a one-off effective-key stub and point the resolver at it via
 * ASDLC_API_URL (read at call time, so per-test reassignment is enough). The
 * handler receives the running request count so tests can assert coalescing.
 */
function startServer(
  handler: (req: IncomingMessage, res: ServerResponse, count: number) => void,
): Promise<TestServer> {
  return new Promise((resolve) => {
    const ctx: TestServer = {
      server: undefined as unknown as Server,
      baseUrl: "",
      requests: 0,
    };
    const server = createServer((req, res) => {
      ctx.requests += 1;
      handler(req, res, ctx.requests);
    });
    ctx.server = server;
    server.listen(0, "127.0.0.1", () => {
      const port = (server.address() as AddressInfo).port;
      ctx.baseUrl = `http://127.0.0.1:${port}`;
      process.env.ASDLC_API_URL = ctx.baseUrl;
      resolve(ctx);
    });
  });
}

function stopServer(ts: TestServer): Promise<void> {
  return new Promise((resolve) => {
    // Drop any still-open (e.g. deliberately hung) sockets so close resolves.
    ts.server.closeAllConnections?.();
    ts.server.close(() => resolve());
  });
}

function jsonOk(res: ServerResponse, body: unknown): void {
  res.setHeader("Content-Type", "application/json");
  res.statusCode = 200;
  res.end(JSON.stringify(body));
}

test("resolveAnthropicKey: empty orgId is a 400 resolver_error", async () => {
  resetAnthropicCache();
  await assert.rejects(
    () => resolveAnthropicKey(""),
    (err: unknown) =>
      err instanceof AnthropicKeyError &&
      err.code === "resolver_error" &&
      err.status === 400,
  );
});

test("resolveAnthropicKey: coalesces concurrent misses into one upstream call", async () => {
  resetAnthropicCache();
  const ts = await startServer((_req, res) => {
    // Delay so all six callers pile up while the first fetch is in flight.
    setTimeout(() => jsonOk(res, { source: "org", key: "sk-ant-aaa" }), 40);
  });
  try {
    const results = await Promise.all(
      Array.from({ length: 6 }, () => resolveAnthropicKey("org-sf")),
    );
    for (const r of results) {
      assert.equal(r.source, "org");
      assert.equal(r.key, "sk-ant-aaa");
    }
    assert.equal(ts.requests, 1, "expected a single coalesced upstream call");
  } finally {
    await stopServer(ts);
  }
});

test("resolveAnthropicKey: serves from cache, refetches after invalidate", async () => {
  resetAnthropicCache();
  const ts = await startServer((_req, res) =>
    jsonOk(res, { source: "platform", key: "sk-ant-bbb" }),
  );
  try {
    const a = await resolveAnthropicKey("org-cache");
    assert.equal(a.key, "sk-ant-bbb");
    assert.equal(ts.requests, 1);

    const b = await resolveAnthropicKey("org-cache");
    assert.equal(b.key, "sk-ant-bbb");
    assert.equal(ts.requests, 1, "second call should hit the in-process cache");

    invalidateAnthropicCache("org-cache");
    const c = await resolveAnthropicKey("org-cache");
    assert.equal(c.key, "sk-ant-bbb");
    assert.equal(ts.requests, 2, "invalidate should force a refetch");
  } finally {
    await stopServer(ts);
  }
});

test("resolveAnthropicKey: times out a hung upstream instead of hanging", async () => {
  resetAnthropicCache();
  const prev = process.env.ANTHROPIC_KEY_RESOLVE_TIMEOUT_MS;
  process.env.ANTHROPIC_KEY_RESOLVE_TIMEOUT_MS = "120";
  const ts = await startServer((_req, res) => {
    // Never respond. The fallback timer is unref'd so it can't keep the test
    // process alive; the resolver's own timeout should fire first.
    const t = setTimeout(() => res.end(), 5_000);
    t.unref?.();
  });
  try {
    const started = Date.now();
    await assert.rejects(
      () => resolveAnthropicKey("org-timeout"),
      (err: unknown) => {
        assert.ok(err instanceof AnthropicKeyError);
        assert.equal((err as AnthropicKeyError).code, "resolver_unreachable");
        assert.equal((err as AnthropicKeyError).status, 502);
        assert.match((err as AnthropicKeyError).message, /did not respond within/);
        return true;
      },
    );
    const elapsed = Date.now() - started;
    assert.ok(elapsed < 2_000, `expected a fast timeout, took ${elapsed}ms`);
  } finally {
    process.env.ANTHROPIC_KEY_RESOLVE_TIMEOUT_MS = prev;
    await stopServer(ts);
  }
});

test("resolveAnthropicKey: 'none' rejects and is not cached", async () => {
  resetAnthropicCache();
  const ts = await startServer((_req, res) =>
    jsonOk(res, { source: "none", key: "" }),
  );
  try {
    await assert.rejects(
      () => resolveAnthropicKey("org-none"),
      (err: unknown) =>
        err instanceof AnthropicKeyError &&
        err.code === "no_anthropic_key_configured" &&
        err.status === 503,
    );
    await assert.rejects(() => resolveAnthropicKey("org-none"), AnthropicKeyError);
    assert.equal(ts.requests, 2, "absence must not be cached");
  } finally {
    await stopServer(ts);
  }
});

test("resolveAnthropicKey: non-2xx surfaces resolver_error", async () => {
  resetAnthropicCache();
  const ts = await startServer((_req, res) => {
    res.statusCode = 500;
    res.end("boom");
  });
  try {
    await assert.rejects(
      () => resolveAnthropicKey("org-5xx"),
      (err: unknown) =>
        err instanceof AnthropicKeyError && err.code === "resolver_error",
    );
  } finally {
    await stopServer(ts);
  }
});
