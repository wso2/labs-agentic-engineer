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

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { WorkspaceAuthzStep } from "./WorkspaceAuthzStep";

// `mutate` is a module-level constant, matching React Query, where it is
// stable across renders. The component's effect depends on that identity, so
// a fresh closure per render would re-fire it forever — an artifact of the
// mock rather than anything the component does.
const mocks = vi.hoisted(() => {
  const idle = () => ({
    isSuccess: false,
    isError: false,
    isPending: false,
    error: null as Error | null,
  });
  const calls: string[] = [];
  return {
    calls,
    authz: { ...idle(), mutate: () => void calls.push("authz") },
    idle,
  };
});

vi.mock("../../settings/api/queries", () => ({
  useEnsureAuthzRole: () => mocks.authz,
}));

function reset() {
  mocks.calls.length = 0;
  Object.assign(mocks.authz, mocks.idle());
}

describe("WorkspaceAuthzStep", () => {
  beforeEach(reset);

  it("ensures the workspace authz role on mount", async () => {
    render(<WorkspaceAuthzStep onDone={() => {}} />);
    await waitFor(() => expect(mocks.calls).toEqual(["authz"]));
  });

  it("calls onDone once ensure succeeds", async () => {
    mocks.authz.isSuccess = true;
    const onDone = vi.fn();
    render(<WorkspaceAuthzStep onDone={onDone} />);
    await waitFor(() => expect(onDone).toHaveBeenCalledOnce());
  });

  // This is the hard gate ahead of every other onboarding step's OC writes —
  // unlike the skills-sync step, there is no "Continue anyway".
  it("offers no way past a failed ensure, only retry", async () => {
    mocks.authz.isError = true;
    mocks.authz.error = new Error("authz failed");
    render(<WorkspaceAuthzStep onDone={() => {}} />);

    await screen.findByText(/couldn't finish configuring your workspace/i);
    expect(screen.queryByRole("button", { name: /continue anyway/i })).toBeNull();
    expect(screen.getByRole("button", { name: /retry/i })).toBeTruthy();
  });

  it("retries ensure on click", async () => {
    mocks.authz.isError = true;
    mocks.authz.error = new Error("authz failed");
    render(<WorkspaceAuthzStep onDone={() => {}} />);
    await waitFor(() => expect(mocks.calls).toEqual(["authz"]));

    fireEvent.click(screen.getByRole("button", { name: /retry/i }));
    expect(mocks.calls).toEqual(["authz", "authz"]);
  });

  it("escalates the message after repeated failures", async () => {
    mocks.authz.isError = true;
    mocks.authz.error = new Error("authz failed");
    render(<WorkspaceAuthzStep onDone={() => {}} />);
    await waitFor(() => expect(mocks.calls).toEqual(["authz"]));

    const retry = screen.getByRole("button", { name: /retry/i });
    fireEvent.click(retry);
    fireEvent.click(retry);

    await screen.findByText(/taking longer than expected/i);
  });
});
