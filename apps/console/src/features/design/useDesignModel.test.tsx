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
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { artifactMarker } from "./model/artifacts";

// The design model on a real project: worked out from the room's design
// files, which carry no design revisions. Nothing the review shows may lean
// on a revision it does not have: no artifact reads "new" forever.

const ROOT = "../../../../../evals/spec-agents/scenarios/fixtures/expense-tracker-design/";
const raw = import.meta.glob<string>(
  "../../../../../evals/spec-agents/scenarios/fixtures/expense-tracker-design/specs/**/*.{md,json,yaml,cell,dsl,feature}",
  { query: "?raw", import: "default", eager: true },
);
const files = Object.fromEntries(Object.entries(raw).map(([path, content]) => [path.slice(ROOT.length), content]));

vi.mock("../spec/collab/specDoc", () => ({ useSpecDoc: () => null }));
vi.mock("../spec/collab/useRoomFiles", () => ({ useRoomFiles: () => files }));
vi.mock("../agent-chat/useProjectChat", () => ({ useProjectChat: () => ({ turn: { phase: "idle" } }) }));
vi.mock("../../api/client", () => ({ client: { GET: async () => ({ data: { items: [] } }) } }));

const { useDesignModel } = await import("./useDesignModel");

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;
}

describe("useDesignModel on the platform", () => {
  it("tracks no revision, so no artifact is marked new or changed", async () => {
    const { result } = renderHook(() => useDesignModel("s0"), { wrapper });
    await waitFor(() => expect(result.current.data?.artifacts.length).toBeGreaterThan(0));
    const model = result.current.data!;
    expect(model.revision).toBeNull();
    expect(model.artifacts.map((a) => artifactMarker(a, model.revision, []))).toEqual(model.artifacts.map(() => null));
    expect(artifactMarker(model.artifacts[0]!, model.revision, model.artifacts[0]!.features)).toBe("out of date");
  });
});
