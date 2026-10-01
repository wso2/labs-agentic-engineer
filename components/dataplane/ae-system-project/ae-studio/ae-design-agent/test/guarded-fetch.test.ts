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
 * The model request host guard (`guarded-fetch.ts`): the address table, and a
 * fetch whose socket dials only what the guard checked. A stub resolver stands
 * in for DNS; a local server counts what actually arrives.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer, type IncomingMessage, type Server } from "node:http";
import type { LookupAddress } from "node:dns";
import type { AddressInfo } from "node:net";
import { generateText, streamText } from "ai";
import { createGuardedFetch, HostRefusedError, isPublicAddress, RedirectRefusedError } from "../src/shared/guarded-fetch.js";
import { anthropicConnection, createModel, type ModelConnection } from "../src/shared/model.js";
import { MODEL_MAX_RETRIES } from "../src/shared/provider-limit.js";

const NON_PUBLIC = [
  "127.0.0.1", // loopback
  "127.255.255.254",
  "::1",
  "169.254.169.254", // cloud metadata (link-local)
  "10.0.0.1", // 10/8
  "10.255.255.255",
  "172.16.0.1", // 172.16/12
  "172.31.255.255",
  "192.168.1.1", // 192.168/16
  "100.64.0.1", // CGNAT
  "100.127.255.255",
  "64:ff9b::a9fe:a9fe", // NAT64 of 169.254.169.254
  "64:ff9b::a00:1", // NAT64 of 10.0.0.1
  "64:ff9b:1::1", // NAT64 local-use
  "fc00::1", // ULA
  "fd12:3456:789a::1",
  "fe80::1", // IPv6 link-local
  "::ffff:10.0.0.1", // IPv4-mapped private
  "::ffff:127.0.0.1",
  "0.0.0.0",
  "::",
  "224.0.0.1", // multicast
  "ff02::1",
  "255.255.255.255",
  "not-an-ip",
  "",
];

const PUBLIC = ["8.8.8.8", "1.1.1.1", "172.32.0.1", "100.128.0.1", "192.169.0.1", "2606:4700:4700::1111", "::ffff:8.8.8.8"];

test("isPublicAddress refuses every non-public range and admits public unicast", () => {
  for (const address of NON_PUBLIC) assert.equal(isPublicAddress(address), false, address);
  for (const address of PUBLIC) assert.equal(isPublicAddress(address), true, address);
});

/** A local server that records every request it receives. */
async function recordingServer(
  handler: (req: IncomingMessage, res: import("node:http").ServerResponse) => void = (_req, res) => res.end("ok"),
): Promise<{ port: number; seen: IncomingMessage[]; server: Server; close: () => Promise<void> }> {
  const seen: IncomingMessage[] = [];
  const server = createServer((req, res) => {
    seen.push(req);
    handler(req, res);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as AddressInfo;
  return {
    port,
    seen,
    server,
    close: () =>
      new Promise<void>((resolve) => {
        server.closeAllConnections();
        server.close(() => resolve());
      }),
  };
}

/** A resolver that answers `table[host]` and records each question. */
function stubResolver(table: Record<string, string[]>): {
  asked: string[];
  resolve: (host: string) => Promise<LookupAddress[]>;
} {
  const asked: string[] = [];
  return {
    asked,
    resolve: async (host) => {
      asked.push(host);
      return (table[host] ?? []).map((address) => ({ address, family: address.includes(":") ? 6 : 4 }));
    },
  };
}

/** Whether `p` rejects with a HostRefusedError naming `host` and none of `hidden`. */
async function assertRefused(p: Promise<unknown>, host: string, hidden: string[] = []): Promise<void> {
  await assert.rejects(p, (err: unknown) => {
    assert.ok(err instanceof HostRefusedError, `expected HostRefusedError, got ${String(err)}`);
    assert.equal(err.hostname, host);
    for (const address of hidden) assert.ok(!err.message.includes(address), "the resolved address is never echoed");
    return true;
  });
}

test("a host that resolves to any non-public address is refused before a socket opens", async () => {
  const target = await recordingServer();
  const { resolve } = stubResolver({
    "private.test": ["10.0.0.1"],
    "metadata.test": ["169.254.169.254"],
    "nat64.test": ["64:ff9b::a9fe:a9fe"],
    // A resolver answering public then private: the whole answer is refused.
    "rebind.test": ["8.8.8.8", "127.0.0.1"],
  });
  const guarded = createGuardedFetch({ resolve });
  try {
    await assertRefused(guarded(`http://private.test:${target.port}/`), "private.test", ["10.0.0.1"]);
    await assertRefused(guarded(`http://metadata.test:${target.port}/`), "metadata.test", ["169.254.169.254"]);
    await assertRefused(guarded(`http://nat64.test:${target.port}/`), "nat64.test", ["64:ff9b"]);
    await assertRefused(guarded(`http://rebind.test:${target.port}/`), "rebind.test", ["127.0.0.1"]);
    assert.equal(target.seen.length, 0);
  } finally {
    await target.close();
  }
});

test("an IP-literal URL, which skips the lookup, is checked at connect", async () => {
  const target = await recordingServer();
  const guarded = createGuardedFetch({ resolve: stubResolver({}).resolve });
  try {
    await assertRefused(guarded(`http://127.0.0.1:${target.port}/`), "127.0.0.1");
    await assertRefused(guarded(`http://[::1]:${target.port}/`), "::1");
    await assertRefused(guarded("http://169.254.169.254/latest/meta-data/"), "169.254.169.254");
    assert.equal(target.seen.length, 0);
  } finally {
    await target.close();
  }
});

test("the socket dials the address the guard checked, resolved exactly once", async () => {
  const target = await recordingServer();
  const { asked, resolve } = stubResolver({ "model.test": ["127.0.0.1"] });
  const checked: string[] = [];
  // Admit loopback for this test only, so the checked address is reachable.
  const permit = (address: string): boolean => {
    checked.push(address);
    return address === "127.0.0.1";
  };
  const guarded = createGuardedFetch({ resolve, permit });
  try {
    const res = await guarded(`http://model.test:${target.port}/v1/messages`, { method: "POST", body: "{}" });
    assert.equal(res.status, 200);
    assert.equal(await res.text(), "ok");
    assert.deepEqual(asked, ["model.test"], "one resolution, no second lookup at dial time");
    assert.deepEqual(checked, ["127.0.0.1"]);
    assert.equal(target.seen.length, 1);
    assert.equal(target.seen[0]!.headers.host, `model.test:${target.port}`, "the request still names the host");
    assert.equal(target.seen[0]!.socket.remoteAddress, "127.0.0.1");
  } finally {
    await target.close();
  }
});

test("a redirect is refused, so the key never reaches a second host", async () => {
  const second = await recordingServer();
  const first = await recordingServer((_req, res) => {
    res.writeHead(307, { location: `http://elsewhere.test:${second.port}/v1/messages` });
    res.end();
  });
  const { resolve } = stubResolver({ "model.test": ["127.0.0.1"], "elsewhere.test": ["127.0.0.1"] });
  const guarded = createGuardedFetch({ resolve, permit: (address) => address === "127.0.0.1" });
  try {
    await assert.rejects(
      guarded(`http://model.test:${first.port}/v1/messages`, {
        method: "POST",
        headers: { "x-api-key": "sk-test-key-000000" },
        body: "{}",
      }),
      (err: unknown) => err instanceof RedirectRefusedError && err.hostname === `model.test:${first.port}`,
    );
    assert.equal(first.seen.length, 1);
    assert.equal(second.seen.length, 0, "the redirect target received nothing");
  } finally {
    await first.close();
    await second.close();
  }
});

/** A minimal Anthropic Messages SSE answer carrying `text`. */
function anthropicStream(text: string): string {
  const event = (name: string, data: unknown): string => `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`;
  return [
    event("message_start", {
      type: "message_start",
      message: { id: "msg_1", type: "message", role: "assistant", model: "claude-sonnet-5", content: [], stop_reason: null, usage: { input_tokens: 1, output_tokens: 1 } },
    }),
    event("content_block_start", { type: "content_block_start", index: 0, content_block: { type: "text", text: "" } }),
    event("content_block_delta", { type: "content_block_delta", index: 0, delta: { type: "text_delta", text } }),
    event("content_block_stop", { type: "content_block_stop", index: 0 }),
    event("message_delta", { type: "message_delta", delta: { stop_reason: "end_turn" }, usage: { output_tokens: 1 } }),
    event("message_stop", { type: "message_stop" }),
  ].join("");
}

// The production path end to end: the AI SDK streaming a turn through undici's
// own fetch (its Response body, the caller's AbortSignal), not a stub fetch.
test("a model streams a turn through the guarded fetch", async () => {
  const target = await recordingServer((_req, res) => {
    res.writeHead(200, { "content-type": "text/event-stream" });
    res.end(anthropicStream("hello from the guard"));
  });
  const { resolve } = stubResolver({ "model.test": ["127.0.0.1"] });
  const fetch = createGuardedFetch({ resolve, permit: (address) => address === "127.0.0.1" });
  const key = "sk-ant-test-key-000000000000";
  const conn = { ...anthropicConnection(key, "claude-sonnet-5"), baseURL: `http://model.test:${target.port}/v1` };
  try {
    const result = streamText({
      model: createModel(conn, { fetch }),
      prompt: "hi",
      abortSignal: new AbortController().signal,
    });
    let text = "";
    for await (const chunk of result.textStream) text += chunk;
    assert.equal(text, "hello from the guard");
    assert.equal(target.seen.length, 1);
    assert.equal(target.seen[0]!.url, "/v1/messages");
    assert.equal(target.seen[0]!.headers["x-api-key"], key);
  } finally {
    await target.close();
  }
});

/** An OpenAI-compatible connection on `baseURL`, as aep-api resolves one. */
function openAICompatible(baseURL: string): ModelConnection {
  return {
    format: "openai-compatible",
    baseURL,
    authScheme: "bearer",
    capabilities: { claudeCode: false, claudeSubscription: false, promptCache: false, generatedAgents: false, nativePdf: false, webSearch: "none", imageInput: "unknown" },
    apiKey: "ollama-test-key-0000000000",
    model: "gpt-oss:20b",
  };
}

// A refusal is an answer, not a blip: the SDK must not retry it (six retries
// would re-resolve the host seven times over two minutes of backoff).
test("a connection whose host resolves private is refused once, before any byte leaves, with the turn's retries on", async () => {
  const target = await recordingServer();
  const { asked, resolve } = stubResolver({ "llm.test": ["127.0.0.1"] });
  const conn = openAICompatible(`http://llm.test:${target.port}/v1`);
  const started = Date.now();
  try {
    await assert.rejects(
      generateText({ model: createModel(conn, { fetch: createGuardedFetch({ resolve }) }), prompt: "hi", maxRetries: MODEL_MAX_RETRIES }),
      (err: unknown) => err instanceof HostRefusedError && err.hostname === "llm.test",
    );
    assert.deepEqual(asked, ["llm.test"], "resolved once: not retried");
    assert.ok(Date.now() - started < 5_000, "no backoff");
    assert.equal(target.seen.length, 0, "no byte reached the host");
  } finally {
    await target.close();
  }
});

test("a redirect answer fails the call once, without a retry", async () => {
  const first = await recordingServer((_req, res) => {
    res.writeHead(302, { location: "https://elsewhere.test/v1/chat/completions" });
    res.end();
  });
  const { resolve } = stubResolver({ "llm.test": ["127.0.0.1"] });
  const fetch = createGuardedFetch({ resolve, permit: (address) => address === "127.0.0.1" });
  try {
    await assert.rejects(
      generateText({ model: createModel(openAICompatible(`http://llm.test:${first.port}/v1`), { fetch }), prompt: "hi", maxRetries: MODEL_MAX_RETRIES }),
      RedirectRefusedError,
    );
    assert.equal(first.seen.length, 1);
  } finally {
    await first.close();
  }
});
