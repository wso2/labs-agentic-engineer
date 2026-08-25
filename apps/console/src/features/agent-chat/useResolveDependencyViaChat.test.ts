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

import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { useResolveDependencyViaChat } from "./useResolveDependencyViaChat";
import { buildDependencyResolutionMessage } from "../projects/lib/dependencyResolutionMessage";
import { chatKeyFor, consumePendingSeed } from "./chatStore";
import type { components } from "../../generated/aep-api";

type Dependency = components["schemas"]["Dependency"];

const dep: Dependency = {
  kind: "external",
  name: "email-provider",
  status: "ambiguous",
  reason: "2 candidates available",
};

describe("useResolveDependencyViaChat — the Task 9 seam (#252 Task 5)", () => {
  beforeEach(() => {
    // Drain any leftover seed between tests (pendingSeeds is module-scoped).
    consumePendingSeed(chatKeyFor("acme", "proj1"));
  });

  it("seeds the project's chat with the built resolution message (resolve intent)", () => {
    const { result } = renderHook(() => useResolveDependencyViaChat("acme", "proj1"));
    result.current("checkout-api", dep, "resolve");

    const seeded = consumePendingSeed(chatKeyFor("acme", "proj1"))?.message;
    expect(seeded).toBe(
      buildDependencyResolutionMessage("checkout-api", dep, "resolve"),
    );
  });

  it("seeds the project's chat with the reconsider message (reconsider intent)", () => {
    const { result } = renderHook(() => useResolveDependencyViaChat("acme", "proj1"));
    result.current("checkout-api", dep, "reconsider");

    const seeded = consumePendingSeed(chatKeyFor("acme", "proj1"))?.message;
    expect(seeded).toBe(
      buildDependencyResolutionMessage("checkout-api", dep, "reconsider"),
    );
  });

  it("scopes the seed to the given (org, project) — no cross-project leak", () => {
    const { result } = renderHook(() => useResolveDependencyViaChat("acme", "proj1"));
    result.current("checkout-api", dep, "resolve");

    expect(consumePendingSeed(chatKeyFor("acme", "proj2"))).toBeNull();
    expect(consumePendingSeed(chatKeyFor("other-org", "proj1"))).toBeNull();
  });

  it("returns a stable callback across re-renders with the same org/project", () => {
    const { result, rerender } = renderHook(
      ({ org, project }) => useResolveDependencyViaChat(org, project),
      { initialProps: { org: "acme", project: "proj1" } },
    );
    const first = result.current;
    rerender({ org: "acme", project: "proj1" });
    expect(result.current).toBe(first);
  });
});
