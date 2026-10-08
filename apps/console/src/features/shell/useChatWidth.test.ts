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

import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { GOLDEN_SHARE } from "./chatWidth";
import { RAIL_WIDTH } from "./layout";
import { useChatWidth } from "./useChatWidth";

const KEY = "aep:shell:chat-width";

function resizeWindow(width: number) {
  act(() => {
    window.innerWidth = width;
    window.dispatchEvent(new Event("resize"));
  });
}

afterEach(() => {
  localStorage.clear();
  resizeWindow(1024);
});

describe("useChatWidth", () => {
  it("tracks the golden share as the window changes until the user drags", () => {
    resizeWindow(1440);
    const { result } = renderHook(() => useChatWidth());
    expect(result.current.width).toBe(Math.round(1440 * GOLDEN_SHARE - RAIL_WIDTH));

    resizeWindow(1920);
    expect(result.current.width).toBe(Math.round(1920 * GOLDEN_SHARE - RAIL_WIDTH));
    expect(localStorage.getItem(KEY)).toBeNull();
  });

  it("keeps a dragged width across windows and reloads", () => {
    resizeWindow(1440);
    const { result, unmount } = renderHook(() => useChatWidth());
    act(() => result.current.resizeTo(420));
    resizeWindow(1920);
    expect(result.current.width).toBe(420);
    unmount();

    expect(renderHook(() => useChatWidth()).result.current.width).toBe(420);
  });

  it("pins a drag past half the room at the cap", () => {
    resizeWindow(1440);
    const { result } = renderHook(() => useChatWidth());
    act(() => result.current.resizeTo(1200));
    expect(localStorage.getItem(KEY)).toBe(String((1440 - RAIL_WIDTH) / 2));
  });

  it("returns to the golden share on reset and forgets the width", () => {
    resizeWindow(1440);
    const { result } = renderHook(() => useChatWidth());
    act(() => result.current.resizeTo(420));
    act(() => result.current.reset());
    expect(result.current.width).toBe(Math.round(1440 * GOLDEN_SHARE - RAIL_WIDTH));
    expect(localStorage.getItem(KEY)).toBeNull();
  });
});
