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

// What startTurn puts on the wire and how it reads the server's refusals,
// after the old console's api/turns.transport.test.ts.

const post = vi.fn<(path: string, init: Record<string, unknown>) => Promise<unknown>>();

const get = vi.fn<(path: string, init: Record<string, unknown>) => Promise<unknown>>();
vi.mock("../../../api/client", () => ({
  client: {
    POST: (path: string, init: Record<string, unknown>) => post(path, init),
    GET: (path: string, init: Record<string, unknown>) => get(path, init),
  },
}));

const { ConversationRotatedError, IssueClosedError, TurnInProgressError, getActiveTurn, startTurn } = await import("./turns");

function refused(status: number, error: unknown) {
  post.mockResolvedValueOnce({ data: undefined, error, response: { status } });
}

describe("startTurn", () => {
  beforeEach(() => post.mockReset());

  it("posts the scoped body to the project's conversation and returns the turn id", async () => {
    post.mockResolvedValueOnce({ data: { turnId: "t-1" }, error: undefined, response: { status: 202 } });
    const body = { instruction: "Go", collab: true, target: "specs/requirements/prd.md" };
    expect(await startTurn("shop", "conv-1", body)).toBe("t-1");
    expect(post).toHaveBeenCalledWith("/projects/{projectName}/agents/{conversationId}/messages", {
      params: { path: { projectName: "shop", conversationId: "conv-1" } },
      body,
    });
  });

  it("reads a 409 turn_in_progress as the running turn, by id", async () => {
    refused(409, { code: "turn_in_progress", activeTurnId: "t-9" });
    const err = await startTurn("shop", "conv-1", { instruction: "Go" }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TurnInProgressError);
    expect((err as InstanceType<typeof TurnInProgressError>).activeTurnId).toBe("t-9");
  });

  it("reads a 409 conversation_rotated as a replaced thread", async () => {
    refused(409, { code: "conversation_rotated" });
    await expect(startTurn("shop", "conv-1", { instruction: "Go" })).rejects.toBeInstanceOf(ConversationRotatedError);
  });

  it("reads a 409 issue_closed as the issue being closed", async () => {
    refused(409, { code: "issue_closed" });
    await expect(startTurn("shop", "conv-1", { instruction: "Go", view: "issue", issueNumber: 7 })).rejects.toBeInstanceOf(
      IssueClosedError,
    );
  });

  it("carries the server's message for any other refusal", async () => {
    refused(400, { code: "invalid_request", message: "instruction is required" });
    await expect(startTurn("shop", "conv-1", { instruction: " " })).rejects.toThrow("instruction is required");
  });
});

describe("getActiveTurn", () => {
  beforeEach(() => get.mockReset());

  it("asks for the main chat's running turn without a view", async () => {
    get.mockResolvedValueOnce({ data: undefined, error: undefined, response: { status: 204 } });
    expect(await getActiveTurn("shop")).toBeNull();
    expect(get).toHaveBeenCalledWith("/projects/{projectName}/turns/active", {
      params: { path: { projectName: "shop" } },
    });
  });

  it("asks for the issues chat's running turn in the issues view", async () => {
    const turn = { turnId: "t-2" };
    get.mockResolvedValueOnce({ data: turn, error: undefined, response: { status: 200 } });
    expect(await getActiveTurn("shop", "issues")).toBe(turn);
    expect(get).toHaveBeenCalledWith("/projects/{projectName}/turns/active", {
      params: { path: { projectName: "shop" }, query: { view: "issues" } },
    });
  });

  it("asks for an issue's running turn in the issue view, by its number", async () => {
    get.mockResolvedValueOnce({ data: undefined, error: undefined, response: { status: 204 } });
    await getActiveTurn("shop", "issue", 7);
    expect(get).toHaveBeenCalledWith("/projects/{projectName}/turns/active", {
      params: { path: { projectName: "shop" }, query: { view: "issue", issueNumber: 7 } },
    });
  });
});
