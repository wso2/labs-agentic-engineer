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

import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SAMPLE_MANIFEST, SAMPLE_SOURCE } from "../../mocks/fixtures/prototype";
import { prototypeHash } from "@wso2/prototype-kit/feedback";
import { markReviewed } from "./model/reviewed";
import { appPrototypes, manifestPath, sourcePath, type AppPrototype } from "./model/prototypes";

const C = "expense-web";
let prototypes: AppPrototype[] | undefined;
vi.mock("./usePrototypes", () => ({ usePrototypes: () => prototypes }));

const { usePrototypeDot } = await import("./usePrototypeDot");

const written = (source = SAMPLE_SOURCE) => appPrototypes([C], { [manifestPath(C)]: SAMPLE_MANIFEST, [sourcePath(C)]: source }, null);

beforeEach(() => {
  localStorage.clear();
  prototypes = undefined;
});
afterEach(cleanup);

describe("the Prototype tab's dot", () => {
  it("shows for a written prototype not yet reviewed, and clears once a review shows that revision", async () => {
    prototypes = written();
    const { result } = renderHook(() => usePrototypeDot("acme"));
    await waitFor(() => expect(result.current).toBe(true));
    act(() => markReviewed("acme", C, "unrelated"));
    expect(result.current).toBe(true);
    act(() => markReviewed("acme", C, "unrelated-yet"));
    const hash = prototypeHash(SAMPLE_MANIFEST, SAMPLE_SOURCE);
    act(() => markReviewed("acme", C, hash));
    await waitFor(() => expect(result.current).toBe(false));
  });

  it("comes back when the agent writes a new revision", async () => {
    markReviewed("acme", C, prototypeHash(SAMPLE_MANIFEST, SAMPLE_SOURCE));
    prototypes = written();
    const { result, rerender } = renderHook(() => usePrototypeDot("acme"));
    await act(async () => {});
    expect(result.current).toBe(false);
    prototypes = written(`${SAMPLE_SOURCE}\n// revised`);
    rerender();
    await waitFor(() => expect(result.current).toBe(true));
  });

  it("is off with no prototype written, or an invalid one", async () => {
    prototypes = appPrototypes([C], { [manifestPath(C)]: SAMPLE_MANIFEST }, null);
    const { result } = renderHook(() => usePrototypeDot("acme"));
    await act(async () => {});
    expect(result.current).toBe(false);
  });

  it("is off while the prototype is being revised: there is nothing new to review yet", async () => {
    prototypes = appPrototypes([C], { [manifestPath(C)]: SAMPLE_MANIFEST, [sourcePath(C)]: SAMPLE_SOURCE }, { component: C });
    const { result } = renderHook(() => usePrototypeDot("acme"));
    await act(async () => {});
    expect(result.current).toBe(false);
  });

  it("works where Web Crypto's crypto.subtle is missing (a console served over plain HTTP)", async () => {
    // The kit's hash, worked out before Web Crypto goes (it matches node:crypto's: the kit's tests pin that).
    const expected = prototypeHash(SAMPLE_MANIFEST, SAMPLE_SOURCE);
    vi.stubGlobal("crypto", {});
    try {
      prototypes = written();
      const { result } = renderHook(() => usePrototypeDot("acme"));
      expect(result.current).toBe(true);
      act(() => markReviewed("acme", C, expected));
      expect(result.current).toBe(false);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("stays on when storage is unavailable", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    prototypes = written();
    const { result } = renderHook(() => usePrototypeDot("acme"));
    await waitFor(() => expect(result.current).toBe(true));
    vi.restoreAllMocks();
  });
});
