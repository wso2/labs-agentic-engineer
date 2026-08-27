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

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render as rtlRender, screen } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    children,
    ...rest
  }: {
    to: string;
    children?: React.ReactNode;
  } & Record<string, unknown>) => (
    <a href={to} {...rest}>
      {children}
    </a>
  ),
  Outlet: () => <div data-testid="settings-outlet" />,
  useLocation: () => ({ pathname: "/settings/credentials" }),
}));

import { SettingsLayout } from "./SettingsLayout";

const render = () =>
  rtlRender(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <SettingsLayout />
    </OxygenUIThemeProvider>,
  );

afterEach(cleanup);

describe("SettingsLayout", () => {
  it("lists Credentials, Skills, and Usage, and has no Resources item", () => {
    render();

    expect(screen.getByRole("tab", { name: "Credentials" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Skills" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Usage" })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Resources" })).not.toBeInTheDocument();
  });
});
