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

// @vitest-environment jsdom

import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../auth/token", () => ({
  getAccessToken: () => Promise.resolve("tok"),
  renewAccessToken: () => Promise.resolve(null),
  redirectToSignIn: () => undefined,
}));

const { setAeStudioUrls } = await import("../../../api/aeStudio");
const { resolveMarketplaceConversation, startMarketplaceConversation, storedMarketplaceConversation } =
  await import("./marketplaceConversation");

const DESIGN = "http://ae-design-agent.mock";
const MARKET = `${DESIGN}/v1/marketplace`;
const KEY = "aep.chat.v1.acme.~marketplace";
const server = setupServer();

let started = 0;
function pod(options: { messagesStatus?: number } = {}) {
  server.use(
    http.post(`${MARKET}/conversations`, () => {
      started += 1;
      return HttpResponse.json({ conversationId: `c-${started}` }, { status: 201 });
    }),
    http.get(`${MARKET}/conversations/:id/messages`, () =>
      options.messagesStatus === 404
        ? HttpResponse.json({ code: "conversation_unknown" }, { status: 404 })
        : options.messagesStatus === 503
          ? HttpResponse.json({ code: "unavailable" }, { status: 503 })
          : HttpResponse.json({ messages: [] }),
    ),
  );
}

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
beforeEach(() => {
  started = 0;
  sessionStorage.clear();
  setAeStudioUrls({ tools: "http://ae-studio-tools.mock", designAgent: DESIGN });
});
afterEach(() => {
  server.resetHandlers();
  setAeStudioUrls(null);
});
afterAll(() => server.close());

describe("the marketplace register conversation", () => {
  it("starts one on the pod and keeps its id for the session", async () => {
    pod();
    await expect(startMarketplaceConversation(KEY)).resolves.toBe("c-1");
    expect(storedMarketplaceConversation(KEY)).toBe("c-1");
  });

  it("starts once for concurrent callers", async () => {
    pod();
    const [a, b] = await Promise.all([startMarketplaceConversation(KEY), startMarketplaceConversation(KEY)]);
    expect(a).toBe(b);
    expect(started).toBe(1);
  });

  it("starts a conversation when the session has none", async () => {
    pod();
    const onRestarted = vi.fn();
    await expect(resolveMarketplaceConversation(KEY, onRestarted)).resolves.toBe("c-1");
    expect(onRestarted).not.toHaveBeenCalled();
  });

  it("picks the remembered conversation back up after a reload", async () => {
    pod();
    sessionStorage.setItem(`aep.chat.conv.${KEY}`, "c-kept");
    const onRestarted = vi.fn();
    await expect(resolveMarketplaceConversation(KEY, onRestarted)).resolves.toBe("c-kept");
    expect(started).toBe(0);
    expect(onRestarted).not.toHaveBeenCalled();
  });

  it("starts fresh, and says so, when the pod no longer knows the remembered conversation", async () => {
    pod({ messagesStatus: 404 });
    sessionStorage.setItem(`aep.chat.conv.${KEY}`, "c-old");
    const onRestarted = vi.fn();
    await expect(resolveMarketplaceConversation(KEY, onRestarted)).resolves.toBe("c-1");
    expect(onRestarted).toHaveBeenCalledTimes(1);
    expect(storedMarketplaceConversation(KEY)).toBe("c-1");
  });

  it("keeps the remembered conversation when the pod just does not answer", async () => {
    pod({ messagesStatus: 503 });
    sessionStorage.setItem(`aep.chat.conv.${KEY}`, "c-kept");
    const onRestarted = vi.fn();
    await expect(resolveMarketplaceConversation(KEY, onRestarted)).resolves.toBe("c-kept");
    expect(onRestarted).not.toHaveBeenCalled();
  });
});
