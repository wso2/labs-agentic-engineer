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

// Generate / Regenerate prototype is a GUARDED seed: the panel drops it while
// the chat is mid-exchange. The hook says so up front, and a click that lands
// anyway opens the chat on the exchange instead of vanishing.

import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  addMessage,
  chatKeyFor,
  consumePendingSeed,
  peekChatOpenRequest,
  peekPendingSeed,
  replaceMessages,
} from "../../agent-chat/chatStore";
import { usePrototypeTurn } from "./usePrototypeTurn";

const ORG = "acme";
const PROJECT = "lunch";
const KEY = chatKeyFor(ORG, PROJECT);

vi.mock("../../../auth/SessionContext", () => ({ useSession: () => ({ orgHandle: ORG }) }));
vi.mock("../../agent-chat/useConversationLog", () => ({
  useConversationLog: () => ({ historyReady: true, resync: () => {} }),
}));

function mount() {
  const queryClient = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return renderHook(() => usePrototypeTurn(PROJECT), { wrapper });
}

beforeEach(() => {
  replaceMessages(KEY, []);
  consumePendingSeed(KEY);
});

describe("usePrototypeTurn", () => {
  it("seeds /prototype, guarded, when nothing is waiting", () => {
    const { result } = mount();
    expect(result.current.blockedReason).toBe("");
    act(() => result.current.run());
    expect(peekPendingSeed(KEY)).toEqual({ message: "/prototype", guarded: true });
  });

  it("says why while the agent waits on an answer, and opens the chat instead of seeding", () => {
    addMessage(KEY, {
      role: "question",
      turnId: "t1",
      toolCallId: "tc1",
      questions: [{ question: "Who signs in?", options: [{ label: "Anyone" }, { label: "Invited only" }] }],
    });
    const { result } = mount();
    expect(result.current.blockedReason).toMatch(/waiting on your answer/i);
    const opened = peekChatOpenRequest(KEY);
    act(() => result.current.run());
    expect(peekPendingSeed(KEY)).toBeNull();
    expect(peekChatOpenRequest(KEY)).toBe(opened + 1);
  });

  it("says why while a turn is in flight", () => {
    addMessage(KEY, { role: "user", content: "/design", turnId: "t1", status: "in_flight" });
    const { result } = mount();
    expect(result.current.blockedReason).toMatch(/still working/i);
  });
});
