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

// The marketplace register chat's scope: the conversation is started on the
// pod's /v1/marketplace/* routes and remembered for the browser session, a
// reload picks it back up, and a conversation the pod no longer knows starts
// fresh with a note. The pod is real HTTP (MSW); an unhandled project route
// would fail the test, which is the point.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../auth/token", () => ({
  getAccessToken: () => Promise.resolve("tok"),
  renewAccessToken: () => Promise.resolve(null),
  redirectToSignIn: () => undefined,
}));
vi.mock("../ae-studio/api/queries", () => ({
  usePodQueryOptions: () => ({ enabled: true, retryDelay: () => 0 }),
}));
// The signed-in user (the access token's sub); a test may switch it.
let currentUser = "u1";
vi.mock("./currentUser", () => ({
  useCurrentAuthor: () => ({ id: currentUser, displayName: currentUser === "u1" ? "Ada" : "Bob" }),
}));
const mockAttach = vi.fn();
vi.mock("./runTurn", () => ({
  attachAndFoldTurn: (...a: unknown[]) => mockAttach(...a),
}));

const { setAeStudioUrls } = await import("../../api/aeStudio");
const { MARKETPLACE_SCOPE, chatKeyForScope } = await import("./chatScope");
const { getMessages, replaceMessages } = await import("./chatStore");
const { useAgentChat } = await import("./useAgentChat");

const DESIGN = "http://ae-design-agent.mock";
const MARKET = `${DESIGN}/v1/marketplace`;
const ORG = "acme";
const KEY = chatKeyForScope(ORG, MARKETPLACE_SCOPE, "u1");
const STORED = `aep.chat.conv.${KEY}`;
const server = setupServer();

let started = 0;
const posted: string[] = [];
let messagesStatus = 200;
let turnAnswer: () => Response;

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
beforeEach(() => {
  currentUser = "u1";
  started = 0;
  posted.length = 0;
  messagesStatus = 200;
  turnAnswer = () => HttpResponse.json({ turnId: "t-1" }, { status: 202 });
  mockAttach.mockReset();
  mockAttach.mockResolvedValue(true);
  sessionStorage.clear();
  replaceMessages(KEY, []);
  setAeStudioUrls({ tools: "http://ae-studio-tools.mock", designAgent: DESIGN });
  server.use(
    http.post(`${MARKET}/conversations`, () => {
      started += 1;
      return HttpResponse.json({ conversationId: `c-${started}` }, { status: 201 });
    }),
    http.get(`${MARKET}/conversations/:id/messages`, () =>
      messagesStatus === 404
        ? HttpResponse.json({ code: "conversation_unknown" }, { status: 404 })
        : HttpResponse.json({
            messages: [{ role: "user", content: "earlier register" }, { role: "assistant", content: "earlier reply" }],
          }),
    ),
    http.post(`${MARKET}/conversations/:id/turns`, ({ params }) => {
      posted.push(String(params.id));
      return turnAnswer();
    }),
  );
});
afterEach(() => {
  server.resetHandlers();
  setAeStudioUrls(null);
});
afterAll(() => server.close());

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderHook(() => useAgentChat(ORG, MARKETPLACE_SCOPE), {
    wrapper: ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    ),
  });
}

describe("useAgentChat on the marketplace scope", () => {
  it("is per user on a shared browser: another user neither paints nor resumes the first user's conversation (R3-M1)", async () => {
    const first = mount();
    await waitFor(() => expect(first.result.current.historyReady).toBe(true));
    expect(getMessages(KEY).some((m) => "content" in m && m.content === "earlier register")).toBe(true);
    expect(sessionStorage.getItem(STORED)).toBe("c-1");
    first.unmount();

    currentUser = "u2";
    const bobKey = chatKeyForScope(ORG, MARKETPLACE_SCOPE, "u2");
    expect(bobKey).not.toBe(KEY);
    expect(getMessages(bobKey)).toEqual([]);
    const second = mount();
    await waitFor(() => expect(second.result.current.conversationReady).toBe(true));
    expect(started).toBe(2);
    expect(sessionStorage.getItem(`aep.chat.conv.${bobKey}`)).toBe("c-2");
    expect(sessionStorage.getItem(STORED)).toBe("c-1");
    second.unmount();
    replaceMessages(bobKey, []);
  });

  it("starts a conversation, then sends that conversation's turns", async () => {
    const { result } = mount();
    await waitFor(() => expect(result.current.conversationReady).toBe(true));
    expect(started).toBe(1);
    expect(sessionStorage.getItem(STORED)).toBe("c-1");

    await act(async () => {
      expect(await result.current.send("register stripe")).toBe(true);
    });

    expect(posted).toEqual(["c-1"]);
    await waitFor(() => expect(mockAttach).toHaveBeenCalled());
    expect(mockAttach.mock.calls[0]!.slice(0, 3)).toEqual([KEY, MARKETPLACE_SCOPE, "t-1"]);
  });

  it("rehydrates a reload from the remembered conversation without starting another", async () => {
    sessionStorage.setItem(STORED, "c-kept");
    const { result } = mount();
    await waitFor(() => expect(result.current.historyReady).toBe(true));

    expect(started).toBe(0);
    const contents = getMessages(KEY).map((m) => ("content" in m ? m.content : ""));
    expect(contents).toEqual(["earlier register", "earlier reply"]);

    await act(async () => {
      await result.current.send("and the key?");
    });
    expect(posted).toEqual(["c-kept"]);
  });

  it("starts fresh with a note when the pod no longer knows the remembered conversation", async () => {
    sessionStorage.setItem(STORED, "c-old");
    messagesStatus = 404;
    const { result } = mount();
    await waitFor(() => expect(result.current.conversationReady).toBe(true));

    expect(started).toBe(1);
    expect(sessionStorage.getItem(STORED)).toBe("c-1");
    expect(getMessages(KEY).some((m) => m.role === "error" && /starts fresh/.test(String(m.content)))).toBe(true);
  });

  it("attaches the turn that refused a send (409 turn_in_progress) and notes it", async () => {
    turnAnswer = () => HttpResponse.json({ code: "turn_in_progress", activeTurnId: "t-9" }, { status: 409 });
    server.use(
      http.get(`${MARKET}/turns/t-9`, () =>
        HttpResponse.json({
          turnId: "t-9",
          conversationId: "c-1",
          kind: "browser",
          flow: "",
          status: "running",
          instruction: "earlier ask",
          authorId: "",
          authorDisplayName: "",
          createdAt: "2026-10-03T00:00:00Z",
        }),
      ),
    );
    // The attached turn is still running, as the note says.
    mockAttach.mockReturnValue(new Promise(() => {}));
    const { result } = mount();
    await waitFor(() => expect(result.current.historyReady).toBe(true));

    await act(async () => {
      expect(await result.current.send("another")).toBe(false);
    });

    await waitFor(() => expect(result.current.notice ?? "").toMatch(/Another turn is running/));
    await waitFor(() => expect(mockAttach).toHaveBeenCalled());
    expect(mockAttach.mock.calls[0]!.slice(0, 3)).toEqual([KEY, MARKETPLACE_SCOPE, "t-9"]);
    expect(getMessages(KEY).some((m) => m.role === "user" && m.content === "another")).toBe(false);
  });
});
