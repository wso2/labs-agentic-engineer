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
import { describe, expect, it } from "vitest";
import { clearProviderWait, providerWaitLabel, setProviderWait, useProviderWait } from "./providerWait";

describe("providerWait", () => {
  it("holds the host a turn is waiting on until the wait is cleared", () => {
    const { result } = renderHook(() => useProviderWait("k1"));
    expect(result.current).toBeUndefined();
    act(() => setProviderWait("k1", "ollama.com"));
    expect(result.current).toBe("ollama.com");
    act(() => clearProviderWait("k1"));
    expect(result.current).toBeUndefined();
  });

  it("is per log: another project's wait does not show here", () => {
    const { result } = renderHook(() => useProviderWait("k2"));
    act(() => setProviderWait("k3", "ollama.com"));
    expect(result.current).toBeUndefined();
    act(() => clearProviderWait("k3"));
  });

  it("labels the working indicator with the host", () => {
    expect(providerWaitLabel("ollama.com")).toBe("Waiting on the model provider (ollama.com)…");
  });
});
