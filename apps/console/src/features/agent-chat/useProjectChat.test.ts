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

const conversationId = vi.fn<(projectName: string, view?: string, issueNumber?: number) => Promise<string>>();
const startTurn = vi.fn<(projectName: string, conversationId: string, body: unknown) => Promise<string>>();
const activeTurn = vi.fn<(projectName: string, view?: string, issueNumber?: number) => Promise<null>>();
vi.mock("./api/conversation", () => ({
  fetchCurrentConversationId: (p: string, v?: string, n?: number) => conversationId(p, v, n),
  fetchConversationMessages: () => Promise.resolve([]),
}));
vi.mock("./api/turns", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api/turns")>()),
  startTurn: (p: string, c: string, b: unknown) => startTurn(p, c, b),
  getActiveTurn: (p: string, v?: string, n?: number) => activeTurn(p, v, n),
  getTurn: () => Promise.resolve(null),
  openTurnStream: () => Promise.reject(new Error("no stream")),
}));
vi.mock("../spec/collab/specRoom", () => ({ flushSpecRoom: vi.fn(() => Promise.resolve()) }));
vi.mock("../spec/collab/specDoc", () => ({ applyAgentWrite: vi.fn() }));

const { chatStore, chatStoreFor } = await import("./useProjectChat");
const { IssueClosedError } = await import("./api/turns");
const { issueMarkedClosed } = await import("./closedIssues");

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
    expect(conversationId).toHaveBeenCalledWith("shop", "issues", undefined);
    expect(activeTurn).toHaveBeenCalledWith("shop", "issues", undefined);
  });

  it("starts an issues turn with the words and the view alone, whatever scope the composer holds", async () => {
    await chatStoreFor("issues").open("shop-send");
    await chatStoreFor("issues").send("shop-send", "The login button is broken", { kind: "design" });
    expect(startTurn).toHaveBeenCalledWith("shop-send", "conv-1", { instruction: "The login button is broken", view: "issues" });
  });

  it("reads the main thread without a view and starts its turns as room turns", async () => {
    await chatStore.open("shop-main");
    await chatStore.send("shop-main", "Add approvals", { kind: "product" });
    expect(conversationId).toHaveBeenCalledWith("shop-main", undefined, undefined);
    expect(activeTurn).toHaveBeenCalledWith("shop-main", undefined, undefined);
    expect(startTurn).toHaveBeenCalledWith("shop-main", "conv-1", { instruction: "Add approvals", collab: true });
  });

  it("keeps one store per issue, made when first asked for, apart from the main and Issues stores", () => {
    const seven = chatStoreFor("issue", 7);
    expect(chatStoreFor("issue", 7)).toBe(seven);
    expect(chatStoreFor("issue", 8)).not.toBe(seven);
    expect(seven).not.toBe(chatStore);
    expect(seven).not.toBe(chatStoreFor("issues"));
  });

  it("reads an issue's own thread and running turn, by its number", async () => {
    await chatStoreFor("issue", 7).open("shop-issue");
    expect(conversationId).toHaveBeenCalledWith("shop-issue", "issue", 7);
    expect(activeTurn).toHaveBeenCalledWith("shop-issue", "issue", 7);
  });

  it("starts an issue's turn with the words, the view and the issue's number", async () => {
    await chatStoreFor("issue", 7).open("shop-issue-send");
    await chatStoreFor("issue", 7).send("shop-issue-send", "Comment that it is fixed", { kind: "product" });
    expect(startTurn).toHaveBeenCalledWith("shop-issue-send", "conv-1", {
      instruction: "Comment that it is fixed",
      view: "issue",
      issueNumber: 7,
    });
  });

  it("marks the issue closed when its thread turns out removed (409 issue_closed), and that issue only", async () => {
    conversationId.mockRejectedValueOnce(new IssueClosedError());
    await chatStoreFor("issue", 9).open("shop-closed");
    expect(issueMarkedClosed("shop-closed", 9)).toBe(true);
    expect(issueMarkedClosed("shop-closed", 7)).toBe(false);
    expect(chatStoreFor("issue", 9).get("shop-closed").status).toBe("error");
  });

  it("marks the issue closed when a turn sent to it is refused as closed", async () => {
    await chatStoreFor("issue", 10).open("shop-closed-send");
    startTurn.mockRejectedValueOnce(new IssueClosedError());
    await chatStoreFor("issue", 10).send("shop-closed-send", "Hello", { kind: "product" });
    expect(issueMarkedClosed("shop-closed-send", 10)).toBe(true);
  });
});
