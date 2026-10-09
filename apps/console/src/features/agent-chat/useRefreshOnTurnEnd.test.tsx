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

import type { ReactNode } from "react";
import { cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { designKey } from "../design/api/designModel";
import { issueDetailKey, issuesListKey } from "../issues/api/issues";
import { specKey } from "../spec/api/specModel";

// When a turn ends, the shell reads again what the agent may have changed:
// the spec and the design. A `/prototype` turn is one of them: its prototype
// lands in the design card's Prototype tab.

// The stores, in the order they are made: the main store and the Issues
// store when the module loads, an issue's store when first asked for.
type Ended = ((projectName: string, outcome: "completed" | "failed") => void) | null;
const made: { ended: Ended }[] = [];
vi.mock("./chatStore", () => ({
  createChatStore: () => {
    const store: { ended: Ended } = { ended: null };
    made.push(store);
    return {
      onTurnEnd: (fn: Ended) => {
        store.ended = fn;
        return () => {
          store.ended = null;
        };
      },
      onQuestionsAsked: () => () => {},
    };
  },
}));

const ended = (at: number) => made[at]?.ended ?? null;

const { chatStoreFor, useRefreshOnTurnEnd } = await import("./useProjectChat");
const { markIssueClosed } = await import("./closedIssues");

afterEach(cleanup);

describe("useRefreshOnTurnEnd", () => {
  it("reads the design and the spec again when a turn, a /prototype one included, ends", () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
    const { unmount } = renderHook(() => useRefreshOnTurnEnd(), { wrapper });
    ended(0)!("acme-expenses", "completed");
    expect(invalidate).toHaveBeenCalledWith({ queryKey: designKey("acme-expenses") });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: specKey("acme-expenses") });
    unmount();
    expect(ended(0)).toBeNull();
    expect(ended(1)).toBeNull();
  });

  it("reads the issues again, and not the spec, when the Issues agent's turn ends", () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
    renderHook(() => useRefreshOnTurnEnd(), { wrapper });
    ended(1)!("acme-expenses", "completed");
    expect(invalidate).toHaveBeenCalledWith({ queryKey: issuesListKey("acme-expenses") });
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: specKey("acme-expenses") });
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: designKey("acme-expenses") });
  });

  it("reads the issue list and that issue's own detail again when an issue's turn ends", () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
    renderHook(() => useRefreshOnTurnEnd(), { wrapper });
    chatStoreFor("issue", 7);
    made.at(-1)!.ended!("acme-expenses", "completed");
    expect(invalidate).toHaveBeenCalledWith({ queryKey: issuesListKey("acme-expenses") });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: issueDetailKey("acme-expenses", 7) });
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: specKey("acme-expenses") });
  });

  it("reads them again when an issue's thread turns out removed: the issue was closed", () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
    renderHook(() => useRefreshOnTurnEnd(), { wrapper });
    markIssueClosed("acme-expenses", 8);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: issuesListKey("acme-expenses") });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: issueDetailKey("acme-expenses", 8) });
  });
});
