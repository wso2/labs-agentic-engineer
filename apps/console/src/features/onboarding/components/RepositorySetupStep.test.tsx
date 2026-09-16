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

import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { RepositorySetupStep } from "./RepositorySetupStep";

// Both mutations are replaced wholesale rather than driven through MSW: what
// this file is about is the ORDER of the two bootstrap calls and the dependency
// between them, which is this component's own logic — the request shapes are
// the queries module's business.
//
// `mutate` is a module-level constant on each, matching React Query, where it
// is stable across renders. The component's effects depend on that identity, so
// a fresh closure per render would re-fire them forever — an artifact of the
// mock rather than anything the component does.
const mocks = vi.hoisted(() => {
  const idle = () => ({
    isSuccess: false,
    isError: false,
    isPending: false,
    error: null as Error | null,
  });
  const order: string[] = [];
  return {
    order,
    authz: { ...idle(), mutate: () => void order.push("authz") },
    sync: { ...idle(), mutate: () => void order.push("skills") },
    idle,
  };
});

vi.mock("../../settings/api/queries", () => ({
  useEnsureAuthzRole: () => mocks.authz,
  useSyncSkills: () => mocks.sync,
}));

function reset() {
  mocks.order.length = 0;
  Object.assign(mocks.authz, mocks.idle());
  Object.assign(mocks.sync, mocks.idle());
}

describe("RepositorySetupStep", () => {
  beforeEach(reset);

  // Skills sync creates the org's skills component in OpenChoreo, which OC
  // authorizes against the AuthzRole the workspace step creates. Run the other
  // way round, a first-time org's first action 403s and the user is told the
  // skills catalogue failed — which is the symptom, not the cause.
  it("configures the workspace before touching the skills repository", async () => {
    render(<RepositorySetupStep onComplete={() => {}} />);

    await waitFor(() => expect(mocks.order).toEqual(["authz"]));
    expect(mocks.order).not.toContain("skills");
  });

  it("starts the skills sync once the workspace is configured", async () => {
    const { rerender } = render(<RepositorySetupStep onComplete={() => {}} />);
    await waitFor(() => expect(mocks.order).toEqual(["authz"]));

    mocks.authz.isSuccess = true;
    rerender(<RepositorySetupStep onComplete={() => {}} />);

    await waitFor(() => expect(mocks.order).toEqual(["authz", "skills"]));
  });

  // It waits for authz to SUCCEED, not merely to settle: syncing skills against
  // an org whose AuthzRole failed to apply reproduces exactly the 403 this
  // ordering exists to avoid.
  it("does not start the skills sync when the workspace step failed", async () => {
    const { rerender } = render(<RepositorySetupStep onComplete={() => {}} />);
    await waitFor(() => expect(mocks.order).toEqual(["authz"]));

    mocks.authz.isError = true;
    mocks.authz.error = new Error("authz failed");
    rerender(<RepositorySetupStep onComplete={() => {}} />);

    await screen.findByText(/couldn't finish configuring your workspace/i);
    expect(mocks.order).toEqual(["authz"]);
  });

  // The workspace row is the hard gate — no "Continue anyway" — so its failure
  // must not offer the skip that the skills failure does.
  it("offers no way past a failed workspace step", async () => {
    const { rerender } = render(<RepositorySetupStep onComplete={() => {}} />);
    await waitFor(() => expect(mocks.order).toEqual(["authz"]));

    mocks.authz.isError = true;
    mocks.authz.error = new Error("authz failed");
    rerender(<RepositorySetupStep onComplete={() => {}} />);

    expect(screen.queryByRole("button", { name: /continue anyway/i })).toBeNull();
    expect(screen.getByRole("button", { name: /retry/i })).toBeTruthy();
  });

  // Skills, by contrast, is skippable: agents lose their skills until it is
  // retried, but the org is usable.
  it("lets a failed skills sync be skipped", async () => {
    mocks.authz.isSuccess = true;
    mocks.sync.isError = true;
    mocks.sync.error = new Error("sync failed");
    render(<RepositorySetupStep onComplete={() => {}} />);

    expect(
      screen.getByRole("button", { name: /continue anyway/i }),
    ).toBeTruthy();
  });
});
