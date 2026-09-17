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
import { RepositorySetupStep } from "./RepositorySetupStep";

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
    sync: { ...idle(), mutate: () => void calls.push("skills") },
    idle,
  };
});

vi.mock("../../settings/api/queries", () => ({
  useSyncSkills: () => mocks.sync,
}));

function reset() {
  mocks.calls.length = 0;
  Object.assign(mocks.sync, mocks.idle());
}

describe("RepositorySetupStep", () => {
  beforeEach(reset);

  // Workspace authz is a separate, earlier wizard step now (WorkspaceAuthzStep)
  // — by the time this step mounts it's guaranteed done, so skills sync fires
  // immediately rather than waiting on anything.
  it("starts the skills sync on mount", async () => {
    render(<RepositorySetupStep onComplete={() => {}} />);
    await waitFor(() => expect(mocks.calls).toEqual(["skills"]));
  });

  it("lets a failed skills sync be skipped", async () => {
    mocks.sync.isError = true;
    mocks.sync.error = new Error("sync failed");
    render(<RepositorySetupStep onComplete={() => {}} />);

    expect(
      screen.getByRole("button", { name: /continue anyway/i }),
    ).toBeTruthy();
  });

  // ...and the skip has to actually let them out. The failure is sticky —
  // sync.isError stays true after "Continue anyway" — so a status derivation
  // that reads the error before the skip leaves the user on a step whose only
  // exit they already took.
  it("completes after skipping a failed skills sync", async () => {
    mocks.sync.isError = true;
    mocks.sync.error = new Error("sync failed");
    const onComplete = vi.fn();
    render(<RepositorySetupStep onComplete={onComplete} />);

    fireEvent.click(screen.getByRole("button", { name: /continue anyway/i }));

    const go = await screen.findByRole("button", { name: /go to console/i });
    fireEvent.click(go);
    expect(onComplete).toHaveBeenCalledOnce();
  });

  // The skipped state is reported honestly rather than as success: the row
  // stays an error, and the closing copy says skills were skipped.
  it("still shows the skills row as failed after skipping", async () => {
    mocks.sync.isError = true;
    mocks.sync.error = new Error("sync failed");
    render(<RepositorySetupStep onComplete={() => {}} />);

    fireEvent.click(screen.getByRole("button", { name: /continue anyway/i }));

    await screen.findByText(/skills catalogue setup was skipped/i);
  });

  it("completes once skills sync succeeds", async () => {
    mocks.sync.isSuccess = true;
    const onComplete = vi.fn();
    render(<RepositorySetupStep onComplete={onComplete} />);

    const go = await screen.findByRole("button", { name: /go to console/i });
    fireEvent.click(go);
    expect(onComplete).toHaveBeenCalledOnce();
  });
});
