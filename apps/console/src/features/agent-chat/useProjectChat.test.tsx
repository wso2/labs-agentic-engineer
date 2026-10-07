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
import { afterEach, describe, expect, it, vi } from "vitest";

// The chat lives on the org's design agent, so it is read only while AE
// Studio is ready, and read again when AE Studio comes back.

const watch = vi.fn<(projectName: string) => () => void>();
const LOADING = { status: "loading", error: null, items: [], turn: { phase: "idle" } };
vi.mock("./chatStore", () => ({
  createChatStore: () => ({
    get: () => LOADING,
    subscribe: () => () => undefined,
    watch: (projectName: string) => watch(projectName),
  }),
}));

const { setAeStudioUrls } = await import("../../api/aeStudio");
const { useProjectChat } = await import("./useProjectChat");

afterEach(() => {
  cleanup();
  setAeStudioUrls(null);
  watch.mockReset();
});

describe("useProjectChat", () => {
  it("watches the conversation only while AE Studio is ready, and again once it is back", () => {
    const unwatch = vi.fn();
    watch.mockReturnValue(unwatch);
    renderHook(() => useProjectChat("acme"));
    expect(watch).not.toHaveBeenCalled();

    act(() => setAeStudioUrls({ designAgent: "http://ae-design-agent.mock" }));
    expect(watch).toHaveBeenCalledTimes(1);
    expect(watch).toHaveBeenCalledWith("acme");

    act(() => setAeStudioUrls(null));
    expect(unwatch).toHaveBeenCalledTimes(1);

    act(() => setAeStudioUrls({ designAgent: "http://ae-design-agent.mock" }));
    expect(watch).toHaveBeenCalledTimes(2);
  });

  it("keeps watching when only the answer repeats", () => {
    watch.mockReturnValue(() => undefined);
    setAeStudioUrls({ designAgent: "http://ae-design-agent.mock" });
    renderHook(() => useProjectChat("acme"));
    act(() => setAeStudioUrls({ designAgent: "http://ae-design-agent.mock" }));
    expect(watch).toHaveBeenCalledTimes(1);
  });
});
