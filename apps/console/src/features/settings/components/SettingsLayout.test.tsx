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

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render as rtlRender, screen } from "@testing-library/react";
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

// Every test but the dedicated "no permission" ones below holds every
// settings-relevant permission, so Credentials/Skills/Usage all read as
// reachable by default — only the specific denial tests narrow this set.
const ALL_SETTINGS_PERMISSIONS = [
  "ae:github-config",
  "ae:model-config",
  "ae:skill-view",
  "ae:skill-config",
  "ae:usage-view",
];
const heldPermissions = vi.hoisted(() => new Set<string>());
vi.mock("../../../auth/permissions", () => ({
  useHasPermission: (permission: string) => heldPermissions.has(permission),
  useHasAnyPermission: (permissions: string[]) =>
    permissions.some((p) => heldPermissions.has(p)),
}));

import { SettingsLayout } from "./SettingsLayout";

const render = () =>
  rtlRender(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <SettingsLayout />
    </OxygenUIThemeProvider>,
  );

beforeEach(() => {
  heldPermissions.clear();
  ALL_SETTINGS_PERMISSIONS.forEach((p) => heldPermissions.add(p));
});
afterEach(cleanup);

describe("SettingsLayout", () => {
  it("lists Credentials, Skills, and Usage, and has no Resources item", () => {
    render();

    expect(screen.getByRole("tab", { name: "Credentials" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Skills" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Usage" })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Resources" })).not.toBeInTheDocument();
  });

  it("enables the Usage tab when the caller holds ae:usage-view", () => {
    render();
    expect(screen.getByRole("tab", { name: "Usage" })).not.toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("disables the Usage tab without ae:usage-view, with an explanatory tooltip", async () => {
    heldPermissions.delete("ae:usage-view");
    render();

    const usageTab = screen.getByRole("tab", { name: "Usage" });
    expect(usageTab).toHaveAttribute("aria-disabled", "true");

    fireEvent.mouseOver(usageTab.closest("span") ?? usageTab);
    expect(
      await screen.findByText("You don't have permission to view usage."),
    ).toBeInTheDocument();
  });

  it("enables the Credentials tab when the caller holds either ae:github-config or ae:model-config", () => {
    render();
    expect(screen.getByRole("tab", { name: "Credentials" })).not.toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("keeps Credentials enabled holding only ae:model-config (not ae:github-config)", () => {
    heldPermissions.delete("ae:github-config");
    render();
    expect(screen.getByRole("tab", { name: "Credentials" })).not.toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("disables the Credentials tab holding NEITHER ae:github-config nor ae:model-config, with an explanatory tooltip", async () => {
    heldPermissions.delete("ae:github-config");
    heldPermissions.delete("ae:model-config");
    render();

    const credentialsTab = screen.getByRole("tab", { name: "Credentials" });
    expect(credentialsTab).toHaveAttribute("aria-disabled", "true");

    fireEvent.mouseOver(credentialsTab.closest("span") ?? credentialsTab);
    expect(
      await screen.findByText("You don't have permission to view credentials."),
    ).toBeInTheDocument();
  });

  it("enables the Skills tab when the caller holds either ae:skill-view or ae:skill-config", () => {
    render();
    expect(screen.getByRole("tab", { name: "Skills" })).not.toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("keeps Skills enabled holding only ae:skill-view (not ae:skill-config)", () => {
    heldPermissions.delete("ae:skill-config");
    render();
    expect(screen.getByRole("tab", { name: "Skills" })).not.toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("disables the Skills tab holding NEITHER ae:skill-view nor ae:skill-config, with an explanatory tooltip", async () => {
    heldPermissions.delete("ae:skill-view");
    heldPermissions.delete("ae:skill-config");
    render();

    const skillsTab = screen.getByRole("tab", { name: "Skills" });
    expect(skillsTab).toHaveAttribute("aria-disabled", "true");

    fireEvent.mouseOver(skillsTab.closest("span") ?? skillsTab);
    expect(
      await screen.findByText("You don't have permission to view skills."),
    ).toBeInTheDocument();
  });
});
