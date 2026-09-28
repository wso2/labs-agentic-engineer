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

import { afterEach, describe, expect, it, vi } from "vitest";
import { sendTurn } from "./chat";

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

describe("sendTurn", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("posts the turn to /chat with the bearer and reads the reply", async () => {
    const fetchMock = vi.fn(async () =>
      json({ conversationId: "c1", text: "Severity: SEV1", toolCalls: [{ toolName: "getOnCall", args: {} }] }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await sendTurn("http://gw/c-http/", "tok", { message: "hi" });

    expect(result).toEqual({
      kind: "reply",
      conversationId: "c1",
      text: "Severity: SEV1",
      toolCalls: [{ toolName: "getOnCall" }],
    });
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("http://gw/c-http/chat");
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer tok");
    expect(JSON.parse(String(init.body))).toEqual({ message: "hi" });
  });

  it("continues a conversation by naming it", async () => {
    const fetchMock = vi.fn(async () => json({ conversationId: "c1", text: "ok" }));
    vi.stubGlobal("fetch", fetchMock);
    await sendTurn("http://gw", "tok", { message: "and then?", conversationId: "c1" });
    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(JSON.parse(String(init.body))).toEqual({ message: "and then?", conversationId: "c1" });
  });

  it("names a refused token", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("unauthorized", { status: 401 })));
    expect(await sendTurn("http://gw", "tok", { message: "hi" })).toEqual({ kind: "refused" });
  });

  it("tells a missing scope apart from a refused token", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("forbidden", { status: 403 })));
    expect(await sendTurn("http://gw", "tok", { message: "hi" })).toEqual({ kind: "forbidden" });
  });

  it("carries any other upstream status with its body", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("boom", { status: 500 })));
    expect(await sendTurn("http://gw", "tok", { message: "hi" })).toEqual({
      kind: "upstream",
      status: 500,
      body: "boom",
    });
  });

  it("reports a network failure as unreachable", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("Failed to fetch");
      }),
    );
    expect(await sendTurn("http://gw", "tok", { message: "hi" })).toEqual({
      kind: "unreachable",
      message: "Failed to fetch",
    });
  });
});
