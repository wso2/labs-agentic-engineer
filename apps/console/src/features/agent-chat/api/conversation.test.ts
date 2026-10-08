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

// Which thread the project's conversation read resolves: the main chat's, or
// a view's own.

const get = vi.fn<(path: string, init: Record<string, unknown>) => Promise<unknown>>();
vi.mock("../../../api/client", () => ({
  client: { GET: (path: string, init: Record<string, unknown>) => get(path, init) },
}));

const { fetchCurrentConversationId } = await import("./conversation");
const { IssueClosedError } = await import("./errors");

const threads = {
  data: { conversations: [{ conversationId: "old" }, { conversationId: "now", current: true }] },
  error: undefined,
};

describe("fetchCurrentConversationId", () => {
  beforeEach(() => get.mockReset().mockResolvedValue(threads));

  it("reads the main chat's current thread without a view", async () => {
    expect(await fetchCurrentConversationId("shop")).toBe("now");
    expect(get).toHaveBeenCalledWith("/projects/{projectName}/agents/conversations", {
      params: { path: { projectName: "shop" } },
    });
  });

  it("reads the issues chat's current thread in the issues view", async () => {
    expect(await fetchCurrentConversationId("shop", "issues")).toBe("now");
    expect(get).toHaveBeenCalledWith("/projects/{projectName}/agents/conversations", {
      params: { path: { projectName: "shop" }, query: { view: "issues" } },
    });
  });

  it("never puts the main chat on the wire as a view", async () => {
    await fetchCurrentConversationId("shop", "main");
    expect(get).toHaveBeenCalledWith("/projects/{projectName}/agents/conversations", {
      params: { path: { projectName: "shop" } },
    });
  });

  it("reads an issue's own thread in the issue view, by its number", async () => {
    expect(await fetchCurrentConversationId("shop", "issue", 7)).toBe("now");
    expect(get).toHaveBeenCalledWith("/projects/{projectName}/agents/conversations", {
      params: { path: { projectName: "shop" }, query: { view: "issue", issueNumber: 7 } },
    });
  });

  it("reads a 409 issue_closed as the issue being closed: it has no thread", async () => {
    get.mockResolvedValueOnce({ data: undefined, error: { code: "issue_closed", message: "the issue is closed" }, response: { status: 409 } });
    await expect(fetchCurrentConversationId("shop", "issue", 7)).rejects.toBeInstanceOf(IssueClosedError);
  });
});
