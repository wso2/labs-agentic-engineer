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

import { beforeEach, describe, expect, it, vi } from "vitest";

// Each view's chat store talks to the server in its own view: the issues
// store reads its own thread and its own running turn, and starts its turns
// as an issues turn (no spec room, no scope); the main store sends no view.

const conversationId = vi.fn<(projectName: string, view?: string) => Promise<string>>();
const startTurn = vi.fn<(projectName: string, conversationId: string, body: unknown) => Promise<string>>();
const activeTurn = vi.fn<(projectName: string, view?: string) => Promise<null>>();
vi.mock("./api/conversation", () => ({
  fetchCurrentConversationId: (p: string, v?: string) => conversationId(p, v),
  fetchConversationMessages: () => Promise.resolve([]),
}));
vi.mock("./api/turns", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api/turns")>()),
  startTurn: (p: string, c: string, b: unknown) => startTurn(p, c, b),
  getActiveTurn: (p: string, v?: string) => activeTurn(p, v),
  getTurn: () => Promise.resolve(null),
  openTurnStream: () => Promise.reject(new Error("no stream")),
}));
vi.mock("../spec/collab/specRoom", () => ({ flushSpecRoom: vi.fn(() => Promise.resolve()) }));
vi.mock("../spec/collab/specDoc", () => ({ applyAgentWrite: vi.fn() }));

const { chatStore, chatStoreFor } = await import("./useProjectChat");

describe("chatStoreFor", () => {
  beforeEach(() => {
    conversationId.mockReset().mockResolvedValue("conv-1");
    startTurn.mockReset().mockRejectedValue(new Error("stop here"));
    activeTurn.mockReset().mockResolvedValue(null);
  });

  it("is the app's main store for the main chat", () => {
    expect(chatStoreFor("main")).toBe(chatStore);
    expect(chatStoreFor("issues")).not.toBe(chatStore);
    expect(chatStoreFor("issues")).toBe(chatStoreFor("issues"));
  });

  it("reads the issues thread and its running turn in the issues view", async () => {
    await chatStoreFor("issues").open("shop");
    expect(conversationId).toHaveBeenCalledWith("shop", "issues");
    expect(activeTurn).toHaveBeenCalledWith("shop", "issues");
  });

  it("starts an issues turn with the words and the view alone, whatever scope the composer holds", async () => {
    await chatStoreFor("issues").open("shop-send");
    await chatStoreFor("issues").send("shop-send", "The login button is broken", { kind: "design" });
    expect(startTurn).toHaveBeenCalledWith("shop-send", "conv-1", { instruction: "The login button is broken", view: "issues" });
  });

  it("reads the main thread without a view and starts its turns as room turns", async () => {
    await chatStore.open("shop-main");
    await chatStore.send("shop-main", "Add approvals", { kind: "product" });
    expect(conversationId).toHaveBeenCalledWith("shop-main", undefined);
    expect(activeTurn).toHaveBeenCalledWith("shop-main", undefined);
    expect(startTurn).toHaveBeenCalledWith("shop-main", "conv-1", { instruction: "Add approvals", collab: true });
  });
});
