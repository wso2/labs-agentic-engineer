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

import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";
import { SessionContext, type Session } from "../../../auth/SessionContext";
import { BELL_POLL_MS } from "../../alerts/api/queries";
import type { IssueInfo } from "../api/queries";
import {
  attentionItemId,
  collectAttentionItems,
  countUnreadAttention,
  useAttentionUnread,
} from "./useAttentionUnread";

const { mockGET } = vi.hoisted(() => ({ mockGET: vi.fn() }));
vi.mock("../../../api/client", () => ({
  client: { GET: (...args: unknown[]) => mockGET(...args) },
}));

type RcaAgentReport = components["schemas"]["RcaAgentReport"];

function report(issueNumber?: number): RcaAgentReport {
  return {
    id: `r-${issueNumber ?? "none"}`,
    title: "Alert",
    summary: "summary",
    diagnosis: "diagnosis",
    project: "shop",
    classification: "code-level",
    createdAt: "2026-09-18T00:00:00Z",
    dispatched: true,
    deployed: false,
    ...(issueNumber ? { issueNumber } : {}),
  };
}

describe("attention unread helpers", () => {
  it("collects only alert-linked issues with server attention reasons", () => {
    const issues: IssueInfo[] = [
      {
        Number: 1,
        Title: "ordinary",
        Body: "",
        URL: "https://github.com/acme/shop/issues/1",
        State: "open",
        Labels: ["incident"],
      },
      {
        Number: 2,
        Title: "low confidence",
        Body: "",
        URL: "https://github.com/acme/shop/issues/2",
        State: "open",
        Labels: ["incident"],
        attentionReason: "unverified_fix",
      },
      {
        Number: 3,
        Title: "repeated",
        Body: "",
        URL: "https://github.com/acme/shop/issues/3",
        State: "open",
        Labels: ["incident"],
        attentionReason: "escalated",
      },
    ];

    const items = collectAttentionItems(
      [report(1), report(2), report(3), report()],
      new Map([["shop", issues]]),
    );

    expect(items.map((item) => item.id)).toEqual([
      "shop#2:unverified_fix",
      "shop#3:escalated",
    ]);
  });

  it("does not count viewed attention items as unread", () => {
    const items = [
      {
        id: attentionItemId("shop", 2, "unverified_fix"),
        projectName: "shop",
        issueNumber: 2,
        title: "low confidence",
        reason: "unverified_fix" as const,
      },
      {
        id: attentionItemId("shop", 3, "no_change_verdict"),
        projectName: "shop",
        issueNumber: 3,
        title: "no code fix",
        reason: "no_change_verdict" as const,
      },
    ];

    expect(countUnreadAttention(items, ["shop#2:unverified_fix"])).toBe(1);
  });
});

const escalatedIssue: IssueInfo = {
  Number: 3,
  Title: "repeated",
  Body: "",
  URL: "https://github.com/acme/shop/issues/3",
  State: "open",
  Labels: ["incident"],
  attentionReason: "escalated",
};

function session(email: string, orgHandle: string | null): Session {
  return { user: { name: email, email }, orgHandle, signOut: () => {} };
}

function renderAttention(email: string, reports: RcaAgentReport[] = [report(3)], orgHandle: string | null = "acme") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  let current = session(email, orgHandle);
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <SessionContext.Provider value={current}>{children}</SessionContext.Provider>
      </QueryClientProvider>
    );
  }
  const rendered = renderHook(() => useAttentionUnread(reports), { wrapper: Wrapper });
  return {
    ...rendered,
    queryClient,
    switchUser(nextEmail: string) {
      current = session(nextEmail, current.orgHandle);
      rendered.rerender();
    },
    switchOrg(nextOrgHandle: string | null) {
      current = session(current.user.email, nextOrgHandle);
      rendered.rerender();
    },
  };
}

describe("useAttentionUnread", () => {
  beforeEach(() => {
    localStorage.clear();
    mockGET.mockReset();
  });

  it("keeps seen state per signed-in account", async () => {
    mockGET.mockResolvedValue({ data: [escalatedIssue], error: undefined });
    const { result, switchUser } = renderAttention("alice@example.com");

    await waitFor(() => expect(result.current.unreadCount).toBe(1));
    act(() => result.current.markAllSeen());
    expect(result.current.unreadCount).toBe(0);

    // Another account in the same browser has not seen the item.
    switchUser("bob@example.com");
    expect(result.current.unreadCount).toBe(1);

    // And alice's seen state survives a remount from storage.
    const alice = renderAttention("alice@example.com");
    await waitFor(() => expect(alice.result.current.items).toHaveLength(1));
    expect(alice.result.current.unreadCount).toBe(0);
  });

  it("keeps seen state per active organization", async () => {
    mockGET.mockResolvedValue({ data: [escalatedIssue], error: undefined });
    const { result, switchOrg } = renderAttention("alice@example.com");

    await waitFor(() => expect(result.current.unreadCount).toBe(1));
    act(() => result.current.markAllSeen());
    expect(result.current.unreadCount).toBe(0);

    // The same project name and issue number in another org is a different issue.
    switchOrg("globex");
    expect(result.current.unreadCount).toBe(1);

    switchOrg("acme");
    expect(result.current.unreadCount).toBe(0);
  });

  it("polls the project issue lists on the bell's cadence while mounted", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      mockGET.mockResolvedValue({ data: [], error: undefined });
      const { result } = renderAttention("alice@example.com");
      await waitFor(() => expect(mockGET).toHaveBeenCalledTimes(1));
      expect(result.current.unreadCount).toBe(0);

      // The issue gains attention without any new alert report.
      mockGET.mockResolvedValue({ data: [escalatedIssue], error: undefined });
      await act(() => vi.advanceTimersByTimeAsync(BELL_POLL_MS));

      await waitFor(() => expect(result.current.unreadCount).toBe(1));
    } finally {
      vi.useRealTimers();
    }
  });

  it("reports projects whose issue list failed and retries them", async () => {
    mockGET.mockResolvedValueOnce({ data: undefined, error: { message: "boom" } });
    const { result } = renderAttention("alice@example.com");

    await waitFor(() => expect(result.current.failedProjects).toEqual(["shop"]));
    expect(result.current.items).toEqual([]);

    mockGET.mockResolvedValue({ data: [escalatedIssue], error: undefined });
    act(() => result.current.retryFailed());

    await waitFor(() => expect(result.current.items).toHaveLength(1));
    expect(result.current.failedProjects).toEqual([]);
  });
});
