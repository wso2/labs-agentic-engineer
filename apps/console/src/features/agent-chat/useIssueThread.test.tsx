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

import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { IssueInfo } from "../issues/model/issues";

// Whether an issue's own chat can be drawn: the issue is open. The issue list
// says so, unless the server has since said the issue's thread is gone (409
// `issue_closed`): GitHub's list can lag a close, so that word stands until
// the list catches up.

let issues: IssueInfo[] | undefined;
vi.mock("../issues/api/issues", () => ({ useProjectIssues: () => ({ data: issues }) }));
let threads: { issueNumber: number; count: number }[] = [];
const forgetIssueChat = vi.fn();
vi.mock("./useProjectChat", () => ({
  useIssueThreads: () => threads,
  chatStore: { post: vi.fn() },
  forgetIssueChat: (p: string, n: number) => forgetIssueChat(p, n),
}));

const { useIssueThreadState, useOpenIssueThreads } = await import("./useIssueThread");
const { issueMarkedClosed, markIssueClosed } = await import("./closedIssues");

const issue = (Number: number, State: "open" | "closed"): IssueInfo => ({ Number, State, Title: `#${Number}`, Body: "", URL: "", Labels: [] });

beforeEach(() => {
  issues = [issue(7, "open"), issue(9, "closed")];
});
afterEach(cleanup);

describe("useIssueThreadState", () => {
  it("is the issue list's word: open, closed, or unknown until it is read or for an issue it does not have", () => {
    expect(renderHook(() => useIssueThreadState("shop", 7)).result.current).toBe("open");
    expect(renderHook(() => useIssueThreadState("shop", 9)).result.current).toBe("closed");
    expect(renderHook(() => useIssueThreadState("shop", 404)).result.current).toBe("unknown");
    expect(renderHook(() => useIssueThreadState("shop", null)).result.current).toBe("unknown");
    issues = undefined;
    expect(renderHook(() => useIssueThreadState("shop", 7)).result.current).toBe("unknown");
  });

  it("is closed once the server says the thread is gone, though the list still says open", () => {
    const hook = renderHook(() => useIssueThreadState("lagging", 7));
    expect(hook.result.current).toBe("open");
    act(() => markIssueClosed("lagging", 7));
    expect(hook.result.current).toBe("closed");
  });

  it("forgets the server's word once the list shows the issue closed: the list is the word again (a reopen shows)", () => {
    markIssueClosed("caught-up", 7);
    issues = [issue(7, "closed")];
    const hook = renderHook(() => useIssueThreadState("caught-up", 7));
    expect(hook.result.current).toBe("closed");
    expect(issueMarkedClosed("caught-up", 7)).toBe(false);
    issues = [issue(7, "open")];
    hook.rerender();
    expect(hook.result.current).toBe("open");
  });

  it("forgets the issue's chat in this tab once it is closed, so a reopen starts on a fresh thread", () => {
    forgetIssueChat.mockClear();
    const hook = renderHook(() => useIssueThreadState("forget", 7));
    expect(forgetIssueChat).not.toHaveBeenCalled();
    issues = [issue(7, "closed")];
    hook.rerender();
    expect(forgetIssueChat).toHaveBeenCalledWith("forget", 7);
  });
});

describe("useOpenIssueThreads", () => {
  it("lists the open issues' chats only: a closed issue's chat was removed with it", () => {
    threads = [
      { issueNumber: 7, count: 2 },
      { issueNumber: 9, count: 4 },
      { issueNumber: 8, count: 1 },
    ];
    issues = [issue(7, "open"), issue(9, "closed"), issue(8, "open")];
    markIssueClosed("menu", 8);
    expect(renderHook(() => useOpenIssueThreads("menu")).result.current).toEqual([{ issueNumber: 7, count: 2 }]);
  });
});
