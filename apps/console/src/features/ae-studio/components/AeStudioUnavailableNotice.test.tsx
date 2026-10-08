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
import { cleanup, render, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { afterEach, describe, expect, it, vi } from "vitest";

// A plain anchor stands in for the router's link, so no RouterProvider is needed.
vi.mock("@tanstack/react-router", () => ({
  createLink:
    () =>
    ({ to, search, children }: { to: string; search?: Record<string, string>; children?: ReactNode }) => (
      <a href={`${to}${search ? `?${new URLSearchParams(search).toString()}` : ""}`}>{children}</a>
    ),
}));

const { AeStudioUnavailableNotice } = await import("./AeStudioUnavailableNotice");

afterEach(cleanup);

const renderIt = (ui: ReactNode) => render(<OxygenUIThemeProvider theme={OxygenTheme}>{ui}</OxygenUIThemeProvider>);

describe("AeStudioUnavailableNotice", () => {
  it("says AE Studio is restarting, and that the read is retried", () => {
    renderIt(<AeStudioUnavailableNotice kind="restarting" />);
    expect(screen.getByText("AE Studio is restarting — retrying…")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("sends the user to connect GitHub in Settings", () => {
    renderIt(<AeStudioUnavailableNotice kind="github" />);
    expect(screen.getByText("Connect GitHub to continue")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Connect GitHub" })).toHaveAttribute("href", "/settings?section=github");
  });

  it("says a misconfigured AE Studio is the administrator's to fix, with nothing to retry", () => {
    renderIt(<AeStudioUnavailableNotice kind="misconfigured" />);
    expect(screen.getByText("AE Studio is misconfigured — contact your administrator")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
