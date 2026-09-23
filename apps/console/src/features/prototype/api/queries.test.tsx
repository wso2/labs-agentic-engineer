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

import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { stablePrototypeJson } from "@aep/prototype-model";
import { expenseApproval } from "../testing/fixtures";
import { prototypeKeys } from "./keys";

const mockGET = vi.fn();
vi.mock("../../../api/client", () => ({
  client: { GET: (...args: unknown[]) => mockGET(...args) },
}));

const { usePrototype } = await import("./queries");

const PATH = "specs/design/components/approvals-portal/prototype.json";

function renderUsePrototype(component = "approvals-portal") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return { queryClient, ...renderHook(() => usePrototype("proj1", component), { wrapper }) };
}

const file = (content: string) => ({ data: { path: PATH, content, sha: "abc123" }, error: undefined });

beforeEach(() => mockGET.mockReset());

describe("usePrototype", () => {
  it("reads the component's prototype path and yields the parsed model with its sha", async () => {
    mockGET.mockResolvedValue(file(stablePrototypeJson(expenseApproval)));
    const { result, queryClient } = renderUsePrototype();

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual({ ok: true, model: expenseApproval, sha: "abc123" });
    expect(mockGET.mock.calls[0]![0]).toBe(`/projects/proj1/files/${PATH}`);
    expect(queryClient.getQueryData(prototypeKeys.file("proj1", "approvals-portal"))).toEqual(result.current.data);
  });

  it("yields coded issues, without throwing, for a file that fails the model", async () => {
    mockGET.mockResolvedValue(file(JSON.stringify({ ...expenseApproval, schemaVersion: 2 })));
    const { result } = renderUsePrototype();

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toMatchObject({ ok: false, sha: "abc123", issues: [{ code: "UNSUPPORTED_VERSION" }] });
  });

  it("refuses a prototype filed under another component", async () => {
    mockGET.mockResolvedValue(file(stablePrototypeJson(expenseApproval)));
    const { result } = renderUsePrototype("storefront");

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toMatchObject({ ok: false, issues: [{ code: "PROTOTYPE_COMPONENT_MISMATCH" }] });
  });

  it("yields an INVALID_JSON issue for a body that is not JSON", async () => {
    mockGET.mockResolvedValue(file("{ nope"));
    const { result } = renderUsePrototype();

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toMatchObject({ ok: false, issues: [{ code: "INVALID_JSON", path: "" }] });
  });

  it("yields null when the component has no prototype", async () => {
    mockGET.mockResolvedValue({ data: undefined, error: { code: "not_found", message: "no spec file" } });
    const { result } = renderUsePrototype();

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toBeNull();
  });

  it("surfaces any other failed read as the query's error", async () => {
    mockGET.mockResolvedValue({ data: undefined, error: { code: "internal", message: "boom" } });
    const { result } = renderUsePrototype();

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error?.message).toBe("boom");
  });
});
