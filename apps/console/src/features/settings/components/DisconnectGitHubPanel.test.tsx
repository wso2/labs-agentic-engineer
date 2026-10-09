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
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { afterEach, describe, expect, it, vi } from "vitest";

// Disconnecting drops the org's GitHub connection; the platform no longer
// installs or uninstalls a GitHub App, so there is nothing else to choose.

const mutate = vi.fn();
vi.mock("../api/queries", () => ({
  useDisconnectGitProvider: () => ({ mutate, isPending: false, isError: false, error: null }),
  useConnectGitHubPat: () => ({ mutate: vi.fn(), isPending: false, isError: false, error: null }),
}));

const { GitHubSection } = await import("./GitHubSection");

afterEach(() => {
  cleanup();
  mutate.mockReset();
});

const renderIt = (ui: ReactNode) => render(<OxygenUIThemeProvider theme={OxygenTheme}>{ui}</OxygenUIThemeProvider>);

describe("DisconnectGitHubPanel", () => {
  // A connection recorded as made through the App (mode "app") is
  // disconnected the same way: no uninstall to offer.
  it.each(["app", "pat"] as const)("asks once for a %s connection, offers no App uninstall, and sends nothing else", (mode) => {
    renderIt(
      <GitHubSection
        gitProvider={{ kind: "github", mode, status: "connected", connectedAt: "2026-10-01T09:00:00Z", githubLogin: "acme" }}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveTextContent("Disconnect GitHub?");
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(dialog).not.toHaveTextContent(/GitHub App/);
    fireEvent.click(screen.getAllByRole("button", { name: "Disconnect" }).at(-1)!);
    expect(mutate).toHaveBeenCalledTimes(1);
    expect(mutate.mock.calls[0]![0]).toBeUndefined();
    act(() => mutate.mock.calls[0]![1].onSuccess());
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
