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
import { specKey } from "../spec/api/specModel";

// When a turn ends, the shell reads again what the agent may have changed:
// the spec and the design. A `/prototype` turn is one of them: its prototype
// lands in the design card's Prototype tab.

let ended: ((projectName: string, outcome: "completed" | "failed") => void) | null = null;
vi.mock("./chatStore", () => ({
  createChatStore: () => ({
    onTurnEnd: (fn: typeof ended) => {
      ended = fn;
      return () => {
        ended = null;
      };
    },
  }),
}));

const { useRefreshOnTurnEnd } = await import("./useProjectChat");

afterEach(cleanup);

describe("useRefreshOnTurnEnd", () => {
  it("reads the design and the spec again when a turn, a /prototype one included, ends", () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
    const { unmount } = renderHook(() => useRefreshOnTurnEnd(), { wrapper });
    ended!("acme-expenses", "completed");
    expect(invalidate).toHaveBeenCalledWith({ queryKey: designKey("acme-expenses") });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: specKey("acme-expenses") });
    unmount();
    expect(ended).toBeNull();
  });
});
